package efforts

import (
	"limmr/internal/model"
	"strings"
	"time"
)

// Audit contains only aggregate counts. Filtered message counts (self/otp/media/forward)
// and rejected candidate counts have different units. Untitled strong efforts
// are retained and counted under naming_queued, never under filtered candidates.
type Audit struct {
	MessagesFiltered   map[string]int `json:"messages_filtered"`
	CandidatesFiltered map[string]int `json:"candidates_filtered"`
	Named              int            `json:"named_efforts"`
	Internal           int            `json:"internal_efforts"`
	NamingQueued       int            `json:"naming_queued"`
	TooWeak            WeakHistogram  `json:"too_weak_histogram"`
	Accepted           SizeHistogram  `json:"accepted_effort_size_histogram"`
	Largest            LargestEffort  `json:"largest_effort"`
	Confidence         map[string]int `json:"confidence_histogram"`
	TitleSources       map[string]int `json:"title_source_counts"`
	DuplicateTitles    int            `json:"duplicate_titles"`
	GenericTitles      int            `json:"generic_titles"`
}

type LargestEffort struct {
	Messages int `json:"messages"`
	Chats    int `json:"chats"`
}

type SizeHistogram struct {
	Messages map[string]int `json:"messages"`
	Chats    map[string]int `json:"chats"`
	People   map[string]int `json:"people"`
	SpanDays map[string]int `json:"span_days"`
}

func newSizeHistogram() SizeHistogram {
	return SizeHistogram{
		Messages: buckets("1", "2", "3-5", "6-10", "11-20", "21-50", "51-128", "129-999", "1000+"),
		Chats:    buckets("1", "2", "3-4", "5-14", "15-31", "32+"),
		People:   buckets("0", "1", "2", "3-4", "5-14", "15-31", "32+"),
		SpanDays: buckets("<1", "1-2", "3-6", "7-13", "14-29", "30+"),
	}
}

func incrementBucket(axis map[string]int, n int, bounds []int, labels []string) {
	for i, bound := range bounds {
		if n <= bound {
			axis[labels[i]]++
			return
		}
	}
	axis[labels[len(labels)-1]]++
}

func (h SizeHistogram) add(e Effort, messages map[string]model.Message) {
	incrementBucket(h.Messages, len(e.MessageRefs), []int{1, 2, 5, 10, 20, 50, 128, 999}, []string{"1", "2", "3-5", "6-10", "11-20", "21-50", "51-128", "129-999", "1000+"})
	incrementBucket(h.Chats, len(e.Chats), []int{1, 2, 4, 14, 31}, []string{"1", "2", "3-4", "5-14", "15-31", "32+"})
	people := map[string]bool{}
	for _, r := range e.MessageRefs {
		if p := speaker(messages[key(r)]); p != "" {
			people[p] = true
		}
	}
	incrementBucket(h.People, len(people), []int{0, 1, 2, 4, 14, 31}, []string{"0", "1", "2", "3-4", "5-14", "15-31", "32+"})
	incrementBucket(h.SpanDays, int(e.LastAt.Sub(e.FirstAt)/(24*time.Hour)), []int{0, 2, 6, 13, 29}, []string{"<1", "1-2", "3-6", "7-13", "14-29", "30+"})
}

// Every axis counts the same rejected candidates once. Keys are fixed buckets,
// never vocabulary, filenames, IDs, participant labels or message text.
type WeakHistogram struct {
	MessageCount map[string]int `json:"message_count"`
	Participants map[string]int `json:"distinct_participants"`
	SpanDays     map[string]int `json:"span_days"`
	TopicSignal  map[string]int `json:"topic_signal_score"`
	Artefacts    map[string]int `json:"shared_file_link"`
	ChatKind     map[string]int `json:"chat_kind"`
}

func buckets(keys ...string) map[string]int {
	out := map[string]int{}
	for _, key := range keys {
		out[key] = 0
	}
	return out
}

func newWeakHistogram() WeakHistogram {
	return WeakHistogram{
		MessageCount: buckets("1", "2", "3-5", "6-10", "11-20", "21+"),
		Participants: buckets("0", "1", "2", "3", "4+"),
		SpanDays:     buckets("<1", "1-2", "3-6", "7-13", "14-29", "30+"),
		TopicSignal:  buckets("0", "1", "2", "3", "4-5", "6+"),
		Artefacts:    buckets("neither", "file", "link", "file-and-link"),
		ChatKind:     buckets("direct", "group", "mixed"),
	}
}

func (h WeakHistogram) axes() [6]map[string]int {
	return [6]map[string]int{h.MessageCount, h.Participants, h.SpanDays, h.TopicSignal, h.Artefacts, h.ChatKind}
}

// Cache only fixed bucket labels, not per-cluster maps or identifying content.
// Unchanged rejected segments need no tokenization on incremental snapshots.
func (h WeakHistogram) addBins(bins [6]string) {
	for i, axis := range h.axes() {
		axis[bins[i]]++
	}
}

func rejectionBins(ms []model.Message, q qualityRules) [6]string {
	var bins [6]string
	n := len(ms)
	switch {
	case n == 1:
		bins[0] = "1"
	case n == 2:
		bins[0] = "2"
	case n <= 5:
		bins[0] = "3-5"
	case n <= 10:
		bins[0] = "6-10"
	case n <= 20:
		bins[0] = "11-20"
	default:
		bins[0] = "21+"
	}
	speakers := map[string]bool{}
	first, last := ms[0].Timestamp, ms[0].Timestamp
	file, link, group, direct := false, false, false, false
	for _, m := range ms {
		if p := speaker(m); p != "" {
			speakers[p] = true
		}
		if m.Timestamp.Before(first) {
			first = m.Timestamp
		}
		if m.Timestamp.After(last) {
			last = m.Timestamp
		}
		file = file || (m.MediaType == "document" && goodFile(m.Filename))
		link = link || linkPattern.MatchString(m.Content)
		if strings.HasSuffix(m.ChatJID, "@g.us") {
			group = true
		} else {
			direct = true
		}
	}
	switch len(speakers) {
	case 0:
		bins[1] = "0"
	case 1:
		bins[1] = "1"
	case 2:
		bins[1] = "2"
	case 3:
		bins[1] = "3"
	default:
		bins[1] = "4+"
	}
	days := int(last.Sub(first) / (24 * time.Hour))
	switch {
	case days < 1:
		bins[2] = "<1"
	case days < 3:
		bins[2] = "1-2"
	case days < 7:
		bins[2] = "3-6"
	case days < 14:
		bins[2] = "7-13"
	case days < 30:
		bins[2] = "14-29"
	default:
		bins[2] = "30+"
	}
	// Score = maximum number of messages corroborating the same two terms;
	// vocabulary extraction and the 64-term bound match the production gate.
	score := 0
	if len(ms) == 1 {
		if len(q.terms(ms[0])) >= 2 {
			score = 1
		}
	} else {
		for _, ev := range termPairs(ms, q) {
			score = max(score, ev.n)
		}
	}
	switch {
	case score == 0:
		bins[3] = "0"
	case score == 1:
		bins[3] = "1"
	case score == 2:
		bins[3] = "2"
	case score == 3:
		bins[3] = "3"
	case score < 6:
		bins[3] = "4-5"
	default:
		bins[3] = "6+"
	}
	switch {
	case file && link:
		bins[4] = "file-and-link"
	case file:
		bins[4] = "file"
	case link:
		bins[4] = "link"
	default:
		bins[4] = "neither"
	}
	switch {
	case group && direct:
		bins[5] = "mixed"
	case group:
		bins[5] = "group"
	default:
		bins[5] = "direct"
	}
	return bins
}

func (h WeakHistogram) add(ms []model.Message, q qualityRules) { h.addBins(rejectionBins(ms, q)) }

func (s *Service) Audit() (Audit, error) {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	c, err := s.snapshot(nil)
	if err != nil {
		return Audit{}, err
	}
	out := Audit{MessagesFiltered: map[string]int{}, CandidatesFiltered: map[string]int{}, TooWeak: c.weak, Accepted: newSizeHistogram(), Confidence: buckets("<0.50", "0.50-0.59", "0.60-0.69", "0.70-0.79", "0.80-0.89", "0.90-1.00"), TitleSources: buckets("request_phrase", "document_name", "naming_queue", "assigned_title")}
	for _, reason := range []string{"otp", "media", "self", "forward"} {
		out.MessagesFiltered[reason] = c.filtered[reason]
	}
	out.CandidatesFiltered["too-weak"] = c.filtered["too-weak"]
	out.DuplicateTitles, out.GenericTitles = titleCounts(c.efforts)
	for _, e := range c.efforts {
		out.Accepted.add(e, c.messages)
		if len(e.MessageRefs) > out.Largest.Messages || len(e.MessageRefs) == out.Largest.Messages && len(e.Chats) > out.Largest.Chats {
			out.Largest = LargestEffort{len(e.MessageRefs), len(e.Chats)}
		}
		incrementBucket(out.Confidence, int(e.Confidence*100+0.5), []int{49, 59, 69, 79, 89}, []string{"<0.50", "0.50-0.59", "0.60-0.69", "0.70-0.79", "0.80-0.89", "0.90-1.00"})
		source := e.titleSource
		if !goodTitle(e.Title) {
			source = "naming_queue"
		}
		out.TitleSources[source]++
		if goodTitle(e.Title) {
			out.Named++
		} else {
			out.Internal++
			if !e.Hidden {
				out.NamingQueued++
			}
		}
	}
	return out, nil
}
