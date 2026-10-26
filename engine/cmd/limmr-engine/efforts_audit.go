package main

import (
	"database/sql"
	"encoding/json"
	"limmr/internal/efforts"
	"limmr/internal/hub"
	"limmr/internal/wa"
	"net/url"
	"os"
	"path/filepath"
)

// No credentials, logs, schema upgrades, session store or network are opened.
// Read-only SQLite connections make the command safe for an existing home.
func auditEfforts(home string) error {
	repo, e := openAuditDB(home, "hub.db")
	if e != nil {
		return e
	}
	defer repo.Close()
	messages, e := openAuditDB(home, "messages.db")
	if e != nil {
		return e
	}
	defer messages.Close()
	h := hub.New(&hub.Repo{DB: repo}, &wa.Store{DB: messages}, home)
	report, e := (&efforts.Service{Hub: h}).Audit()
	if e != nil {
		return e
	}
	return json.NewEncoder(os.Stdout).Encode(report)
}

func openAuditDB(home, name string) (*sql.DB, error) {
	u := url.URL{Scheme: "file", Path: filepath.Join(home, name)}
	db, e := sql.Open("sqlite3", u.String()+"?mode=ro&_query_only=on&_busy_timeout=5000")
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	if e = db.Ping(); e != nil {
		db.Close()
		return nil, e
	}
	return db, nil
}
