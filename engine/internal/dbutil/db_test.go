package dbutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCredentialPersistencePermissionsAndInvalidFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credential")
	a, e := Credential(path)
	if e != nil {
		t.Fatal(e)
	}
	b, e := Credential(path)
	if e != nil || a != b || len(a) < 32 {
		t.Fatal("credential not stable")
	}
	st, e := os.Stat(path)
	if e != nil || st.Mode().Perm() != 0600 {
		t.Fatal("credential permissions")
	}
	if e = os.WriteFile(path, []byte(strings.Repeat(" ", 40)), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = Credential(path); e == nil {
		t.Fatal("blank credential accepted")
	}
}
func TestBackupIncludesUncheckpointedWAL(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source.db")
	db, e := Open(src)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec("PRAGMA wal_autocheckpoint=0;CREATE TABLE fixture(value TEXT);INSERT INTO fixture VALUES('invented')"); e != nil {
		t.Fatal(e)
	}
	dst := filepath.Join(dir, "copy.db")
	if e = BackupSQLite(src, dst); e != nil {
		t.Fatal(e)
	}
	copy, e := Open(dst)
	if e != nil {
		t.Fatal(e)
	}
	defer copy.Close()
	var v string
	if e = copy.QueryRow("SELECT value FROM fixture").Scan(&v); e != nil || v != "invented" {
		t.Fatal("snapshot missed WAL")
	}
}
