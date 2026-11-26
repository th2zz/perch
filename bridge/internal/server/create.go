package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/th2zz/perch/bridge/internal/core"
)

type createProjectRequest struct {
	Label string   `json:"label"`
	Cwd   string   `json:"cwd"`
	Kind  string   `json:"kind"`
	Name  string   `json:"name"`
	Args  []string `json:"args"`
	Host  string   `json:"host"`
}

type createSessionRequest struct {
	WorkspaceID string   `json:"workspace_id"`
	Kind        string   `json:"kind"`
	Name        string   `json:"name"`
	Args        []string `json:"args"`
	Host        string   `json:"host"`
}

func (s *Server) handleHarnesses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "GET only"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	view, err := s.core.Harnesses(ctx, r.URL.Query().Get("host"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": err.Error(),
			"fix":   "open this project on the computer running Perch",
		})
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST only"})
		return
	}
	var body createProjectRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "expected {label, cwd, kind, name, args}",
			"fix":   "nothing was created; fill in the project and try again",
		})
		return
	}
	if strings.TrimSpace(body.Label) == "" || strings.TrimSpace(body.Kind) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "a project needs a name and a harness",
			"fix":   "name the project and choose an installed harness",
		})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	created, err := s.core.CreateProject(
		ctx, body.Host, body.Label, body.Cwd, body.Kind, body.Name, body.Args,
	)
	if err != nil {
		var setup *core.HarnessSetupError
		if errors.As(err, &setup) {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": setup.Error(),
				"fix":   setup.FixText(),
			})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": err.Error(),
			"fix":   "nothing was created; check the folder, harness and arguments and try again",
		})
		return
	}
	writeJSON(w, http.StatusOK, created)
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST only"})
		return
	}
	var body createSessionRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "expected {workspace_id, kind, name, args}",
			"fix":   "nothing was created; open the project and try again",
		})
		return
	}
	if strings.TrimSpace(body.WorkspaceID) == "" || strings.TrimSpace(body.Kind) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "a session needs a project and a harness",
			"fix":   "open a project and choose an installed harness",
		})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	created, err := s.core.CreateSession(
		ctx, body.Host, body.WorkspaceID, body.Kind, body.Name, body.Args,
	)
	if err != nil {
		var setup *core.HarnessSetupError
		if errors.As(err, &setup) {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": setup.Error(),
				"fix":   setup.FixText(),
			})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": err.Error(),
			"fix":   "nothing was created; check the project's folder, harness and arguments and try again",
		})
		return
	}
	writeJSON(w, http.StatusOK, created)
}
