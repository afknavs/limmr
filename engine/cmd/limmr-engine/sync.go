package main

import (
	"context"
	"flag"
	"fmt"
	"limmr/internal/hub"
	knowledgeSync "limmr/internal/sync"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func runSync(args []string) error {
	pairing := len(args) > 0 && args[0] == "pair"
	unpairing := len(args) > 0 && args[0] == "unpair"
	if pairing || unpairing {
		args = args[1:]
	}
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	peer := fs.String("peer", "", "Mac public address, including /limmr when used")
	once := fs.Bool("once", false, "Run one exchange and exit (recommended for schedules)")
	home := fs.String("home", os.Getenv("LIMMR_HOME"), "Private engine data directory")
	if e := fs.Parse(args); e != nil {
		return e
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected sync arguments")
	}
	if *home == "" {
		return fmt.Errorf("set LIMMR_HOME to the dot's private data directory")
	}
	if *peer == "" && !unpairing {
		return fmt.Errorf("specify --peer https://<mac>/limmr")
	}
	valid := ""
	var e error
	if !unpairing {
		if valid, e = knowledgeSync.ValidatePeer(*peer); e != nil {
			return e
		}
	}
	if e = os.MkdirAll(*home, 0700); e != nil {
		return e
	}
	if e = os.Chmod(*home, 0700); e != nil {
		return e
	}
	repo, e := hub.Open(filepath.Join(*home, "hub.db"))
	if e != nil {
		return fmt.Errorf("cannot open dot knowledge store")
	}
	defer repo.Close()
	s, e := knowledgeSync.New(repo, *home, nil)
	if e != nil {
		return fmt.Errorf("cannot initialize sync")
	}
	if unpairing {
		// For a dot whose Mac is gone: it becomes standalone and runs its own
		// reminders. Unpair on the Mac too, or both will run them.
		if e = s.Unpair(); e != nil {
			return e
		}
		if e = os.Remove(filepath.Join(*home, "sync-oauth.json")); e != nil && !os.IsNotExist(e) {
			return e
		}
		fmt.Println("Unpaired. This dot now runs on its own; unpair on your Mac too.")
		return nil
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if pairing {
		if e = s.PairOutbound(ctx, valid, func(code string) {
			fmt.Println("On your Mac: Settings → Integrations → Sync with another Limmr → Pair…. Enter code", code)
		}); e != nil {
			return interrupted(ctx, e)
		}
		fmt.Println("Paired. Sync complete; this Mac runs reminders and check-ins.")
		return nil
	}
	for {
		runCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
		e = s.RunOnce(runCtx, valid)
		if e != nil {
			e = interrupted(runCtx, e)
		}
		cancel()
		if e != nil {
			return e
		}
		fmt.Println("Sync complete. Tasks, people and circles are up to date.")
		if *once {
			return nil
		}
		timer := time.NewTimer(5 * time.Minute)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

// interrupted explains a stopped exchange: cursors are saved per page, so the next run resumes.
func interrupted(ctx context.Context, e error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("sync stopped before it finished; progress is saved, run sync --once again to continue")
	}
	return e
}
