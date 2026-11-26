package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/th2zz/perch/bridge/internal/core"
)

// The two doors that change what exists, rather than what is being said.
//
// `POST /v1/rename` and `POST /v1/close`, each with a `target` of "session" or "project". Two paths rather
// than four because the product's question is "what can the phone change?", and the answer — a name, or
// whether something exists — is worth being able to read in one place.
//
// Both are writes of a kind this app did not have before 2026-10-15, and both are *reversible only by hand*
// (a rename can be typed again; a close cannot be undone at all). That asymmetry is why the README, the
// constitution and `ApiSurfaceTest.WRITES` all have to be edited together with this file, and why closing a
// project demands an explicit `confirmed` from the app.

type renameRequest struct {
	Target string `json:"target"`
	ID     string `json:"id"`
	Label  string `json:"label"`
	Host   string `json:"host"`
}

type closeRequest struct {
	Target string `json:"target"`
	ID     string `json:"id"`
	Host   string `json:"host"`
	// Confirmed is the app reporting that a person saw what would be lost and said yes. Required for
	// projects, meaningless for a single session.
	Confirmed bool `json:"confirmed"`
}

func (s *Server) handleRename(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST only"})
		return
	}
	var body renameRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "expected {target, id, label}",
			"fix":   "reopen the session or the project on the phone and try the rename again",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	var (
		done *core.Equivalent
		err  error
	)
	switch core.Target(body.Target) {
	case core.TargetSession:
		done, err = s.core.RenameSession(ctx, body.Host, body.ID, body.Label)
	case core.TargetProject:
		done, err = s.core.RenameProject(ctx, body.Host, body.ID, body.Label)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "target must be \"session\" or \"project\"",
			"fix":   "this app asked for a rename of something that does not exist",
		})
		return
	}
	if err != nil {
		// A name that herdr refused is usually a name the *product* refused (empty, too long, a control
		// character), and those are fixable right there in the dialog — so the fix says what to do instead
		// of sending the user to their computer.
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": err.Error(),
			"fix":   "nothing was renamed; try another name on the phone",
		})
		return
	}
	writeJSON(w, http.StatusOK, done)
}

func (s *Server) handleClose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST only"})
		return
	}
	var body closeRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "expected {target, id}",
			"fix":   "reopen the session or the project on the phone and try again",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	var (
		done *core.Equivalent
		err  error
	)
	switch core.Target(body.Target) {
	case core.TargetSession:
		done, err = s.core.CloseSession(ctx, body.Host, body.ID)
	case core.TargetProject:
		done, err = s.core.CloseProject(ctx, body.Host, body.ID, body.Confirmed)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "target must be \"session\" or \"project\"",
			"fix":   "this app asked to close something that does not exist",
		})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": err.Error(),
			"fix":   "nothing was closed; if a project will not close, close its sessions one at a time",
		})
		return
	}
	writeJSON(w, http.StatusOK, done)
}
