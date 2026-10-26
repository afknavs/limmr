package main

import (
	"encoding/json"
	"io"
	"limmr/internal/hub"
	"limmr/internal/model"
	"limmr/internal/wa"
)

// Fixed keys and integer leaves only. Never serialize entities or dynamic keys.
// Open existing stores read-only; no migrations, inference writes or services.
func auditInference(home string, out io.Writer) error {
	db, err := openAuditDB(home, "hub.db")
	if err != nil {
		return err
	}
	defer db.Close()
	messages, err := openAuditDB(home, "messages.db")
	if err != nil {
		return err
	}
	defer messages.Close()
	r := &hub.Repo{DB: db}
	ps, err := hub.List[model.Person](r, "people")
	if err != nil {
		return err
	}
	cs, err := hub.List[model.Circle](r, "circles")
	if err != nil {
		return err
	}
	ins, err := hub.List[model.RelationshipInference](r, "relationship_inferences")
	if err != nil {
		return err
	}
	chats, err := (&wa.Store{DB: messages}).Chats()
	if err != nil {
		return err
	}
	aliases, err := (&wa.Store{DB: messages}).Aliases()
	if err != nil {
		return err
	}
	canonical := func(j string) string {
		if v := aliases[j]; v != "" {
			return v
		}
		return j
	}
	peopleIDs, chatIDs := map[string]bool{}, map[string]bool{}
	p := map[string]int{"total": len(ps), "named": 0, "business": 0, "broadcast": 0, "with_relationship": 0, "inference_pending": len(ins)}
	c := map[string]int{"total": len(cs), "confirmed": 0, "pending": 0, "archived": 0, "missing_chat_refs": 0, "missing_person_refs": 0, "duplicate_chat_refs": 0}
	for _, v := range ps {
		peopleIDs[v.ID] = true
		if model.Named(v) {
			p["named"]++
		}
		if v.Business {
			p["business"]++
		}
		if v.Broadcast {
			p["broadcast"]++
		}
		if v.Role != "" || v.HowKnown != "" {
			p["with_relationship"]++
		}
	}
	for _, v := range chats {
		chatIDs[canonical(v.JID)] = true
	}
	for _, v := range cs {
		if v.Confirmed {
			c["confirmed"]++
		} else {
			c["pending"]++
		}
		if v.Archived {
			c["archived"]++
		}
		seen := map[string]bool{}
		for _, ref := range v.Chats {
			j := canonical(ref.ChatJID)
			if !chatIDs[j] {
				c["missing_chat_refs"]++
			}
			if seen[j] {
				c["duplicate_chat_refs"]++
			}
			seen[j] = true
		}
		for _, ref := range v.People {
			if !peopleIDs[ref.PersonID] {
				c["missing_person_refs"]++
			}
		}
	}
	return json.NewEncoder(out).Encode(map[string]any{"people": p, "circles": c, "chats": map[string]int{"total": len(chats)}})
}
