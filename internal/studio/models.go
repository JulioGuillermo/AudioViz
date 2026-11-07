package studio

import (
	"net/http"

	"audioviz/internal/aimodel"
)

// The downloaded model files (aimodel's catalog): what each takes on disk and which features use
// it, to delete the ones not wanted; they download again when a feature needs them.

// hModels: GET the files in the models folder and their total
func (s *Server) hModels(w http.ResponseWriter, r *http.Request) {
	files := aimodel.Files()
	var total int64
	for _, f := range files {
		total += f.Size
	}
	writeJSON(w, map[string]any{"files": files, "total": total, "dir": aimodel.ModelsDir()})
}

// hModelRemove: DELETE ?file=<name> (a plain file name inside the models folder)
func (s *Server) hModelRemove(w http.ResponseWriter, r *http.Request) {
	if err := aimodel.Remove(r.URL.Query().Get("file")); err != nil {
		fail(w, 400, err)
		return
	}
	s.hModels(w, r)
}
