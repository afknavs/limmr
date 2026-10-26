package health

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"limmr/internal/dbutil"
	"limmr/internal/hub"
	"limmr/internal/model"
	"limmr/internal/wa"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Version is embedded by the release build and must match the signed feed.
var Version = "0.1.0"

type Connection interface {
	Linked() bool
	Connected() bool
	SyncDone() bool
	Outdated() bool
	PhoneSeen() *time.Time
	Reconnect(context.Context) error
	RefreshGroups(context.Context) error
	RequestHistory(context.Context) error
}
type Service struct {
	Hub           *hub.Service
	Link          *wa.Link
	Connection    Connection
	Home          string
	LocalPort     int
	Demo          bool
	RemoteCheck   func(context.Context) []model.Check
	RestartRemote func() error
	ProbePorts    bool
	WatchesCheck  func() model.Check
	TasksCheck    func() model.Check
}

func (s *Service) Checks(ctx context.Context) []model.Check {
	checks := []model.Check{{ID: "version", Title: "Engine", State: "ok", Detail: "Limmr " + Version}}
	state := s.Link.State()
	linked := state.State == "ready" || state.State == "syncing"
	connected := linked
	syncDone := state.State == "ready"
	outdated := false
	if s.Connection != nil {
		linked = s.Connection.Linked()
		connected = s.Connection.Connected()
		syncDone = s.Connection.SyncDone()
		outdated = s.Connection.Outdated()
	}
	c := model.Check{ID: "whatsapp", Title: "WhatsApp", State: "ok", Detail: "Connected"}
	if !linked {
		c.State = "warn"
		c.Detail = "Link WhatsApp to get started."
	} else if !connected {
		c.State = "fail"
		c.Detail = "Disconnected"
		c.FixID = "reconnect"
	}
	if !connected && state.State == "error" && state.Reason != "" {
		c.Detail = state.Reason
	}
	if outdated {
		c.State = "fail"
		c.Detail = "The WhatsApp client needs an update (405)."
		c.FixID = "update_client"
	}
	checks = append(checks, c)
	phone := model.Check{ID: "phone", Title: "Phone", State: "warn", Detail: "No recent phone connection evidence."}
	if s.Demo {
		phone.State = "ok"
		phone.Detail = "Recently connected"
	} else if s.Connection != nil {
		if at := s.Connection.PhoneSeen(); at != nil {
			phone.State = "ok"
			phone.Detail = "Last observed " + at.UTC().Format(time.RFC3339)
		}
	}
	checks = append(checks, phone)
	hist := model.Check{ID: "history", Title: "History sync", State: "ok", Detail: "Complete"}
	if !syncDone {
		hist.State = "warn"
		hist.Detail = "History sync has not completed."
		hist.FixID = "history_sync"
	}
	checks = append(checks, hist)
	for _, db := range []struct {
		id        string
		path      string
		integrity func() bool
	}{{"messages", filepath.Join(s.Home, "messages.db"), func() bool {
		var result string
		return s.Hub.Messages.DB.QueryRow("PRAGMA quick_check").Scan(&result) == nil && result == "ok"
	}}, {"hub", filepath.Join(s.Home, "hub.db"), func() bool {
		var result string
		return s.Hub.Repo.DB.QueryRow("PRAGMA quick_check").Scan(&result) == nil && result == "ok"
	}}} {
		detail := "Database available"
		if stat, e := os.Stat(db.path); e == nil {
			detail = fmt.Sprintf("%d bytes; integrity checked", stat.Size())
		}
		state := "ok"
		if !db.integrity() {
			state = "fail"
			detail = "Database integrity check failed."
		}
		checks = append(checks, model.Check{ID: db.id + "_db", Title: strings.Title(db.id) + " database", State: state, Detail: detail})
	}
	logCheck := model.Check{ID: "logs", Title: "Engine log", State: "ok", Detail: "Within size limit"}
	if info, e := os.Stat(filepath.Join(s.Home, "logs", "engine.log")); e == nil && info.Size() > 20<<20 {
		logCheck.State = "warn"
		logCheck.Detail = "Log exceeds 20 MiB"
		logCheck.FixID = "trim_logs"
	}
	checks = append(checks, logCheck)
	local := model.Check{ID: "local_mcp", Title: "Local MCP", State: "ok", Detail: "Loopback listener available"}
	if s.ProbePorts {
		conn, e := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", s.LocalPort), 200*time.Millisecond)
		if e != nil {
			local.State = "fail"
			local.Detail = "Local listener unavailable"
		} else {
			conn.Close()
		}
	}
	checks = append(checks, local)
	if s.RemoteCheck != nil {
		// The legacy callback returns a listener check and calls HTTPDiscoveryCheck.
		// Collect its cached discovery detail without changing the callback contract.
		var discovery *DiscoveryResult
		remoteChecks := s.RemoteCheck(context.WithValue(ctx, discoveryResultKey{}, &discovery))
		if discovery != nil {
			for i := range remoteChecks {
				if remoteChecks[i].ID == "remote" {
					remoteChecks[i].Detail = discovery.Detail()
				}
			}
		}
		checks = append(checks, remoteChecks...)
	}
	if s.TasksCheck != nil {
		checks = append(checks, s.TasksCheck())
	}
	if s.WatchesCheck != nil {
		checks = append(checks, s.WatchesCheck())
	}
	agents, _ := s.Hub.Agents()
	for _, a := range agents {
		state := "ok"
		detail := "Recently used"
		if a.LastSeenAt == nil || s.Hub.Now().Sub(*a.LastSeenAt) > 7*24*time.Hour {
			state = "warn"
			detail = "Not used in the last seven days."
		}
		if !a.Enabled {
			state = "ok"
			detail = "Disabled"
		}
		checks = append(checks, model.Check{ID: "agent_" + a.ID, Title: a.Name, State: state, Detail: detail})
	}
	if s.ProbePorts {
		for _, port := range []int{8080, 18795} {
			conn, e := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
			if e == nil {
				conn.Close()
				checks = append(checks, model.Check{ID: "old_bridge", Title: "Previous WhatsApp setup", State: "warn", Detail: "A previous service may still be running.", FixID: "migrate_whatsapp_mcp"})
				break
			}
		}
	}
	if s.Demo {
		checks = append(checks, model.Check{ID: "demo_history", Title: "Older history", State: "warn", Detail: "Some older media is unavailable in this demo."})
	}
	return checks
}
func Overall(checks []model.Check) string {
	state := "ok"
	for _, c := range checks {
		if c.State == "fail" {
			return "fail"
		}
		if c.State == "warn" {
			state = "warn"
		}
	}
	return state
}
func (s *Service) Fix(ctx context.Context, id string) error {
	switch id {
	case "rebuild_search":
		return s.Hub.Messages.Rebuild()
	case "vacuum":
		// Retention deletes hub activity; compact both stores on this explicit
		// maintenance action and checkpoint WAL so the disk space is reclaimed.
		for _, db := range []dbutil.Database{s.Hub.Messages.DB, s.Hub.Repo.DB} {
			if _, e := db.Exec("VACUUM"); e != nil {
				return e
			}
			var busy, pages, checkpointed int
			if e := db.QueryRow("PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &pages, &checkpointed); e != nil {
				return e
			}
			if busy != 0 {
				return model.ErrConflict
			}
		}
		return nil
	case "trim_logs":
		return TrimLog(filepath.Join(s.Home, "logs", "engine.log"), 20<<20, 5<<20)
	case "restart_remote":
		if s.RestartRemote != nil {
			return s.RestartRemote()
		}
	case "reconnect":
		if s.Connection != nil {
			return s.Connection.Reconnect(ctx)
		}
	case "refresh_groups":
		if s.Connection != nil {
			return s.Connection.RefreshGroups(ctx)
		}
	case "history_sync":
		if s.Connection != nil {
			return s.Connection.RequestHistory(ctx)
		}
	}
	return model.ErrInvalid
}
func (s *Service) Diagnostics(ctx context.Context) string {
	// Construct from an allowlist of numeric/boolean facts. Never redact free-form
	// strings after the fact: names, text, tokens and arbitrary errors never enter.
	chats, messages := s.Hub.Messages.Counts()
	agents, _ := s.Hub.Agents()
	linked, connected := false, false
	if s.Connection != nil {
		linked = s.Connection.Linked()
		connected = s.Connection.Connected()
	} else {
		st := s.Link.State()
		linked = st.State == "ready" || st.State == "syncing"
		connected = linked
	}
	return fmt.Sprintf("Limmr %s\nlinked=%t\nconnected=%t\nchats=%d\nmessages=%d\nagents=%d\npending_approvals=%d\n", Version, linked, connected, chats, messages, len(agents), s.Hub.PendingCount())
}

// DiscoveryResult is a cached public probe, never a local health dependency.
type DiscoveryResult struct {
	Reachable bool
	Reason    string
	CheckedAt time.Time
}

func (r DiscoveryResult) Detail() string {
	if r.CheckedAt.IsZero() {
		return "Checking public discovery…"
	}
	detail := "Public discovery is not reachable yet."
	switch r.Reason {
	case "public address missing from public DNS":
		detail = "Public address is missing from public DNS (Tailscale Funnel). Turn Funnel off and on."
	case "relay unavailable":
		detail = "Public address resolves but the relay didn't answer."
	case "public DNS unavailable":
		detail = "Public DNS could not be checked."
	}
	if r.Reachable {
		detail = "Public discovery is reachable."
	}
	return detail + " Last checked " + r.CheckedAt.UTC().Format(time.RFC3339)
}

type discoveryResultKey struct{}
type discoveryEntry struct {
	result    DiscoveryResult
	running   bool
	watchers  int
	cancel    context.CancelFunc
	transport http.RoundTripper
	resolver  discoveryResolver
	failures  int
}

var discoveryCache = struct {
	sync.Mutex
	entries map[string]*discoveryEntry
}{entries: map[string]*discoveryEntry{}}

// WatchDiscovery starts an immediate probe and a 60-second timer. The listener
// owns its lifetime, so reconfiguration cancels old probes and drops old results.
func WatchDiscovery(publicURL string) func() {
	return WatchDiscoveryWithTransport(publicURL, nil)
}

// WatchDiscoveryWithTransport supports HTTP over an injected offline transport.
// A nil transport uses the public-IP-pinned HTTP transport.
func WatchDiscoveryWithTransport(publicURL string, transport http.RoundTripper) func() {
	return watchDiscovery(publicURL, transport, nil)
}

type discoveryResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

func watchDiscovery(publicURL string, transport http.RoundTripper, resolver discoveryResolver) func() {
	discoveryCache.Lock()
	entry := discoveryCache.entries[publicURL]
	if entry == nil {
		entry = &discoveryEntry{transport: transport, resolver: resolver}
		discoveryCache.entries[publicURL] = entry
	}
	entry.watchers++
	if entry.cancel == nil {
		ctx, cancel := context.WithCancel(context.Background())
		entry.cancel = cancel
		go func() {
			refreshDiscovery(ctx, publicURL, entry)
			ticker := time.NewTicker(60 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					refreshDiscovery(ctx, publicURL, entry)
				}
			}
		}()
	}
	discoveryCache.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			discoveryCache.Lock()
			defer discoveryCache.Unlock()
			entry.watchers--
			if entry.watchers == 0 {
				entry.cancel()
				delete(discoveryCache.entries, publicURL)
			}
		})
	}
}

func refreshDiscovery(ctx context.Context, publicURL string, entry *discoveryEntry) {
	discoveryCache.Lock()
	if entry.running {
		discoveryCache.Unlock()
		return
	}
	entry.running = true
	discoveryCache.Unlock()
	result := probeDiscovery(ctx, publicURL, entry.transport, entry.resolver)
	discoveryCache.Lock()
	defer discoveryCache.Unlock()
	entry.running = false
	if ctx.Err() == nil {
		if result.Reason == "relay unavailable" {
			entry.failures++
			if entry.result.Reachable && entry.failures < 2 {
				result.Reachable = true
				result.Reason = ""
			}
		} else {
			entry.failures = 0
		}
		result.CheckedAt = time.Now()
		entry.result = result
	}
}

// HTTPDiscoveryCheck retains the bool callback API but only reads a cache.
// An unwatched URL gets a background one-shot check, also never on the caller.
func HTTPDiscoveryCheck(ctx context.Context, publicURL string) bool {
	discoveryCache.Lock()
	entry := discoveryCache.entries[publicURL]
	if entry == nil {
		entry = &discoveryEntry{}
		discoveryCache.entries[publicURL] = entry
	}
	result := entry.result
	if entry.cancel == nil && !entry.running && time.Since(result.CheckedAt) >= time.Minute {
		go refreshDiscovery(context.Background(), publicURL, entry)
	}
	discoveryCache.Unlock()
	if dst, ok := ctx.Value(discoveryResultKey{}).(**DiscoveryResult); ok {
		*dst = &result
	}
	return result.Reachable
}

// Probes send no credentials, follow no redirects and ignore response bodies.
// publicResolver never falls back to the system resolver or MagicDNS.
type publicResolver struct {
	dial func(context.Context, string, string) (net.Conn, error)
}

func (r publicResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	var last error
	for _, server := range []string{"1.1.1.1:53", "8.8.8.8:53"} {
		lookupCtx, cancel := context.WithTimeout(ctx, time.Second)
		resolver := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			dial := r.dial
			if dial == nil {
				dial = (&net.Dialer{Timeout: time.Second}).DialContext
			}
			return dial(ctx, network, server)
		}}
		ips, err := resolver.LookupIPAddr(lookupCtx, host)
		cancel()
		if err == nil {
			return ips, nil
		}
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return nil, err
		}
		last = err
	}
	return nil, last
}

func probeDiscovery(ctx context.Context, publicURL string, transport http.RoundTripper, resolver discoveryResolver) DiscoveryResult {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	failed := DiscoveryResult{Reason: "relay unavailable"}
	req, e := http.NewRequestWithContext(ctx, "GET", publicURL+"/.well-known/oauth-authorization-server", nil)
	if e != nil || req.URL.User != nil {
		return failed
	}
	host := req.URL.Hostname()
	if net.ParseIP(host) == nil && !strings.EqualFold(host, "localhost") {
		if resolver == nil {
			resolver = publicResolver{}
		}
		ips, err := resolver.LookupIPAddr(ctx, host)
		if err != nil {
			var dnsErr *net.DNSError
			if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
				return DiscoveryResult{Reason: "public address missing from public DNS"}
			}
			return DiscoveryResult{Reason: "public DNS unavailable"}
		}
		if len(ips) == 0 {
			return DiscoveryResult{Reason: "public address missing from public DNS"}
		}
		if transport == nil {
			tr := &http.Transport{TLSClientConfig: &tls.Config{ServerName: host}, DisableKeepAlives: true}
			tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				_, port, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}
				var last error
				for _, ip := range ips {
					conn, err := (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
					if err == nil {
						return conn, nil
					}
					last = err
				}
				return nil, last
			}
			defer tr.CloseIdleConnections()
			transport = tr
		}
	}
	if transport == nil {
		transport = &http.Transport{DisableKeepAlives: true}
	}
	client := http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	resp, e := client.Do(req)
	if e != nil {
		return failed
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return failed
	}
	return DiscoveryResult{Reachable: true}
}

// TrimLog retains the tail in the same inode, following the upstream bridge.
func TrimLog(path string, limit, keep int64) error {
	logMaintenance.Lock()
	defer logMaintenance.Unlock()
	return trimLog(path, limit, keep)
}

// Serialize manual/timer maintenance with writer rotation as well as writes.
var logMaintenance sync.Mutex

func trimLog(path string, limit, keep int64) error {
	f, e := os.OpenFile(path, os.O_RDWR, 0)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() <= limit {
		return e
	}
	tail := make([]byte, keep)
	if _, e = f.ReadAt(tail, info.Size()-keep); e != nil {
		return e
	}
	if _, e = f.WriteAt(tail, 0); e != nil {
		return e
	}
	return f.Truncate(keep)
}

type BoundedLog struct {
	mu   sync.Mutex
	file *os.File
	path string
}

func OpenLog(home string) (*BoundedLog, error) {
	dir := filepath.Join(home, "logs")
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	path := filepath.Join(dir, "engine.log")
	if e := TrimLog(path, 20<<20, 5<<20); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		return nil, e
	}
	return &BoundedLog{file: f, path: path}, nil
}
func (l *BoundedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	logMaintenance.Lock()
	defer logMaintenance.Unlock()
	if e := trimLog(l.path, 20<<20, 5<<20); e != nil {
		return 0, e
	}
	n, err := l.file.Write(p)
	if err != nil {
		return n, err
	}
	// Enforce the bound on this write, including a final oversized record.
	return n, trimLog(l.path, 20<<20, 5<<20)
}
func (l *BoundedLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.file.Close()
}

var _ io.Writer = (*BoundedLog)(nil)
