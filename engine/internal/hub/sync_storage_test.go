package hub

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSyncStorageResumesAndShrinksLargeLegacyStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	r, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// Installed shape, invented values: 600k versions, ~600 MB, 600 live keys.
	_, err = r.DB.Exec(`DELETE FROM sync_log; WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<600000) INSERT INTO sync_log(bucket,id,hlc,origin,op,data) SELECT 'people','synthetic-'||(x%600),x,'fixture','upsert',printf('%0900d',x) FROM n; PRAGMA wal_checkpoint(TRUNCATE)`)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(path)
	if _, err = r.compactSyncPage(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = r.PrepareSyncStorage(ctx)
	var cursor int64
	if err = r.DB.QueryRow(`SELECT cursor FROM sync_storage`).Scan(&cursor); err != nil || cursor == 0 {
		t.Fatalf("no saved progress: %d %v", cursor, err)
	}
	r.Close()
	r, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	var resumed int64
	_ = r.DB.QueryRow(`SELECT cursor FROM sync_storage`).Scan(&resumed)
	if resumed != cursor {
		t.Fatal("lost progress on restart")
	}
	start := time.Now()
	// The service completes the saved pass after reopening. No yield in this
	// regression, to keep a 600 MB fixture practical on shared test machines.
	ctx, cancel = context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err = r.compactSync(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if err = r.reclaimSyncStorage(ctx); err != nil {
		t.Fatal(err)
	}
	var count, mode int
	var integrity string
	_ = r.DB.QueryRow(`SELECT count(*) FROM sync_log`).Scan(&count)
	_ = r.DB.QueryRow(`PRAGMA auto_vacuum`).Scan(&mode)
	_ = r.DB.QueryRow(`PRAGMA integrity_check`).Scan(&integrity)
	after, _ := os.Stat(path)
	if count != 600 || mode != 2 || integrity != "ok" || after.Size() > before.Size()/10 {
		t.Fatalf("count=%d mode=%d integrity=%s size=%d/%d", count, mode, integrity, after.Size(), before.Size())
	}
	t.Logf("synthetic storage: %d -> %d bytes; completed after reopen in %s; saved cursor %d", before.Size(), after.Size(), time.Since(start), cursor)
}
