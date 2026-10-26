// Package attachments handles untrusted AI file sources without ambient credentials.
package attachments

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const MaxFetch = 20 << 20
const MaxBinary = 8 << 20

type Source struct {
	AttachmentID string
	Text         *string
	Base64       *string
	Filename     string
	URL          string
	File         any
}
type Fetcher struct {
	// Roots supports a trusted test TLS server without weakening certificate verification.
	Roots  *x509.CertPool
	Lookup func(context.Context, string) ([]net.IPAddr, error)
	Dial   func(context.Context, string, string) (net.Conn, error)
}

func Public(ip net.IP) bool {
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	a = a.Unmap()
	// Internet IPv6 unicast is allocated from 2000::/3; refuse site-local,
	// discard-only and translation ranges outside it as well as ULA/link-local.
	if a.Is6() && !netip.MustParsePrefix("2000::/3").Contains(a) {
		return false
	}
	if !a.IsGlobalUnicast() || a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() || a.IsUnspecified() {
		return false
	}
	for _, p := range []string{"100.64.0.0/10", "0.0.0.0/8", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "2001::/23", "2002::/16", "64:ff9b::/96"} {
		if netip.MustParsePrefix(p).Contains(a) {
			return false
		}
	}
	return true
}
func Name(s string) string {
	s = path.Base(strings.ReplaceAll(s, "\\", "/"))
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == ':' {
			return -1
		}
		return r
	}, s)
	s = strings.Trim(s, " .")
	if s == "" {
		return "attachment"
	}
	if len(s) > 180 {
		s = string([]rune(s)[:min(60, len([]rune(s)))])
	}
	return s
}
func (f *Fetcher) addresses(ctx context.Context, host string) ([]net.IPAddr, error) {
	lookup := f.Lookup
	if lookup == nil {
		lookup = net.DefaultResolver.LookupIPAddr
	}
	ips, err := lookup(ctx, host)
	if err != nil || len(ips) == 0 {
		return nil, fmt.Errorf("File host could not be resolved.")
	}
	for _, ip := range ips {
		if !Public(ip.IP) || ip.Zone != "" {
			return nil, fmt.Errorf("File URLs must resolve only to public internet addresses.")
		}
	}
	return ips, nil
}
func (f *Fetcher) Fetch(ctx context.Context, raw string) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	guard := func(raw string) (*url.URL, error) {
		u, e := url.Parse(raw)
		if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
			return nil, fmt.Errorf("Use an HTTPS file URL without credentials.")
		}
		if _, e = f.addresses(ctx, u.Hostname()); e != nil {
			return nil, e
		}
		return u, nil
	}
	u, err := guard(raw)
	if err != nil {
		return nil, "", err
	}
	tr := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: f.Roots, MinVersion: tls.VersionTLS12}, Proxy: nil, DisableKeepAlives: true, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second}
	tr.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(address)
		if e != nil {
			return nil, e
		}
		ips, e := f.addresses(ctx, host)
		if e != nil {
			return nil, e
		}
		dial := f.Dial
		if dial == nil {
			dial = (&net.Dialer{Timeout: 10 * time.Second}).DialContext
		}
		return dial(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 30 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 {
			return fmt.Errorf("Too many file redirects.")
		}
		_, e := guard(req.URL.String())
		req.Header = make(http.Header)
		return e
	}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("File download refused or unavailable.")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || resp.ContentLength > MaxFetch {
		return nil, "", fmt.Errorf("File unavailable or exceeds 20 MB.")
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, MaxFetch+1))
	if err != nil || len(b) == 0 || len(b) > MaxFetch {
		return nil, "", fmt.Errorf("File unavailable or exceeds 20 MB.")
	}
	return b, Name(resp.Request.URL.Path), nil
}
func (f *Fetcher) Read(ctx context.Context, s Source) ([]byte, string, error) {
	n := 0
	for _, on := range []bool{s.AttachmentID != "", s.Text != nil, s.Base64 != nil, s.URL != "", s.File != nil} {
		if on {
			n++
		}
	}
	if n != 1 {
		return nil, "", fmt.Errorf("Choose exactly one file source.")
	}
	if s.AttachmentID != "" {
		return nil, "", nil
	}
	name := s.Filename
	if s.Text != nil {
		if name == "" || !utf8.ValidString(*s.Text) || len(*s.Text) == 0 || len(*s.Text) > MaxFetch {
			return nil, "", fmt.Errorf("Text files need a filename and at most 20 MB of UTF-8 text.")
		}
		return []byte(*s.Text), Name(name), nil
	}
	if s.Base64 != nil {
		if name == "" || len(*s.Base64) > base64.StdEncoding.EncodedLen(MaxBinary) {
			return nil, "", fmt.Errorf("Binary files need a filename and at most 8 MB decoded.")
		}
		b, e := base64.StdEncoding.Strict().DecodeString(*s.Base64)
		if e != nil || len(b) == 0 || len(b) > MaxBinary {
			return nil, "", fmt.Errorf("Invalid base64 file (maximum 8 MB decoded).")
		}
		return b, Name(name), nil
	}
	raw := s.URL
	if s.File != nil {
		switch v := s.File.(type) {
		case string:
			raw = v
		case map[string]any:
			raw, _ = v["download_url"].(string)
			if x, _ := v["name"].(string); name == "" {
				name = x
			}
		default:
			return nil, "", fmt.Errorf("File reference needs a download_url; use content_text or content_base64 if unavailable.")
		}
	}
	b, suggested, e := f.Fetch(ctx, raw)
	if name == "" {
		name = suggested
	}
	return b, Name(name), e
}
