package sync_test

import (
	"limmr/internal/hub"
	"limmr/internal/model"
	"testing"
)

func TestN1CircleHueAndSymbolRoundTripToDot(t *testing.T) {
	a, b := newEngine(t), newEngine(t)
	pair(t, a, b)
	put(t, a, "circles", "study", model.Circle{ID: "study", Name: "Study circle", Confirmed: true})
	icon := hub.EntityIcon{Kind: "symbol", Value: "graduationcap", Hue: "blue"}
	must(t, a.h.SetEntityIcon("circles", "study", &icon))
	exchange(t, a, b)
	var received hub.EntityIcon
	must(t, b.h.Repo.Get("entity_icons", "circles/study", &received))
	if received != icon {
		t.Fatal("dot lost circle identity")
	}
	b.now = b.now.Add(1000000000)
	must(t, b.clock())
	icon.Hue = "purple"
	icon.Value = "book"
	must(t, b.h.SetEntityIcon("circles", "study", &icon))
	exchange(t, b, a)
	must(t, a.h.Repo.Get("entity_icons", "circles/study", &received))
	if received != icon {
		t.Fatal("Mac lost dot identity change")
	}
	exchange(t, a, b)
}
