package server

import (
	"context"
	"net/http"
	"time"
)

// handleFolders lists one directory on the computer for the working-folder picker.
//
// It is a read, and deliberately the same shape as reading a session: the phone asks about the
// folder it is showing, and every "up", "home" or tap on a row is another request. Nothing is
// cached here, because a folder that existed when the picker opened can be gone by the time the
// person taps Create, and the create path is the one that must make the final decision.
func (s *Server) handleFolders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "GET only"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	view, err := s.core.Folders(
		ctx, r.URL.Query().Get("host"), r.URL.Query().Get("path"),
	)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": err.Error(),
			"fix":   "choose another folder, or type its full path and try again",
		})
		return
	}
	writeJSON(w, http.StatusOK, view)
}
