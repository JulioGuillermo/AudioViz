package studio

import (
	"encoding/json"
	"net/http"

	"audioviz/internal/engine"
)

// POST /api/expr: an expression tried on a property ({src, target, path, times}): its mistake or
// its values over those times (the advanced settings dialog: its check and its graph, and the
// value shown under the property)
func (s *Server) hExpr(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Src    string            `json:"src"`
		Target engine.ExprTarget `json:"target"`
		Path   string            `json:"path"`
		Times  []float64         `json:"times"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.Times) > 2000 {
		req.Times = req.Times[:2000]
	}
	s.mu.Lock()
	sess := s.sess
	s.mu.Unlock()
	if sess == nil {
		http.Error(w, "no project", http.StatusConflict)
		return
	}
	writeJSON(w, sess.ExprSeries(req.Src, req.Target, req.Path, req.Times))
}
