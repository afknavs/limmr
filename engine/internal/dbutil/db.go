package dbutil

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

func ID() string {
	b := make([]byte, 24)
	if _, e := rand.Read(b); e != nil {
		panic("random source unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func Digest(v string) string { s := sha256.Sum256([]byte(v)); return hex.EncodeToString(s[:]) }
func Open(path string) (*sql.DB, error) {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	f.Close()
	if e = os.Chmod(path, 0600); e != nil {
		return nil, e
	}
	u := url.URL{Scheme: "file", Path: path}
	db, e := sql.Open("sqlite3", u.String()+"?_foreign_keys=on&_busy_timeout=10000&_journal_mode=WAL")
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
func JSON(v any) string {
	b, e := json.Marshal(v)
	if e != nil {
		panic(e)
	}
	return string(b)
}
func Credential(path string) (string, error) {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e == nil {
		_, e = f.WriteString(ID() + ID() + "\n")
		ce := f.Close()
		if e != nil {
			return "", e
		}
		if ce != nil {
			return "", ce
		}
	} else if !os.IsExist(e) {
		return "", e
	}
	if e = os.Chmod(path, 0600); e != nil {
		return "", e
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return "", e
	}
	if len(b) < 33 {
		return "", fmt.Errorf("credential file invalid")
	}
	value := strings.TrimSpace(string(b))
	if len(value) < 32 {
		return "", fmt.Errorf("credential file invalid")
	}
	return value, nil
}
