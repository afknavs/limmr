// Package remote ports the approve-on-Mac OAuth design from whatsapp-mcp's
// remote.py (see NOTICE). Tokens persist as digests; approval is local only.
package remote

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"limmr/internal/dbutil"
	"limmr/internal/health"
	"limmr/internal/hub"
	"limmr/internal/mcpserver"
	"limmr/internal/model"
	knowledgeSync "limmr/internal/sync"
)

type Config struct {
	Enabled   bool   `json:"enabled"`
	PublicURL string `json:"public_url"`
	// Optional compare-and-set protects wizard rollback against concurrent edits/approvals.
	OnlyIf           *Config `json:"only_if,omitempty"`
	RequireNoClients bool    `json:"require_no_clients,omitempty"`
}
type Client struct {
	ID           string     `json:"client_id"`
	Name         string     `json:"name"`
	RedirectURIs []string   `json:"redirect_uris"`
	AuthMethod   string     `json:"token_endpoint_auth_method"`
	SecretDigest string     `json:"secret_digest,omitempty"`
	AgentID      string     `json:"agent_id,omitempty"`
	ApprovedAt   *time.Time `json:"approved_at"`
	LastUse      *time.Time `json:"last_use"`
	ExpiresAt    time.Time  `json:"expires_at"`
}
type Pending struct {
	Sync         bool      `json:"sync"`
	PeerEngineID string    `json:"peer_engine_id,omitempty"`
	Code         string    `json:"code"`
	Name         string    `json:"name"`
	Kind         string    `json:"kind"`
	ReturnsTo    string    `json:"returns_to"`
	RequestedAt  time.Time `json:"requested_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	State        string    `json:"state"`
	Tx           string    `json:"-"`
	ClientID     string    `json:"-"`
	RedirectURI  string    `json:"-"`
	Challenge    string    `json:"-"`
	OAuthState   string    `json:"-"`
	Redirect     string    `json:"-"`
}
type grant struct {
	ClientID, AgentID, RedirectURI, Challenge, Resource string
	ExpiresAt                                           time.Time
}
type tokenRecord struct {
	Kind      string    `json:"kind"`
	ClientID  string    `json:"client_id"`
	AgentID   string    `json:"agent_id"`
	Resource  string    `json:"resource"`
	ExpiresAt time.Time `json:"expires_at"`
}
type Status struct {
	Enabled           bool      `json:"enabled"`
	PublicURL         string    `json:"public_url"`
	MCPURL            string    `json:"mcp_url"`
	Port              int       `json:"port"`
	Pending           []Pending `json:"pending"`
	Clients           []Client  `json:"clients"`
	Error             string    `json:"error,omitempty"`
	ClientsOnOtherURL int       `json:"clients_on_other_url"`
	ClientsWithTokens int       `json:"clients_with_tokens"`
}
type Service struct {
	Knowledge     *knowledgeSync.Service
	mediaKey      string
	media         map[string]mediaGrant
	Hub           *hub.Service
	MCP           *mcpserver.Service
	Port          int
	Now           func() time.Time
	mu            sync.Mutex
	config        Config
	unapproved    map[string]Client
	pending       map[string]*Pending
	grants        map[string]grant
	server        *http.Server
	listener      net.Listener
	stopDiscovery func()
	lastError     string
	Gate          *sync.RWMutex
	Listen        func(string, string) (net.Listener, error)
	// Logger receives redacted remote auth failures (see authlog.go); nil disables them.
	Logger  *log.Logger
	authLog authLogger
}

func New(h *hub.Service, m *mcpserver.Service, port int) *Service {
	s := &Service{mediaKey: dbutil.ID() + dbutil.ID(), media: map[string]mediaGrant{}, Hub: h, MCP: m, Port: port, Now: time.Now, unapproved: map[string]Client{}, pending: map[string]*Pending{}, grants: map[string]grant{}}
	if h.Repo.Get("remote_settings", "main", &s.config) != nil {
		s.config = Config{}
	}
	m.RemoteMedia = s.MediaURL
	return s
}

// basePath allows a public URL mounted under a plain prefix (e.g. Funnel /limmr on :443 beside another app).
var basePath = regexp.MustCompile(`^(/[a-z0-9][a-z0-9-]{0,31}){0,3}$`)

func ValidURL(raw string) (string, error) {
	u, e := url.Parse(strings.TrimRight(raw, "/"))
	if e != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || u.RawPath != "" || !basePath.MatchString(u.Path) {
		return "", model.ErrInvalid
	}
	local := u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")
	if u.Scheme != "https" && !local {
		return "", model.ErrInvalid
	}
	return u.String(), nil
}
func (s *Service) Configuration() Config { s.mu.Lock(); defer s.mu.Unlock(); return s.config }
func (s *Service) Configure(c Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c.OnlyIf != nil && (c.OnlyIf.Enabled != s.config.Enabled || c.OnlyIf.PublicURL != s.config.PublicURL) {
		return model.ErrConflict
	}
	if c.RequireNoClients {
		clients, err := hub.List[Client](s.Hub.Repo, "oauth_clients")
		if err != nil {
			return err
		}
		if len(clients) > 0 {
			return model.ErrConflict
		}
	}
	c.OnlyIf = nil
	c.RequireNoClients = false
	if c.Enabled {
		u, e := ValidURL(c.PublicURL)
		if e != nil {
			return e
		}
		c.PublicURL = u
	} else if c.PublicURL != "" {
		u, e := ValidURL(c.PublicURL)
		if e != nil {
			return e
		}
		c.PublicURL = u
	}
	if !c.Enabled || c.PublicURL != s.config.PublicURL {
		s.media = map[string]mediaGrant{}
	}
	if c.PublicURL != s.config.PublicURL {
		s.pending = map[string]*Pending{}
		s.grants = map[string]grant{}
		s.unapproved = map[string]Client{}
	}
	if e := s.Hub.Repo.Put("remote_settings", "main", c); e != nil {
		return e
	}
	s.config = c
	return s.restartLocked()
}
func (s *Service) Start() error { s.mu.Lock(); defer s.mu.Unlock(); return s.restartLocked() }
func (s *Service) restartLocked() error {
	if s.stopDiscovery != nil {
		s.stopDiscovery()
		s.stopDiscovery = nil
	}
	if s.server != nil {
		_ = s.server.Close()
		s.server = nil
		s.listener = nil
	}
	if !s.config.Enabled {
		return nil
	}
	if _, e := ValidURL(s.config.PublicURL); e != nil {
		s.lastError = "Remote configuration is invalid."
		return e
	}
	listen := s.Listen
	if listen == nil {
		listen = net.Listen
	}
	ln, e := listen("tcp", fmt.Sprintf("127.0.0.1:%d", s.Port))
	if e != nil {
		s.lastError = "Remote listener could not start."
		return fmt.Errorf("remote listener unavailable")
	}
	s.stopDiscovery = health.WatchDiscovery(s.config.PublicURL)
	s.listener = ln
	s.server = &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second}
	srv := s.server
	s.lastError = ""
	go func() { _ = srv.Serve(ln) }()
	return nil
}
func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopDiscovery != nil {
		s.stopDiscovery()
		s.stopDiscovery = nil
	}
	if s.server != nil {
		e := s.server.Close()
		s.server = nil
		return e
	}
	return nil
}
func (s *Service) expireLocked() {
	now := s.Now()
	for k, p := range s.pending {
		if !now.Before(p.ExpiresAt) {
			delete(s.pending, k)
		}
	}
	for k, c := range s.unapproved {
		if !now.Before(c.ExpiresAt) {
			delete(s.unapproved, k)
		}
	}
	for k, g := range s.grants {
		if !now.Before(g.ExpiresAt) {
			delete(s.grants, k)
		}
	}
}
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	out := Status{Enabled: s.config.Enabled && s.server != nil, PublicURL: s.config.PublicURL, Port: s.Port, Pending: []Pending{}, Clients: []Client{}, Error: s.lastError}
	if s.config.PublicURL != "" {
		out.MCPURL = s.config.PublicURL + "/mcp"
	}
	for _, p := range s.pending {
		if p.State == "waiting" {
			out.Pending = append(out.Pending, *p)
		}
	}
	cs, _ := hub.List[Client](s.Hub.Repo, "oauth_clients")
	tokens, _ := hub.List[tokenRecord](s.Hub.Repo, "oauth_tokens")
	withTokens, otherURL := map[string]bool{}, map[string]bool{}
	for _, token := range tokens {
		if s.Now().Before(token.ExpiresAt) {
			withTokens[token.ClientID] = true
			if token.Resource != s.config.PublicURL+"/mcp" {
				otherURL[token.ClientID] = true
			}
		}
	}
	for _, c := range cs {
		if withTokens[c.ID] {
			out.ClientsWithTokens++
		}
		if otherURL[c.ID] {
			out.ClientsOnOtherURL++
		}
		c.SecretDigest = ""
		out.Clients = append(out.Clients, c)
	}
	return out
}
func (s *Service) clientLocked(id string) (Client, error) {
	var c Client
	if e := s.Hub.Repo.Get("oauth_clients", id, &c); e == nil {
		return c, nil
	}
	c, ok := s.unapproved[id]
	if !ok {
		return c, model.ErrNotFound
	}
	return c, nil
}
func inferKind(name string) string {
	n := strings.ToLower(name)
	if strings.Contains(n, "chatgpt") {
		return "chatgpt"
	}
	if strings.Contains(n, "claude") {
		return "claude_ai"
	}
	return "custom"
}
func (s *Service) Approve(code, agentID string, newAgent *struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}, access *model.Access, fillIn bool, presets ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	var p *Pending
	for _, v := range s.pending {
		if v.Code == code && v.State == "waiting" {
			p = v
		}
	}
	if p == nil {
		return model.ErrNotFound
	}
	c, e := s.clientLocked(p.ClientID)
	if e != nil {
		return e
	}
	if (agentID == "") == (newAgent == nil) {
		return model.ErrInvalid
	}
	preset := "own"
	if len(presets) > 0 && presets[0] != "" {
		preset = presets[0]
	} else if access != nil || fillIn {
		preset = "given"
	} // legacy access/fill_in payloads
	if preset != "own" && preset != "given" && preset != "dot" {
		return model.ErrInvalid
	}
	if p.Sync != (preset == "dot") || preset == "dot" && (s.Knowledge == nil || newAgent == nil) {
		return model.ErrInvalid
	}
	var a model.Agent
	if newAgent != nil {
		kind := newAgent.Kind
		if kind == "" {
			kind = inferKind(c.Name)
		}
		name := newAgent.Name
		if name == "" {
			name = c.Name
		}
		a, _, e = s.Hub.CreateAgent(name, kind, "remote")
	} else {
		a, e = s.Hub.Agent(agentID)
	}
	if e != nil {
		return e
	}
	if a.Transport != "remote" || !a.Enabled {
		return model.ErrInvalid
	}
	a.Preset = preset
	a.Permissions = model.Preset(preset)
	if preset == "given" && (len(presets) == 0 || presets[0] == "") && access == nil {
		a.Permissions.Access = model.AllAccess()
	}
	if preset == "given" {
		if access != nil || fillIn {
			if access != nil {
				a.Permissions.Access = *access
			}
			// "Fill in" lets your own AI set Limmr up: suggest people details, propose circles and write briefs.
			// Sending and media stay off for remote AIs until the owner allows them.
			if fillIn {
				a.Permissions.WritePeople, a.Permissions.SaveSummaries = true, true
			}
		}
	}
	if preset == "dot" {
		a.Preset = "given"
		a.Permissions = model.Permissions{Sync: true, Chats: model.Scope{Mode: "only", JIDs: []string{}}, Access: model.Access{Circles: []string{}}}
		if e = s.Knowledge.Pair(p.PeerEngineID, s.Knowledge.EngineID); e != nil {
			return e
		}
		var peer knowledgeSync.Peer
		if e = s.Hub.Repo.Get("settings", "sync_peer", &peer); e != nil {
			return e
		}
		peer.AgentID = a.ID
		if e = s.Hub.Repo.Put("settings", "sync_peer", peer); e != nil {
			return e
		}
	}
	if e = s.Hub.UpdateAgent(a); e != nil {
		return e
	}
	now := s.Now().UTC()
	c.AgentID = a.ID
	c.ApprovedAt = &now
	if e = s.Hub.Repo.Put("oauth_clients", c.ID, c); e != nil {
		return e
	}
	delete(s.unapproved, c.ID)
	raw := dbutil.ID() + dbutil.ID()
	s.grants[dbutil.Digest(raw)] = grant{c.ID, a.ID, p.RedirectURI, p.Challenge, s.config.PublicURL + "/mcp", now.Add(5 * time.Minute)}
	u, _ := url.Parse(p.RedirectURI)
	q := u.Query()
	q.Set("code", raw)
	if p.OAuthState != "" {
		q.Set("state", p.OAuthState)
	}
	u.RawQuery = q.Encode()
	p.Redirect = u.String()
	p.State = "approved"
	return nil
}
func (s *Service) Deny(code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	for _, p := range s.pending {
		if p.Code == code && p.State == "waiting" {
			p.State = "denied"
			u, _ := url.Parse(p.RedirectURI)
			q := u.Query()
			q.Set("error", "access_denied")
			q.Set("state", p.OAuthState)
			u.RawQuery = q.Encode()
			p.Redirect = u.String()
			return nil
		}
	}
	return model.ErrNotFound
}
func (s *Service) Revoke(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.revokeLocked(id)
}
func (s *Service) revokeLocked(id string) error {
	var c Client
	if e := s.Hub.Repo.Get("oauth_clients", id, &c); e != nil {
		return e
	}
	tx, e := s.Hub.Repo.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec("DELETE FROM objects WHERE bucket='oauth_clients' AND id=?", id); e != nil {
		return e
	}
	if _, e = tx.Exec("DELETE FROM objects WHERE bucket='oauth_tokens' AND json_extract(data,'$.client_id')=?", id); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	for k, p := range s.pending {
		if p.ClientID == id {
			delete(s.pending, k)
		}
	}
	for k, g := range s.grants {
		if g.ClientID == id {
			delete(s.grants, k)
		}
	}
	return nil
}

// Consume a refresh token in the same transaction that creates its replacement.
// A storage failure must leave neither a second usable refresh token nor orphaned credentials.
func (s *Service) issueLocked(c Client, consume string) (map[string]any, error) {
	now := s.Now().UTC()
	access, refresh := dbutil.ID()+dbutil.ID(), dbutil.ID()+dbutil.ID()
	tx, e := s.Hub.Repo.DB.Begin()
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	for _, v := range []struct {
		raw, kind string
		ttl       time.Duration
	}{{access, "access", time.Hour}, {refresh, "refresh", 30 * 24 * time.Hour}} {
		rec := tokenRecord{v.kind, c.ID, c.AgentID, s.config.PublicURL + "/mcp", now.Add(v.ttl)}
		if _, e = tx.Exec("INSERT INTO objects VALUES('oauth_tokens',?,?)", dbutil.Digest(v.raw), dbutil.JSON(rec)); e != nil {
			return nil, e
		}
	}
	if consume != "" {
		result, err := tx.Exec("DELETE FROM objects WHERE bucket='oauth_tokens' AND id=?", consume)
		if err != nil {
			return nil, err
		}
		if n, err := result.RowsAffected(); err != nil || n != 1 {
			return nil, model.ErrInvalid
		}
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return map[string]any{"access_token": access, "refresh_token": refresh, "token_type": "Bearer", "expires_in": 3600, "scope": "mcp"}, nil
}
func (s *Service) Verify(ctx context.Context, token string, r *http.Request) (*auth.TokenInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Same checks and results as before; deny only adds a redacted log line.
	deny := func(reason string) (*auth.TokenInfo, error) {
		s.authFailure("mcp", "none", "invalid_token", reason)
		return nil, auth.ErrInvalidToken
	}
	var rec tokenRecord
	switch {
	case !s.config.Enabled:
		return deny("remote_disabled")
	case s.Hub.Repo.Get("oauth_tokens", dbutil.Digest(token), &rec) != nil:
		return deny("token_unknown")
	case rec.Kind != "access":
		return deny("token_kind")
	case !s.Now().Before(rec.ExpiresAt):
		return deny("token_expired")
	case rec.Resource != s.config.PublicURL+"/mcp":
		return deny("token_resource_mismatch")
	}
	c, e := s.clientLocked(rec.ClientID)
	if e != nil {
		return deny("client_unknown")
	}
	if c.AgentID != rec.AgentID {
		return deny("client_agent_mismatch")
	}
	a, e := s.Hub.ActiveAgent(rec.AgentID)
	if e != nil {
		return deny("agent_inactive")
	}
	if a.Transport != "remote" {
		return deny("agent_not_remote")
	}
	now := s.Now().UTC()
	c.LastUse = &now
	if e = s.Hub.Repo.Put("oauth_clients", c.ID, c); e != nil {
		return deny("storage")
	}
	return &auth.TokenInfo{Scopes: []string{"mcp"}, Expiration: rec.ExpiresAt, UserID: a.ID}, nil
}
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/sync/", s.SyncHandler())
	mux.HandleFunc("GET /media/{token}", s.serveMedia)
	mux.HandleFunc("POST /register", s.register)
	mux.HandleFunc("GET /authorize", s.authorize)
	mux.HandleFunc("GET /pair", s.pair)
	mux.HandleFunc("GET /pair/status", s.pairStatus)
	mux.HandleFunc("GET /pair/continue", s.pair)
	mux.HandleFunc("POST /token", s.token)
	mux.HandleFunc("POST /revoke", s.revoke)
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		c := s.Configuration()
		u := c.PublicURL
		reply(w, map[string]any{"issuer": u, "authorization_endpoint": u + "/authorize", "token_endpoint": u + "/token", "registration_endpoint": u + "/register", "revocation_endpoint": u + "/revoke", "response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code", "refresh_token"}, "code_challenge_methods_supported": []string{"S256"}, "token_endpoint_auth_methods_supported": []string{"none", "client_secret_post", "client_secret_basic"}, "scopes_supported": []string{"mcp"}})
	})
	resource := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := s.Configuration()
		auth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{Resource: c.PublicURL + "/mcp", AuthorizationServers: []string{c.PublicURL}, BearerMethodsSupported: []string{"header"}, ScopesSupported: []string{"mcp"}}).ServeHTTP(w, r)
	})
	mux.Handle("/.well-known/oauth-protected-resource", resource)
	mux.Handle("/.well-known/oauth-protected-resource/mcp", resource)
	mcpHandler := s.MCP.Handler(true)
	mux.Handle("/mcp", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := s.Configuration()
		// RequireBearerToken rejects these before Verify; record only that, never the header.
		// Logging only: bearerLabel mirrors the SDK's parsing; the SDK still decides.
		if reason := bearerLabel(r.Header.Get("Authorization")); reason != "" {
			s.authFailure("mcp", "none", "invalid_token", reason)
		}
		auth.RequireBearerToken(s.Verify, &auth.RequireBearerTokenOptions{ResourceMetadataURL: cfg.PublicURL + "/.well-known/oauth-protected-resource/mcp", Scopes: []string{"mcp"}})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ti := auth.TokenInfoFromContext(r.Context())
			a, e := s.Hub.Agent(ti.UserID)
			if e != nil {
				s.authFailure("mcp", "none", "invalid_token", "agent_missing")
				fail(w, "invalid_token", 401)
				return
			}
			mcpHandler.ServeHTTP(w, mcpserver.WithAgent(r, a))
		})).ServeHTTP(w, r)
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Gate != nil {
			s.Gate.RLock()
			defer s.Gate.RUnlock()
		}
		c := s.Configuration()
		endpoint := endpointKind(r.URL.Path)
		guarded := endpoint != "other"
		if !c.Enabled {
			if guarded {
				s.authFailure(endpoint, "none", "not_found", "remote_disabled")
			}
			http.NotFound(w, r)
			return
		}
		u, _ := url.Parse(c.PublicURL)
		if r.Host != u.Host || len(r.Header.Values("Authorization")) > 1 {
			if guarded {
				reason := "host_mismatch"
				if r.Host == u.Host {
					reason = "duplicate_authorization"
				}
				s.authFailure(endpoint, "none", "forbidden", reason)
			}
			fail(w, "forbidden", 403)
			return
		}
		if origin, ok := r.Header["Origin"]; ok && !strings.HasPrefix(r.URL.Path, "/.well-known/") {
			if len(origin) != 1 || origin[0] != u.Scheme+"://"+u.Host {
				if guarded {
					s.authFailure(endpoint, "none", "forbidden", "origin_rejected")
				}
				fail(w, "forbidden", 403)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		maxBody := int64(64 << 10)
		if r.URL.Path == "/mcp" {
			maxBody = 28 << 20
		}
		if r.URL.Path == "/sync/push" {
			maxBody = 4 << 20
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		mux.ServeHTTP(w, r)
	})
}
func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, code string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}
func redirectOK(raw string) bool {
	u, e := url.Parse(raw)
	if e != nil || u.User != nil || u.Fragment != "" || u.Hostname() == "" {
		return false
	}
	return u.Scheme == "https" || u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")
}
func (s *Service) register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClientName   string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
		AuthMethod   string   `json:"token_endpoint_auth_method"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || len(req.RedirectURIs) == 0 || len(req.RedirectURIs) > 10 || len(req.ClientName) > 100 {
		fail(w, "invalid_client_metadata", 400)
		return
	}
	for _, u := range req.RedirectURIs {
		if !redirectOK(u) {
			fail(w, "invalid_redirect_uri", 400)
			return
		}
	}
	if req.AuthMethod == "" {
		req.AuthMethod = "none"
	}
	if !model.Contains([]string{"none", "client_secret_post", "client_secret_basic"}, req.AuthMethod) {
		fail(w, "invalid_client_metadata", 400)
		return
	}
	name := strings.TrimSpace(req.ClientName)
	if name == "" {
		name = "An app"
	}
	for _, c := range name {
		if c < 32 || c == 127 {
			fail(w, "invalid_client_metadata", 400)
			return
		}
	}
	c := Client{ID: dbutil.ID(), Name: name, RedirectURIs: req.RedirectURIs, AuthMethod: req.AuthMethod, ExpiresAt: s.Now().Add(time.Hour)}
	secret := ""
	if c.AuthMethod != "none" {
		secret = dbutil.ID() + dbutil.ID()
		c.SecretDigest = dbutil.Digest(secret)
	}
	s.mu.Lock()
	s.expireLocked()
	if len(s.unapproved) >= 64 {
		s.mu.Unlock()
		fail(w, "temporarily_unavailable", 429)
		return
	}
	s.unapproved[c.ID] = c
	s.mu.Unlock()
	out := map[string]any{"client_id": c.ID, "client_name": c.Name, "redirect_uris": c.RedirectURIs, "token_endpoint_auth_method": c.AuthMethod, "grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}}
	if secret != "" {
		out["client_secret"] = secret
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	reply(w, out)
}
func (s *Service) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	for _, values := range q {
		if len(values) != 1 {
			fail(w, "invalid_request", 400)
			return
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	c, e := s.clientLocked(q.Get("client_id"))
	if e != nil || !model.Contains(c.RedirectURIs, q.Get("redirect_uri")) {
		fail(w, "invalid_request", 400)
		return
	}
	challenge := q.Get("code_challenge")
	decoded, e := base64.RawURLEncoding.DecodeString(challenge)
	if e != nil || len(decoded) != 32 || q.Get("code_challenge_method") != "S256" || q.Get("response_type") != "code" || q.Get("resource") != s.config.PublicURL+"/mcp" || (q.Get("scope") != "" && q.Get("scope") != "mcp") {
		fail(w, "invalid_request", 400)
		return
	}
	peerID := q.Get("peer_engine_id")
	syncRequest := q.Get("sync") == "true"
	if syncRequest && (peerID == "" || len(peerID) > 128) || !syncRequest && peerID != "" {
		fail(w, "invalid_request", 400)
		return
	}
	if len(s.pending) >= 8 {
		fail(w, "temporarily_unavailable", 429)
		return
	}
	code := ""
	for {
		n, e := rand.Int(rand.Reader, big.NewInt(1000000))
		if e != nil {
			fail(w, "server_error", 500)
			return
		}
		code = fmt.Sprintf("%06d", n.Int64())
		taken := false
		for _, p := range s.pending {
			if p.Code == code {
				taken = true
			}
		}
		if !taken {
			break
		}
	}
	u, _ := url.Parse(q.Get("redirect_uri"))
	now := s.Now().UTC()
	tx := dbutil.ID()
	s.pending[tx] = &Pending{Sync: syncRequest, PeerEngineID: peerID, Code: code, Name: c.Name, Kind: inferKind(c.Name), ReturnsTo: u.Hostname(), RequestedAt: now, ExpiresAt: now.Add(10 * time.Minute), State: "waiting", Tx: tx, ClientID: c.ID, RedirectURI: q.Get("redirect_uri"), Challenge: challenge, OAuthState: q.Get("state")}
	http.Redirect(w, r, s.config.PublicURL+"/pair?tx="+tx, http.StatusSeeOther)
}

var page = template.Must(template.New("pair").Parse(`<!doctype html>
<html lang="en">
<meta charset="utf-8">
<meta name="viewport" content="width=device-width">
<meta http-equiv="refresh" content="5;url={{.Continue}}">
<title>Connect Limmr</title>
<style>body{font:17px system-ui;margin:15vh auto;max-width:30rem;text-align:center;padding:1rem;color-scheme:light dark}.code{font:600 42px monospace;letter-spacing:.14em}p{color:#777}</style>
<h1>Connect {{.Name}}</h1>
<p>Open Limmr on your Mac and approve this code.</p>
<div class="code">{{.Code}}</div>
<p>Then return to {{.ReturnsTo}}.</p>
<p><a href="{{.Continue}}">I’ve approved it, continue</a></p>
<noscript><p>JavaScript is off. <a href="{{.Continue}}">Continue after approval</a>.</p></noscript>
<script nonce="{{.Nonce}}">
const poll=async()=>{
  try {
    const r=await fetch({{.StatusURL}},{cache:'no-store'});
    const a=await r.json();
    if(a.redirect){location.replace(a.redirect);return}
    if(a.state!=='waiting'){document.querySelector('h1').textContent='This request has expired';return}
  } catch(e) {}
  setTimeout(poll,1500)
};
poll();
</script>
</html>`))

func (s *Service) pair(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	var p *Pending
	if txs := r.URL.Query()["tx"]; len(txs) == 1 {
		p = s.pending[txs[0]]
	}
	if p == nil || (p.State != "waiting" && p.State != "approved") {
		http.Error(w, "This request has expired", 410)
		return
	}
	if p.State == "approved" {
		http.Redirect(w, r, p.Redirect, http.StatusSeeOther)
		return
	}
	nonce := dbutil.ID()
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'nonce-"+nonce+"'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = page.Execute(w, struct{ Name, Code, ReturnsTo, Continue, StatusURL, Nonce string }{p.Name, p.Code, p.ReturnsTo, s.config.PublicURL + "/pair/continue?tx=" + url.QueryEscape(p.Tx), s.config.PublicURL + "/pair/status?tx=" + url.QueryEscape(p.Tx), nonce})
}
func (s *Service) pairStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	var p *Pending
	if txs := r.URL.Query()["tx"]; len(txs) == 1 {
		p = s.pending[txs[0]]
	}
	if p == nil {
		reply(w, map[string]string{"state": "gone"})
		return
	}
	reply(w, map[string]string{"state": p.State, "redirect": p.Redirect, "code": p.Code})
}
func (s *Service) authenticateClientLocked(r *http.Request) (Client, error) {
	c, _, e := s.authenticateClientReason(r)
	return c, e
}

// authenticateClientReason is authenticateClientLocked plus a fixed failure reason for the auth log.
func (s *Service) authenticateClientReason(r *http.Request) (Client, string, error) {
	id, secret := r.Form.Get("client_id"), r.Form.Get("client_secret")
	basic := false
	if u, p, ok := r.BasicAuth(); ok {
		id, secret = u, p
		basic = true
	}
	c, e := s.clientLocked(id)
	if e != nil {
		return c, "client_unknown", model.ErrForbidden
	}
	switch c.AuthMethod {
	case "none":
		if basic || secret != "" {
			return c, "client_auth_mismatch", model.ErrForbidden
		}
	case "client_secret_post":
		if basic || !hub.Equal(c.SecretDigest, dbutil.Digest(secret)) {
			return c, "client_auth_mismatch", model.ErrForbidden
		}
	case "client_secret_basic":
		if !basic || !hub.Equal(c.SecretDigest, dbutil.Digest(secret)) {
			return c, "client_auth_mismatch", model.ErrForbidden
		}
	}
	return c, "", nil
}
func (s *Service) token(w http.ResponseWriter, r *http.Request) {
	// Responses are unchanged; deny only adds a redacted, fixed-vocabulary log line.
	deny := func(code string, status int, reason string) {
		s.authFailure("token", grantKind(r.Form.Get("grant_type")), code, reason)
		fail(w, code, status)
	}
	if r.ParseForm() != nil || !singleValues(r.Form) {
		deny("invalid_request", 400, "form_invalid")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	c, reason, e := s.authenticateClientReason(r)
	if e != nil {
		deny("invalid_client", 401, reason)
		return
	}
	if c.AgentID == "" {
		deny("invalid_grant", 400, "client_unapproved")
		return
	}
	a, e := s.Hub.Agent(c.AgentID)
	if e != nil || !a.Enabled {
		deny("invalid_grant", 400, "agent_inactive")
		return
	}
	if resource := r.Form.Get("resource"); resource != "" && resource != s.config.PublicURL+"/mcp" {
		deny("invalid_target", 400, "resource_mismatch")
		return
	}
	switch r.Form.Get("grant_type") {
	case "authorization_code":
		key := dbutil.Digest(r.Form.Get("code"))
		g, ok := s.grants[key]
		verifier := r.Form.Get("code_verifier")
		hash := shaChallenge(verifier)
		reason := ""
		switch {
		case !ok:
			reason = "code_unknown"
		case g.ClientID != c.ID:
			reason = "code_client_mismatch"
		case g.AgentID != c.AgentID:
			reason = "code_agent_mismatch"
		case g.RedirectURI != r.Form.Get("redirect_uri"):
			reason = "redirect_mismatch"
		case g.Resource != s.config.PublicURL+"/mcp":
			reason = "code_resource_mismatch"
		case !validVerifier(verifier):
			reason = "verifier_invalid"
		case !hub.Equal(g.Challenge, hash):
			reason = "verifier_mismatch"
		}
		if reason != "" {
			deny("invalid_grant", 400, reason)
			return
		}
		out, e := s.issueLocked(c, "")
		if e != nil {
			deny("server_error", 500, "storage")
			return
		}
		delete(s.grants, key)
		s.authIssued("authorization_code")
		reply(w, out)
	case "refresh_token":
		key := dbutil.Digest(r.Form.Get("refresh_token"))
		var old tokenRecord
		reason := ""
		switch {
		case s.Hub.Repo.Get("oauth_tokens", key, &old) != nil:
			reason = "refresh_unknown"
		case old.Kind != "refresh":
			reason = "refresh_kind"
		case old.ClientID != c.ID:
			reason = "refresh_client_mismatch"
		case old.AgentID != c.AgentID:
			reason = "refresh_agent_mismatch"
		case old.Resource != s.config.PublicURL+"/mcp":
			reason = "refresh_resource_mismatch"
		case !s.Now().Before(old.ExpiresAt):
			reason = "refresh_expired"
		case r.Form.Get("scope") != "" && r.Form.Get("scope") != "mcp":
			reason = "scope_invalid"
		}
		if reason != "" {
			deny("invalid_grant", 400, reason)
			return
		}
		out, e := s.issueLocked(c, key)
		if e != nil {
			reason := "storage"
			if errors.Is(e, model.ErrInvalid) {
				reason = "refresh_consumed"
			}
			deny("server_error", 500, reason)
			return
		}
		s.authIssued("refresh_token")
		reply(w, out)
	default:
		deny("unsupported_grant_type", 400, "grant_unsupported")
	}
}
func (s *Service) revoke(w http.ResponseWriter, r *http.Request) {
	if r.ParseForm() != nil || !singleValues(r.Form) {
		fail(w, "invalid_request", 400)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c, e := s.authenticateClientLocked(r)
	if e != nil {
		fail(w, "invalid_client", 401)
		return
	}
	var rec tokenRecord
	if s.Hub.Repo.Get("oauth_tokens", dbutil.Digest(r.Form.Get("token")), &rec) == nil && rec.ClientID == c.ID {
		if e = s.revokeLocked(c.ID); e != nil {
			fail(w, "server_error", 500)
			return
		}
	}
	reply(w, map[string]bool{"ok": true})
}
