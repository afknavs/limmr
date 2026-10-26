package dbutil

import (
	"context"
	"database/sql"
	"github.com/mattn/go-sqlite3"
	"net/url"
	"os"
)

// BackupSQLite captures a consistent source including WAL without modifying it.
func BackupSQLite(src, dst string) error {
	u := url.URL{Scheme: "file", Path: src}
	source, e := sql.Open("sqlite3", u.String()+"?mode=ro&_busy_timeout=10000")
	if e != nil {
		return e
	}
	defer source.Close()
	dest, e := Open(dst)
	if e != nil {
		return e
	}
	defer dest.Close()
	ctx := context.Background()
	sc, e := source.Conn(ctx)
	if e != nil {
		return e
	}
	defer sc.Close()
	dc, e := dest.Conn(ctx)
	if e != nil {
		return e
	}
	defer dc.Close()
	e = dc.Raw(func(d any) error {
		return sc.Raw(func(s any) error {
			backup, e := d.(*sqlite3.SQLiteConn).Backup("main", s.(*sqlite3.SQLiteConn), "main")
			if e != nil {
				return e
			}
			for {
				done, e := backup.Step(128)
				if e != nil {
					backup.Close()
					return e
				}
				if done {
					break
				}
			}
			return backup.Finish()
		})
	})
	if e != nil {
		os.Remove(dst)
	}
	return e
}
