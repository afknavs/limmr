package tasks

import (
	"sort"
	"strings"
)

func chatSet(chats []Entity) string {
	ids := map[string]bool{}
	for _, c := range chats {
		ids[c.ID] = true
	}
	keys := []string{}
	for id := range ids {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	return strings.Join(keys, "\x00")
}
func detectorChatsCovered(chats []Entity, records []record) bool {
	if len(chats) == 0 {
		return false
	}
	covered := map[string]bool{}
	for _, r := range records {
		if r.Purged {
			continue
		}
		if r.Status == "suggested" && chatSet(chats) == chatSet(r.Chats) {
			return true
		}
		if r.Status == "active" || r.Status == "waiting" || r.Status == "quiet" {
			for _, ch := range r.Chats {
				covered[ch.ID] = true
			}
		}
	}
	for _, ch := range chats {
		if !covered[ch.ID] {
			return false
		}
	}
	return true
}
