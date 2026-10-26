package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"limmr/internal/model"
	"limmr/internal/testkit"
)

func TestInferenceAuditCountsOnlyAndReadOnly(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	testkit.Must(t, h.Repo.Put("circles", "PRIVATE-ID", model.Circle{ID: "PRIVATE-ID", Name: "PRIVATE-NAME", Confirmed: true, Chats: []model.CircleChat{{ChatJID: testkit.Phone}, {ChatJID: "PRIVATE-JID"}, {ChatJID: testkit.Phone}}, People: []model.CirclePerson{{PersonID: "PRIVATE-PERSON"}}}))
	testkit.Must(t, h.Repo.Put("relationship_inferences", "PRIVATE-PERSON", model.RelationshipInference{Label: "PRIVATE-LABEL", HowKnown: "PRIVATE-TEXT", Status: "inferred"}))
	hash := func() map[string][32]byte {
		t.Helper()
		out := map[string][32]byte{}
		entries, err := os.ReadDir(h.Home)
		testkit.Must(t, err)
		for _, entry := range entries {
			// SQLite WAL readers update shared-memory reader-lock metadata even
			// in mode=ro. Database and WAL bytes must remain unchanged.
			if strings.HasSuffix(entry.Name(), "-shm") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(h.Home, entry.Name()))
			testkit.Must(t, err)
			out[entry.Name()] = sha256.Sum256(b)
		}
		return out
	}
	before := hash()
	var out bytes.Buffer
	testkit.Must(t, auditInference(h.Home, &out))
	var counts map[string]map[string]int
	testkit.Must(t, json.Unmarshal(out.Bytes(), &counts))
	if counts["people"]["inference_pending"] != 1 || counts["circles"]["missing_chat_refs"] != 1 || counts["circles"]["duplicate_chat_refs"] != 1 || counts["circles"]["missing_person_refs"] != 1 {
		t.Fatal("wrong health counts")
	}
	if bytes.Contains(out.Bytes(), []byte("PRIVATE")) || bytes.Contains(out.Bytes(), []byte(testkit.Phone)) {
		t.Fatal("private values in output")
	}
	after := hash()
	if len(before) != len(after) {
		t.Fatal("audit created files")
	}
	for path, sum := range before {
		if after[path] != sum {
			t.Fatal("audit changed store", path)
		}
	}
	db, err := openAuditDB(h.Home, "hub.db")
	testkit.Must(t, err)
	defer db.Close()
	if _, err = db.Exec("DELETE FROM objects"); err == nil {
		t.Fatal("audit connection is writable")
	}
}
func TestAuditMissingHomeCreatesNothing(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	if auditInference(home, &out) == nil || auditEfforts(home) == nil {
		t.Fatal("missing stores accepted")
	}
	entries, err := os.ReadDir(home)
	testkit.Must(t, err)
	if len(entries) != 0 || out.Len() != 0 {
		t.Fatal("failed audit created state or printed details")
	}
}
