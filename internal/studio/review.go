package studio

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Reviewing what agents changed: their batches (of the project's recent ones, collab.go's log),
// each to accept (it stays, out of the list) or to revert (its inverse, moved past what came
// after it, applied as a change of its own). A revert warns of later changes to the same places.

// agentChange: an agent's batch as the studio lists it
type agentChange struct {
	V        int      `json:"v"`
	Time     int64    `json:"t"`
	Name     string   `json:"name"`
	Ops      []jsonOp `json:"ops"`
	Status   string   `json:"status,omitempty"`
	Conflict []string `json:"conflict,omitempty"` // who changed the same places since
}

// overlaps: whether two paths are the same place or one is inside the other
func overlaps(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

// hChanges: the agents' changes still in the log (newest first)
func (s *Server) hChanges(w http.ResponseWriter, r *http.Request) {
	c := s.collab
	c.mu.Lock()
	defer c.mu.Unlock()
	out := []agentChange{}
	for i := len(c.log) - 1; i >= 0; i-- {
		b := c.log[i]
		if !b.Agent || b.Reverts > 0 {
			continue
		}
		ch := agentChange{V: b.V, Time: b.Time, Name: b.Name, Ops: b.Ops, Status: b.Status}
		if b.Status == "" {
			ch.Conflict = laterConflicts(c.log[i+1:], b)
		}
		out = append(out, ch)
	}
	writeJSON(w, map[string]any{"changes": out})
}

// laterConflicts: who changed the same places as b after it (not b's own revert)
func laterConflicts(later []opBatch, b opBatch) []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range later {
		if l.Reverts == b.V || l.Client == b.Client {
			continue
		}
		for _, o := range l.Ops {
			for _, p := range b.Ops {
				if overlaps(o.Path, p.Path) && !seen[l.Name] {
					seen[l.Name] = true
					name := l.Name
					if name == "" {
						name = "?"
					}
					out = append(out, name)
				}
			}
		}
	}
	return out
}

// hReview: {client, v, action: "accept" | "revert"}
func (s *Server) hReview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Client string `json:"client"`
		V      int    `json:"v"`
		Action string `json:"action"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, err)
		return
	}
	var err error
	switch req.Action {
	case "accept":
		err = s.setChangeStatus(req.V, "accepted")
	case "revert":
		err = s.revertChange(req.V, req.Client)
	default:
		err = fmt.Errorf("unknown action %q", req.Action)
	}
	if err != nil {
		fail(w, 400, err)
		return
	}
	s.hub.send("changes", map[string]any{"v": req.V, "action": req.Action})
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) setChangeStatus(v int, status string) error {
	c := s.collab
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.log {
		if c.log[i].V == v {
			if c.log[i].Status == "reverted" {
				return errors.New("it was reverted already")
			}
			c.log[i].Status = status
			return nil
		}
	}
	return errors.New("that change is too old to review")
}

// revertChange: batch v undone (its inverse moved past the batches after it), as client's change
func (s *Server) revertChange(v int, client string) error {
	c := s.collab
	c.mu.Lock()
	idx := -1
	for i := range c.log {
		if c.log[i].V == v {
			idx = i
		}
	}
	if idx < 0 {
		c.mu.Unlock()
		return errors.New("that change is too old to revert")
	}
	b := c.log[idx]
	if b.Status == "reverted" {
		c.mu.Unlock()
		return errors.New("it was reverted already")
	}
	// its inverse is on the project right after it: moved past all that came after
	var later []jsonOp
	for _, l := range c.log[idx+1:] {
		later = append(later, l.Ops...)
	}
	inv := transformBatch(b.Inv, later)
	epoch, base := c.epoch, c.ver
	c.mu.Unlock()
	if len(inv) == 0 {
		return s.setChangeStatus(v, "reverted")
	}
	// (on the version just read: whatever comes in meanwhile is gone past too)
	if _, err := s.applyOpsR(epoch, base, inv, "review:"+client, "", s.peerName(client), false, v); err != nil {
		return err
	}
	return s.setChangeStatus(v, "reverted")
}
