package server

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/th2zz/perch/bridge/internal/state"
)

// A picture the user asked to send is saved where they can find it, and a body that is not a picture is
// refused rather than written to disk.
func TestScreenshotIsSavedAndOnlyWhenItIsAPicture(t *testing.T) {
	ts, _ := newTestServer(t)
	stateDir, err := state.Dir()
	if err != nil {
		t.Fatalf("state dir: %v", err)
	}

	// A one-pixel PNG: real enough for the saving path, small enough to write out here by hand.
	png := onePixelPNG()
	body, _ := json.Marshal(map[string]string{
		"pane_id":    "wA:p1",
		"png_base64": base64.StdEncoding.EncodeToString(png),
	})
	status, response := call(t, ts, http.MethodPost, "/v1/feedback/screenshot", testToken, string(body))
	if status != http.StatusOK {
		t.Fatalf("saving a picture answered %d: %v", status, response)
	}

	saved, _ := response["saved"]
	if !strings.HasPrefix(saved, filepath.Join(stateDir, "screenshots")) {
		t.Errorf("the picture was saved outside the state directory: %s", saved)
	}
	onDisk, err := os.ReadFile(saved)
	if err != nil {
		t.Fatalf("the answer named a file that is not there: %v", err)
	}
	if string(onDisk) != string(png) {
		t.Errorf("the file on disk is not the picture that was sent (%d vs %d bytes)", len(onDisk), len(png))
	}
	if !strings.Contains(filepath.Base(saved), "wA_p1") {
		t.Errorf("the file name does not say which screen it is: %s", saved)
	}

	// Anything that is not a picture is refused, including a pane id that is trying to be a path.
	for _, bad := range []string{
		`{"pane_id":"wA:p1","png_base64":""}`,
		`{"pane_id":"../../etc/passwd","png_base64":"bm90IGEgcGljdHVyZQ=="}`,
	} {
		status, _ := call(t, ts, http.MethodPost, "/v1/feedback/screenshot", testToken, bad)
		if status == http.StatusOK {
			t.Errorf("this was accepted: %s", bad)
		}
	}
	entries, _ := os.ReadDir(filepath.Join(stateDir, "screenshots"))
	for _, entry := range entries {
		if strings.ContainsAny(entry.Name(), "/\\") || strings.Contains(entry.Name(), "..") {
			t.Errorf("a submitted pane id reached the file system as a path: %s", entry.Name())
		}
	}

	// And an oversized body is cut off rather than written: the cap is what stops a phone bug from filling
	// the user's disk.
	big := strings.Repeat("A", 9<<20)
	status, _ = call(t, ts, http.MethodPost, "/v1/feedback/screenshot", testToken,
		`{"pane_id":"wA:p1","png_base64":"`+big+`"}`)
	if status == http.StatusOK {
		t.Errorf("a 9 MB body was accepted despite an 8 MB cap")
	}
}

// onePixelPNG is the smallest valid PNG: 1x1, opaque black.
func onePixelPNG() []byte {
	return []byte{
		0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xde,
		0x00, 0x00, 0x00, 0x0c, 'I', 'D', 'A', 'T',
		0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00, 0x00,
		0x00, 0x03, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00,
		0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
	}
}

var _ = io.Discard
