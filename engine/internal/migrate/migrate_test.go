package migrate

import (
	"context"
	"errors"
	"limmr/internal/dbutil"
	"limmr/internal/hub"
	"os"
	"path/filepath"
	"testing"
)

type runner struct {
	calls int
	fail  bool
}

func (r *runner) Run(context.Context, string, ...string) error {
	r.calls++
	if r.fail && r.calls == 4 {
		return errors.New("fixture bootstrap failure")
	}
	return nil
}
func fixture(t *testing.T) (*Service, *runner) {
	t.Helper()
	dir := t.TempDir()
	s := &Service{Home: filepath.Join(dir, "new"), Source: filepath.Join(dir, "old"), Plists: filepath.Join(dir, "plists"), UID: 501}
	r := &runner{}
	s.Runner = r
	for _, d := range []string{s.Home, filepath.Join(s.Source, "store"), s.Plists} {
		if e := os.MkdirAll(d, 0700); e != nil {
			t.Fatal(e)
		}
	}
	for _, p := range []string{filepath.Join(s.Home, "session.db"), filepath.Join(s.Home, "messages.db"), filepath.Join(s.Source, "store", "whatsapp.db"), filepath.Join(s.Source, "store", "messages.db")} {
		db, e := dbutil.Open(p)
		if e != nil {
			t.Fatal(e)
		}
		_, e = db.Exec("CREATE TABLE marker(v TEXT);INSERT INTO marker VALUES('before')")
		if e != nil {
			t.Fatal(e)
		}
		db.Close()
	}
	for _, job := range jobs {
		if e := os.WriteFile(filepath.Join(s.Plists, job+".plist"), []byte("invented"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	return s, r
}
func TestUndoRetryPreservesReturnedSessionAndJobProgress(t *testing.T) {
	s, r := fixture(t)
	ctx := context.Background()
	if _, e := s.Run(ctx, false); e != nil {
		t.Fatal(e)
	}
	db, e := dbutil.Open(filepath.Join(s.Home, "session.db"))
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec("UPDATE marker SET v='current-session'")
	if e != nil {
		t.Fatal(e)
	}
	db.Close()
	r.fail = true
	if _, e = s.Undo(ctx); e == nil {
		t.Fatal("expected bootstrap failure")
	}
	if !s.Plan().CanUndo {
		t.Fatal("interrupted undo cannot resume")
	}
	r.fail = false
	if _, e = s.Undo(ctx); e != nil {
		t.Fatal(e)
	}
	if r.calls != 5 {
		t.Fatal("already restarted job was repeated")
	}
	db, e = dbutil.Open(filepath.Join(s.Source, "store", "whatsapp.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var value string
	if e = db.QueryRow("SELECT v FROM marker").Scan(&value); e != nil {
		t.Fatal(e)
	}
	if value != "current-session" {
		t.Fatal("undo retry overwrote returned session")
	}
}
func TestDryRunNeverInvokesLifecycle(t *testing.T) {
	s, r := fixture(t)
	s.Pause = func() error { t.Fatal("dry run paused engine"); return nil }
	s.Upgrade = func() error { t.Fatal("dry run upgraded DB"); return nil }
	p, e := s.Run(context.Background(), true)
	if e != nil || !p.Available || !p.DryRun || r.calls != 0 {
		t.Fatal("dry run changed state")
	}
	if _, e = os.Stat(s.statePath()); !os.IsNotExist(e) {
		t.Fatal("dry run persisted a manifest")
	}
}

func TestHistoryImportIsAtomicAndIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.db")
	source, err := dbutil.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	_, err = source.Exec("CREATE TABLE agent_actions(id INTEGER PRIMARY KEY,at REAL,agent TEXT,tool TEXT,chat_jid TEXT,text_preview TEXT,message_id TEXT);INSERT INTO agent_actions VALUES(1,100,'fixture','send_message','fixture@g.us','Invented one','one'),(2,101,'fixture','send_file','fixture@g.us','Invented two','two')")
	if err != nil {
		t.Fatal(err)
	}
	repo, err := hub.Open(filepath.Join(dir, "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	_, err = repo.DB.Exec("CREATE TRIGGER reject_second BEFORE INSERT ON activity WHEN json_extract(new.data,'$.tool')='send_file' BEGIN SELECT RAISE(ABORT,'fixture'); END")
	if err != nil {
		t.Fatal(err)
	}
	if ImportActivity(path, repo) == nil {
		t.Fatal("expected partial import failure")
	}
	var count int
	if err = repo.DB.QueryRow("SELECT COUNT(*) FROM activity").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("partial activity import committed")
	}
	_, err = repo.DB.Exec("DROP TRIGGER reject_second")
	if err != nil {
		t.Fatal(err)
	}
	if err = ImportActivity(path, repo); err != nil {
		t.Fatal(err)
	}
	if err = ImportActivity(path, repo); err != nil {
		t.Fatal(err)
	}
	if err = repo.DB.QueryRow("SELECT COUNT(*) FROM activity").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatal("retry duplicated history")
	}
}
