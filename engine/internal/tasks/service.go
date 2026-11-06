package tasks

import (
	"context"
	"database/sql"
	"encoding/json"
	"limmr/internal/dbutil"
	"limmr/internal/efforts"
	"limmr/internal/hub"
	"limmr/internal/model"
	"limmr/internal/wa"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type Service struct {
	CoverMedia CoverMedia
	coverMu    sync.Mutex
	Hub        *hub.Service
	Efforts    *efforts.Service
	Location   *time.Location
	Accessory      *Accessory
	// Optional on-device worker: nil means unavailable. Never use a remote model.
	NudgeDraft func(context.Context, Task) (string, error)
	incoming   chan wa.StoredEvent
	tickMu     sync.Mutex
	// MayRunAutomations is the Y6 lease hook. Nil defaults to true.
	MayRunAutomations func(context.Context) bool
	lastDetection     time.Time
}

func New(h *hub.Service, e *efforts.Service) *Service {
	s := &Service{Hub: h, Efforts: e, Location: time.Local, incoming: make(chan wa.StoredEvent, 512)}
	h.TaskSendValidate = s.ValidateSend
	h.TaskSent = s.RecordSent
	h.TaskSuggestionDone = s.SuggestionDone
	return s
}
func (s *Service) now() time.Time { return s.Hub.Now() }
func (s *Service) Enqueue(e wa.StoredEvent) {
	select {
	case s.incoming <- e:
	default:
	}
}
func (s *Service) Run(ctx context.Context) error {
	coverCtx, stopCovers := context.WithCancel(ctx)
	coverDone := make(chan struct{})
	go func() { defer close(coverDone); s.runCoverDownloads(coverCtx) }()
	defer func() { stopCovers(); <-coverDone }()
	if err := s.ReevaluateCovers(ctx); err != nil {
		return err
	}
	if err := s.BackfillCircles(ctx); err != nil {
		return err
	}
	if err := s.BackfillChatPeople(ctx); err != nil {
		return err
	}
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	if err := s.Tick(ctx); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case e := <-s.incoming:
			if err := s.Incoming(ctx, e.Message); err != nil {
				return err
			}
		case <-tick.C:
			if err := s.Tick(ctx); err != nil {
				return err
			}
		}
	}
}
func (s *Service) raw(id string) (record, error) {
	var raw string
	var r record
	err := s.Hub.Repo.DB.QueryRow("SELECT data FROM tasks WHERE id=?", id).Scan(&raw)
	if err == sql.ErrNoRows {
		return r, model.ErrNotFound
	}
	if err != nil {
		return r, err
	}
	err = json.Unmarshal([]byte(raw), &r)
	return r, err
}
func (s *Service) records() ([]record, error) {
	rows, err := s.Hub.Repo.DB.Query("SELECT data FROM tasks ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	xs := []record{}
	for rows.Next() {
		var raw string
		var r record
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &r); err != nil {
			return nil, err
		}
		xs = append(xs, r)
	}
	return xs, rows.Err()
}
func (s *Service) visible(a *model.Agent, t Task) bool {
	if a == nil {
		return true
	}
	if !a.Permissions.ReadMessages {
		return false
	}
	for _, id := range t.CircleIDs {
		var c model.Circle
		if s.Hub.Repo.Get("circles", id, &c) != nil || !s.Hub.CircleAccess(a, c) {
			return false
		}
	}
	for _, c := range t.Chats {
		if !s.Hub.CanChat(a, c.ID) {
			return false
		}
	}
	return true
}
func (s *Service) authorize(a *model.Agent, t Task, write bool) error {
	if !s.visible(a, t) {
		return model.ErrNotFound
	}
	if a != nil && write && (!a.Permissions.SaveSummaries || t.CreatedBy.AgentID != a.ID && a.Preset != "own") {
		return model.ErrForbidden
	}
	return nil
}
func (s *Service) present(a *model.Agent, r record) Task {
	t := r.Task
	t.OwnerNotes = append([]OwnerNote{}, r.OwnerNotes...)
	sort.Slice(t.OwnerNotes, func(i, j int) bool { return t.OwnerNotes[i].CreatedAt.After(t.OwnerNotes[j].CreatedAt) })
	if a != nil {
		t.SummaryAttempt = nil
	}
	t.Items = []Item{}
	for _, it := range r.Items {
		if it.MessageRef != nil && (s.Hub.HiddenChat(it.MessageRef.ChatJID) || !s.Hub.CanChat(a, it.MessageRef.ChatJID)) {
			continue
		}
		if ref := it.MessageRef; ref != nil {
			ms, err := s.Hub.Messages.Messages(wa.Query{Chat: ref.ChatJID, ID: ref.MessageID, Limit: 1}, []string{ref.ChatJID})
			if err != nil || len(ms) == 0 || ms[0].DeletedAt != nil {
				continue
			}
		}
		t.Items = append(t.Items, it)
	}
	t.Cover = nil
	if !t.CoverRemoved {
		for _, it := range t.Items {
			if r.Cover != nil && it.ID == r.Cover.ID {
				copy := it
				t.Cover = &copy
				break
			}
		}
	}
	return t
}
func (s *Service) Get(a *model.Agent, id string) (Task, error) {
	r, err := s.raw(id)
	if err != nil {
		return Task{}, err
	}
	if err = s.authorize(a, r.Task, false); err != nil {
		return Task{}, err
	}
	t := s.present(a, r)
	// History is owner-only: agent responses retain their existing filtered task
	// contract and must not expose labels from inaccessible linked work.
	if a == nil {
		c, contextErr := s.chatContext()
		if contextErr != nil {
			return Task{}, contextErr
		}
		t.Latest, err = s.latestInContext(t, c)
		if err != nil {
			return Task{}, err
		}

		t.People = mergeEntities(t.People, c.allPeopleFor(t.Chats))
		visiblePeople := []Entity{}
		for _, person := range t.People {
			p, known := c.people[person.ID]
			if !known || !c.hiddenPerson(p) {
				visiblePeople = append(visiblePeople, person)
			}
		}
		t.People = visiblePeople
		t.History, err = s.History(id)
		if err != nil {
			return Task{}, err
		}
	}
	return t, nil
}

func (s *Service) History(id string) ([]Event, error) {
	agents, err := hub.List[model.Agent](s.Hub.Repo, "agents")
	if err != nil {
		return nil, err
	}
	names := map[string]string{"you": "you", "This Mac": "This Mac"}
	for _, agent := range agents {
		if agent.Name != "" {
			names[agent.ID] = agent.Name
		}
	}
	rows, err := s.Hub.Repo.DB.Query("SELECT at,kind,text,payload FROM task_events WHERE task_id=? ORDER BY julianday(at) DESC,rowid DESC", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		var at, payload string
		if err = rows.Scan(&at, &e.Kind, &e.Text, &payload); err != nil {
			return nil, err
		}
		e.At, err = time.Parse(time.RFC3339Nano, at)
		if err != nil {
			return nil, err
		}
		var p struct {
			By string `json:"by"`
		}
		_ = json.Unmarshal([]byte(payload), &p)
		if p.By != "" {
			name := names[p.By]
			if name == "" {
				name = "an AI"
			}
			e.Text += " · " + name
		}
		e.Kind = strings.TrimPrefix(e.Kind, "task_")
		out = append(out, e)
	}
	return out, rows.Err()
}
func (s *Service) summarize(t Task) TaskSummary {
	if t.LastActiveAt.IsZero() {
		t.LastActiveAt = t.UpdatedAt
	}
	cover := t.Cover
	if cover != nil && cover.MessageRef != nil {
		ref := cover.MessageRef
		if s.Hub.HiddenChat(ref.ChatJID) {
			cover = nil
		} else {
			ms, err := s.Hub.Messages.Messages(wa.Query{Chat: ref.ChatJID, ID: ref.MessageID, Limit: 1}, []string{ref.ChatJID})
			if err != nil || len(ms) == 0 || ms[0].DeletedAt != nil {
				cover = nil
			}
		}
	}
	if cover != nil {
		cover = &Item{ID: cover.ID, Kind: cover.Kind, Title: cover.Title, MediaType: cover.MediaType, MessageRef: cover.MessageRef, AttachmentID: cover.AttachmentID, AddedAt: cover.AddedAt, Source: cover.Source}
	}
	out := TaskSummary{ClosedAt: t.ClosedAt, CloseReason: t.CloseReason, CircleSuggestions: t.CircleSuggestions, Cover: cover, CreatedAt: t.CreatedAt, LastActiveAt: t.LastActiveAt, CircleIDs: t.CircleIDs, Origin: t.Origin, Suggestion: t.Suggestion, CreatedBy: t.CreatedBy, AutoKeptAt: t.AutoKeptAt, UndoChangeID: t.UndoChangeID, ID: t.ID, Title: t.Title, Kind: t.Kind, Status: t.Status, Progress: t.Progress, NeedsYou: t.NeedsYou, NeedsYouReason: t.NeedsYouReason, EffortID: t.EffortID}
	if t.CheckIn != nil && !t.CheckIn.NextAt.IsZero() {
		at := t.CheckIn.NextAt
		out.NextCheckInAt = &at
	}
	if t.Waiting != nil {
		w := t.Waiting
		out.Waiting = &WaitingSummary{What: w.What, Since: w.Since, Days: max(0, int(s.now().Sub(w.Since).Hours()/24))}
		if w.On != nil {
			out.Waiting.OnName = w.On.Name
		}
	}
	if len(t.UpdatesSent) > 0 {
		at := t.UpdatesSent[len(t.UpdatesSent)-1].At
		out.LastUpdateAt = &at
	}
	return out
}
func (s *Service) List(a *model.Agent, status string, limit int) ([]TaskSummary, error) {
	return s.ListForPerson(a, status, "", limit)
}

// Filter before limiting so a person's tasks cannot disappear behind unrelated rows.
func (s *Service) ListForPerson(a *model.Agent, status, person string, limit int) ([]TaskSummary, error) {
	return s.listForPerson(a, status, person, limit, false)
}
func (s *Service) ListRecent(a *model.Agent, status, person string, limit int) ([]TaskSummary, error) {
	return s.listForPerson(a, status, person, limit, true)
}
func (s *Service) listForPerson(a *model.Agent, status, person string, limit int, recent bool) ([]TaskSummary, error) {
	if status == "" {
		status = "open"
	}
	if !model.Contains([]string{"open", "closed", "all", "suggested", "quiet"}, status) || limit < 1 || limit > 500 {
		return nil, model.ErrInvalid
	}
	// Home never needs full workspace items, drafts, summaries or sent bodies.
	rows, err := s.Hub.Repo.DB.Query(`SELECT data FROM task_cards ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	xs := []record{}
	byID := map[string]Task{}
	for rows.Next() {
		var data string
		var r record
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(data), &r); err != nil {
			return nil, err
		}
		xs = append(xs, r)
		byID[r.ID] = r.Task
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	out := []TaskSummary{}
	for _, r := range xs {
		if person != "" {
			matches := false
			for _, p := range r.People {
				if p.ID == person {
					matches = true
					break
				}
			}
			if !matches {
				continue
			}
		}
		closed := r.Status == "done" || r.Status == "dropped"
		if status == "quiet" && r.Status != "quiet" || status == "open" && closed || status == "closed" && !closed || status == "suggested" && r.Status != "suggested" || status == "open" && !open(r.Task) || !s.visible(a, r.Task) {
			continue
		}
		if a == nil {
			if err := s.suggestCircles(nil, &r.Task); err != nil {
				return nil, err
			}
		}
		summary := s.summarize(r.Task)
		out = append(out, summary)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !recent && out[i].NeedsYou != out[j].NeedsYou {
			return out[i].NeedsYou
		}
		if !out[i].LastActiveAt.Equal(out[j].LastActiveAt) {
			return out[i].LastActiveAt.After(out[j].LastActiveAt)
		}
		return out[i].ID < out[j].ID
	})
	out = out[:min(limit, len(out))]
	for i := range out {
		out[i].CoverCandidate = s.coverCandidate(a, byID[out[i].ID])
	}
	return out, nil
}
func open(t Task) bool             { return t.Status == "active" || t.Status == "waiting" }
func length(s string, n int) bool  { return utf8.ValidString(s) && utf8.RuneCountInString(s) <= n }
func short(s string, n int) string { r := []rune(s); return string(r[:min(n, len(r))]) }
func (s *Service) validate(t Task) error {
	if strings.TrimSpace(t.Title) == "" || !length(t.Title, 60) || !length(t.Goal, 1000) || !length(t.Summary.Text, 1500) || !model.Contains([]string{"long", "short"}, t.Kind) || !model.Contains([]string{"active", "waiting", "done", "dropped", "suggested", "quiet"}, t.Status) || t.LifespanDays < 1 || t.LifespanDays > 3650 {
		return model.ErrInvalid
	}
	for _, st := range t.Checklist {
		if strings.TrimSpace(st.Text) == "" || !length(st.Text, 200) {
			return model.ErrInvalid
		}
	}
	for _, it := range t.Items {
		if !model.Contains([]string{"link", "file", "note"}, it.Kind) || !model.Contains([]string{"chat", "ai", "owner"}, it.Source) || !length(it.Title, 120) || !length(it.Text, 2000) {
			return model.ErrInvalid
		}
		if it.Kind == "link" && normalizeURL(it.URL) == "" || it.Kind == "file" && it.AttachmentID == "" || it.Kind == "note" && strings.TrimSpace(it.Text) == "" {
			return model.ErrInvalid
		}
		if it.Source == "chat" && it.MessageRef == nil {
			return model.ErrInvalid
		}
	}
	if w := t.Waiting; w != nil {
		if strings.TrimSpace(w.What) == "" || !length(w.What, 200) || w.NudgeAfterDays < 1 || w.NudgeAfterDays > 30 || !model.Contains([]string{"none", "queued", "sent", "skipped"}, w.NudgeState) {
			return model.ErrInvalid
		}
		if w.On != nil {
			found := false
			for _, p := range t.People {
				found = found || p.ID == w.On.ID
			}
			if !found {
				return model.ErrInvalid
			}
		}
	}
	if t.EndCondition != "" && !model.Contains([]string{"agent_done", "checklist_done"}, t.EndCondition) {
		return model.ErrInvalid
	}
	if c := t.CheckIn; c != nil {
		if !model.Contains([]string{"weekly", "every_n_days", "once", "reply"}, c.Rule) || !clockTime(c.AtTime) || !length(c.Note, 300) || c.NextAt.IsZero() {
			return model.ErrInvalid
		}
		if c.Rule == "weekly" && (c.Weekday == nil || *c.Weekday < 0 || *c.Weekday > 6) || c.Rule == "every_n_days" && (c.EveryDays < 1 || c.EveryDays > 60) {
			return model.ErrInvalid
		}
		found := false
		for _, ch := range t.Chats {
			found = found || ch.ID == c.To
		}
		if !found {
			return model.ErrInvalid
		}
	}
	return nil
}
func progress(t *Task) {
	t.Progress = Progress{Total: len(t.Checklist)}
	for _, st := range t.Checklist {
		if st.Done {
			t.Progress.Done++
		}
	}
}
func closeTask(r *record, now time.Time, reason string) {
	r.Status = "done"
	r.ClosedAt = &now
	r.CloseReason = reason
	r.NeedsYou = false
	r.NeedsYouReason = ""
}

type Create struct {
	EndAt        *time.Time `json:"end_at"`
	EndCondition string     `json:"end_condition"`
	Title        string     `json:"title"`
	Goal         string     `json:"goal"`
	Kind         string     `json:"kind"`
	EffortID     string     `json:"effort_id"`
	Chats        []Entity   `json:"chats"`
	People       []Entity   `json:"people"`
	Items        []Item     `json:"items"`
	Checklist    []Step     `json:"checklist"`
	CheckIn      *CheckIn   `json:"check_in"`
	LifespanDays int        `json:"lifespan_days"`
}

func (s *Service) Create(a *model.Agent, in Create) (Task, error) {
	return s.create(a, in, nil, false)
}
func (s *Service) Suggest(a *model.Agent, in Create, suggestion Suggestion) (Task, error) {
	return s.create(a, in, &suggestion, false)
}
func (s *Service) create(a *model.Agent, in Create, suggestion *Suggestion, auto bool) (Task, error) {
	if a != nil && !a.Permissions.SaveSummaries {
		return Task{}, model.ErrForbidden
	}
	now := s.now()
	r := record{Task: Task{EndAt: in.EndAt, EndCondition: in.EndCondition, LastActiveAt: now, Origin: "owner", ID: "tsk_" + dbutil.ID(), Title: strings.TrimSpace(in.Title), Goal: in.Goal, Kind: in.Kind, Status: "active", EffortID: in.EffortID, Chats: in.Chats, People: in.People, CircleIDs: []string{}, CreatedBy: Credit{AgentID: "you", Name: "You"}, CreatedAt: now, UpdatedAt: now, Items: []Item{}, Checklist: []Step{}, CheckIn: in.CheckIn, LifespanDays: in.LifespanDays, UpdatesSent: []SentUpdate{}}}
	if a != nil {
		r.CreatedBy = Credit{AgentID: a.ID, Name: a.Name}
		r.Origin = "agent"
	}
	if r.LifespanDays == 0 {
		r.LifespanDays = 14
	}
	refs := []Ref{}
	if in.EffortID != "" {
		if s.Efforts == nil {
			return Task{}, model.ErrNotFound
		}
		e, err := s.Efforts.Get(a, in.EffortID)
		if err != nil {
			return Task{}, err
		}
		r.Chats = mergeEntities(e.Chats, r.Chats)
		r.People = mergeEntities(e.People, r.People)
		r.CircleIDs = e.CircleIDs
		refs = e.MessageRefs
		r.EffortNeeds = e.Status == "on_you"
	}
	if r.Chats == nil {
		r.Chats = []Entity{}
	}
	if r.People == nil {
		r.People = []Entity{}
	}
	// Canonical, existing chats only. Permission to inspect every chat is required.
	for i, ch := range r.Chats {
		jid, err := s.Hub.ResolveChat(ch.ID)
		if err != nil {
			return Task{}, err
		}
		var name string
		if err = s.Hub.Messages.DB.QueryRow("SELECT COALESCE(name,'') FROM chats WHERE jid=?", jid).Scan(&name); err != nil {
			return Task{}, model.ErrNotFound
		}
		r.Chats[i] = Entity{ID: jid, Name: name}
	}
	if err := s.suggestCircles(a, &r.Task); err != nil {
		return Task{}, err
	}
	if !s.visible(a, r.Task) {
		return Task{}, model.ErrNotFound
	}
	for _, p := range r.People {
		var person model.Person
		if err := s.Hub.Repo.Get("people", p.ID, &person); err != nil {
			return Task{}, model.ErrNotFound
		}
		visible := a == nil
		for _, jid := range person.JIDs {
			if s.Hub.CanChat(a, jid) && !s.Hub.HiddenChat(jid) {
				visible = true
			}
		}
		if !visible {
			return Task{}, model.ErrNotFound
		}
	}

	for _, it := range in.Items {
		it.ID = "itm_" + dbutil.ID()
		it.AddedAt = now
		it.AddedBy = r.CreatedBy.AgentID
		if a != nil {
			it.Source = "ai"
		} else {
			it.Source = "owner"
		}
		inTask := it.MessageRef == nil
		if it.MessageRef != nil {
			for _, ch := range r.Chats {
				inTask = inTask || ch.ID == it.MessageRef.ChatJID
			}
		}
		if !inTask || it.MessageRef != nil && !s.Hub.CanChat(a, it.MessageRef.ChatJID) {
			return Task{}, model.ErrNotFound
		}
		r.Items = appendItem(r.Items, it)
	}
	for i, st := range in.Checklist {
		st.ID = "stp_" + dbutil.ID()
		st.Order = i
		st.DoneAt = nil
		if st.Done {
			st.DoneAt = &now
		}
		r.Checklist = append(r.Checklist, st)
	}
	if r.CheckIn != nil {
		if r.CheckIn.NextAt.IsZero() {
			r.CheckIn.NextAt = s.next(*r.CheckIn, now)
		}
		r.CheckIn.LastAt = nil
	}
	if err := s.scan(&r, refs); err != nil {
		return Task{}, err
	}
	progress(&r.Task)
	r.NeedsYou = r.EffortNeeds
	if r.NeedsYou {
		r.NeedsYouReason = "Your effort needs attention"
	}
	if (r.Kind == "short" || r.EndCondition == "checklist_done") && r.Progress.Total > 0 && r.Progress.Done == r.Progress.Total {
		closeTask(&r, now, "done")
	}
	if suggestion != nil {
		r.Suggestion = suggestion
		r.Status = "suggested"
		r.Origin = "limmr"
		if a != nil {
			r.Origin = "agent"
		}
		r.NeedsYou = false
		r.NeedsYouReason = ""
		r.EffortNeeds = false
		if auto {
			r.Status = "active"
			r.AutoKeptAt = &now
			r.UndoChangeID = "chg_" + dbutil.ID()
		}
	}
	if err := s.validate(r.Task); err != nil {
		return Task{}, err
	}
	tx, err := s.Hub.Repo.DB.Begin()
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("INSERT INTO tasks VALUES(?,?)", r.ID, dbutil.JSON(r)); err == nil {
		err = s.event(tx, r, "created", "created", "Task created", map[string]string{"by": r.CreatedBy.AgentID})
		if err == nil && r.CheckIn != nil {
			err = s.event(tx, r, "scheduled:"+now.Format(time.RFC3339Nano), "check_in_scheduled", "Check-in scheduled", nil)
		}
	}
	if err == nil && suggestion != nil {
		err = s.suggestedEvent(tx, r)
		if err == nil && auto {
			line := "Limmr started tracking " + r.Title
			act := model.Activity{At: now, AgentID: r.CreatedBy.AgentID, Kind: "summary", Tool: "task_auto_keep", MessageIDs: []string{}, Count: 1, Note: line, ChatJIDs: []string{}}
			for _, ch := range r.Chats {
				act.ChatJIDs = append(act.ChatJIDs, ch.ID)
			}
			if _, err = tx.Exec("INSERT INTO activity(at,data) VALUES(?,?)", now, dbutil.JSON(act)); err != nil {
				return Task{}, err
			}
			if err = s.event(tx, r, "auto_kept", "auto_kept", "Started tracking automatically", map[string]string{"by": r.CreatedBy.AgentID}); err != nil {
				return Task{}, err
			}
			change := hub.TaskSurfacingChange{ID: r.UndoChangeID, Kind: "auto_kept_task", TaskID: r.ID, At: now}
			_, err = tx.Exec("INSERT INTO objects VALUES('changes',?,?)", change.ID, dbutil.JSON(change))
		}
	}
	if err == nil && r.ClosedAt != nil {
		err = s.event(tx, r, "closed:"+now.Format(time.RFC3339Nano), "task_closed", "Task closed", nil)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		return Task{}, err
	}
	return s.present(a, r), nil
}
func mergeEntities(a, b []Entity) []Entity {
	out := append([]Entity{}, a...)
	for _, v := range b {
		found := false
		for _, x := range out {
			found = found || x.ID == v.ID
		}
		if !found {
			out = append(out, v)
		}
	}
	return out
}

// event text is an allowlisted timeline label, never draft/message text.
func (s *Service) event(tx *sql.Tx, r record, key, kind, text string, payload any) error {
	id := r.ID + ":" + key
	result, err := tx.Exec("INSERT OR IGNORE INTO task_events VALUES(?,?,?,?,?,?)", id, r.ID, s.now().UTC().Format(time.RFC3339Nano), kind, text, dbutil.JSON(payload))
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n > 0 && kind == "task_owner_note" {
		_, err = tx.Exec(`INSERT INTO agent_updates(agent,at,kind,ref) SELECT id,?,?,? FROM objects WHERE bucket='agents' AND (json_extract(data,'$.preset')='own' OR id=?)`, s.now().UTC().Format(time.RFC3339Nano), kind, id, r.CreatedBy.AgentID)
		return err
	}
	if n > 0 && model.Contains([]string{"task_due", "task_waiting_cleared", "task_closed", "task_owner_note"}, kind) {
		_, err = tx.Exec("INSERT INTO agent_updates(agent,at,kind,ref) VALUES(?,?,?,?)", r.CreatedBy.AgentID, s.now().UTC().Format(time.RFC3339Nano), kind, id)
	}
	return err
}

// Mutations reserve SQLite's writer before reading so concurrent ticks/clients cannot lose updates.
func (s *Service) mutate(a *model.Agent, id string, fn func(*record, *sql.Tx) error) (Task, error) {
	r, err := s.raw(id)
	if err != nil {
		return Task{}, err
	}
	if err = s.authorize(a, r.Task, true); err != nil {
		return Task{}, err
	}
	tx, err := s.Hub.Repo.DB.Begin()
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE sync_state SET clock=clock WHERE id=1 AND ? IS NOT NULL", id); err != nil {
		return Task{}, err
	}
	var raw string
	if err = tx.QueryRow("SELECT data FROM tasks WHERE id=?", id).Scan(&raw); err != nil {
		return Task{}, err
	}
	if err = json.Unmarshal([]byte(raw), &r); err != nil {
		return Task{}, err
	}
	if r.Purged {
		return Task{}, model.ErrConflict
	}
	before := r.Task
	wasClosed := r.Status == "done" || r.Status == "dropped"
	if err = fn(&r, tx); err != nil {
		return Task{}, err
	}
	now := s.now()
	activity := dbutil.JSON(before.OwnerNotes) != dbutil.JSON(r.OwnerNotes) || dbutil.JSON(before.Checklist) != dbutil.JSON(r.Checklist) || len(before.Items) != len(r.Items) || len(before.UpdatesSent) != len(r.UpdatesSent)
	if activity {
		r.LastActiveAt = now
		if before.Status == "quiet" && r.Status == "quiet" {
			wakeTask(&r)
		}
	}
	progress(&r.Task)
	if r.Progress.Done < r.Progress.Total {
		r.PauseChecklistClose = false
	}
	if open(r.Task) && !r.PauseChecklistClose && (r.Kind == "short" || r.EndCondition == "checklist_done") && r.Progress.Total > 0 && r.Progress.Done == r.Progress.Total {
		closeTask(&r, now, "done")
	}
	if (r.Status == "done" || r.Status == "dropped") && r.ClosedAt == nil {
		r.ClosedAt = &now
		if r.Status == "dropped" {
			r.CloseReason = "owner"
		} else {
			if r.CloseReason == "" {
				r.CloseReason = "done"
			}
		}
	}
	if !wasClosed && (r.Status == "done" || r.Status == "dropped") {
		if err = s.event(tx, r, "closed:"+now.Format(time.RFC3339Nano), "task_closed", closureLabel(r), nil); err != nil {
			return Task{}, err
		}
		if _, err = tx.Exec("UPDATE objects SET data=json_set(data,'$.state','dismissed') WHERE bucket='suggestions' AND json_extract(data,'$.task_id')=? AND json_extract(data,'$.state')='pending'", id); err != nil {
			return Task{}, err
		}
	}
	labels := []struct {
		changed    bool
		kind, text string
	}{
		{before.Status != r.Status, "status_changed", "Status changed to " + r.Status},
		{len(before.Items) < len(r.Items), "item_added", "Workspace item added"},
		{len(before.Checklist) < len(r.Checklist), "step_added", "Checklist item added"},
		{before.Summary.Text != r.Summary.Text, "summary_changed", "Summary updated"},
	}
	credit := actorCredit(a)
	if r.eventActor != "" {
		credit = map[string]string{"by": r.eventActor}
	}
	for _, label := range labels {
		if label.changed {
			if err = s.event(tx, r, dbutil.ID(), label.kind, label.text, credit); err != nil {
				return Task{}, err
			}
		}
	}
	if err = s.refreshNeeds(tx, &r); err != nil {
		return Task{}, err
	}
	if err = s.validate(r.Task); err != nil {
		return Task{}, err
	}
	// Compare meaningful state before allowing a timestamp-only edit.
	var previous record
	if err = json.Unmarshal([]byte(raw), &previous); err != nil {
		return Task{}, err
	}
	compare := r
	compare.UpdatedAt = previous.UpdatedAt
	if dbutil.JSON(compare) == dbutil.JSON(previous) {
		r.UpdatedAt = previous.UpdatedAt
	}
	result, err := tx.Exec("UPDATE tasks SET data=? WHERE id=? AND data IS NOT ?", dbutil.JSON(r), id, dbutil.JSON(r))
	if err != nil {
		return Task{}, err
	}
	if err = tx.Commit(); err != nil {
		return Task{}, err
	}
	if n, _ := result.RowsAffected(); n > 0 {
		s.Hub.NotifyTaskChanged()
	}
	return s.present(a, r), nil
}
func (s *Service) Feed(since time.Time) ([]FeedItem, error) {
	xs, err := s.records()
	if err != nil {
		return nil, err
	}
	out := []FeedItem{}
	for _, r := range xs {
		if r.UpdatedAt.Before(since) {
			continue
		}
		f := FeedItem{TaskSummary: s.summarize(r.Task), CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, ClosedAt: r.ClosedAt, Events: []Event{}}
		rows, err := s.Hub.Repo.DB.Query("SELECT at,kind,text FROM task_events WHERE task_id=? AND kind<>'task_due' AND julianday(at)>=julianday(?) ORDER BY julianday(at),id", r.ID, since)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var e Event
			var at string
			if err = rows.Scan(&at, &e.Kind, &e.Text); err != nil {
				break
			}
			e.At, err = time.Parse(time.RFC3339Nano, at)
			if err != nil {
				break
			}
			e.Kind = strings.TrimPrefix(e.Kind, "task_")
			f.Events = append(f.Events, e)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

// WithHub scopes SQL to a request deadline without creating a second runtime.
func (s *Service) WithHub(h *hub.Service) *Service {
	scoped := &Service{MayRunAutomations: s.MayRunAutomations, Hub: h, Efforts: s.Efforts.WithHub(h), Location: s.Location, NudgeDraft: s.NudgeDraft}
	h.TaskSendValidate = scoped.ValidateSend
	h.TaskSent = scoped.RecordSent
	h.TaskSuggestionDone = scoped.SuggestionDone
	return scoped
}

func actorCredit(a *model.Agent) map[string]string {
	by := "you"
	if a != nil {
		by = a.ID
	}
	return map[string]string{"by": by}
}
