package core

import (
	"strings"
	"sync"
	"time"

	"github.com/th2zz/perch/bridge/internal/state"
)

// sessionEntry is the last session the bridge successfully read for one pane.
//
// It is the bridge's own persistent identity map, not a second source of truth: a
// successful read renews it, a different title (a new session or `/new`) makes it
// ineligible, and the pane's PID lets a restart be recognized without losing the
// session. The map exists because the harness's state database and terminal title
// can both change while the same pane keeps the same session; re-deriving the
// identity on every poll is what made `readable` flip.
type sessionEntry struct {
	PaneID    string    `json:"pane_id"`
	SessionID string    `json:"session_id"`
	PID       int       `json:"pid"`
	Title     string    `json:"title"`
	CWD       string    `json:"cwd"`
	SavedAt   time.Time `json:"saved_at"`
}

type sessionFile struct {
	Sessions map[string]sessionEntry `json:"sessions"`
}

type sessionMap struct {
	mu      sync.Mutex
	store   *state.Store
	entries map[string]sessionEntry
}

func loadSessionMap(store *state.Store) *sessionMap {
	m := &sessionMap{store: store, entries: map[string]sessionEntry{}}
	var file sessionFile
	if err := store.ReadJSON(state.FileSessions, &file); err == nil && file.Sessions != nil {
		m.entries = file.Sessions
	}
	return m
}

func cleanSessionTitle(title string) string {
	return strings.ToLower(strings.TrimSpace(title))
}

// lookup returns the session last read for this pane. A changed title means a
// different session (or `/new`), so the entry is not eligible. A changed PID is
// allowed: the process restarting on the same titled session is the case this
// map exists to survive.
func (m *sessionMap) lookup(paneID string, pid int, title string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.entries[paneID]
	if !ok || entry.SessionID == "" {
		return "", false
	}
	if entry.Title != "" && cleanSessionTitle(title) != cleanSessionTitle(entry.Title) {
		return "", false
	}
	return entry.SessionID, true
}

func (m *sessionMap) save(paneID, sessionID string, pid int, title, cwd string) {
	if paneID == "" || sessionID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.entries[paneID]; ok &&
		old.SessionID == sessionID && old.PID == pid &&
		old.Title == title && old.CWD == cwd {
		return
	}
	m.entries[paneID] = sessionEntry{
		PaneID: paneID, SessionID: sessionID, PID: pid, Title: title,
		CWD: cwd, SavedAt: time.Now().UTC(),
	}
	_ = m.store.WriteJSON(state.FileSessions, sessionFile{Sessions: m.entries})
}
