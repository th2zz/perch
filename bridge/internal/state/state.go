// Package state owns the bridge's on-disk footprint.
//
// Constitution III (zero residue) and the bridge-plugin contract §2 both say
// the same thing: the bridge writes only inside HERDR_PLUGIN_STATE_DIR, which
// is a directory herdr itself hands to plugin processes and removes when the
// plugin is unlinked. Nothing under ~/.config/herdr is ever written, and no
// file outside the state dir is created. Uninstalling is therefore
// "stop the unit, delete the plugin": there is no backup or cleanup step.
package state

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/th2zz/perch/bridge/internal/herdr"
)

// EnvStateDir is the directory herdr injects for plugin-owned state. The name is herdr's, so it is spelled
// where the rest of herdr's names are.
const EnvStateDir = herdr.EnvPluginStateDir

// File names used inside the state directory.
const (
	FilePID       = "bridge.pid"
	FileSnapshot  = "snapshot.json"
	FileDecisions = "decisions.json"
	FilePairing   = "pairing.json"
	FileEventLog  = "events.jsonl"
	FileHealth    = "health.json"
	FileValue     = "value.json"
	// FileClient records when the phone last asked for something. Written by the
	// daemon, read by one-shot commands — which otherwise cannot see the phone at all.
	FileClient  = "client.json"
	FilePush    = "push.json"
	FileLatency = "latency.json"
	// FileSessions is the bridge's persistent pane→session identity map, used when
	// a harness's own records cannot name the resumed session again.
	FileSessions = "sessions.json"
)

// Store is a handle on the state directory.
type Store struct{ dir string }

// Dir resolves the state directory. Outside a plugin process (for example when
// the user runs `perch-bridge status` from a shell) there is no injected dir,
// so a per-user cache path is used instead. That fallback is still a single
// directory, so removal stays a one-liner.
func Dir() (string, error) {
	if d := os.Getenv(EnvStateDir); d != "" {
		return d, nil
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve a state directory: %w", err)
	}
	return filepath.Join(cache, "perch-bridge"), nil
}

// Open returns a Store, creating the directory if needed.
func Open() (*Store, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create state dir %s: %w", dir, err)
	}
	return &Store{dir: dir}, nil
}

// Dir returns the directory this store writes to.
func (s *Store) Dir() string { return s.dir }

// Path returns the absolute path of a named state file.
func (s *Store) Path(name string) string { return filepath.Join(s.dir, name) }

// WriteJSON writes a state file atomically so a crash cannot leave a truncated
// file that the next start would fail to parse.
func (s *Store) WriteJSON(name string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return s.writeFile(name, data)
}

// ReadJSON reads a state file. A missing file is reported as ErrNotExist so
// callers can distinguish "first run" from "corrupt".
func (s *Store) ReadJSON(name string, v any) error {
	data, err := os.ReadFile(s.Path(name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return err
		}
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("state file %s is corrupt (%w); delete it to reset", name, err)
	}
	return nil
}

// AppendLine appends a line to a state file. Used by the event spool, where the
// hook process must stay as cheap as possible.
func (s *Store) AppendLine(name, line string) error {
	f, err := os.OpenFile(s.Path(name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.WriteString(line + "\n")
	return err
}

func (s *Store) writeFile(name string, data []byte) error {
	tmp := s.Path(name + ".tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.Path(name))
}

// ---------------------------------------------------------------- single instance

// Pairing holds the shared secret the phone uses to talk to this bridge.
//
// It is generated on the host and never leaves it except when the user
// deliberately pairs a device. It lives in the state directory with 0600
// permissions, which is one reason the state directory should not be synced.
type Pairing struct {
	Token     string    `json:"token"`
	CreatedAt time.Time `json:"created_at"`
	// LastRotated records when the previous token was created, kept only so the
	// check-up can say how old the current secret is.
	LastRotated time.Time `json:"last_rotated,omitempty"`
}

// EnsureToken returns the existing pairing token, creating one on first use.
func (s *Store) EnsureToken() (string, error) {
	var p Pairing
	if err := s.ReadJSON(FilePairing, &p); err == nil && p.Token != "" {
		return p.Token, nil
	}
	return s.RotateToken()
}

// RotateToken replaces the pairing token, which invalidates every paired
// device. It doubles as the "unpair everything" action.
func (s *Store) RotateToken() (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	var prev Pairing
	rotated := time.Time{}
	if err := s.ReadJSON(FilePairing, &prev); err == nil {
		rotated = prev.CreatedAt
	}
	p := Pairing{Token: token, CreatedAt: time.Now(), LastRotated: rotated}
	if err := s.WriteJSON(FilePairing, p); err != nil {
		return "", err
	}
	return token, nil
}

// PairingInfo reads the pairing record without creating one.
func (s *Store) PairingInfo() (Pairing, error) {
	var p Pairing
	err := s.ReadJSON(FilePairing, &p)
	return p, err
}

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("cannot generate a pairing token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// ErrAlreadyRunning is returned when another bridge process holds the lock.
var ErrAlreadyRunning = errors.New("another perch bridge is already running")

// Lock is an advisory exclusive lock on the state directory. It is what makes
// "install once, start once" true even if the plugin's startup hook fires and
// the systemd unit starts at the same time.
type Lock struct{ f *os.File }

// Acquire takes the lock, writing the holder's pid for diagnostics.
func (s *Store) Acquire() (*Lock, error) {
	f, err := os.OpenFile(s.Path(FilePID), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, ErrAlreadyRunning
	}
	_ = f.Truncate(0)
	_, _ = f.WriteAt([]byte(strconv.Itoa(os.Getpid())), 0)
	return &Lock{f: f}, nil
}

// Release drops the lock. The pid file itself is left in place because it lives
// inside the state dir and disappears with it.
func (l *Lock) Release() {
	if l == nil || l.f == nil {
		return
	}
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	_ = l.f.Close()
	l.f = nil
}

// RunningPID reports the pid recorded in the state dir, and whether that
// process is alive. It is only a hint: the lock is the authority.
func (s *Store) RunningPID() (int, bool) {
	data, err := os.ReadFile(s.Path(FilePID))
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(string(trimSpace(data)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	if err := syscall.Kill(pid, 0); err != nil {
		return pid, false
	}
	return pid, true
}

func trimSpace(b []byte) []byte {
	start, end := 0, len(b)
	for start < end && (b[start] == ' ' || b[start] == '\n' || b[start] == '\r' || b[start] == '\t') {
		start++
	}
	for end > start && (b[end-1] == ' ' || b[end-1] == '\n' || b[end-1] == '\r' || b[end-1] == '\t') {
		end--
	}
	return b[start:end]
}
