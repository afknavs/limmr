package tasks

import (
	"limmr/internal/testkit"
	"strings"
	"testing"
)

func TestN1OverviewTargetsPreserveRecentlyAndVisibility(t *testing.T) {
	s, a, _ := fixture(t)
	task := create(t, s, a, "long")
	_, err := s.OwnerNote(nil, task.ID, "", "PRIVATE NOTE BODY", false)
	testkit.Must(t, err)
	legacy, err := s.Recently(a, "")
	testkit.Must(t, err)
	text, lines, err := s.RecentlyWithTargets(a, "")
	testkit.Must(t, err)
	if strings.Join(legacy, "\n") != strings.Join(text, "\n") {
		t.Fatal("MCP text changed")
	}
	if len(lines) != 1 || lines[0].TargetID != task.ID || lines[0].Kind != "task" || strings.Contains(lines[0].Text, "PRIVATE NOTE") {
		t.Fatalf("unsafe or missing navigation targets: %+v", lines)
	}
	settings := s.Hub.Settings()
	settings.HiddenChats = []string{testkit.Phone}
	testkit.Must(t, s.Hub.SetSettings(settings))
	_, lines, err = s.RecentlyWithTargets(a, "")
	testkit.Must(t, err)
	if len(lines) != 0 {
		t.Fatal("hidden task leaked through target IDs")
	}
	a.Permissions.ReadMessages = false
	_, lines, err = s.RecentlyWithTargets(a, "")
	testkit.Must(t, err)
	if len(lines) != 0 {
		t.Fatal("revoked agent retained targets")
	}
}
