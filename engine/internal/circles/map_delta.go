package circles

import (
	"limmr/internal/model"
	"reflect"
	"strings"
)

// since accepts a cursor returned by a prior map. Unknown/expired cursors resync.
func (s *Service) MapDelta(cursor string) (any, error) {
	if !strings.HasPrefix(cursor, "p1.") || len(cursor) != 19 {
		return nil, model.ErrInvalid
	}
	v, e := s.Map()
	if e != nil {
		return nil, e
	}
	current := v.(UnderstandingMap)
	s.shared().mapMu.Lock()
	previous, ok := s.shared().mapHistory[cursor]
	s.shared().mapMu.Unlock()
	if !ok {
		return current, nil
	}
	out := UnderstandingMap{Cursor: current.Cursor, Delta: true, Nodes: []MapNode{}, Edges: []MapEdge{}, Events: []MapEvent{}, Pulses: current.Pulses, Summary: current.Summary}
	oldNodes := map[string]MapNode{}
	newNodes := map[string]bool{}
	for _, n := range previous.Nodes {
		oldNodes[n.ID] = n
	}
	for _, n := range current.Nodes {
		newNodes[n.ID] = true
		if old, ok := oldNodes[n.ID]; !ok || !reflect.DeepEqual(old, n) {
			out.Nodes = append(out.Nodes, n)
		}
	}
	for _, n := range previous.Nodes {
		if !newNodes[n.ID] {
			out.RemovedNodes = append(out.RemovedNodes, n.ID)
		}
	}
	oldEdges := map[string]MapEdge{}
	newEdges := map[string]bool{}
	for _, n := range previous.Edges {
		oldEdges[n.ID] = n
	}
	for _, n := range current.Edges {
		newEdges[n.ID] = true
		if old, ok := oldEdges[n.ID]; !ok || !reflect.DeepEqual(old, n) {
			out.Edges = append(out.Edges, n)
		}
	}
	for _, n := range previous.Edges {
		if !newEdges[n.ID] {
			out.RemovedEdges = append(out.RemovedEdges, n.ID)
		}
	}
	events := map[string]bool{}
	for _, e := range previous.Events {
		events[e.ID] = true
	}
	for _, e := range current.Events {
		if !events[e.ID] {
			out.Events = append(out.Events, e)
		}
	}
	return out, nil
}
