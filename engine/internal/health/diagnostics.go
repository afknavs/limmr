package health

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func diagnosticCheckID(id string) string {
	switch id {
	case "version", "whatsapp", "phone", "history", "messages_db", "hub_db", "sync_log", "logs", "local_mcp", "remote", "remote_listener", "tasks", "watches", "old_bridge", "demo_history", "engine_restarts":
		return id
	default:
		if strings.HasPrefix(id, "agent_") {
			return "ai_app"
		}
		return "other"
	}
}
func (s *Service) diagnosticLog() string {
	f, err := os.Open(filepath.Join(s.Home, "logs", "engine.log"))
	if err != nil {
		return "Recent warnings: unavailable\n"
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return ""
	}
	offset := max(int64(0), info.Size()-65536)
	_, _ = f.Seek(offset, io.SeekStart)
	tail, _ := io.ReadAll(io.LimitReader(f, 65536))
	lines := strings.Split(string(tail), "\n")
	if offset > 0 && len(lines) > 0 {
		lines = lines[1:]
	}
	warnings := []string{}
	timings := []string{}
	stages := map[string]bool{"hub.open": true, "messages.open": true, "listener": true, "migration.sync": true, "migration.sync_storage": true, "storage.maintenance": true, "warmup.hub": true, "warmup.people": true, "warmup.circles": true, "remote.start": true}
	for _, line := range lines {
		// Never copy a log line. Even an unknown warning with message text or a token
		// produces only a fixed category. Timestamps are parsed and formatted anew.
		at := ""
		if len(line) >= 19 {
			if t, e := time.Parse("2006/01/02 15:04:05", line[:19]); e == nil {
				at = t.UTC().Format(time.RFC3339) + " "
			}
		}
		if strings.Contains(line, "WARN ") {
			kind := "Engine warning (private details omitted)"
			switch {
			case strings.Contains(line, "engine watchdog:"):
				kind = "Engine stopped responding; restart requested"
			case strings.Contains(line, "sync storage maintenance deferred"):
				kind = "Storage cleanup saved for later"
			case strings.Contains(line, "remote listener unavailable"):
				kind = "Remote access unavailable"
			case strings.Contains(line, "WhatsApp reconnect unavailable"):
				kind = "WhatsApp reconnect unavailable"
			}
			warnings = append(warnings, at+kind)
		}
		fields := strings.Fields(line)
		for i, field := range fields {
			if strings.HasPrefix(field, "stage=") && stages[strings.TrimPrefix(field, "stage=")] && i+1 < len(fields) && strings.HasPrefix(fields[i+1], "duration_ms=") {
				if ms, e := strconv.ParseFloat(strings.TrimPrefix(fields[i+1], "duration_ms="), 64); e == nil && ms >= 0 && ms < 3600000 {
					timings = append(timings, fmt.Sprintf("%s %s duration_ms=%.3f", at, field, ms))
				}
			}
		}
	}
	if len(warnings) > 10 {
		warnings = warnings[len(warnings)-10:]
	}
	if len(timings) > 15 {
		timings = timings[len(timings)-15:]
	}
	return "Recent warnings:\n" + strings.Join(warnings, "\n") + "\nRecent startup timings:\n" + strings.Join(timings, "\n") + "\n"
}
