package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func auditFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "offline.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE tasks(id TEXT PRIMARY KEY,data TEXT);CREATE TABLE objects(bucket TEXT,id TEXT,data TEXT);CREATE TABLE task_events(id TEXT,task_id TEXT,at TEXT,kind TEXT,text TEXT,payload TEXT);
 INSERT INTO tasks VALUES('invented','{"id":"invented","title":"Review poster","status":"active","origin":"owner","created_by":{"agent_id":"you","name":"Secret owner name"},"created_at":"2026-11-10T00:00:00Z","last_active_at":"2026-11-10T00:00:00Z","lifespan_days":14,"people":[{"id":"person","name":"Secret person name"}],"chats":[{"id":"15555550123@s.whatsapp.net","name":"Secret chat name"}]}');`)
	if err != nil {
		t.Fatal(err)
	}
	return path
}
func TestProtocolCLIReadOnlyFormatsAndPrivacy(t *testing.T) {
	path := auditFixture(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"", "text", "json"} {
		t.Run(format, func(t *testing.T) {
			args := []string{"--protocol", "--database", path, "--now", "2026-11-11T00:00:00Z", "--timezone", "UTC"}
			if format != "" {
				args = append(args, "--format", format)
			}
			var out bytes.Buffer
			if err := run(args, &out); err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{"Secret owner name", "Secret person name", "Secret chat name", "15555550123"} {
				if strings.Contains(out.String(), secret) {
					t.Fatal("leaked", secret)
				}
			}
			if format == "json" {
				var v any
				if err := json.Unmarshal(out.Bytes(), &v); err != nil {
					t.Fatal(err)
				}
			} else if !strings.Contains(out.String(), "Stays a task: 1") || !strings.Contains(out.String(), "Not checked: 2") {
				t.Fatal(out.String())
			}
		})
	}
	db, err := openOffline(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("CREATE TABLE forbidden_write(id TEXT)"); err == nil {
		t.Fatal("CLI database writable")
	}
	var out bytes.Buffer
	if err := run([]string{"--database", path}, &out); err != nil {
		t.Fatal("legacy JSON", err)
	}
	if !strings.Contains(out.String(), "would_auto_archive") {
		t.Fatal(out.String())
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("input changed")
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err = os.Stat(path + suffix); !os.IsNotExist(err) {
			t.Fatal("created sidecar", suffix, err)
		}
	}
}
func TestProtocolCLIRefusesLivePaths(t *testing.T) {
	// Lexical refusal happens before filesystem access: these need not exist.
	for _, path := range []string{"/invented/Library/Application Support/Limmr/hub.db", "/invented/whatsapp-mcp/store/messages.db", "/invented/library/application support/limmr/hub.db"} {
		if _, err := openOffline(path); err == nil || !strings.Contains(err.Error(), "forbidden") {
			t.Fatal(path, err)
		}
	}
	// A symlink into an invented forbidden-shaped directory is refused too.
	root := t.TempDir()
	dir := filepath.Join(root, "whatsapp-mcp", "store")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "invented.db")
	if err := os.WriteFile(target, []byte("invented"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "offline.db")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := openOffline(link); err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatal(err)
	}
}
func TestProtocolCLIRejectsInvalidOptions(t *testing.T) {
	for _, args := range [][]string{{"--protocol"}, {"--protocol", "--database", "missing", "--format", "xml"}, {"--database", "missing", "--format", "text"}, {"--protocol", "--database", "missing", "--now", "bad"}, {"--protocol", "--database", "missing", "--timezone", "invalid/zone"}, {"--protocol", "--apply"}, {"extra"}} {
		if err := run(args, &bytes.Buffer{}); err == nil {
			t.Fatal(args)
		}
	}
}

func TestProtocolCLIKeepPreviewReadOnly(t *testing.T) {
	path := auditFixture(t)
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("UPDATE tasks SET data=json_set(data,'$.origin','agent','$.created_by.agent_id','fixture-ai')")
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"--protocol", "--database", path, "--now", "2026-11-11T00:00:00Z", "--timezone", "UTC"}
	var without, with bytes.Buffer
	if err = run(args, &without); err != nil {
		t.Fatal(err)
	}
	if err = run(append(args, "--keep", "invented,missing,invented"), &with); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(without.String(), "Becomes a suggestion: 1") || !strings.Contains(with.String(), "Stays a task: 1") || !strings.Contains(with.String(), "Kept in your review of old tasks") {
		t.Fatal(without.String(), with.String())
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("keep preview wrote database")
	}
	for _, extra := range [][]string{{"--keep", "invented"}, {"--protocol", "--keep", ","}} {
		if run(extra, &bytes.Buffer{}) == nil {
			t.Fatal("invalid keep accepted")
		}
	}
}
