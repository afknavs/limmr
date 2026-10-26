package health

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type resolveFunc func(context.Context, string) ([]net.IPAddr, error)

func (f resolveFunc) LookupIPAddr(c context.Context, h string) ([]net.IPAddr, error) { return f(c, h) }

type discoveryTransport func(*http.Request) (*http.Response, error)

func (f discoveryTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestPublicDiscoveryResults(t *testing.T) {
	var dnsErr error
	empty, fail := false, false
	calls := 0
	resolver := resolveFunc(func(context.Context, string) ([]net.IPAddr, error) {
		if empty {
			return nil, nil
		}
		return []net.IPAddr{{IP: net.ParseIP("203.0.113.7")}}, dnsErr
	})
	transport := discoveryTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path != "/.well-known/oauth-authorization-server" || r.Header.Get("Authorization") != "" {
			t.Fatal("unsafe request")
		}
		if fail {
			return nil, errors.New("invented relay reset")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ignored"))}, nil
	})
	entry := &discoveryEntry{resolver: resolver, transport: transport}
	refresh := func() { refreshDiscovery(context.Background(), "https://maya-mac.tail0000.ts.net", entry) }
	refresh()
	if !entry.result.Reachable {
		t.Fatal("200 failed")
	}
	fail = true
	refresh()
	if !entry.result.Reachable {
		t.Fatal("first failure cleared success")
	}
	refresh()
	if entry.result.Reachable || entry.result.Reason != "relay unavailable" {
		t.Fatal("second failure ignored")
	}
	fail = false
	refresh()
	fail = true
	refresh()
	if !entry.result.Reachable {
		t.Fatal("success did not reset failures")
	}
	dnsErr = &net.DNSError{IsNotFound: true, Name: "maya-mac.tail0000.ts.net"}
	before := calls
	refresh()
	if entry.result.Reachable || entry.result.Reason != "public address missing from public DNS" || calls != before {
		t.Fatal("NXDOMAIN not immediate")
	}
	dnsErr = nil
	empty = true
	refresh()
	if entry.result.Reason != "public address missing from public DNS" {
		t.Fatal("empty answer not missing")
	}
	for _, reason := range []string{"public address missing from public DNS", "relay unavailable", "public DNS unavailable"} {
		detail := (DiscoveryResult{Reason: reason, CheckedAt: time.Now()}).Detail()
		for _, private := range []string{"maya-mac", "203.0.113.7", "invented relay"} {
			if strings.Contains(detail, private) {
				t.Fatal("detail leaked private data")
			}
		}
	}
}
func TestPublicResolverOnlyDialsPublicDNS(t *testing.T) {
	var mu sync.Mutex
	servers := map[string]int{}
	resolver := publicResolver{dial: func(_ context.Context, _, addr string) (net.Conn, error) {
		mu.Lock()
		defer mu.Unlock()
		if addr != "1.1.1.1:53" && addr != "8.8.8.8:53" {
			t.Error("system DNS used")
		}
		servers[addr]++
		return nil, errors.New("offline")
	}}
	_, err := resolver.LookupIPAddr(context.Background(), "maya-mac.tail0000.ts.net")
	if err == nil || servers["1.1.1.1:53"] == 0 || servers["8.8.8.8:53"] == 0 {
		t.Fatal("fallback missing")
	}
}
func TestDiscoveryLocalSkipsDNS(t *testing.T) {
	resolver := resolveFunc(func(context.Context, string) ([]net.IPAddr, error) { t.Fatal("local DNS used"); return nil, nil })
	transport := discoveryTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	for _, url := range []string{"http://localhost", "http://127.0.0.1", "http://[::1]"} {
		if !probeDiscovery(context.Background(), url, transport, resolver).Reachable {
			t.Fatal("local failed")
		}
	}
}
