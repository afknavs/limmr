package efforts

import (
	"fmt"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// All identities, documents and conversations are invented. Bursts resume after
// multi-day gaps; wording varies, and short replies rely on explicit context.
func e5Round2Shapes() []recallCase {
	cases := []recallCase{}
	for _, spec := range []struct {
		name, topic, file      string
		n, chats, days, people int
	}{
		{"sustained document draft", "cedar manuscript", "Cedar-Manuscript.docx", 170, 5, 21, 4},
		{"exam preparation", "saffron exam revision", "Saffron-Revision-Plan.pdf", 65, 5, 14, 3},
		{"direct pitch discussion", "juniper pitch", "", 390, 1, 18, 2},
		{"production request", "orchid production report", "Orchid-Production-Report.pdf", 4, 1, 1, 2},
	} {
		ms := []model.Message{}
		roots := map[string]string{}
		// Four sessions, with gaps of days between them, spread across all chats.
		for i := 0; i < spec.n; i++ {
			chat := fmt.Sprintf("r2-%s-%d@g.us", strings.Fields(spec.topic)[0], i%spec.chats)
			if spec.chats == 1 {
				chat = fmt.Sprintf("155507%05d@s.whatsapp.net", spec.n)
			}
			session := i * 4 / spec.n
			at := testkit.Epoch.AddDate(0, 0, -spec.days+session*spec.days/3).Add(-time.Duration(spec.n-i) * time.Minute)
			text := []string{spec.topic + " references need a clearer explanation", spec.topic + " section ordering looks consistent", spec.topic + " changes are ready for another review", spec.topic + " compare the revised outline", spec.topic + " feedback incorporated"}[(i/spec.chats)%5]
			if i == 0 {
				text = "Please review the " + spec.topic + "; due tomorrow"
			}
			m := model.Message{ID: fmt.Sprintf("%s-%d", strings.Fields(spec.topic)[0], i), ChatJID: chat, Sender: fmt.Sprintf("155508%05d@s.whatsapp.net", i%spec.people), IsFromMe: i%spec.people == 1, Content: text, Timestamp: at}
			if root, ok := roots[chat]; ok {
				m.QuotedMessageID = root
				if i%4 == 3 {
					m.Content = "I will fix that section"
				}
			} else {
				roots[chat] = m.ID
			}
			if spec.file != "" && i%17 == 0 {
				m.MediaType, m.Filename = "document", spec.file
			}
			ms = append(ms, m)
		}
		cases = append(cases, recallCase{spec.name, true, ms})
	}
	for _, spec := range []struct{ n, chats int }{{3200, 32}, {1600, 15}} {
		ms := []model.Message{}
		for i := 0; i < spec.n; i++ {
			// Recurring work filenames and generic class vocabulary must not supply
			// the missing body-topic and stable task-participant evidence.
			m := model.Message{ID: fmt.Sprintf("blob-%d-%d", spec.n, i), ChatJID: fmt.Sprintf("ambient-%d-%d@g.us", spec.n, i%spec.chats), Sender: fmt.Sprintf("155509%05d@s.whatsapp.net", i%37), Content: fmt.Sprintf("class notes tangent%d lunch%d", i/3, i%19), Timestamp: testkit.Epoch.Add(-time.Duration(spec.n-i) * 20 * time.Minute)}
			if i%3 == 0 {
				m.MediaType, m.Filename = "document", "Shared-Research-Notes (1).pdf"
			}
			ms = append(ms, m)
		}
		cases = append(cases, recallCase{fmt.Sprintf("ambient blob %d messages %d chats", spec.n, spec.chats), false, ms})
	}
	return cases
}

func putRound2(t *testing.T, s *Service, ms []model.Message) {
	t.Helper()
	sort.Slice(ms, func(i, j int) bool {
		if ms[i].Timestamp.Equal(ms[j].Timestamp) {
			return key(ref(ms[i])) < key(ref(ms[j]))
		}
		return ms[i].Timestamp.Before(ms[j].Timestamp)
	})
	chats := map[string]bool{}
	for _, m := range ms {
		if !chats[m.ChatJID] {
			testkit.Must(t, s.Hub.Messages.PutChat(model.Chat{JID: m.ChatJID}))
			chats[m.ChatJID] = true
		}
		testkit.Must(t, s.Hub.Messages.PutMessage(m))
	}
}

func exactRound2(e Effort, ms []model.Message) bool {
	if len(e.MessageRefs) != len(ms) {
		return false
	}
	refs := map[string]bool{}
	for _, m := range ms {
		refs[key(ref(m))] = true
	}
	for _, r := range e.MessageRefs {
		if !refs[key(r)] {
			return false
		}
	}
	return true
}

func TestE5Round2Shapes(t *testing.T) {
	tp, fp, fn := 0, 0, 0
	for _, tc := range e5Round2Shapes() {
		t.Run(tc.name, func(t *testing.T) {
			s := qualitySeed(t, nil)
			putRound2(t, s, tc.ms)
			xs, err := s.List(nil, Filter{IncludeUntitled: true, Limit: 500})
			testkit.Must(t, err)
			found := false
			sizes := []int{}
			for _, e := range xs {
				sizes = append(sizes, len(e.MessageRefs))
				if tc.positive && exactRound2(e, tc.ms) && !found {
					tp++
					found = true
				} else {
					fp++
				}
			}
			if tc.positive && !found {
				fn++
				t.Error("sustained task fragmented or missing")
			}
			if !tc.positive && len(xs) > 0 {
				t.Error("ambient blob promoted")
			}
			t.Logf("expected=%d positive=%v returned_sizes=%v", len(tc.ms), tc.positive, sizes)
		})
	}
	t.Logf("round2 shapes TP=%d FP=%d FN=%d precision=%.4f recall=%.4f", tp, fp, fn, float64(tp)/float64(max(1, tp+fp)), float64(tp)/4)
}

func round2MixedCorpus() ([]model.Message, []recallCase) {
	tasks := e5Round2Shapes()[:4]
	all := []model.Message{}
	for _, tc := range tasks {
		all = append(all, tc.ms...)
	}
	for i := 0; i < 8356; i++ {
		n := 1
		if i >= 6745 {
			n = 2
		}
		if i >= 8000 {
			n = 3
		}
		for j := 0; j < n; j++ {
			text := fmt.Sprintf("fragment%d detail%d", i, i)
			quote := ""
			at := testkit.Epoch.Add(-time.Duration(20000+i*3-j) * time.Minute)
			if j == 2 {
				text = fmt.Sprintf("aside%d", i)
				quote = fmt.Sprintf("noise-%d-0", i)
				if i >= 8310 {
					at = at.Add(48 * time.Hour)
				}
			}
			all = append(all, model.Message{ID: fmt.Sprintf("noise-%d-%d", i, j), ChatJID: "tiny-segments@g.us", Sender: testkit.Phone, Content: text, QuotedMessageID: quote, Timestamp: at})
		}
	}
	// Ambient negatives coexist with the sustained threads, rather than being
	// evaluated only in separately labelled conversations.
	for _, tc := range e5Round2Shapes()[4:] {
		all = append(all, tc.ms...)
	}
	return all, tasks
}

func TestE5Round2LiveShapedDistribution(t *testing.T) {
	all, tasks := round2MixedCorpus()
	s := qualitySeed(t, nil)
	putRound2(t, s, all)
	start := time.Now()
	xs, err := s.List(nil, Filter{IncludeUntitled: true, Limit: 500})
	testkit.Must(t, err)
	t.Logf("mixed corpus cold=%s messages=%d efforts=%d", time.Since(start), len(all), len(xs))
	if len(xs) != len(tasks) {
		t.Errorf("want exactly four sustained tasks, got %d efforts", len(xs))
	}
	for _, tc := range tasks {
		found := false
		for _, e := range xs {
			found = found || exactRound2(e, tc.ms)
		}
		if !found {
			t.Errorf("lost complete %s (%d refs)", tc.name, len(tc.ms))
		}
	}
	audit, err := s.Audit()
	testkit.Must(t, err)
	t.Logf("accepted_sizes=%v weak_sizes=%v weak_spans=%v confidence=%v titles=%v", audit.Accepted.Messages, audit.TooWeak.MessageCount, audit.TooWeak.SpanDays, audit.Confidence, audit.TitleSources)
	// E7 discards short non-task fragments before clustering. Only the
	// ambient blobs remain in these weak bins; none becomes an effort.
	if !reflect.DeepEqual(audit.TooWeak.MessageCount, map[string]int{"1": 1600, "2": 1600, "3-5": 0, "6-10": 0, "11-20": 0, "21+": 0}) {
		t.Errorf("unexpected post-filler weak population: %v", audit.TooWeak.MessageCount)
	}
	if audit.Largest.Messages != 390 || audit.Largest.Chats != 1 {
		t.Error("sustained direct task truncated")
	}
	// Append a chronological, quoted continuation after the previous snapshot.
	pitch := tasks[2].ms
	m := pitch[len(pitch)-1]
	m.ID = "pitch-arrival"
	m.Timestamp = testkit.Epoch
	m.Content = "Juniper pitch pricing is agreed"
	m.QuotedMessageID = pitch[0].ID
	testkit.Must(t, s.Hub.Messages.PutMessage(m))
	start = time.Now()
	updated, err := s.List(nil, Filter{IncludeUntitled: true, Limit: 500})
	testkit.Must(t, err)
	elapsed := time.Since(start)
	freshService := &Service{Hub: s.Hub}
	cold, err := freshService.List(nil, Filter{IncludeUntitled: true, Limit: 500})
	testkit.Must(t, err)
	if !reflect.DeepEqual(updated, cold) {
		t.Error("incremental and cold sustained-task inference differ")
	}
	if len(updated) != 4 {
		t.Error("arrival fragmented sustained tasks")
	}
	wantPitch := append(append([]model.Message{}, pitch...), m)
	foundPitch := false
	for _, e := range updated {
		foundPitch = foundPitch || exactRound2(e, wantPitch)
	}
	if !foundPitch {
		t.Error("incremental arrival lost the complete 391-message pitch")
	}
	updatedAudit, err := s.Audit()
	testkit.Must(t, err)
	coldAudit, err := freshService.Audit()
	testkit.Must(t, err)
	if !reflect.DeepEqual(updatedAudit, coldAudit) {
		t.Error("cached weak buckets differ from cold audit")
	}
	t.Logf("mixed corpus incremental=%s", elapsed)
	if elapsed >= 200*time.Millisecond {
		t.Errorf("incremental recompute exceeds 200ms: %s", elapsed)
	}
}

func TestE5Round2RejoinBusySegmentsAndChatBound(t *testing.T) {
	var pieces []*cluster
	for session := 0; session < 2; session++ {
		g := newGrouper()
		for i, text := range []string{"Please review cedar manuscript; due tomorrow", "Cedar manuscript references updated", "Cedar manuscript section approved"} {
			m := model.Message{ID: fmt.Sprintf("session-%d-%d", session, i), ChatJID: testkit.Group, Sender: testkit.Phone, IsFromMe: i == 1, Content: text, Timestamp: testkit.Epoch.AddDate(0, 0, -14+session*14).Add(-time.Duration(3-i) * time.Minute)}
			if session == 1 {
				m.Content = strings.TrimPrefix(m.Content, "Please review ")
			}
			g.add([]model.Message{m})
		}
		pieces = append(pieces, g.cs...)
	}
	joined := consolidateClusters(pieces, map[string]bool{testkit.Group: true})
	if len(joined) != 1 || len(joined[0].ms) != 6 {
		t.Fatal("busy same-chat pieces did not rejoin across two weeks")
	}
	if len(pieces) != 2 || len(pieces[0].ms) != 3 || len(pieces[1].ms) != 3 {
		t.Fatal("consolidation mutated source segments")
	}
	ms := []model.Message{}
	for i := 0; i < 45; i++ {
		ms = append(ms, model.Message{ID: fmt.Sprint(i), ChatJID: fmt.Sprintf("bound-%d@g.us", i%9), Sender: testkit.Phone, IsFromMe: i%2 == 1, Content: "Cedar manuscript review", Timestamp: testkit.Epoch.Add(-time.Duration(45-i) * time.Minute)})
	}
	cs := dedupeClusters(group(ms))
	total := 0
	for _, c := range cs {
		total += len(c.ms)
		chats := map[string]bool{}
		for _, m := range c.ms {
			chats[m.ChatJID] = true
		}
		if len(chats) > 8 {
			t.Fatal("ninth chat bypassed cohesion bounds")
		}
	}
	if len(cs) != 2 || total != 45 {
		t.Fatal("chat overflow lost evidence instead of splitting")
	}
	// Exact quotes can carry short coordination, but many off-topic replies
	// must eventually split instead of dominating the original task centroid.
	drift := []model.Message{}
	for i := 0; i < 80; i++ {
		m := model.Message{ID: fmt.Sprintf("drift-%d", i), ChatJID: testkit.Phone, Sender: testkit.Phone, IsFromMe: i%2 == 1, Content: "Cedar manuscript review", Timestamp: testkit.Epoch.Add(-time.Duration(80-i) * time.Minute)}
		if i >= 10 {
			m.Content = "Cobalt poster colors"
			m.QuotedMessageID = "drift-0"
		}
		drift = append(drift, m)
	}
	cs = dedupeClusters(group(drift))
	if len(cs) < 2 || len(cs[0].ms) >= 30 {
		t.Fatal("quoted off-topic chatter defeated the cohesion split")
	}
}

func TestE5Round2SustainedTaskInsideBusyGroups(t *testing.T) {
	task := e5Round2Shapes()[0]
	s := qualitySeed(t, nil)
	background := []model.Message{}
	for i := 0; i < 400; i++ {
		background = append(background, model.Message{ID: fmt.Sprintf("ambient-aside-%d", i), ChatJID: task.ms[i%5].ChatJID, Sender: testkit.Phone, Content: fmt.Sprintf("canteen chatter%d", i), Timestamp: testkit.Epoch.AddDate(0, 0, -30).Add(time.Duration(i) * time.Minute)})
	}
	putRound2(t, s, append(background, task.ms[:85]...))
	xs, err := s.List(nil, Filter{IncludeUntitled: true, Limit: 500})
	testkit.Must(t, err)
	if len(xs) != 1 || !exactRound2(xs[0], task.ms[:85]) {
		sizes := []int{}
		refs := map[string]bool{}
		for _, e := range xs {
			sizes = append(sizes, len(e.MessageRefs))
			for _, r := range e.MessageRefs {
				refs[r.MessageID] = true
			}
		}
		missing := []string{}
		for _, m := range task.ms[:85] {
			if !refs[m.ID] {
				missing = append(missing, m.ID)
			}
		}
		t.Fatalf("first two sessions fragmented inside ambient groups: sizes=%v missing=%v", sizes, missing)
	}
	for _, m := range task.ms[85:] {
		testkit.Must(t, s.Hub.Messages.PutMessage(m))
	}
	xs, err = s.List(nil, Filter{IncludeUntitled: true, Limit: 500})
	testkit.Must(t, err)
	if len(xs) != 1 || !exactRound2(xs[0], task.ms) {
		t.Fatal("busy-group continuation lost short quoted replies across multi-day gaps")
	}
	cold, err := (&Service{Hub: s.Hub}).List(nil, Filter{IncludeUntitled: true, Limit: 500})
	testkit.Must(t, err)
	if !reflect.DeepEqual(xs, cold) {
		t.Fatal("busy sustained task differs after cold recompute")
	}
}
