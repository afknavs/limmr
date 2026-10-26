package wa

import (
	"limmr/internal/model"
	"strings"
)

// NormalizeMapping merges legacy LID chat rows under the phone JID without
// dropping media, quote IDs, retraction state or resolved names. It also remembers
// aliases so existing hidden-chat and per-agent scopes continue to apply.
func (s *Store) NormalizeMapping(lid, pn string) error {
	if !strings.HasSuffix(lid, "@lid") || !strings.HasSuffix(pn, "@s.whatsapp.net") {
		return model.ErrInvalid
	}
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec("INSERT INTO lid_aliases VALUES(?,?) ON CONFLICT(lid) DO UPDATE SET pn=excluded.pn", lid, pn); e != nil {
		return e
	}
	if _, e = tx.Exec(`INSERT INTO chats(jid,name,last_message_time,ephemeral_expiration,ephemeral_setting_timestamp)
 SELECT ?,name,last_message_time,ephemeral_expiration,ephemeral_setting_timestamp FROM chats WHERE jid=?
 ON CONFLICT(jid) DO UPDATE SET name=CASE WHEN COALESCE(chats.name,'')='' THEN excluded.name ELSE chats.name END,last_message_time=CASE WHEN chats.last_message_time IS NULL OR excluded.last_message_time>chats.last_message_time THEN excluded.last_message_time ELSE chats.last_message_time END,
 ephemeral_expiration=CASE WHEN excluded.ephemeral_setting_timestamp>chats.ephemeral_setting_timestamp THEN excluded.ephemeral_expiration ELSE chats.ephemeral_expiration END,ephemeral_setting_timestamp=MAX(chats.ephemeral_setting_timestamp,excluded.ephemeral_setting_timestamp)`, pn, lid); e != nil {
		return e
	}
	if _, e = tx.Exec(`INSERT INTO messages(id,chat_jid,sender,content,timestamp,is_from_me,media_type,filename,url,media_key,file_sha256,file_enc_sha256,file_length,deleted_at,quoted_message_id,direct_path)
 SELECT id,?,sender,content,timestamp,is_from_me,media_type,filename,url,media_key,file_sha256,file_enc_sha256,file_length,deleted_at,quoted_message_id,direct_path FROM messages WHERE chat_jid=?
 ON CONFLICT(id,chat_jid) DO UPDATE SET
 sender=COALESCE(NULLIF(messages.sender,''),excluded.sender),
 content=COALESCE(NULLIF(messages.content,''),excluded.content),
 media_type=COALESCE(NULLIF(messages.media_type,''),excluded.media_type),
 filename=COALESCE(NULLIF(messages.filename,''),excluded.filename),
 url=COALESCE(NULLIF(messages.url,''),excluded.url),
 direct_path=COALESCE(NULLIF(messages.direct_path,''),excluded.direct_path),
 media_key=COALESCE(NULLIF(messages.media_key,x''),excluded.media_key),
 file_sha256=COALESCE(NULLIF(messages.file_sha256,x''),excluded.file_sha256),
 file_enc_sha256=COALESCE(NULLIF(messages.file_enc_sha256,x''),excluded.file_enc_sha256),
 file_length=COALESCE(NULLIF(messages.file_length,0),excluded.file_length),
 quoted_message_id=COALESCE(NULLIF(messages.quoted_message_id,''),excluded.quoted_message_id),
 deleted_at=COALESCE(messages.deleted_at,excluded.deleted_at)`, pn, lid); e != nil {
		return e
	}
	if _, e = tx.Exec("UPDATE messages SET sender=? WHERE sender=? OR sender=?", pn, lid, strings.TrimSuffix(lid, "@lid")); e != nil {
		return e
	}
	if _, e = tx.Exec("DELETE FROM messages WHERE chat_jid=?", lid); e != nil {
		return e
	}
	if _, e = tx.Exec("DELETE FROM chats WHERE jid=?", lid); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) Aliases() (map[string]string, error) {
	rows, e := s.DB.Query("SELECT lid,pn FROM lid_aliases")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var lid, pn string
		if e = rows.Scan(&lid, &pn); e != nil {
			return nil, e
		}
		out[lid] = pn
	}
	return out, rows.Err()
}
