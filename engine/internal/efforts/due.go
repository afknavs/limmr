package efforts

import (
	"limmr/internal/model"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var deadlineWeek = regexp.MustCompile(`(?i)\b(?:due(?:\s+on)?|by|before|deadline(?:\s+is)?)\s+(sunday|monday|tuesday|wednesday|thursday|friday|saturday|sun|mon|tue|wed|thu|fri|sat)\b`)
var relativeDate = regexp.MustCompile(`(?i)\b(today|tomorrow)\b`)
var deadlineRelative = regexp.MustCompile(`(?i)\b(?:due|by|before|deadline(?:\s+is)?)\s+(today|tomorrow)\b`)
var explicitDate = regexp.MustCompile(`(?i)\b(\d{4}-\d{2}-\d{2}|\d{1,2}\s+(?:jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|jun(?:e)?|jul(?:y)?|aug(?:ust)?|sep(?:tember)?|oct(?:ober)?|nov(?:ember)?|dec(?:ember)?)(?:\s+\d{4})?)\b`)
var clockTime = regexp.MustCompile(`(?i)^\s*(?:at\s+)?(\d{1,2})(?::(\d{2}))?\s*(am|pm)\b|^\s*(?:at\s+)?(\d{1,2}):(\d{2})\b`)

func dateClock(day time.Time, suffix string) time.Time {
	day = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	hour, minute := 0, 0
	if x := clockTime.FindStringSubmatch(suffix); x != nil {
		if x[4] != "" {
			hour, _ = strconv.Atoi(x[4])
			minute, _ = strconv.Atoi(x[5])
		} else {
			hour, _ = strconv.Atoi(x[1])
			minute, _ = strconv.Atoi(x[2])
			if hour < 1 || hour > 12 {
				return day
			}
			hour %= 12
			if strings.EqualFold(x[3], "pm") {
				hour += 12
			}
		}
		if hour > 23 || minute > 59 {
			return day
		}
	}
	return time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, day.Location())
}
func messageDue(m model.Message) *time.Time {
	text := m.Content
	// Expressions are inspected in textual order. A later explicit expression wins.
	type hit struct {
		pos, end int
		date     time.Time
	}
	hits := []hit{}
	for _, loc := range explicitDate.FindAllStringIndex(text, -1) {
		value := text[loc[0]:loc[1]]
		var d time.Time
		for _, layout := range []string{"2006-01-02", "2 Jan 2006", "2 January 2006", "2 Jan", "2 January"} {
			parsed, e := time.ParseInLocation(layout, value, m.Timestamp.Location())
			if e != nil {
				continue
			}
			d = parsed
			if parsed.Year() == 0 {
				d = time.Date(m.Timestamp.Year(), parsed.Month(), parsed.Day(), 0, 0, 0, 0, m.Timestamp.Location())
				if d.Before(time.Date(m.Timestamp.Year(), m.Timestamp.Month(), m.Timestamp.Day(), 0, 0, 0, 0, m.Timestamp.Location())) {
					d = d.AddDate(1, 0, 0)
				}
			}
			break
		}
		if !d.IsZero() {
			hits = append(hits, hit{loc[0], loc[1], d})
		}
	}
	for _, loc := range deadlineWeek.FindAllStringSubmatchIndex(text, -1) {
		name := strings.ToLower(text[loc[2]:loc[3]])[:3]
		for i, day := range []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"} {
			if name == day {
				d := m.Timestamp.AddDate(0, 0, (i-int(m.Timestamp.Weekday())+7)%7)
				hits = append(hits, hit{loc[0], loc[1], d})
			}
		}
	}
	for _, loc := range relativeDate.FindAllStringIndex(text, -1) {
		// A bare weekday is conversation; a relative date with a clock is explicit.
		prefix := text[:loc[1]]
		if !deadlineRelative.MatchString(prefix) && !clockTime.MatchString(text[loc[1]:]) {
			continue
		}
		days := 0
		if strings.EqualFold(text[loc[0]:loc[1]], "tomorrow") {
			days = 1
		}
		hits = append(hits, hit{loc[0], loc[1], m.Timestamp.AddDate(0, 0, days)})
	}
	var result *time.Time
	last := -1
	for _, h := range hits {
		if h.pos >= last {
			d := dateClock(h.date, text[h.end:])
			result = &d
			last = h.pos
		}
	}
	return result
}
func due(ms []model.Message) *time.Time {
	var result *time.Time
	for _, m := range ms {
		if d := messageDue(m); d != nil {
			result = d
		}
	}
	return result
}
