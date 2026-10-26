package watches

import (
	"context"
	"limmr/internal/dbutil"
	"limmr/internal/model"
	"limmr/internal/triggers"
)

type boundRunner struct {
	s    *Service
	next triggers.Runner
	url  string
}

func (r boundRunner) Run(ctx context.Context, req triggers.RunRequest) (triggers.RunResult, error) {
	w, err := r.s.Store.Get(ctx, req.HandoffID)
	if err != nil {
		return triggers.RunResult{ExitCode: -1}, err
	}
	a, err := r.s.Hub.Agent(w.CreatedVia.Agent)
	if err != nil || !a.Enabled || !a.Permissions.ReadMessages || !r.s.scopeAllowed(a, w) || !r.s.Hub.CanChat(&a, w.Handoff.SendGrant.ChatJID) {
		return triggers.RunResult{ExitCode: -1}, model.ErrForbidden
	}
	token := dbutil.ID() + dbutil.ID()
	_, err = r.s.Hub.Repo.DB.ExecContext(ctx, "INSERT INTO trigger_run_credentials VALUES(?,?,?,?,?)", dbutil.Digest(token), a.ID, w.ID, w.CreatedVia.SessionID, req.ID)
	if err != nil {
		return triggers.RunResult{ExitCode: -1}, err
	}
	defer r.s.Hub.Repo.DB.Exec("DELETE FROM trigger_run_credentials WHERE run_id=?", req.ID)
	req.MCPToken = token
	req.MCPURL = r.url
	return r.next.Run(ctx, req)
}
