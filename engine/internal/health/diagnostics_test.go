package health

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiagnosticLogNeverCopiesPrivateText(t *testing.T) {
	home := t.TempDir()
	_ = os.Mkdir(filepath.Join(home, "logs"), 0700)
	_ = os.WriteFile(filepath.Join(home, "logs", "engine.log"), []byte("2026/10/28 10:00:00 WARN CANARY_TOKEN +15550100099 CANARY_MESSAGE\n2026/10/28 10:00:01 WARN engine watchdog: HTTP ping stopped responding; restarting engine CANARY\n2026/10/28 10:00:02 INFO startup stage=hub.open duration_ms=1.25 CANARY\n2026/10/28 10:00:03 INFO startup stage=CANARY duration_ms=123\n"), 0600)
	report := (&Service{Home: home}).diagnosticLog()
	for _, canary := range []string{"CANARY", "15550100099", home} {
		if strings.Contains(report, canary) {
			t.Fatal("diagnostics disclosed canary")
		}
	}
	if !strings.Contains(report, "duration_ms=1.250") || !strings.Contains(report, "restart requested") {
		t.Fatal("missing allowed diagnostics")
	}
	if diagnosticCheckID("agent_CANARY") != "ai_app" {
		t.Fatal("private identifier copied")
	}
}
