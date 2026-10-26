package circles

import (
	"encoding/json"
	"sort"
	"strings"
	"unicode/utf8"
)

func mapText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	prefix := s[:n]
	if boundary := strings.LastIndexAny(prefix, " \n\t"); boundary >= 0 {
		return prefix[:boundary] + "…"
	}
	return "…"
}

// Bounded topology also needs a byte bound when profile notes/roles are long.
func boundMap(out *UnderstandingMap) {
	for i := range out.Nodes {
		n := &out.Nodes[i]
		n.Title = mapText(n.Title, 80)
		n.HowKnown = mapText(n.HowKnown, 160)
		n.CircleAffinity = append(n.CircleAffinity[:0:0], n.CircleAffinity...)
		if len(n.CircleAffinity) > 3 {
			n.CircleAffinity = n.CircleAffinity[:3]
		}
		for j := range n.CircleAffinity {
			a := &n.CircleAffinity[j]
			if len(a.Reasons) > 2 {
				a.Reasons = a.Reasons[:2]
			}
			reasons := []string{}
			for _, reason := range a.Reasons {
				reasons = append(reasons, mapText(reason, 80))
			}
			a.Reasons = reasons
		}
		n.GroupsInCommon = append(n.GroupsInCommon[:0:0], n.GroupsInCommon...)
		if len(n.GroupsInCommon) > 3 {
			n.GroupsInCommon = n.GroupsInCommon[:3]
		}
		for j := range n.GroupsInCommon {
			g := &n.GroupsInCommon[j]
			g.Name = mapText(g.Name, 64)
			if len(g.CircleIDs) > 4 {
				g.CircleIDs = g.CircleIDs[:4]
			}
		}
		if len(n.Circles) > 4 {
			n.Circles = n.Circles[:4]
		}
	}
	for i := range out.Events {
		out.Events[i].Text = mapText(out.Events[i].Text, 160)
		out.Events[i].By = mapText(out.Events[i].By, 64)
	}
	// Latest stays complete for the app hover disclosure.
	edges := out.Edges
	out.Edges = []MapEdge{}
	base, _ := json.Marshal(out)
	// Reserve bytes for topology before optional card metadata can consume the payload.
	// Complete metadata remains available from the person endpoint on deliberate hover.
	for pass := 0; len(base) > 55000 && pass < 3; pass++ {
		for i := range out.Nodes {
			n := &out.Nodes[i]
			if pass == 0 {
				if len(n.CircleAffinity) > 1 {
					n.CircleAffinity = n.CircleAffinity[:1]
				}
				for j := range n.CircleAffinity {
					if len(n.CircleAffinity[j].Reasons) > 1 {
						n.CircleAffinity[j].Reasons = n.CircleAffinity[j].Reasons[:1]
					}
				}
				if len(n.GroupsInCommon) > 1 {
					n.GroupsInCommon = n.GroupsInCommon[:1]
				}
			} else if pass == 1 {
				n.GroupsInCommon = nil
			} else {
				n.CircleAffinity = nil
			}
		}
		base, _ = json.Marshal(out)
	}
	size := len(base)
	sort.SliceStable(edges, func(i, j int) bool {
		a, b := edges[i].Origin.ByKind, edges[j].Origin.ByKind
		if a == b {
			return edges[i].ID < edges[j].ID
		}
		return a == "you" || a == "agent" && b == "structure"
	})
	circleIDs := map[string]bool{}
	for _, n := range out.Nodes {
		if n.Kind == "circle" {
			circleIDs[n.ID] = true
		}
	}
	represented := map[string]bool{}
	priority := []MapEdge{}
	remainder := []MapEdge{}
	for _, edge := range edges {
		if circleIDs[edge.Source] && !represented[edge.Source] {
			priority = append(priority, edge)
			represented[edge.Source] = true
		} else {
			remainder = append(remainder, edge)
		}
	}
	edges = append(priority, remainder...)
	for _, e := range edges {
		e.Label = mapText(e.Label, 100)
		e.Origin.By = mapText(e.Origin.By, 64)
		data, _ := json.Marshal(e)
		if size+len(data)+1 > 89000 {
			break
		}
		out.Edges = append(out.Edges, e)
		size += len(data) + 1
	}
	// Summary describes the real graph, even if some edges are omitted by the byte budget.
	sort.Slice(out.Edges, func(i, j int) bool { return out.Edges[i].ID < out.Edges[j].ID })
}
