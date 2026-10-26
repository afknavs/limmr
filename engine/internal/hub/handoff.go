package hub

import (
	"context"
	"limmr/internal/dbutil"
	"limmr/internal/model"
	"time"
)

type RunIdentity struct{ HandoffID, RunID, AgentID, SessionID string }
type runKey struct{}

func WithHandoff(ctx context.Context, r RunIdentity) context.Context {
	return WithBackground(context.WithValue(ctx, runKey{}, r), true)
}
func HandoffIdentity(ctx context.Context) (RunIdentity, bool) {
	r, ok := ctx.Value(runKey{}).(RunIdentity)
	return r, ok
}
func (s *Service) AuthenticateRun(token string) (model.Agent, RunIdentity, error) {
	var r RunIdentity
	err := s.Repo.DB.QueryRow(`SELECT c.agent_id,c.handoff_id,c.run_id,c.session_id FROM trigger_run_credentials c JOIN trigger_runs r ON r.id=c.run_id JOIN trigger_watches w ON w.id=c.handoff_id WHERE c.digest=? AND r.state='running' AND w.status='active' AND json_extract(w.definition_json,'$.created_via.agent')=c.agent_id AND json_extract(w.definition_json,'$.created_via.session_id')=c.session_id`, dbutil.Digest(token)).Scan(&r.AgentID, &r.HandoffID, &r.RunID, &r.SessionID)
	if err != nil {
		return model.Agent{}, r, model.ErrForbidden
	}
	a, err := s.ActiveAgent(r.AgentID)
	return a, r, err
}
func (s *Service) RunScope(ctx context.Context, a model.Agent) (model.Agent, error) {
	r, ok := HandoffIdentity(ctx)
	if !ok {
		return a, nil
	}
	if s.WatchStore == nil || r.AgentID != a.ID {
		return a, model.ErrForbidden
	}
	w, err := s.WatchStore.Get(ctx, r.HandoffID)
	if err != nil || w.Handoff == nil || w.CreatedVia.Agent != a.ID || w.CreatedVia.SessionID != r.SessionID {
		return a, model.ErrForbidden
	}
	for _, id := range w.Scope.Circles {
		var c model.Circle
		if s.Repo.Get("circles", id, &c) != nil || !s.CircleAccess(&a, c) {
			return a, model.ErrForbidden
		}
	}
	var n int
	err = s.Repo.DB.QueryRowContext(ctx, "SELECT count(*) FROM trigger_runs r JOIN trigger_run_credentials c ON c.run_id=r.id WHERE r.id=? AND r.handoff_id=? AND r.state='running' AND c.agent_id=? AND c.session_id=?", r.RunID, r.HandoffID, r.AgentID, r.SessionID).Scan(&n)
	if err != nil || n != 1 {
		return a, model.ErrForbidden
	}
	d, err := s.Watches.SendPolicy(ctx, r.HandoffID, s.Now())
	if err != nil || d.Mode == "deny" {
		return a, model.ErrForbidden
	}
	chat := w.Handoff.SendGrant.ChatJID
	if !s.CanChat(&a, chat) {
		return a, model.ErrForbidden
	}
	a.Permissions.Chats = model.Scope{Mode: "only", JIDs: []string{chat}}
	a.RunChat = chat
	return a, nil
}

// Drafts persist their run binding. A later foreground call cannot strip it.
func (s *Service) handoffPolicy(ctx context.Context, d model.Draft, reserve bool) (string, error) {
	identity, bound := HandoffIdentity(ctx)
	owner := isOwnerReview(ctx) && !bound && d.State == "suggested"
	if d.HandoffID == "" {
		if bound {
			return "", model.ErrForbidden
		}
		return "allow", nil
	}
	if s.Watches == nil || s.WatchStore == nil {
		return "", model.ErrForbidden
	}
	w, err := s.WatchStore.Get(ctx, d.HandoffID)
	if err != nil || w.Handoff == nil || w.CreatedVia.Agent != d.AgentID || d.ChatJID != w.Handoff.SendGrant.ChatJID || d.Kind == "reaction" {
		return "", model.ErrForbidden
	}
	if bound && (identity.HandoffID != d.HandoffID || identity.RunID != d.RunID || identity.AgentID != d.AgentID || identity.SessionID != w.CreatedVia.SessionID) {
		return "", model.ErrForbidden
	}
	if bound {
		a, err := s.Agent(d.AgentID)
		if err != nil {
			return "", err
		}
		if _, err = s.RunScope(ctx, a); err != nil {
			return "", err
		}
	}
	// Owner sends of parked drafts still recheck the current handoff policy. Only
	// a currently authenticated run may deliver an unparked handoff draft.
	if !bound && !owner {
		return "", model.ErrForbidden
	}
	var decision struct{ Mode string }
	if reserve {
		v, e := s.Watches.ReserveSend(ctx, w.ID, d.ChatJID, "", s.Now())
		err = e
		decision.Mode = v.Mode
	} else {
		v, e := s.Watches.SendPolicy(ctx, w.ID, s.Now())
		err = e
		decision.Mode = v.Mode
	}
	if err != nil || decision.Mode == "deny" {
		return "", model.ErrForbidden
	}
	return decision.Mode, nil
}
func (s *Service) parkDraft(d model.Draft, why string) (model.Draft, error) {
	d.NeedsConfirmation = true
	d.State = "suggested"
	d.SuggestionID = dbutil.ID()
	x := model.Suggestion{TaskID: d.TaskID, ID: d.SuggestionID, Kind: "reply", CircleID: s.circleForChat(d.ChatJID), ChatJID: d.ChatJID, DraftText: d.FinalText, Variations: d.Variations, Why: why, CreatedBy: d.AgentID, CreatedAt: s.Now().UTC(), ExpiresAt: d.ExpiresAt, State: "pending", DraftID: d.ID}
	return d, s.Repo.Apply(Mutation{Bucket: "drafts", ID: d.ID, Value: d}, Mutation{Bucket: "suggestions", ID: x.ID, Value: x})
}
func (s *Service) recordOutbound(ctx context.Context, d model.Draft) error {
	if d.MessageID == "" {
		return nil
	}
	tx, err := s.Repo.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO trigger_outbound VALUES(?,?,?,?,?)", d.ChatJID, d.MessageID, d.HandoffID, d.RunID, s.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	// Keep provenance in the same transaction as the outbound marker.
	if d.AgentID != "" {
		name := "Former AI"
		var raw string
		if e := tx.QueryRowContext(ctx, "SELECT json_extract(data,'$.name') FROM objects WHERE bucket='agents' AND id=?", d.AgentID).Scan(&raw); e == nil {
			name = raw
		}
		if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO send_authors VALUES(?,?,?,?)", d.ChatJID, d.MessageID, d.AgentID, name); err != nil {
			return err
		}
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n > 0 && d.HandoffID != "" {
		if _, err = tx.ExecContext(ctx, "UPDATE trigger_watch_state SET replies_sent=replies_sent+1 WHERE watch_id=?", d.HandoffID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
