package attachments

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"io"
	"math/big"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Exercise real HTTPS, DNS pinning and redirects over a pipe, never a network listener.
func fixtureFetcher(t *testing.T, handle func(*http.Request) *http.Response) *Fetcher {
	t.Helper()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{"files.example"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, e := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	parsed, e := x509.ParseCertificate(der)
	if e != nil {
		t.Fatal(e)
	}
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	return &Fetcher{Roots: roots, Lookup: func(_ context.Context, host string) ([]net.IPAddr, error) {
		ip := "8.8.8.8"
		if host == "private.example" {
			ip = "10.1.2.3"
		}
		return []net.IPAddr{{IP: net.ParseIP(ip)}}, nil
	}, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "8.8.8.8:443" {
			t.Errorf("unvalidated dial %s", address)
		}
		client, server := net.Pipe()
		go func() {
			conn := tls.Server(server, &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12})
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
			req, e := http.ReadRequest(bufio.NewReader(conn))
			if e != nil {
				return
			}
			if req.Header.Get("Authorization") != "" || req.Header.Get("Cookie") != "" {
				t.Error("ambient credential sent")
			}
			response := handle(req)
			response.ProtoMajor = 1
			response.ProtoMinor = 1
			response.Close = true
			_ = response.Write(conn)
		}()
		return client, nil
	}}
}
func response(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body))}
}
func TestFileSources(t *testing.T) {
	f := fixtureFetcher(t, func(*http.Request) *http.Response { return response("downloaded") })
	text, b64 := "hello π", base64.StdEncoding.EncodeToString([]byte{0, 1, 255})
	for _, tc := range []struct {
		name   string
		source Source
		want   string
	}{
		{"existing", Source{AttachmentID: "existing"}, ""},
		{"text", Source{Text: &text, Filename: "../notes.md"}, text},
		{"binary", Source{Base64: &b64, Filename: "test.bin"}, string([]byte{0, 1, 255})},
		{"url", Source{URL: "https://files.example/a.txt"}, "downloaded"},
		{"reference", Source{File: map[string]any{"download_url": "https://files.example/a.txt", "name": "../report.txt", "file_id": "opaque", "mime_type": "text/plain"}}, "downloaded"},
		{"string-reference", Source{File: "https://files.example/a.txt"}, "downloaded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, name, e := f.Read(context.Background(), tc.source)
			if e != nil || string(b) != tc.want {
				t.Fatalf("source failed: %v", e)
			}
			if strings.ContainsAny(name, "/\\") {
				t.Fatal("unsafe name")
			}
		})
	}
	for _, src := range []Source{{}, {Text: &text}, {Text: &text, Filename: "x", URL: "https://files.example/x"}, {File: map[string]any{}}, {File: 42}, {Base64: &text, Filename: "x"}} {
		if _, _, e := f.Read(context.Background(), src); e == nil {
			t.Fatal("invalid source accepted")
		}
	}
	if Name("../../bad\x00:name.txt") != "badname.txt" {
		t.Fatal("filename not sanitized")
	}
}
func TestPublicAddressGuard(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "::1", "10.0.0.1", "172.16.0.1", "192.168.1.1", "169.254.1.1", "100.64.0.1", "100.127.255.254", "fc00::1", "fd7a:115c:a1e0::1", "fe80::1", "fec0::1", "100::1", "64:ff9b:1::7f00:1", "2001:2::1", "2001:20::1", "224.0.0.1", "ff02::1", "0.0.0.0", "::", "::ffff:127.0.0.1", "2001:db8::1", "64:ff9b::7f00:1"} {
		t.Run(ip, func(t *testing.T) {
			if Public(net.ParseIP(ip)) {
				t.Fatal("private/reserved accepted")
			}
		})
	}
	for _, ip := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if !Public(net.ParseIP(ip)) {
			t.Fatal("public refused")
		}
	}
	f := &Fetcher{Lookup: func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP("10.0.0.1")}}, nil
	}}
	if _, _, e := f.Fetch(context.Background(), "https://mixed.example/x"); e == nil {
		t.Fatal("mixed DNS accepted")
	}
	for _, raw := range []string{"http://files.example/x", "https://u:p@files.example/x", "file:///etc/passwd", "https://files.example/x#fragment"} {
		if _, _, e := f.Fetch(context.Background(), raw); e == nil {
			t.Fatal("unsafe URL accepted")
		}
	}
}
func TestRedirectGuard(t *testing.T) {
	for _, tc := range []struct {
		name, location string
		ok             bool
	}{
		{"public", "https://files.example/final", true}, {"private", "https://private.example/secret", false}, {"http", "http://files.example/final", false}, {"credentials", "https://user:pass@files.example/final", false}, {"loop", "https://files.example/start", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			f := fixtureFetcher(t, func(r *http.Request) *http.Response {
				calls++
				if r.URL.Path == "/final" {
					return response("ok")
				}
				v := response("")
				v.StatusCode = 302
				v.Header.Set("Location", tc.location)
				return v
			})
			_, _, e := f.Fetch(context.Background(), "https://files.example/start")
			if (e == nil) != tc.ok {
				t.Fatalf("redirect: %v", e)
			}
			if tc.name == "loop" && calls != 4 {
				t.Fatalf("redirect limit: %d", calls)
			}
			if tc.name == "private" && calls != 1 {
				t.Fatal("private destination contacted")
			}
		})
	}
}
func TestFileCaps(t *testing.T) {
	for _, length := range []int64{MaxFetch + 1, -1} {
		t.Run(big.NewInt(length).String(), func(t *testing.T) {
			f := fixtureFetcher(t, func(*http.Request) *http.Response {
				v := response("")
				v.ContentLength = length
				v.Body = io.NopCloser(bytes.NewReader(bytes.Repeat([]byte{'x'}, MaxFetch+1)))
				return v
			})
			if _, _, e := f.Fetch(context.Background(), "https://files.example/big"); e == nil {
				t.Fatal("oversize fetch")
			}
		})
	}
	b64 := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{'x'}, MaxBinary+1))
	if _, _, e := (&Fetcher{}).Read(context.Background(), Source{Base64: &b64, Filename: "x"}); e == nil {
		t.Fatal("oversize binary")
	}
	text := strings.Repeat("x", MaxFetch+1)
	if _, _, e := (&Fetcher{}).Read(context.Background(), Source{Text: &text, Filename: "x"}); e == nil {
		t.Fatal("oversize text")
	}
}
