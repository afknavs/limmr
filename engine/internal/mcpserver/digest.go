package mcpserver

import (
	"limmr/internal/hub"
	"limmr/internal/model"
)

func digestTool() Tool {
	t := def("get_digest", "A summary of what changed for the owner. Use when the owner asks what they missed.", "", "period circle_id cursor")
	p := t.InputSchema["properties"].(map[string]any)
	p["period"] = map[string]any{"type": "string", "enum": []string{"today", "since_last", "week"}, "default": "today"}
	p["cursor"] = map[string]any{"type": "string", "description": "Optional cursor from a previous digest with the same access and circle, for since_last. Without it, since_last resumes your last successful digest."}
	return t
}

func (s *Service) callDigest(a model.Agent, args Args, act *model.Activity) (any, error) {
	d, err := s.Hub.GetDigest(&a, hub.DigestOptions{Period: args.str("period"), CircleID: args.str("circle_id"), Cursor: args.str("cursor")}, s.CircleService())
	if err != nil {
		return nil, err
	}
	// Keep the digest itself out of message-read totals. Its summaries do not
	// represent full message reads or advance the circle catch-up cursor.
	for _, update := range d.CircleUpdates {
		c, e := s.CircleService().Circle(&a, update.CircleID)
		if e != nil {
			return nil, e
		}
		for _, chat := range c.Chats {
			if !model.Contains(act.ChatJIDs, chat.ChatJID) {
				act.ChatJIDs = append(act.ChatJIDs, chat.ChatJID)
			}
		}
	}
	return d, err
}
