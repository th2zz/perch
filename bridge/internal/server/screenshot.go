package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/th2zz/perch/bridge/internal/state"
)

func (s *Server) handleScreenshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST only"})
		return
	}
	var body struct {
		PaneID    string `json:"pane_id"`
		PNGBase64 string `json:"png_base64"`
	}
	// 8 MB of body: a full-screen PNG of a phone is well under this, and the cap is what stops a bug on the
	// phone from filling the user's disk.
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "expected {pane_id, png_base64}",
			"fix":   "the app sends this itself; nothing for you to send",
		})
		return
	}
	png, err := base64.StdEncoding.DecodeString(strings.TrimSpace(body.PNGBase64))
	if err != nil || !isPNG(png) {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "that is not a picture",
			"fix":   "try again; if it keeps happening, the screen it is capturing is the problem",
		})
		return
	}

	stateDir, err := state.Dir()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	dir := filepath.Join(stateDir, "screenshots")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// The pane id is part of the file name so a picture of the right screen is obvious later; it is also
	// untrusted input, so everything but a small safe alphabet is replaced rather than pasted into a path.
	label := safeLabel(body.PaneID)
	name := time.Now().Format("2006-01-02T15-04-05") + "-" + label + ".png"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, png, 0o600); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.opts.Logger.Info("screenshot saved", "pane", label, "bytes", len(png), "path", path)
	writeJSON(w, http.StatusOK, map[string]any{"saved": path, "bytes": len(png)})
}

// isPNG checks the file signature rather than trusting the client.
//
// The first version only asked "does this base64 decode", which accepts any bytes at all — so a bug on the
// phone would have written megabytes of nothing into the user's state directory, and a test found it by
// sending a path-like pane id with a base64 payload. Eight bytes of magic number is the whole check.
func isPNG(data []byte) bool {
	magic := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	if len(data) < len(magic) {
		return false
	}
	for i, b := range magic {
		if data[i] != b {
			return false
		}
	}
	return true
}

// safeLabel keeps a pane id from becoming a path: letters, digits, dash and underscore survive, everything
// else — including the dots, which are what a path traversal is made of — becomes an underscore.
func safeLabel(raw string) string {
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, strings.TrimSpace(raw))
	if cleaned == "" {
		cleaned = "screen"
	}
	if len(cleaned) > 40 {
		cleaned = cleaned[:40]
	}
	return cleaned
}
