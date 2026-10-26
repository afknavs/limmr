package health_test

import (
	"context"
	"limmr/internal/api"
	"limmr/internal/health"
	"limmr/internal/mcpserver"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHangingDiscoveryNeverBlocksHealthOrStatus(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	public := q1PipeServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))

	defer close(release)
	stop := health.WatchDiscoveryWithTransport(public.URL, public.Client().Transport)
	defer stop()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("background probe did not start")
	}
	h, p, l := testkit.Open(t)
	hs := &health.Service{Hub: h, Link: l, Home: h.Home, RemoteCheck: func(ctx context.Context) []model.Check {
		c := model.Check{ID: "remote", State: "ok", Detail: "Listener ready"}
		if !health.HTTPDiscoveryCheck(ctx, public.URL) {
			c.State = "warn"
			c.Detail = "unreachable"
		}
		return []model.Check{c}
	}}
	server := &api.Server{Hub: h, People: p, Link: l, Health: hs, MCP: &mcpserver.Service{Hub: h, People: p}, Port: 18800, ControlToken: "invented-test-control"}
	handler := server.Handler()
	for _, path := range []string{"/v1/health", "/v1/status"} {
		start := time.Now()
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "http://127.0.0.1:18800"+path, nil)
		req.Header.Set("Authorization", "Bearer invented-test-control")
		handler.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("local endpoint failed: %d", w.Code)
		}
		if time.Since(start) > 500*time.Millisecond {
			t.Fatal("local endpoint waited on public discovery")
		}
		if path == "/v1/health" && !strings.Contains(w.Body.String(), "Checking public discovery") {
			t.Fatal("first result did not report checking")
		}
	}
}

func TestDiscoveryCachesCompletionAndConfigChangeProbesImmediately(t *testing.T) {
	public := q1PipeServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))

	stop := health.WatchDiscoveryWithTransport(public.URL, public.Client().Transport)
	defer stop()
	deadline := time.Now().Add(time.Second)
	for !health.HTTPDiscoveryCheck(context.Background(), public.URL) {
		if time.Now().After(deadline) {
			t.Fatal("background success not cached")
		}
		time.Sleep(time.Millisecond)
	}
	h, _, l := testkit.Open(t)
	hs := &health.Service{Hub: h, Link: l, Home: h.Home, RemoteCheck: func(ctx context.Context) []model.Check {
		health.HTTPDiscoveryCheck(ctx, public.URL)
		return []model.Check{{ID: "remote", State: "ok"}}
	}}
	found := false
	for _, check := range hs.Checks(context.Background()) {
		if check.ID == "remote" && strings.Contains(check.Detail, "Public discovery is reachable. Last checked ") {
			found = true
		}
	}
	if !found {
		t.Fatal("cached result omitted its check time")
	}
	// A different URL starts independently rather than reusing the prior success.
	release := make(chan struct{})
	dead := q1PipeServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))

	defer close(release)
	stopNew := health.WatchDiscoveryWithTransport(dead.URL, dead.Client().Transport)
	defer stopNew()
	if health.HTTPDiscoveryCheck(context.Background(), dead.URL) {
		t.Fatal("new URL reused old success")
	}
}

// Real HTTP serialization over net.Pipe, with no socket bind.
type q1PipeListener struct {
	incoming chan net.Conn
	done     chan struct{}
	once     sync.Once
}

func (l *q1PipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.incoming:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}
func (l *q1PipeListener) Close() error   { l.once.Do(func() { close(l.done) }); return nil }
func (l *q1PipeListener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)} }

type q1Public struct {
	URL       string
	transport http.RoundTripper
}

func (s *q1Public) Client() *http.Client { return &http.Client{Transport: s.transport} }
func q1PipeServer(t *testing.T, handler http.Handler) *q1Public {
	t.Helper()
	l := &q1PipeListener{incoming: make(chan net.Conn), done: make(chan struct{})}
	server := &http.Server{Handler: handler}
	done := make(chan struct{})
	go func() { _ = server.Serve(l); close(done) }()
	transport := &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		client, conn := net.Pipe()
		select {
		case l.incoming <- conn:
			return client, nil
		case <-ctx.Done():
			client.Close()
			conn.Close()
			return nil, ctx.Err()
		}
	}}
	t.Cleanup(func() {
		transport.CloseIdleConnections()
		server.Close()
		l.Close()
		<-done
	})
	return &q1Public{transport: transport, URL: "http://127.0.0.1/" + strings.ToLower(t.Name()) + "/" + time.Now().Format("150405.000000000")}
}

func TestDiscoveryHangingSocketWhenPermitted(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("sandbox denies loopback listener; HTTP pipe regression still runs")
	}
	entered := make(chan struct{}, 1)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		<-r.Context().Done()
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	url := "http://" + listener.Addr().String()
	stop := health.WatchDiscovery(url)
	defer stop()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("probe did not reach hanging listener")
	}
	start := time.Now()
	if health.HTTPDiscoveryCheck(context.Background(), url) {
		t.Fatal("hanging URL reported reachable")
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("cached discovery blocked")
	}
}
