package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"limmr/internal/api"
	"limmr/internal/circles"
	"limmr/internal/dbutil"
	"limmr/internal/demo"
	"limmr/internal/health"
	"limmr/internal/hub"
	"limmr/internal/mcpserver"
	"limmr/internal/migrate"
	"limmr/internal/model"
	"limmr/internal/people"
	"limmr/internal/remote"
	"limmr/internal/startup"
	knowledgeSync "limmr/internal/sync"
	"limmr/internal/tasks"
	"limmr/internal/wa"
	"limmr/internal/watches"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "sync" {
		if e := runSync(os.Args[2:]); e != nil {
			fmt.Fprintln(os.Stderr, "Sync failed:", e)
			os.Exit(1)
		}
		return
	}
	if e := run(); e != nil {
		var networkError *net.OpError
		if errors.As(e, &networkError) {
			fmt.Fprintln(os.Stderr, "Limmr could not open its loopback listener in this environment.")
		} else {
			fmt.Fprintln(os.Stderr, "Limmr could not start. Check the private engine log and data-folder access.")
		}
		os.Exit(1)
	}
}
func run() error {
	userHome, e := os.UserHomeDir()
	if e != nil {
		return e
	}
	defaultHome := os.Getenv("LIMMR_HOME")
	if defaultHome == "" {
		defaultHome = filepath.Join(userHome, "Library", "Application Support", "Limmr")
	}
	homeFlag := flag.String("home", defaultHome, "Private data directory (or LIMMR_HOME)")
	port := flag.Int("port", 18800, "Local control and MCP loopback port")
	remotePort := flag.Int("remote-port", 18801, "Remote OAuth MCP loopback port")
	demoMode := flag.Bool("demo", false, "Serve deterministic invented data with a fake WhatsApp client")
	auditOnly := len(os.Args) > 1 && os.Args[1] == "efforts-audit"
	inferenceAudit := len(os.Args) > 1 && os.Args[1] == "inference-audit"
	planOnly := len(os.Args) > 1 && os.Args[1] == "plan-refresh"
	if planOnly || auditOnly || inferenceAudit {
		os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
	}
	flag.Parse()
	if *port < 1 || *port > 65535 || *remotePort < 1 || *remotePort > 65535 || *port == *remotePort {
		return model.ErrInvalid
	}
	home, e := filepath.Abs(*homeFlag)
	if e != nil {
		return e
	}
	if auditOnly {
		return auditEfforts(home)
	}
	if inferenceAudit {
		return auditInference(home, os.Stdout)
	}
	if e = os.MkdirAll(home, 0700); e != nil {
		return e
	}
	if e = os.Chmod(home, 0700); e != nil {
		return e
	}
	logFile, e := health.OpenLog(home)
	if e != nil {
		return e
	}
	defer logFile.Close()
	logger := log.New(logFile, "", log.LstdFlags|log.LUTC)
	logger.Print("INFO engine starting")
	control, e := dbutil.Credential(filepath.Join(home, "control-token"))
	if e != nil {
		return e
	}
	status, e := dbutil.Credential(filepath.Join(home, "status-token"))
	if e != nil {
		return e
	}
	stages := startup.New(*port, control, status, logger)
	var repo *hub.Repo
	e = stages.Step("hub.open", func() (err error) { repo, err = hub.OpenBase(filepath.Join(home, "hub.db")); return })
	if e != nil {
		return e
	}
	defer repo.Close()
	var messages *wa.Store
	e = stages.Step("messages.open", func() (err error) { messages, err = wa.OpenBaseStore(filepath.Join(home, "messages.db")); return })
	if e != nil {
		return e
	}
	defer func() { _ = messages.Close() }()
	// The listener can answer authenticated readiness before any data backfill.
	// plan-refresh is a command, not a service, and must never bind a port.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var httpServer *http.Server
	errCh := make(chan error, 1)
	if !planOnly {
		var ln net.Listener
		e = stages.Step("listener", func() (err error) { ln, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port)); return })
		if e != nil {
			return e
		}
		httpServer = &http.Server{Handler: stages, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second}
		defer httpServer.Close()
		go func() { errCh <- httpServer.Serve(ln) }()
		logger.Print("INFO local listener ready")
	}
	// All migrations/warmups run behind the startup handler, on the initializing
	// goroutine. No application or MCP request can observe a partially migrated store.
	if e = repo.Migrate(stages.Observe); e != nil {
		return e
	}
	if e = messages.UpgradeTimed(stages.Observe); e != nil {
		return e
	}
	var h *hub.Service
	if e = stages.Step("warmup.hub", func() error { h = hub.New(repo, messages, home); return nil }); e != nil {
		return e
	}
	syncService, e := knowledgeSync.New(repo, home, h.Now)
	if e != nil {
		return e
	}
	h.MayRunAutomations = syncService.MayRunAutomations
	p := &people.Service{Hub: h}
	avatars := &people.AvatarCache{Hub: h}
	fake := &wa.Fake{Messages: messages}
	link := wa.NewLink(fake)
	h.Messenger = fake
	mc := &mcpserver.Service{Hub: h, People: p, Logger: logger}
	if planOnly {
		if *demoMode {
			if e = demo.Seed(h, p, link); e != nil {
				return e
			}
		}
		plan, e := (&circles.Service{Hub: h, People: p}).Plan()
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(plan)
	}
	var live *wa.Live
	if *demoMode {
		if e = demo.Seed(h, p, link); e != nil {
			return e
		}
	}
	wo := watches.Options{MCPURL: fmt.Sprintf("http://127.0.0.1:%d/mcp", *port)}
	if *demoMode {
		wo.Runner = demo.WatchRunner{}
		wo.Clock = demo.WatchClock{}
	}
	var watchesService *watches.Service
	if e = stages.Step("warmup.watches", func() (err error) { watchesService, err = watches.New(h, p, wo); return }); e != nil {
		return e
	}
	watchesService.Engine.MayRunAutomations = syncService.MayRunAutomations
	defer watchesService.Engine.Close()
	mc.Watches = watchesService
	taskService := mc.TaskService()
	if *demoMode {
		if e = demo.SeedTasks(taskService); e != nil {
			return e
		}
	} else {
		taskService.Accessory = &tasks.Accessory{State: h.Repo, URL: "http://127.0.0.1:18770", TokenPath: filepath.Join(userHome, ".accessory", "token")}
	}
	enqueue := func(event wa.StoredEvent) { watchesService.Enqueue(event); taskService.Enqueue(event) }

	if *demoMode {
		if e = demo.SeedWatches(watchesService); e != nil {
			return e
		}
	} else {
		e = stages.Step("warmup.whatsapp", func() (err error) { live, err = wa.NewLive(home, messages, p); return })
		if e != nil {
			return e
		}
		defer func() {
			if live != nil {
				_ = live.Close()
			}
		}()
		live.OnStored = enqueue
		h.Messenger = live
		link.Source = live
		mc.Downloader = live
		avatars.Lookup = live
	}
	gate := &sync.RWMutex{}
	rem := remote.New(h, mc, *remotePort)
	rem.Knowledge = syncService
	rem.Gate = gate
	rem.Logger = logger
	defer rem.Close()
	hs := &health.Service{Hub: h, Link: link, Home: home, LocalPort: *port, Demo: *demoMode, ProbePorts: !*demoMode, RestartRemote: rem.Start, WatchesCheck: watchesService.Check}
	hs.TasksCheck = func() model.Check {
		if taskService.Accessory != nil {
			return taskService.Accessory.Check(h.Settings().AccessoryCue.Enabled)
		}
		return model.Check{ID: "accessory_cue", Title: "Accessory cue", State: "ok", Detail: "Off"}
	}
	if live != nil {
		hs.Connection = live
	}
	hs.RemoteCheck = func(checkCtx context.Context) []model.Check {
		st := rem.Status()
		c := model.Check{ID: "remote", Title: "Remote access", State: "ok", Detail: "Off"}
		if rem.Configuration().Enabled {
			if !st.Enabled {
				c.State = "fail"
				c.Detail = "Remote listener is unavailable"
				c.FixID = "restart_remote"
			} else {
				c.Detail = "Listener ready"
				if !*demoMode && !health.HTTPDiscoveryCheck(checkCtx, st.PublicURL) {
					c.State = "warn"
					c.Detail = "Public discovery is not reachable yet."
					c.FixID = "restart_remote"
				}
			}
		}
		return []model.Check{c}
	}
	migration := &migrate.Service{Home: home, Source: filepath.Join(userHome, "whatsapp-mcp", "whatsapp-bridge"), Plists: filepath.Join(userHome, "Library", "LaunchAgents"), Runner: migrate.Launchctl{}, UID: os.Getuid()}
	if *demoMode {
		migration.Source = filepath.Join(home, "fake-old-bridge")
		migration.Plists = filepath.Join(home, "fake-launchagents")
		migration.Runner = nil
	}
	migration.Pause = func() error {
		link.Cancel()
		if live != nil {
			if err := live.Close(); err != nil {
				return err
			}
			live = nil
		}
		return messages.Close()
	}
	migration.Upgrade = func() error {
		m, e := wa.OpenStore(filepath.Join(home, "messages.db"))
		if e != nil {
			return e
		}
		return m.Close()
	}
	migration.ImportActivity = func(path string) error {
		return migrate.ImportActivity(path, h.Repo)
	}
	migration.Resume = func() error {
		m, e := wa.OpenStore(filepath.Join(home, "messages.db"))
		if e != nil {
			return e
		}
		messages.DB = m.DB
		if !*demoMode {
			live, e = wa.NewLive(home, messages, p)
			if e != nil {
				return e
			}
			live.OnStored = enqueue
			link.Source = live
			h.Messenger = live
			mc.Downloader = live
			avatars.Lookup = live
			hs.Connection = live
			return live.Resume(link.Emit)
		}
		return nil
	}
	mc.Link = link
	server := &api.Server{Knowledge: syncService, Avatars: avatars, Hub: h, People: p, MCP: mc, Link: link, Health: hs, Remote: rem, Migration: migration, Port: *port, ControlToken: control, StatusToken: status, Context: ctx, Gate: gate}
	// Stop serving before initialization-owned services close on every exit path.
	defer httpServer.Close()
	if e = stages.Step("warmup.people", func() error { _, err := p.People(nil, ""); return err }); e != nil {
		return e
	}
	if e = stages.Step("warmup.circles", func() error { _, err := (&circles.Service{Hub: h, People: p}).List(nil); return err }); e != nil {
		return e
	}
	if e = stages.Step("remote.start", rem.Start); e != nil {
		logger.Print("WARN remote listener unavailable")
	}
	stages.Ready(server.Handler())
	logger.Print("INFO engine ready")
	taskDone := make(chan struct{})
	go func() {
		defer close(taskDone)
		if err := taskService.Run(ctx); err != nil && ctx.Err() == nil {
			logger.Print("WARN tasks runtime unavailable")
		}
	}()
	defer func() { stop(); <-taskDone }()
	watchDone := make(chan struct{})
	go func() { defer close(watchDone); _ = watchesService.Run(ctx) }()
	defer func() { stop(); <-watchDone }()

	if live != nil && live.Linked() {
		go func() {
			if live.Resume(link.Emit) != nil {
				logger.Print("WARN WhatsApp reconnect unavailable")
			}
		}()
	}
	effortDone := make(chan struct{})
	go func() {
		defer close(effortDone)
		mc.EffortService().RunWarmup(ctx, func() bool {
			state := link.State().State
			return state == "syncing"
		}, gate)
	}()
	defer func() { stop(); <-effortDone }()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	lastStats := time.Time{}
	for {
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = httpServer.Shutdown(shutdown)
			logger.Print("INFO engine stopped")
			return nil
		case err := <-errCh:
			if err == http.ErrServerClosed {
				return nil
			}
			return err
		case <-ticker.C:
			gate.RLock()
			_ = h.Expire()
			_ = h.Retain()
			h.PruneOutbox()
			_ = health.TrimLog(filepath.Join(home, "logs", "engine.log"), 20<<20, 5<<20)
			if h.Now().Sub(lastStats) >= 24*time.Hour {
				_ = p.CacheStats()
				lastStats = h.Now()
			}
			gate.RUnlock()
		}
	}
}
