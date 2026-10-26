package integration_test

import (
	"context"
	"limmr/internal/dbutil"
	"limmr/internal/migrate"
	"limmr/internal/model"
	"limmr/internal/wa"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	calls  []string
	failAt int
}

func (r *fakeRunner) Run(ctx context.Context, name string, args ...string) error {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	if r.failAt > 0 && len(r.calls) == r.failAt {
		return model.ErrConflict
	}
	return nil
}
func makeDB(t *testing.T, path, value string) {
	t.Helper()
	db, e := dbutil.Open(path)
	must(t, e)
	_, e = db.Exec("CREATE TABLE marker(value TEXT);INSERT INTO marker VALUES(?)", value)
	must(t, e)
	must(t, db.Close())
}
func marker(t *testing.T, path string) string {
	t.Helper()
	db, e := dbutil.Open(path)
	must(t, e)
	defer db.Close()
	var value string
	must(t, db.QueryRow("SELECT value FROM marker").Scan(&value))
	return value
}
func migrationRig(t *testing.T) (*migrate.Service, *fakeRunner) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("LIMMR_HOME", filepath.Join(root, "new"))
	home, source, plists := filepath.Join(root, "new"), filepath.Join(root, "old"), filepath.Join(root, "agents")
	must(t, os.MkdirAll(home, 0700))
	must(t, os.MkdirAll(filepath.Join(source, "store"), 0700))
	must(t, os.MkdirAll(plists, 0700))
	makeDB(t, filepath.Join(home, "session.db"), "new-before")
	makeDB(t, filepath.Join(source, "store", "whatsapp.db"), "old-session")
	makeDB(t, filepath.Join(home, "messages.db"), "new-messages")
	makeDB(t, filepath.Join(source, "store", "messages.db"), "old-messages")
	for _, job := range []string{"dev.limmr.whatsapp-bridge", "dev.limmr.whatsapp-mcp"} {
		must(t, os.WriteFile(filepath.Join(plists, job+".plist"), []byte("invented-plist"), 0600))
	}
	runner := &fakeRunner{}
	s := &migrate.Service{Home: home, Source: source, Plists: plists, UID: 501, Runner: runner}
	s.Upgrade = func() error {
		store, e := wa.OpenStore(filepath.Join(home, "messages.db"))
		if e != nil {
			return e
		}
		return store.Close()
	}
	return s, runner
}
func TestMigrationDryRunRealAndUndo(t *testing.T) {
	s, runner := migrationRig(t)
	before, e := os.ReadFile(filepath.Join(s.Source, "store", "whatsapp.db"))
	must(t, e)
	plan, e := s.Run(context.Background(), true)
	must(t, e)
	if !plan.Available || !plan.DryRun || len(runner.calls) != 0 {
		t.Fatal("dry run mutated services")
	}
	after, e := os.ReadFile(filepath.Join(s.Source, "store", "whatsapp.db"))
	must(t, e)
	if dbutil.Digest(string(before)) != dbutil.Digest(string(after)) {
		t.Fatal("dry run touched source session")
	}
	if marker(t, filepath.Join(s.Home, "session.db")) != "new-before" {
		t.Fatal("dry run copied source")
	}
	paused, resumed := 0, 0
	s.Pause = func() error { paused++; return nil }
	s.Resume = func() error { resumed++; return nil }
	plan, e = s.Run(context.Background(), false)
	must(t, e)
	if !plan.CanUndo || plan.DryRun || paused != 1 || resumed != 1 || len(runner.calls) != 2 {
		t.Fatal("real run lifecycle")
	}
	if marker(t, filepath.Join(s.Home, "session.db")) != "old-session" || marker(t, filepath.Join(s.Home, "messages.db")) != "old-messages" {
		t.Fatal("migration copies wrong")
	}
	for _, job := range []string{"dev.limmr.whatsapp-bridge", "dev.limmr.whatsapp-mcp"} {
		if _, e := os.Stat(filepath.Join(s.Plists, job+".plist.disabled")); e != nil {
			t.Fatal("plist not disabled")
		}
	}
	db, e := dbutil.Open(filepath.Join(s.Home, "session.db"))
	must(t, e)
	_, e = db.Exec("UPDATE marker SET value='migrated-current'")
	must(t, e)
	must(t, db.Close())
	plan, e = s.Undo(context.Background())
	must(t, e)
	if plan.CanUndo || len(runner.calls) != 4 || paused != 2 || resumed != 2 {
		t.Fatal("undo lifecycle")
	}
	if marker(t, filepath.Join(s.Source, "store", "whatsapp.db")) != "migrated-current" {
		t.Fatal("current session not copied back")
	}
	if marker(t, filepath.Join(s.Home, "session.db")) != "new-before" || marker(t, filepath.Join(s.Home, "messages.db")) != "new-messages" {
		t.Fatal("backup not restored")
	}
	if _, e = s.Undo(context.Background()); e != model.ErrConflict {
		t.Fatal("undo replay")
	}
}
func TestMigrationFailureRollsBack(t *testing.T) {
	s, runner := migrationRig(t)
	runner.failAt = 2
	_, e := s.Run(context.Background(), false)
	if e == nil {
		t.Fatal("expected fake launchctl failure")
	}
	if marker(t, filepath.Join(s.Home, "session.db")) != "new-before" {
		t.Fatal("rollback lost target")
	}
	for _, job := range []string{"dev.limmr.whatsapp-bridge", "dev.limmr.whatsapp-mcp"} {
		if _, e = os.Stat(filepath.Join(s.Plists, job+".plist")); e != nil {
			t.Fatal("rollback did not restore plist")
		}
	}
	if !strings.Contains(runner.calls[len(runner.calls)-1], "bootstrap") {
		t.Fatal("rollback did not restart stopped job")
	}
}
func TestMigrationRequiresExplicitDryRunAndConfirmation(t *testing.T) {
	r := newRig(t)
	s, _ := migrationRig(t)
	r.api.Migration = s
	status(t, r.request(t, "POST", "/v1/migrate/whatsapp-mcp", r.api.ControlToken, map[string]any{}, nil), 400)
	status(t, r.request(t, "POST", "/v1/migrate/whatsapp-mcp", r.token, map[string]any{"dry_run": false}, nil), 403)
	status(t, r.request(t, "POST", "/v1/migrate/whatsapp-mcp", r.api.ControlToken, map[string]any{"dry_run": true}, nil), 200)
}
func TestSQLiteBackupIncludesWAL(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "source.db"), filepath.Join(dir, "copy.db")
	db, e := dbutil.Open(src)
	must(t, e)
	defer db.Close()
	_, e = db.Exec("CREATE TABLE values_table(value TEXT);INSERT INTO values_table VALUES('in WAL')")
	must(t, e)
	must(t, dbutil.BackupSQLite(src, dst))
	copy, e := dbutil.Open(dst)
	must(t, e)
	defer copy.Close()
	var value string
	must(t, copy.QueryRow("SELECT value FROM values_table").Scan(&value))
	if value != "in WAL" {
		t.Fatal("WAL rows missing from copy")
	}
}
func TestLegacySchemaAndActivityImport(t *testing.T) {
	dir := t.TempDir()
	db, e := dbutil.Open(filepath.Join(dir, "messages.db"))
	must(t, e)
	_, e = db.Exec(`CREATE TABLE chats(jid TEXT PRIMARY KEY,name TEXT,last_message_time TIMESTAMP);CREATE TABLE messages(id TEXT,chat_jid TEXT,sender TEXT,content TEXT,timestamp TIMESTAMP,is_from_me BOOLEAN,media_type TEXT,filename TEXT,url TEXT,media_key BLOB,file_sha256 BLOB,file_enc_sha256 BLOB,file_length INTEGER,PRIMARY KEY(id,chat_jid));INSERT INTO chats VALUES('fixture@g.us','Fixture',NULL);INSERT INTO messages(id,chat_jid,content) VALUES('legacy','fixture@g.us','legacy searchable');`)
	must(t, e)
	must(t, db.Close())
	store, e := wa.OpenStore(filepath.Join(dir, "messages.db"))
	must(t, e)
	defer store.Close()
	var n int
	must(t, store.DB.QueryRow("SELECT COUNT(*) FROM messages_fts WHERE messages_fts MATCH 'searchable'").Scan(&n))
	if n != 1 {
		t.Fatal("legacy search missing")
	}
	activity, e := dbutil.Open(filepath.Join(dir, "activity.db"))
	must(t, e)
	defer activity.Close()
	_, e = activity.Exec("CREATE TABLE agent_actions(id INTEGER PRIMARY KEY,at REAL,agent TEXT,tool TEXT,chat_jid TEXT,chat_name TEXT,text_preview TEXT,message_id TEXT);INSERT INTO agent_actions VALUES(1,100,'fixture-ai','send_message','fixture@g.us','Fixture','invented sent text','legacy')")
	must(t, e)
	count := 0
	must(t, migrate.ReadActivity(activity, func(at time.Time, agent, tool, chat, text, id string) error {
		count++
		if agent != "fixture-ai" || text != "invented sent text" || id != "legacy" {
			t.Fatal("activity mapping")
		}
		return nil
	}))
	if count != 1 {
		t.Fatal("activity not imported")
	}
}
