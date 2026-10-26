// Storage schema and monotonic upserts ported from whatsapp-mcp; see engine/NOTICE.
package wa

import (
	"database/sql"
	"fmt"
	"limmr/internal/dbutil"
	"limmr/internal/model"
	"limmr/internal/startup"
	"strings"
	"time"
)

type Store struct {
	DB dbutil.Database
	tx *sql.Tx
}

func OpenStore(path string) (*Store, error) {
	s, err := OpenBaseStore(path)
	if err != nil {
		return nil, err
	}
	if err = s.Upgrade(); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func OpenBaseStore(path string) (*Store, error) {
	db, e := dbutil.Open(path)
	if e != nil {
		return nil, e
	}
	return &Store{DB: db}, nil
}
func (s *Store) Close() error   { return s.DB.Close() }
func (s *Store) Upgrade() error { return s.UpgradeTimed(nil) }
func (s *Store) UpgradeTimed(observer startup.Observer) error {
	return startup.Measure(observer, "migration.messages", func() error { return s.upgrade(observer) })
}
func (s *Store) upgrade(observer startup.Observer) error {
	_, e := s.DB.Exec(`CREATE TABLE IF NOT EXISTS engine_meta(key TEXT PRIMARY KEY,value TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS lid_aliases(lid TEXT PRIMARY KEY,pn TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS chats(jid TEXT PRIMARY KEY,name TEXT,last_message_time TIMESTAMP,ephemeral_expiration INTEGER NOT NULL DEFAULT 0,ephemeral_setting_timestamp INTEGER NOT NULL DEFAULT 0);
 CREATE TABLE IF NOT EXISTS messages(id TEXT,chat_jid TEXT,sender TEXT,content TEXT,timestamp TIMESTAMP,is_from_me BOOLEAN,media_type TEXT,filename TEXT,url TEXT,media_key BLOB,file_sha256 BLOB,file_enc_sha256 BLOB,file_length INTEGER,deleted_at TIMESTAMP,quoted_message_id TEXT,direct_path TEXT,PRIMARY KEY(id,chat_jid),FOREIGN KEY(chat_jid) REFERENCES chats(jid));`)
	if e != nil {
		return e
	}
	for _, c := range []struct{ table, col, spec string }{{"chats", "unread_count", "INTEGER NOT NULL DEFAULT 0"}, {"chats", "ephemeral_expiration", "INTEGER NOT NULL DEFAULT 0"}, {"chats", "ephemeral_setting_timestamp", "INTEGER NOT NULL DEFAULT 0"}, {"messages", "deleted_at", "TIMESTAMP"}, {"messages", "quoted_message_id", "TEXT"}, {"messages", "direct_path", "TEXT"}} {
		rows, err := s.DB.Query("PRAGMA table_info(" + c.table + ")")
		if err != nil {
			return err
		}
		found := false
		for rows.Next() {
			var id, nn, pk int
			var name, typ string
			var def any
			if err = rows.Scan(&id, &name, &typ, &nn, &def, &pk); err != nil {
				rows.Close()
				return err
			}
			if name == c.col {
				found = true
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if !found {
			if _, err = s.DB.Exec("ALTER TABLE " + c.table + " ADD COLUMN " + c.col + " " + c.spec); err != nil {
				return err
			}
		}
	}
	_, e = s.DB.Exec(`CREATE INDEX IF NOT EXISTS idx_messages_sender ON messages(sender);
 CREATE INDEX IF NOT EXISTS idx_messages_chat_time ON messages(chat_jid,timestamp);
 CREATE INDEX IF NOT EXISTS idx_messages_chat_instant ON messages(chat_jid,julianday(timestamp),id);
 CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(content,content='messages',content_rowid='rowid');
 CREATE TRIGGER IF NOT EXISTS messages_ai AFTER INSERT ON messages BEGIN INSERT INTO messages_fts(rowid,content) VALUES(new.rowid,new.content); END;
 CREATE TRIGGER IF NOT EXISTS messages_ad AFTER DELETE ON messages BEGIN INSERT INTO messages_fts(messages_fts,rowid,content) VALUES('delete',old.rowid,old.content); END;
 CREATE TRIGGER IF NOT EXISTS messages_au AFTER UPDATE ON messages BEGIN INSERT INTO messages_fts(messages_fts,rowid,content) VALUES('delete',old.rowid,old.content); INSERT INTO messages_fts(rowid,content) VALUES(new.rowid,new.content); END;`)
	if e != nil {
		return e
	}
	if _, e := s.DB.Exec(`CREATE TABLE IF NOT EXISTS p1_revision(id INTEGER PRIMARY KEY CHECK(id=1), value INTEGER NOT NULL); INSERT OR IGNORE INTO p1_revision VALUES(1,0);CREATE TRIGGER IF NOT EXISTS p1_messages_INSERT AFTER INSERT ON messages BEGIN UPDATE p1_revision SET value=value+1; END;CREATE TRIGGER IF NOT EXISTS p1_messages_UPDATE AFTER UPDATE ON messages BEGIN UPDATE p1_revision SET value=value+1; END;CREATE TRIGGER IF NOT EXISTS p1_messages_DELETE AFTER DELETE ON messages BEGIN UPDATE p1_revision SET value=value+1; END;`); e != nil {
		return e
	}
	var complete int
	if e := s.DB.QueryRow("SELECT count(*) FROM engine_meta WHERE key='fts_backfilled_v1'").Scan(&complete); e != nil {
		return e
	}
	if complete == 0 {
		return startup.Measure(observer, "migration.messages.search_backfill", s.Rebuild)
	}
	return nil
}
func (s *Store) Rebuild() error {
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec("INSERT INTO messages_fts(messages_fts) VALUES('rebuild')"); e != nil {
		return e
	}
	if _, e = tx.Exec("INSERT OR REPLACE INTO engine_meta(key,value) VALUES('fts_backfilled_v1','1')"); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) PutChat(c model.Chat) error {
	_, e := s.exec(`INSERT INTO chats(jid,name,last_message_time) VALUES(?,?,?) ON CONFLICT(jid) DO UPDATE SET name=CASE WHEN excluded.name='' THEN chats.name ELSE excluded.name END,last_message_time=CASE WHEN chats.last_message_time IS NULL OR excluded.last_message_time>chats.last_message_time THEN excluded.last_message_time ELSE chats.last_message_time END`, c.JID, c.Name, c.LastMessageTime)
	return e
}
func (s *Store) PutMessage(m model.Message) error {
	if m.Content == "" && m.MediaType == "" {
		return nil
	}
	_, e := s.exec(`INSERT INTO messages(id,chat_jid,sender,content,timestamp,is_from_me,media_type,filename,quoted_message_id) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(id,chat_jid) DO UPDATE SET sender=excluded.sender,content=excluded.content,timestamp=excluded.timestamp,is_from_me=excluded.is_from_me,media_type=excluded.media_type,filename=excluded.filename,quoted_message_id=COALESCE(NULLIF(excluded.quoted_message_id,''),messages.quoted_message_id)`, m.ID, m.ChatJID, m.Sender, m.Content, m.Timestamp, m.IsFromMe, m.MediaType, m.Filename, m.QuotedMessageID)
	return e
}
func (s *Store) Chats() ([]model.Chat, error) {
	rows, e := s.DB.Query("SELECT jid,COALESCE(name,''),last_message_time FROM chats ORDER BY last_message_time DESC,jid")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []model.Chat{}
	for rows.Next() {
		var c model.Chat
		var at sql.NullTime
		if e = rows.Scan(&c.JID, &c.Name, &at); e != nil {
			return nil, e
		}
		if at.Valid {
			c.LastMessageTime = &at.Time
		}
		c.IsGroup = strings.HasSuffix(c.JID, "@g.us")
		out = append(out, c)
	}
	return out, rows.Err()
}

type Query struct {
	Chat, Search, Before, After, ID string
	Limit, Offset                   int
}

func marks(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }
func (s *Store) Messages(q Query, visible []string) ([]model.Message, error) {
	out := []model.Message{}
	if len(visible) == 0 {
		return out, nil
	}
	where := "m.chat_jid IN (" + marks(len(visible)) + ")"
	args := []any{}
	for _, v := range visible {
		args = append(args, v)
	}
	for _, v := range []struct{ col, op, val string }{{"chat_jid", "=", q.Chat}, {"id", "=", q.ID}, {"timestamp", "<", q.Before}, {"timestamp", ">", q.After}} {
		if v.val != "" {
			var value any = v.val
			if v.col == "timestamp" {
				at, e := time.Parse(time.RFC3339Nano, v.val)
				if e != nil {
					return nil, model.ErrInvalid
				}
				value = at.UTC()
			}
			if v.col == "timestamp" {
				where += " AND julianday(m.timestamp)" + v.op + "julianday(?)"
			} else {
				where += " AND m." + v.col + v.op + "?"
			}
			args = append(args, value)
		}
	}
	if q.Search != "" {
		where += " AND m.rowid IN (SELECT rowid FROM messages_fts WHERE messages_fts MATCH ?)"
		args = append(args, ftsQuery(q.Search))
	}
	if q.Limit <= 0 {
		q.Limit = 50
	}
	if q.Limit > 1000 {
		q.Limit = 1000
	}
	if q.Offset < 0 {
		return nil, model.ErrInvalid
	}
	args = append(args, q.Limit, q.Offset)
	rows, e := s.DB.Query(`SELECT m.id,m.chat_jid,COALESCE(m.sender,''),COALESCE(m.content,''),m.timestamp,COALESCE(m.is_from_me,0),COALESCE(m.media_type,''),COALESCE(m.filename,''),COALESCE(m.quoted_message_id,''),m.deleted_at FROM messages m WHERE `+where+` ORDER BY julianday(m.timestamp) DESC,m.id DESC LIMIT ? OFFSET ?`, args...)
	if e != nil {
		return nil, fmt.Errorf("message query failed")
	}
	defer rows.Close()
	for rows.Next() {
		var m model.Message
		var deleted sql.NullTime
		if e = rows.Scan(&m.ID, &m.ChatJID, &m.Sender, &m.Content, &m.Timestamp, &m.IsFromMe, &m.MediaType, &m.Filename, &m.QuotedMessageID, &deleted); e != nil {
			return nil, e
		}
		if deleted.Valid {
			m.DeletedAt = &deleted.Time
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
func (s *Store) Context(chat, id string, n int, visible []string) ([]model.Message, error) {
	if n < 0 || n > 50 {
		return nil, fmt.Errorf("radius must be between 0 and 50: %w", model.ErrInvalid)
	}
	target, e := s.Messages(Query{Chat: chat, ID: id, Limit: 2}, visible)
	if e != nil {
		return nil, e
	}
	if len(target) != 1 {
		return nil, model.ErrNotFound
	}
	m := target[0]
	if n == 0 {
		return target, nil
	}
	// Break timestamp ties with message IDs so a busy group cannot hide all
	// neighbours sharing the target's second. julianday handles legacy offsets.
	neighbours := func(op, order string) ([]model.Message, error) {
		rows, err := s.DB.Query("SELECT id FROM messages WHERE chat_jid=? AND (julianday(timestamp)"+op+"julianday(?) OR (julianday(timestamp)=julianday(?) AND id"+op+"?)) ORDER BY julianday(timestamp) "+order+",id "+order+" LIMIT ?", chat, m.Timestamp, m.Timestamp, m.ID, n)
		if err != nil {
			return nil, err
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		out := []model.Message{}
		for _, id := range ids {
			ms, err := s.Messages(Query{Chat: chat, ID: id, Limit: 1}, visible)
			if err != nil {
				return nil, err
			}
			out = append(out, ms...)
		}
		return out, nil
	}
	before, e := neighbours("<", "DESC")
	if e != nil {
		return nil, e
	}
	after, e := neighbours(">", "ASC")
	if e != nil {
		return nil, e
	}
	out := []model.Message{}
	for i := len(before) - 1; i >= 0; i-- {
		out = append(out, before[i])
	}
	out = append(out, m)
	return append(out, after...), nil
}

func (s *Store) Ephemeral(chat string, expiration uint32, at int64) error {
	if at == 0 {
		return nil
	}
	_, e := s.exec(`INSERT INTO chats(jid,ephemeral_expiration,ephemeral_setting_timestamp) VALUES(?,?,?) ON CONFLICT(jid) DO UPDATE SET ephemeral_expiration=excluded.ephemeral_expiration,ephemeral_setting_timestamp=excluded.ephemeral_setting_timestamp WHERE excluded.ephemeral_setting_timestamp>=chats.ephemeral_setting_timestamp`, chat, expiration, at)
	return e
}
func (s *Store) Counts() (int, int) {
	var c, m int
	_ = s.DB.QueryRow("SELECT COUNT(*) FROM chats").Scan(&c)
	_ = s.DB.QueryRow("SELECT COUNT(*) FROM messages").Scan(&m)
	return c, m
}

func (s *Store) exec(q string, args ...any) (sql.Result, error) {
	if s.tx != nil {
		return s.tx.Exec(q, args...)
	}
	return s.DB.Exec(q, args...)
}
func (s *Store) Batch(fn func(*Store) error) error {
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	b := &Store{DB: s.DB, tx: tx}
	if e = fn(b); e != nil {
		return e
	}
	return tx.Commit()
}

// ftsQuery turns free text into an FTS5 query of quoted terms, so punctuation such as ":" or quotes in a search
// is matched literally instead of being parsed as FTS syntax (e.g. "Inventory:" became a column filter).
func ftsQuery(text string) string {
	terms := strings.Fields(text)
	for i, t := range terms {
		terms[i] = `"` + strings.ReplaceAll(t, `"`, `""`) + `"`
	}
	if len(terms) == 0 {
		return `""`
	}
	return strings.Join(terms, " ")
}
