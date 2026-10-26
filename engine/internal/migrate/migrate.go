// Package migrate only runs on an explicit local control request. Build tests
// use fake stores and a fake launchctl runner; no live account is ever inspected.
package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"limmr/internal/dbutil"
	"limmr/internal/model"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

type Runner interface {
	Run(context.Context, string, ...string) error
}
type Launchctl struct{}

func (Launchctl) Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if cmd.Run() != nil {
		return fmt.Errorf("service operation failed")
	}
	return nil
}

type Plan struct {
	Available       bool     `json:"available"`
	DryRun          bool     `json:"dry_run"`
	Steps           []string `json:"steps"`
	CanUndo         bool     `json:"can_undo"`
	RestartRequired bool     `json:"restart_required"`
}
type manifest struct {
	State   string          `json:"state"`
	Backups map[string]bool `json:"backups"`
	Jobs    []string        `json:"jobs"`
	At      time.Time       `json:"at"`
}
type Service struct {
	Home, Source, Plists string
	Runner               Runner
	UID                  int
	Pause                func() error
	Resume               func() error
	Upgrade              func() error
	ImportActivity       func(string) error
	mu                   sync.Mutex
}

var jobs = []string{"dev.limmr.whatsapp-bridge", "dev.limmr.whatsapp-mcp"}

func (s *Service) statePath() string { return filepath.Join(s.Home, "migration", "manifest.json") }
func (s *Service) readState() (manifest, error) {
	var m manifest
	b, e := os.ReadFile(s.statePath())
	if e != nil {
		return m, e
	}
	e = unmarshal(b, &m)
	return m, e
}
func (s *Service) save(m manifest) error {
	dir := filepath.Dir(s.statePath())
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	tmp := s.statePath() + ".tmp"
	if e := os.WriteFile(tmp, []byte(dbutil.JSON(m)), 0600); e != nil {
		return e
	}
	return os.Rename(tmp, s.statePath())
}
func exists(p string) bool { v, e := os.Stat(p); return e == nil && v.Mode().IsRegular() }
func (s *Service) Plan() Plan {
	available := exists(filepath.Join(s.Source, "store", "whatsapp.db")) && exists(filepath.Join(s.Source, "store", "messages.db"))
	m, e := s.readState()
	return Plan{Available: available, DryRun: true, Steps: []string{"Stop the two old services.", "Disable their launch plists without deleting them.", "Back up Limmr stores and copy a consistent snapshot of the old stores.", "Upgrade message search and import historical agent activity.", "Connect Limmr using the migrated session."}, CanUndo: e == nil && (m.State == "complete" || m.State == "recovery_required" || m.State == "preparing" || m.State == "undoing")}
}
func (s *Service) Run(ctx context.Context, dry bool) (Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.Plan()
	if dry {
		return p, nil
	}
	if !p.Available || p.CanUndo {
		return p, model.ErrConflict
	}
	if m, e := s.readState(); e == nil && m.State != "undone" {
		return p, model.ErrConflict
	}
	if s.Runner == nil {
		return p, model.ErrConflict
	}
	for _, job := range jobs {
		plist := filepath.Join(s.Plists, job+".plist")
		if !exists(plist) || exists(plist+".disabled") {
			return p, model.ErrConflict
		}
	}
	if s.Pause != nil {
		if e := s.Pause(); e != nil {
			return p, e
		}
	}
	resume := func() {
		if s.Resume != nil {
			_ = s.Resume()
		}
	}
	m := manifest{State: "preparing", Backups: map[string]bool{}, Jobs: []string{}, At: time.Now().UTC()}
	backupDir := filepath.Join(s.Home, "migration")
	if e := os.MkdirAll(backupDir, 0700); e != nil {
		resume()
		return p, e
	}
	for _, name := range []string{"session.db", "messages.db"} {
		path := filepath.Join(s.Home, name)
		m.Backups[name] = exists(path)
		if m.Backups[name] {
			if e := dbutil.BackupSQLite(path, filepath.Join(backupDir, name+".before")); e != nil {
				resume()
				return p, e
			}
		}
	}
	if e := s.save(m); e != nil {
		resume()
		return p, e
	}
	rollback := func(cause error) (Plan, error) {
		recoveryCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := s.restoreTargets(m); err != nil {
			m.State = "recovery_required"
			_ = s.save(m)
			return p, fmt.Errorf("migration recovery required; preserve the backups")
		}
		if err := s.restoreJobs(recoveryCtx, &m); err != nil {
			m.State = "recovery_required"
			_ = s.save(m)
			resume()
			return p, fmt.Errorf("old services could not be restored; preserve the backups")
		}
		m.State = "undone"
		if err := s.save(m); err != nil {
			resume()
			return p, err
		}
		resume()
		return p, cause
	}
	for _, job := range jobs {
		plist := filepath.Join(s.Plists, job+".plist")
		if !exists(plist) {
			return rollback(fmt.Errorf("old service plist unavailable"))
		}
		target := "gui/" + strconv.Itoa(s.UID) + "/" + job
		if e := s.Runner.Run(ctx, "launchctl", "bootout", target); e != nil {
			return rollback(e)
		}
		m.Jobs = append(m.Jobs, job)
		if e := s.save(m); e != nil {
			return rollback(e)
		}
		if e := os.Rename(plist, plist+".disabled"); e != nil {
			return rollback(e)
		}
	}
	for _, pair := range [][2]string{{"whatsapp.db", "session.db"}, {"messages.db", "messages.db"}} {
		dst := filepath.Join(s.Home, pair[1])
		if e := replaceDB(filepath.Join(s.Source, "store", pair[0]), dst); e != nil {
			return rollback(e)
		}
	}
	if s.Upgrade != nil {
		if e := s.Upgrade(); e != nil {
			return rollback(e)
		}
	}
	if s.ImportActivity != nil {
		path := filepath.Join(s.Source, "store", "agent_activity.db")
		if exists(path) {
			if e := s.ImportActivity(path); e != nil {
				return rollback(e)
			}
		}
	}
	m.State = "complete"
	if e := s.save(m); e != nil {
		return rollback(e)
	}
	if s.Resume != nil {
		if e := s.Resume(); e != nil {
			return p, e
		}
	}
	p.DryRun = false
	p.CanUndo = true
	return p, nil
}
func replaceDB(src, dst string) error {
	temp := dst + ".migration"
	_ = os.Remove(temp)
	if e := dbutil.BackupSQLite(src, temp); e != nil {
		return e
	}
	for _, ext := range []string{"-wal", "-shm"} {
		if e := os.Remove(dst + ext); e != nil && !os.IsNotExist(e) {
			return e
		}
	}
	return os.Rename(temp, dst)
}
func (s *Service) restoreTargets(m manifest) error {
	for _, name := range []string{"session.db", "messages.db"} {
		dst := filepath.Join(s.Home, name)
		if m.Backups[name] {
			if e := replaceDB(filepath.Join(s.Home, "migration", name+".before"), dst); e != nil {
				return e
			}
		} else {
			for _, ext := range []string{"", "-wal", "-shm"} {
				if e := os.Remove(dst + ext); e != nil && !os.IsNotExist(e) {
					return e
				}
			}
		}
	}
	return nil
}
func (s *Service) restoreJobs(ctx context.Context, m *manifest) error {
	for len(m.Jobs) > 0 {
		job := m.Jobs[0]
		plist := filepath.Join(s.Plists, job+".plist")
		if exists(plist + ".disabled") {
			if exists(plist) {
				return model.ErrConflict
			}
			if e := os.Rename(plist+".disabled", plist); e != nil {
				return e
			}
		}
		if e := s.Runner.Run(ctx, "launchctl", "bootstrap", "gui/"+strconv.Itoa(s.UID), plist); e != nil {
			return e
		}
		m.Jobs = m.Jobs[1:]
		if e := s.save(*m); e != nil {
			return e
		}
	}
	return nil
}
func (s *Service) Undo(ctx context.Context) (Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.Plan()
	m, e := s.readState()
	if e != nil || (m.State != "complete" && m.State != "recovery_required" && m.State != "preparing" && m.State != "undoing") || s.Runner == nil {
		return p, model.ErrConflict
	}
	if s.Pause != nil {
		if e = s.Pause(); e != nil {
			return p, e
		}
	}
	if m.State == "complete" {
		if e = replaceDB(filepath.Join(s.Home, "session.db"), filepath.Join(s.Source, "store", "whatsapp.db")); e != nil {
			return p, e
		}
		// A retry must never copy the restored pre-migration session over the
		// current session already returned to the old bridge.
		m.State = "undoing"
		if e = s.save(m); e != nil {
			return p, e
		}
	}
	if e = s.restoreTargets(m); e != nil {
		return p, e
	}
	if e = s.restoreJobs(ctx, &m); e != nil {
		return p, e
	}
	m.State = "undone"
	if e = s.save(m); e != nil {
		return p, e
	}
	if s.Resume != nil {
		if e = s.Resume(); e != nil {
			return p, e
		}
	}
	p.CanUndo = false
	p.DryRun = false
	return p, nil
}

// Legacy activity is read using a read-only snapshot supplied by the caller.
func ReadActivity(db *sql.DB, consume func(at time.Time, agent, tool, chat, text, messageID string) error) error {
	rows, e := db.Query("SELECT at,COALESCE(agent,''),COALESCE(tool,''),COALESCE(chat_jid,''),COALESCE(text_preview,''),COALESCE(message_id,'') FROM agent_actions ORDER BY id")
	if e != nil {
		return fmt.Errorf("legacy activity schema unavailable")
	}
	defer rows.Close()
	for rows.Next() {
		var at float64
		var agent, tool, chat, text, id string
		if e = rows.Scan(&at, &agent, &tool, &chat, &text, &id); e != nil {
			return e
		}
		if e = consume(time.Unix(int64(at), 0).UTC(), agent, tool, chat, text, id); e != nil {
			return e
		}
	}
	return rows.Err()
}
