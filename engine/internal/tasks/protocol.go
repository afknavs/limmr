package tasks

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"limmr/internal/dbutil"
	"limmr/internal/efforts"
	"limmr/internal/hub"
	"limmr/internal/wa"
)

// ProtocolEvidence deliberately cannot hold names, chat JIDs, quotes or text.
// IDs refer to task, event, item, message or effort records, never participants.
type ProtocolEvidence struct {
	IDs   []string    `json:"ids"`
	Dates []time.Time `json:"dates"`
}
type ProtocolAction struct {
	Rule     string           `json:"rule"`
	Action   string           `json:"action"`
	Reason   string           `json:"reason"`
	Evidence ProtocolEvidence `json:"evidence"`
	members  []Candidate
	hint     *record
}
type ProtocolTask struct {
	ID          string           `json:"id"`
	Title       string           `json:"title"`
	Status      string           `json:"status"`
	Origin      string           `json:"origin"`
	CreatorName string           `json:"creator_name"`
	PeopleCount int              `json:"people_count"`
	ChatCount   int              `json:"chat_count"`
	Outcome     string           `json:"outcome"`
	Actions     []ProtocolAction `json:"actions"`
}
type ProtocolPlan struct {
	At      time.Time      `json:"at"`
	Tasks   []ProtocolTask `json:"tasks"`
	Counts  map[string]int `json:"counts"`
	records map[string]record
}
type protocolEvent struct {
	ID, Kind, By string
	At           time.Time
}
type protocolInput struct {
	record           record
	reviewKeep       bool
	events           []protocolEvent
	messagesPresent  bool
	messages         []finishMessage
	ownerAt, active  time.Time
	effortStatus     string
	follows          FollowsWorkTask
	followsAvailable bool
}

// ProtocolOptions lets review and startup plan against the same keeps and readers.
// Messages is optional: an offline combined database can supply its own tables.
type ProtocolOptions struct {
	KeepTaskIDs []string
	Messages    dbutil.Database
}

// PlanProtocolPass reads only the supplied database. It does not open
// files, migrate, start a runtime, apply rules, or record a protocol marker.
// Loading and pure planning are separate; only shared live read helpers and
// predicates are used. A hub-only copy is useful without a messages database.
func PlanProtocolPass(db dbutil.Database, now time.Time, loc *time.Location, options ...ProtocolOptions) (ProtocolPlan, error) {
	var option ProtocolOptions
	if len(options) > 0 {
		option = options[0]
	}
	inputs, err := loadProtocolInputs(db, now, loc, option.Messages)
	if err != nil {
		return ProtocolPlan{}, err
	}
	for i := range inputs {
		for _, id := range option.KeepTaskIDs {
			if inputs[i].record.ID == id {
				inputs[i].reviewKeep = true
				break
			}
		}
	}
	return planProtocolPass(inputs, now, loc), nil
}

func loadProtocolInputs(db dbutil.Database, now time.Time, loc *time.Location, messages dbutil.Database) ([]protocolInput, error) {
	// Do not call hub.New, tasks.New, hub.Open or wa.OpenStore: they can write.
	h := &hub.Service{Repo: &hub.Repo{DB: db}, Now: func() time.Time { return now }}
	s := &Service{Hub: h, Location: loc}
	var n int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='messages'").Scan(&n); err != nil {
		return nil, err
	}
	if messages == nil && n != 0 {
		messages = db
	}
	hasMessages := messages != nil
	if hasMessages {
		h.Messages = &wa.Store{DB: messages}
		s.Efforts = &efforts.Service{Hub: h}
	}
	rs, err := s.records()
	if err != nil {
		return nil, err
	}
	out := []protocolInput{}
	for _, r := range rs {
		if r.Purged {
			continue
		}
		in := protocolInput{record: r, active: r.LastActiveAt, messagesPresent: hasMessages}
		rows, err := db.Query("SELECT id,at,kind,payload FROM task_events WHERE task_id=? ORDER BY julianday(at),id", r.ID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var e protocolEvent
			var at, payload string
			if err = rows.Scan(&e.ID, &at, &e.Kind, &payload); err != nil {
				break
			}
			if e.At, err = time.Parse(time.RFC3339Nano, at); err != nil {
				break
			}
			var credit struct {
				By string `json:"by"`
			}
			if err = json.Unmarshal([]byte(payload), &credit); err != nil {
				break
			}
			e.By = credit.By
			in.events = append(in.events, e)
			if e.Kind == "reopened" && (in.record.ReopenedAt == nil || e.At.After(*in.record.ReopenedAt)) {
				in.record.ReopenedAt = ptrTime(e.At)
			}
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return nil, err
		}
		if hasMessages && duplicateOpen(r.Task) {
			in.active = s.latestActivity(r.Task)
			in.messages, in.ownerAt, err = s.finishMessages(r.Task)
			if err != nil {
				return nil, err
			}
			if r.EffortID != "" {
				in.follows, err = s.FollowsWorkSnapshot(r.Task)
				if err != nil {
					return nil, err
				}
				in.followsAvailable = in.follows.CurrentEffort != nil
				if in.followsAvailable {
					in.effortStatus = finishedEffortStatus(in.follows.CurrentEffort.Status)
				}
			} else {
				in.followsAvailable = true
			}
		}
		out = append(out, in)
	}
	return out, nil
}

func protocolEvidence(ids []string, dates ...time.Time) ProtocolEvidence {
	e := ProtocolEvidence{IDs: append([]string{}, ids...), Dates: []time.Time{}}
	for _, at := range dates {
		if !at.IsZero() {
			e.Dates = append(e.Dates, at)
		}
	}
	return e
}
func ownerTouch(in protocolInput) (string, ProtocolEvidence) {
	r := in.record
	empty := protocolEvidence(nil)
	if r.OwnerOpenedAt != nil {
		return "you opened it", protocolEvidence(nil, *r.OwnerOpenedAt)
	}
	if r.SubjectEdited {
		return "you corrected the subject", empty
	}
	if r.CirclesEdited {
		return "you changed its circles", empty
	}
	if r.ReopenedAt != nil {
		return "you reopened it", protocolEvidence(nil, *r.ReopenedAt)
	}
	if len(r.OwnerNotes) > 0 {
		n := r.OwnerNotes[0]
		return "you wrote an owner note", protocolEvidence([]string{n.ID}, n.CreatedAt)
	}
	for _, it := range r.Items {
		if it.AddedBy == "you" {
			return "you added a task item", protocolEvidence([]string{it.ID}, it.AddedAt)
		}
	}
	for _, u := range r.UpdatesSent {
		if u.By == "you" {
			return "you sent a task update", protocolEvidence(nil, u.At)
		}
	}
	if len(r.RemovedMembers.Chats)+len(r.RemovedMembers.People) > 0 {
		return "you removed a task member", empty
	}
	for _, e := range in.events {
		why := ""
		switch {
		case e.Kind == "kept" && e.By == "you":
			why = "you kept it"
		case e.Kind == "reopened":
			why = "you reopened it"
		case e.Kind == "task_owner_note":
			why = "you wrote an owner note"
		case (e.Kind == "step_done" || e.Kind == "step_reopened") && e.By == "you":
			why = "you toggled a checklist step"
		case e.Kind == "status_changed" && e.By == "you":
			why = "you changed its status"
		case e.Kind == "owner_remove":
			why = "you removed a task member"
		case (e.Kind == "subject_edited" || e.Kind == "circles_changed") && e.By == "you":
			why = "you corrected its subject or circles"
		case e.Kind == "item_added" && e.By == "you":
			why = "you added a task item"
		}
		if why != "" {
			return why, protocolEvidence([]string{e.ID}, e.At)
		}
	}
	return "", empty
}

// planProtocolPass is pure: no store/service calls or mutation of its inputs.
func planProtocolPass(inputs []protocolInput, now time.Time, loc *time.Location) ProtocolPlan {
	out := ProtocolPlan{At: now, Tasks: []ProtocolTask{}, Counts: map[string]int{"Stays a task": 0, "Becomes a suggestion": 0, "Ends": 0, "Left for you": 0, "Not checked": 0, "Existing suggestions": 0, "Already ended": 0}, records: map[string]record{}}
	remaining := map[int]Task{}
	add := func(t *ProtocolTask, rule, action, why string, evidence ProtocolEvidence) {
		t.Actions = append(t.Actions, ProtocolAction{Rule: rule, Action: action, Reason: why, Evidence: evidence})
	}
	for _, in := range inputs {
		r := in.record
		out.records[r.ID] = r
		creator := r.CreatedBy.Name
		if r.Origin == "owner" || r.CreatedBy.AgentID == "you" {
			creator = "You"
		}
		t := ProtocolTask{ID: r.ID, Title: r.Title, Status: r.Status, Origin: r.Origin, CreatorName: creator, PeopleCount: len(r.People), ChatCount: len(r.Chats), Outcome: "Stays a task", Actions: []ProtocolAction{}}
		demoted := false
		if r.Status != "suggested" {
			automatic := r.AutoKeptAt != nil || r.Origin == "agent" && r.AskedByOwner == nil
			why, evidence := ownerTouch(in)
			if in.reviewKeep {
				why, evidence = "you kept it in your review of old tasks", protocolEvidence(nil, now)
				add(&t, "review", "keep", protocolReviewKeepText, evidence)
			}
			if automatic && why == "" {
				why = "The AI made this without your quoted request; no action from you on record"
				evidence = protocolEvidence(nil, r.CreatedAt)
				if r.AutoKeptAt != nil {
					why = "Limmr made this automatically; no action from you on record"
					evidence = protocolEvidence(nil, *r.AutoKeptAt)
				}
				add(&t, "demote", "back to suggestion", why, evidence)
				t.Outcome = "Becomes a suggestion"
				demoted = true
			} else {
				switch {
				case why != "":
					why = "Stays a task because " + why
				case r.AskedByOwner != nil:
					why = "Stays a task because you asked the AI for it"
					evidence = protocolEvidence(nil, r.AskedByOwner.At)
				case r.Origin == "owner" || r.CreatedBy.AgentID == "you":
					why = "Stays a task because you created it"
				default:
					why = "No automatic creation marker on record"
				}
				add(&t, "demote", "stays a task", why, evidence)
			}
		}
		effective := r.Task
		if demoted {
			effective.Status = "suggested"
		}
		if deadlinePassed(effective, now, loc) {
			add(&t, "deadline", "ends as done", "Its end time passed; the next local calendar day has begun", protocolEvidence(nil, *r.EndAt))
			t.Outcome = "Ends"
		} else if !demoted && obsoleteDue(r, in.active, now) {
			why := fmt.Sprintf("No activity for %d days", inactivityDays(r.Task))
			if r.Kind == "test" {
				why = "Test task, a day without activity"
			}
			active := in.active
			if active.IsZero() {
				active = r.CreatedAt
			}
			add(&t, "obsolete", "ends as dropped", why, protocolEvidence(nil, active))
			t.Outcome = "Ends"
		}
		if !demoted && r.Status == "suggested" && t.Outcome != "Ends" {
			switch {
			case r.Suggestion == nil:
				add(&t, "suggestion_expiry", "not checked", "No suggestion detection date in this copy", protocolEvidence(nil))
				t.Outcome = "Not checked"
			default:
				expires := r.Suggestion.DetectedAt.AddDate(0, 0, 14)
				if suggestionExpired(r.Task, now) {
					add(&t, "suggestion_expiry", "expires", "Suggestion reached its 14-day expiry", protocolEvidence(nil, expires))
					t.Outcome = "Ends"
				} else {
					add(&t, "suggestion_expiry", "stays a suggestion", "Suggestion expires 14 days after detection", protocolEvidence(nil, expires))
					if t.Outcome != "Ends" {
						t.Outcome = "Existing suggestions"
					}
				}
			}
		}
		if !demoted && ended(r.Task) {
			add(&t, "status", "already ended", "Its recorded status is already ended", protocolEvidence(nil))
			t.Outcome = "Already ended"
		}
		if !demoted && t.Outcome == "Stays a task" && duplicateOpen(r.Task) {
			remaining[len(out.Tasks)] = r.Task
		}
		out.Tasks = append(out.Tasks, t)
	}
	// Compare real tasks only, using the exact matcher used at creation. No merge.
	for i := range out.Tasks {
		a, ok := remaining[i]
		if !ok {
			continue
		}
		for j := i + 1; j < len(out.Tasks); j++ {
			b, ok := remaining[j]
			if !ok || !duplicate(a, b) {
				continue
			}
			for _, pair := range []struct {
				index int
				other Task
			}{{i, b}, {j, a}} {
				t := &out.Tasks[pair.index]
				add(t, "duplicate", "possible duplicate of "+pair.other.Title+", left for you", "Shared participants or chats and the same work; real tasks are never merged automatically", protocolEvidence([]string{pair.other.ID}))
				t.Outcome = "Left for you"
			}
		}
	}
	for i, in := range inputs {
		if _, ok := remaining[i]; !ok {
			continue
		}
		t := &out.Tasks[i]
		r := in.record
		if !in.messagesPresent {
			for _, rule := range []string{"finished", "follows_work"} {
				add(t, rule, "not checked", "needs messages, not in this copy", protocolEvidence(nil))
			}
			continue
		}
		// evaluateFinished changes only this value copy's hint fields.
		m := evaluateFinished(&r, in.messages, in.ownerAt, in.effortStatus, now)
		if m != nil {
			add(t, "finished", "ends as done", "Your own completion message refers to this task or a task file", protocolEvidence([]string{m.ID}, m.Timestamp))
			t.Outcome = "Ends"
		} else if checklistFinished(r.Task, r.PauseChecklistClose) {
			add(t, "finished", "ends as done", "Every checklist step is done", protocolEvidence(nil))
			t.Outcome = "Ends"
		} else if r.FinishHint != nil {
			add(t, "finished", "looks finished?", "Weak completion evidence; ask the owner before closing it", protocolEvidence(nil, r.FinishHint.At))
			t.Actions[len(t.Actions)-1].hint = &r
		} else {
			add(t, "finished", "no change", "No strong completion evidence", protocolEvidence(nil))
		}
		if r.EffortID != "" && !in.followsAvailable {
			add(t, "finished", "not checked", "The linked effort cannot be checked in this copy", protocolEvidence([]string{r.EffortID}))
			add(t, "follows_work", "not checked", "The current linked effort is not available in this copy", protocolEvidence([]string{r.EffortID}))
			continue
		}
		snapshot := in.follows
		snapshot.Task = r.Task
		automatic, legacy := followsWorkMembers(snapshot, false), FollowsWorkCandidates(snapshot)
		if len(automatic) > 0 {
			add(t, "follows_work", "remove departed members", fmt.Sprintf("%d effort-derived members no longer belong to this work; task references and owner additions stay", len(automatic)), protocolEvidence([]string{r.EffortID}))
			t.Actions[len(t.Actions)-1].members = automatic
		}
		if len(legacy) > 0 {
			add(t, "follows_work", "left for you", fmt.Sprintf("%d legacy members may belong to other work; no automatic removal", len(legacy)), protocolEvidence([]string{r.EffortID}))
			if t.Outcome == "Stays a task" {
				t.Outcome = "Left for you"
			}
		}
		if len(automatic)+len(legacy) == 0 {
			add(t, "follows_work", "no change", "No unprotected members proved to have left this work", protocolEvidence(nil))
		}
	}
	for _, t := range out.Tasks {
		if t.Outcome != "Not checked" {
			out.Counts[t.Outcome]++
		}
		for _, a := range t.Actions {
			if a.Action == "not checked" {
				out.Counts["Not checked"]++
			}
		}
	}
	return out
}

// WriteProtocolText prints a deliberately narrow projection. Evidence IDs are
// kept in JSON; text shows only dates, task titles/statuses, counts and AI names.
func WriteProtocolText(w io.Writer, plan ProtocolPlan) error {
	groups := []string{"Stays a task", "Becomes a suggestion", "Ends", "Left for you", "Not checked", "Existing suggestions", "Already ended"}
	if _, err := fmt.Fprintf(w, "Protocol dry run at %s — %d records\n", plan.At.Format(time.RFC3339), len(plan.Tasks)); err != nil {
		return err
	}
	for _, g := range groups {
		if _, err := fmt.Fprintf(w, "%s: %d\n", g, plan.Counts[g]); err != nil {
			return err
		}
	}
	for _, g := range groups {
		if _, err := fmt.Fprintf(w, "\n%s\n", g); err != nil {
			return err
		}
		for _, t := range plan.Tasks {
			if t.Outcome != g {
				continue
			}
			if _, err := fmt.Fprintf(w, "\n%s [%s] — %s; %s; %d people, %d chats\n", t.Title, t.Status, t.Origin, t.CreatorName, t.PeopleCount, t.ChatCount); err != nil {
				return err
			}
			for _, a := range t.Actions {
				if _, err := fmt.Fprintf(w, "  %s: %s — %s", a.Rule, a.Action, a.Reason); err != nil {
					return err
				}
				for _, at := range a.Evidence.Dates {
					if _, err := fmt.Fprintf(w, " (%s)", at.Format(time.RFC3339)); err != nil {
						return err
					}
				}
				if _, err := fmt.Fprintln(w); err != nil {
					return err
				}
			}
		}
	}
	_, err := fmt.Fprintln(w, "\nNot checked counts are rule checks, shown in the task blocks. Dry run only; no changes applied.")
	return err
}
