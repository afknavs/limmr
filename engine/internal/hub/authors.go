package hub

import (
	"database/sql"
	"limmr/internal/model"
	"limmr/internal/wa"
	"time"
)

// Send authors outlive draft/activity retention and agent removal. Never infer
// attribution from a signature or chat text, which a contact could imitate.
func (r *Repo) migrateAuthors() error {
	_, err := r.DB.Exec(`CREATE TABLE IF NOT EXISTS send_authors(chat_jid TEXT NOT NULL,message_id TEXT NOT NULL,agent_id TEXT NOT NULL,agent_name TEXT NOT NULL,PRIMARY KEY(chat_jid,message_id));
 CREATE INDEX IF NOT EXISTS send_authors_message ON send_authors(message_id);
 CREATE TABLE IF NOT EXISTS author_meta(key TEXT PRIMARY KEY,value TEXT NOT NULL);
 INSERT OR IGNORE INTO author_meta VALUES('since',strftime('%Y-%m-%dT%H:%M:%fZ','now'));
 INSERT OR IGNORE INTO send_authors
 SELECT json_extract(o.data,'$.chat_jid'),json_extract(o.data,'$.message_id'),json_extract(o.data,'$.agent_id'),COALESCE(json_extract(a.data,'$.name'),'Former AI')
 FROM objects o LEFT JOIN objects a ON a.bucket='agents' AND a.id=json_extract(o.data,'$.agent_id')
 WHERE o.bucket IN ('drafts','approvals') AND json_extract(o.data,'$.message_id') IS NOT NULL AND json_extract(o.data,'$.message_id')!='' AND json_extract(o.data,'$.state')='sent';`)
	return err
}

func (s *Service) Attribute(m model.Message) (model.Message, error) {
	var author model.MessageAuthor
	author.Type = "agent"
	err := s.Repo.DB.QueryRow("SELECT agent_id,agent_name FROM send_authors WHERE chat_jid=? AND message_id=?", m.ChatJID, m.ID).Scan(&author.AgentID, &author.AgentName)
	if err == sql.ErrNoRows && m.IsFromMe {
		author, err = s.aliasedAuthor(m)
	}
	if err == nil {
		if author.AgentID == "you" {
			m.Author = model.MessageAuthor{Type: "owner"}
			return m, nil
		}
		// A rename is visible immediately; retain the recorded name after removal.
		if a, e := s.Agent(author.AgentID); e == nil {
			author.AgentName = a.Name
		}
		m.Author = author
		return m, nil
	}
	if err != sql.ErrNoRows {
		return m, err
	}
	author = model.MessageAuthor{Type: "contact"}
	if m.IsFromMe {
		var raw string
		if err = s.Repo.DB.QueryRow("SELECT value FROM author_meta WHERE key='since'").Scan(&raw); err != nil {
			return m, err
		}
		since, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return m, err
		}
		author.Type = "owner"
		if m.Timestamp.Before(since) {
			author.Type = "owner_or_unknown"
		}
	}
	m.Author = author
	return m, nil
}

// Reserve a message ID and persist its author before sending. If a deadline or
// crash loses the acknowledgement, a later echoed message still has provenance.
func (s *Service) prepareAuthor(agent, chat string) (string, error) {
	a, err := s.Agent(agent)
	if agent == "you" {
		a = model.Agent{ID: "you", Name: "You"}
		err = nil
	}
	if err != nil {
		return "", err
	}
	id := wa.NewMessageID()
	_, err = s.Repo.DB.Exec("INSERT INTO send_authors VALUES(?,?,?,?)", chat, id, a.ID, a.Name)
	return id, err
}

// LID-to-phone normalization can move a message to a canonical chat after its
// send. Match only aliases of that same chat, never an unrelated ID collision.
func (s *Service) aliasedAuthor(m model.Message) (model.MessageAuthor, error) {
	rows, err := s.Repo.DB.Query("SELECT chat_jid,agent_id,agent_name FROM send_authors WHERE message_id=?", m.ID)
	if err != nil {
		return model.MessageAuthor{}, err
	}
	type candidate struct {
		chat   string
		author model.MessageAuthor
	}
	candidates := []candidate{}
	for rows.Next() {
		v := candidate{author: model.MessageAuthor{Type: "agent"}}
		if err = rows.Scan(&v.chat, &v.author.AgentID, &v.author.AgentName); err != nil {
			rows.Close()
			return model.MessageAuthor{}, err
		}
		candidates = append(candidates, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return model.MessageAuthor{}, err
	}
	if len(candidates) == 0 {
		return model.MessageAuthor{}, sql.ErrNoRows
	}
	aliases, err := s.Messages.Aliases()
	if err != nil {
		return model.MessageAuthor{}, err
	}
	canonical := func(j string) string {
		if pn := aliases[j]; pn != "" {
			return pn
		}
		return j
	}
	for _, v := range candidates {
		if canonical(v.chat) == canonical(m.ChatJID) {
			_, err = s.Repo.DB.Exec("INSERT OR IGNORE INTO send_authors VALUES(?,?,?,?)", m.ChatJID, m.ID, v.author.AgentID, v.author.AgentName)
			return v.author, err
		}
	}
	return model.MessageAuthor{}, sql.ErrNoRows
}
