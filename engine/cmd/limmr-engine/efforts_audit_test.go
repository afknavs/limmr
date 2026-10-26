package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"limmr/internal/efforts"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEffortsAuditReadsOnlyAndPrintsOnlyCounts(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	// E7 drops bare repeated fragments; explicit work still tests an unnamed
	// effort without teaching the title extractor invented vocabulary.
	for i, text := range []string{"festival budget", "festival budget", "festival budget", "cobalt proposal", "cobalt proposal", "Please review zorb flerm", "I will send zorb flerm", "Can you send zorb flerm?", "Forwarded: festival budget proposal"} {
		testkit.Must(t, h.Messages.PutMessage(model.Message{ID: fmt.Sprint("audit-", i), ChatJID: testkit.Phone, Sender: testkit.Phone, IsFromMe: i%2 == 1, Content: text, Timestamp: testkit.Epoch.Add(-time.Duration(10-i) * time.Hour)}))
	}
	before, e := os.ReadFile(filepath.Join(h.Home, "hub.db"))
	testkit.Must(t, e)
	beforeMessages, e := os.ReadFile(filepath.Join(h.Home, "messages.db"))
	testkit.Must(t, e)
	// Capture outside the audited home. A pipe read only after auditEfforts
	// returns can block its writer when the aggregate JSON exceeds pipe capacity.
	output, e := os.CreateTemp(t.TempDir(), "audit-output")
	testkit.Must(t, e)
	defer output.Close()
	original := os.Stdout
	defer func() { os.Stdout = original }()
	os.Stdout = output
	err := auditEfforts(h.Home)
	os.Stdout = original
	_, e = output.Seek(0, io.SeekStart)
	testkit.Must(t, e)
	data, e := io.ReadAll(output)
	testkit.Must(t, e)
	testkit.Must(t, err)
	var report efforts.Audit
	testkit.Must(t, json.Unmarshal(data, &report))
	if len(report.MessagesFiltered) != 4 || len(report.CandidatesFiltered) != 1 || report.TooWeak.MessageCount == nil {
		t.Fatal("aggregate schema missing")
	}
	if !bytes.Contains(data, []byte(`"duplicate_titles":`)) || !bytes.Contains(data, []byte(`"generic_titles":`)) || report.DuplicateTitles != 0 || report.GenericTitles != 0 {
		t.Fatal("title quality counts missing or wrong")
	}
	if report.Named != 1 || report.Internal != 1 || report.NamingQueued != 1 || report.TooWeak.MessageCount["2"] != 1 || report.MessagesFiltered["forward"] != 1 {
		t.Fatalf("aggregate counts wrong: %+v", report)
	}
	if report.Accepted.Messages["3-5"] != 2 || report.Accepted.Chats["1"] != 2 || report.Accepted.People["2"] != 2 || report.Accepted.SpanDays["<1"] != 2 || report.Largest.Messages != 3 || report.Largest.Chats != 1 || report.Confidence["0.60-0.69"] != 2 || report.TitleSources["request_phrase"] != 1 || report.TitleSources["naming_queue"] != 1 {
		t.Fatalf("accepted effort counts wrong: %+v", report)
	}
	for _, private := range []string{"Maya", testkit.Phone, "festival", "cobalt", "zorb", "audit-"} {
		if bytes.Contains(data, []byte(private)) {
			t.Fatal("identifying output")
		}
	}
	after, e := os.ReadFile(filepath.Join(h.Home, "hub.db"))
	testkit.Must(t, e)
	if !bytes.Equal(before, after) {
		t.Fatal("audit changed hub database")
	}
	afterMessages, e := os.ReadFile(filepath.Join(h.Home, "messages.db"))
	testkit.Must(t, e)
	if !bytes.Equal(beforeMessages, afterMessages) {
		t.Fatal("audit changed messages database")
	}
	entries, e := os.ReadDir(h.Home)
	testkit.Must(t, e)
	for _, entry := range entries {
		if entry.Name() == "control-token" || entry.Name() == "session.db" || entry.Name() == "logs" {
			t.Fatal("audit opened credentials, session or logs")
		}
	}
}
