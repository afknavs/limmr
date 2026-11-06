package tasks

import (
	"fmt"
	"limmr/internal/testkit"
	"strings"
	"testing"
	"time"
)

func TestRealDataRecentlyNullPayloadSafeNamesFoldingAndPriority(t *testing.T) {
	s, a, now := fixture(t)
	v := create(t, s, a, "long")
	_, err := s.Hub.Repo.DB.Exec("DELETE FROM task_events WHERE task_id=?", v.ID)
	testkit.Must(t, err)
	_, err = s.Hub.Repo.DB.Exec("INSERT INTO task_events VALUES(?,?,?,?,?,?)", "progress", v.ID, now.Add(-48*time.Hour).Format(time.RFC3339Nano), "step_done", "Step completed", "null")
	testkit.Must(t, err)
	for i := 0; i < 3; i++ {
		*now = now.Add(time.Minute)
		_, err = s.Attach(a, v.ID, Item{Kind: "file", AttachmentID: "invented-attachment", Title: fmt.Sprintf("poster-%d.pdf", i), Text: "PRIVATE BODY", Caption: "PRIVATE CAPTION", OCRText: "PRIVATE OCR"})
		testkit.Must(t, err)
	}
	_, err = s.Hub.Repo.DB.Exec("UPDATE task_events SET payload='null' WHERE task_id=?", v.ID)
	testkit.Must(t, err)
	text, lines, err := s.RecentlyWithTargets(a, "")
	testkit.Must(t, err)
	joined := strings.Join(text, "\n")
	if len(lines) != 1 || !strings.Contains(lines[0].Text, "Step completed") || lines[0].By != "" || strings.Contains(joined, "Limmr") || strings.Contains(joined, "PRIVATE") || !strings.Contains(joined, "Added 3 files") {
		t.Fatalf("wrong recently: %v %+v", text, lines)
	}
	_, err = s.Hub.Repo.DB.Exec("DELETE FROM task_events WHERE kind='step_done'")
	testkit.Must(t, err)
	text, lines, err = s.RecentlyWithTargets(a, "")
	testkit.Must(t, err)
	if len(lines) != 1 || !strings.Contains(lines[0].Text, "Added 3 files") || !strings.Contains(strings.Join(text, "\n"), lines[0].Text) {
		t.Fatalf("fold/MCP mismatch: %v %+v", text, lines)
	}
	_, err = s.Hub.Repo.DB.Exec("DELETE FROM task_events")
	testkit.Must(t, err)
	*now = now.Add(2 * time.Hour)
	_, err = s.Attach(a, v.ID, Item{Kind: "file", AttachmentID: "invented-attachment", Title: "poster.pdf", Text: "PRIVATE"})
	testkit.Must(t, err)
	text, _, err = s.RecentlyWithTargets(a, "")
	testkit.Must(t, err)
	if !strings.Contains(strings.Join(text, " "), "Added a file: poster.pdf") {
		t.Fatal(text)
	}
	_, err = s.Hub.Repo.DB.Exec("DELETE FROM task_events")
	testkit.Must(t, err)
	*now = now.Add(2 * time.Hour)
	_, err = s.Attach(a, v.ID, Item{Kind: "link", Title: "Example", URL: "https://example.com/private-path?private-query=1", Text: "PRIVATE"})
	testkit.Must(t, err)
	text, _, err = s.RecentlyWithTargets(a, "")
	testkit.Must(t, err)
	joined = strings.Join(text, " ")
	if !strings.Contains(joined, "Added a link: example.com") || strings.Contains(joined, "private-") || strings.Contains(joined, "PRIVATE") {
		t.Fatal(text)
	}
	a.Permissions.ReadMessages = false
	text, _, err = s.RecentlyWithTargets(a, "")
	testkit.Must(t, err)
	if len(text) != 0 {
		t.Fatal("revoked metadata leaked", text)
	}
}
