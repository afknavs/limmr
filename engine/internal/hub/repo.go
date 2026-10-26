package hub

import (
	"database/sql"
	"encoding/json"
	"limmr/internal/dbutil"
	"limmr/internal/model"
	"limmr/internal/startup"
)

type Repo struct{ DB dbutil.Database }

// Open retains the fully migrated contract for non-server callers.
func Open(path string) (*Repo, error) {
	r, err := OpenBase(path)
	if err != nil {
		return nil, err
	}
	if err = r.Migrate(nil); err != nil {
		r.Close()
		return nil, err
	}
	return r, nil
}

// OpenBase performs only the bounded store open. No routes may use this repo
// until Migrate succeeds; the startup handler serves health independently.
func OpenBase(path string) (*Repo, error) {
	db, e := dbutil.Open(path)
	if e != nil {
		return nil, e
	}
	_, e = db.Exec(`CREATE TABLE IF NOT EXISTS objects(bucket TEXT,id TEXT,data TEXT NOT NULL,PRIMARY KEY(bucket,id)); CREATE TABLE IF NOT EXISTS credentials(digest TEXT PRIMARY KEY,agent_id TEXT NOT NULL); CREATE TABLE IF NOT EXISTS activity(id INTEGER PRIMARY KEY AUTOINCREMENT,at TIMESTAMP NOT NULL,data TEXT NOT NULL);`)
	if e != nil {
		db.Close()
		return nil, e
	}
	return &Repo{db}, nil
}

func (r *Repo) Migrate(observer startup.Observer) error {
	for _, migration := range []struct {
		name string
		run  func() error
	}{
		{"migration.authors", r.migrateAuthors},
		{"migration.circles", r.migrateCircles},
		{"migration.watches", r.migrateWatches},
		{"migration.home_v2", r.migrateHomeV2},
		{"migration.understanding", r.migrateUnderstanding},
		{"migration.revision", func() error {
			_, err := r.DB.Exec(`CREATE TABLE IF NOT EXISTS p1_revision(id INTEGER PRIMARY KEY CHECK(id=1), value INTEGER NOT NULL); INSERT OR IGNORE INTO p1_revision VALUES(1,0);CREATE TRIGGER IF NOT EXISTS p1_objects_INSERT AFTER INSERT ON objects WHEN NEW.bucket IN ('people','memberships','groups','circles','facts','relations','changes','settings','briefs','understanding_changes') BEGIN UPDATE p1_revision SET value=value+1; END;CREATE TRIGGER IF NOT EXISTS p1_objects_UPDATE AFTER UPDATE ON objects WHEN NEW.bucket IN ('people','memberships','groups','circles','facts','relations','changes','settings','briefs','understanding_changes') BEGIN UPDATE p1_revision SET value=value+1; END;CREATE TRIGGER IF NOT EXISTS p1_objects_DELETE AFTER DELETE ON objects WHEN OLD.bucket IN ('people','memberships','groups','circles','facts','relations','changes','settings','briefs','understanding_changes') BEGIN UPDATE p1_revision SET value=value+1; END;`)
			return err
		}},
		{"migration.updates", r.migrateUpdates},
		{"migration.plain_sends", r.migratePlainSends},
		{"migration.people_index", r.indexPeople},
		{"migration.draft_index", func() error {
			_, err := r.DB.Exec(`CREATE INDEX IF NOT EXISTS n1_draft_key ON objects(json_extract(data,'$.agent_id'),json_extract(data,'$.key'),json_extract(data,'$.created_at')) WHERE bucket='drafts'; CREATE INDEX IF NOT EXISTS n1_draft_message ON objects(json_extract(data,'$.chat_jid'),json_extract(data,'$.message_id')) WHERE bucket='drafts';`)
			return err
		}},
		{"migration.inference_index", func() error {
			_, err := r.DB.Exec(`CREATE TRIGGER IF NOT EXISTS p3_inference_INSERT AFTER INSERT ON objects WHEN NEW.bucket='relationship_inferences' BEGIN UPDATE p1_revision SET value=value+1; END;
 CREATE TRIGGER IF NOT EXISTS p3_inference_UPDATE AFTER UPDATE ON objects WHEN NEW.bucket='relationship_inferences' BEGIN UPDATE p1_revision SET value=value+1; END;
 CREATE TRIGGER IF NOT EXISTS p3_inference_DELETE AFTER DELETE ON objects WHEN OLD.bucket='relationship_inferences' BEGIN UPDATE p1_revision SET value=value+1; END;`)
			return err
		}},
		{"migration.structure", r.migrateStructure},
		{"migration.efforts", r.migrateEfforts},
		{"migration.tasks", r.migrateTasks},
		{"migration.task_surfacing", r.migrateTaskSurfacing},
		{"migration.sync", r.migrateSync},
	} {
		if err := startup.Measure(observer, migration.name, migration.run); err != nil {
			return err
		}
	}
	return nil
}
func (r *Repo) Close() error { return r.DB.Close() }
func (r *Repo) Put(bucket, id string, v any) error {
	if bucket == "people" {
		return r.Apply(Mutation{Bucket: bucket, ID: id, Value: v})
	}
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	id, b, e = aliasValue(r.DB, bucket, id, b)
	if e != nil {
		return e
	}
	_, e = r.DB.Exec("INSERT INTO objects VALUES(?,?,?) ON CONFLICT(bucket,id) DO UPDATE SET data=excluded.data", bucket, id, string(b))
	return e
}
func (r *Repo) Get(bucket, id string, v any) error {
	if bucket == "people" {
		var err error
		id, err = personAlias(r.DB, id)
		if err != nil {
			return err
		}
	}
	var b string
	e := r.DB.QueryRow("SELECT data FROM objects WHERE bucket=? AND id=? AND (bucket<>'relations' OR coalesce(json_extract(data,'$.status'),'')<>'removed')", bucket, id).Scan(&b)
	if e == sql.ErrNoRows {
		return model.ErrNotFound
	}
	if e != nil {
		return e
	}
	return json.Unmarshal([]byte(b), v)
}
func (r *Repo) Delete(bucket, id string) error {
	if bucket == "people" {
		var err error
		id, err = personAlias(r.DB, id)
		if err != nil {
			return err
		}
	}
	_, e := r.DB.Exec("DELETE FROM objects WHERE bucket=? AND id=?", bucket, id)
	return e
}
func List[T any](r *Repo, bucket string) ([]T, error) {
	rows, e := r.DB.Query("SELECT data FROM objects WHERE bucket=? AND (bucket<>'relations' OR coalesce(json_extract(data,'$.status'),'')<>'removed') ORDER BY id", bucket)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		var b string
		var v T
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(b), &v); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

type Mutation struct {
	Bucket, ID string
	Value      any
	Delete     bool
}

// Apply commits related object state and its reversible change record together.
func (r *Repo) Apply(xs ...Mutation) error {
	tx, e := r.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, x := range xs {
		if x.Delete {
			_, e = tx.Exec("DELETE FROM objects WHERE bucket=? AND id=?", x.Bucket, x.ID)
		} else {
			var b []byte
			b, e = json.Marshal(x.Value)
			if e == nil {
				x.ID, b, e = aliasValue(tx, x.Bucket, x.ID, b)
			}
			if e == nil {
				_, e = tx.Exec("INSERT INTO objects VALUES(?,?,?) ON CONFLICT(bucket,id) DO UPDATE SET data=excluded.data", x.Bucket, x.ID, string(b))
			}
		}
		if e != nil {
			return e
		}
	}
	return tx.Commit()
}
