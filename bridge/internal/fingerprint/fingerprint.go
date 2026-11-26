// Package fingerprint makes the product's first promise checkable.
//
// The claim under test is the one the whole product rests on: installing Perch
// does not change anything you already had. That is easy to assert and, without
// mechanism, impossible to prove — a config file edited by one stray write looks
// exactly like one that was never touched.
//
// So the bridge can take a fingerprint of the files it must not modify, and
// compare two fingerprints later. The comparison deliberately distinguishes
// changes that are *ours and reversible* (registering a plugin, which
// `plugin unlink` removes) from changes that would mean we broke the promise.
package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/th2zz/perch/bridge/internal/herdr"
)

// PluginID must match plugin/herdr-plugin.toml.
const PluginID = "perch.bridge"

// FileStamp is a file's identity without its contents.
type FileStamp struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	SHA256 string `json:"sha256,omitempty"`
	Size   int64  `json:"size,omitempty"`
	// ModTime is recorded for the human reading a diff. It is never used to
	// decide whether something changed: a file can be rewritten with identical
	// bytes, and that is not a modification anyone cares about.
	ModTime string `json:"mtime,omitempty"`
}

// Fingerprint is the state of everything the product promises to leave alone.
type Fingerprint struct {
	TakenAt time.Time `json:"taken_at"`
	Home    string    `json:"home"`

	HerdrConfig  FileStamp `json:"herdr_config"`
	HerdrPlugins FileStamp `json:"herdr_plugins"`
	// PluginsDir lists what herdr has under its plugins directory, so a stray
	// file left behind by a plugin would show up.
	PluginsDir []string `json:"plugins_dir"`
	// OurPluginRegistered is separated from the plugins.json hash because it is
	// the one change we are allowed to cause, and it is discovered through
	// herdr's own file rather than by guessing.
	OurPluginRegistered bool `json:"our_plugin_registered"`

	StateDir       string `json:"state_dir"`
	StateDirExists bool   `json:"state_dir_exists"`
}

// Take reads the current state.
func Take(home, stateDir string) (Fingerprint, error) {
	configDir := herdr.ConfigDir(home)

	config, err := stamp(filepath.Join(configDir, "config.toml"))
	if err != nil {
		return Fingerprint{}, err
	}
	plugins, err := stamp(herdr.PluginsRegistryPath(home))
	if err != nil {
		return Fingerprint{}, err
	}

	fp := Fingerprint{
		TakenAt:             time.Now(),
		Home:                home,
		HerdrConfig:         config,
		HerdrPlugins:        plugins,
		PluginsDir:          listDir(filepath.Join(configDir, "plugins")),
		OurPluginRegistered: pluginsFileMentions(plugins, PluginID),
		StateDir:            stateDir,
	}
	if stateDir != "" {
		if info, err := os.Stat(stateDir); err == nil && info.IsDir() {
			fp.StateDirExists = true
		}
	}
	return fp, nil
}

// Write stores a fingerprint.
func Write(path string, fp Fingerprint) error {
	data, err := json.MarshalIndent(fp, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// Read loads a fingerprint.
func Read(path string) (Fingerprint, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Fingerprint{}, err
	}
	var fp Fingerprint
	if err := json.Unmarshal(data, &fp); err != nil {
		return Fingerprint{}, fmt.Errorf("%s is not a fingerprint file: %w", path, err)
	}
	return fp, nil
}

// ChangeKind says whether a difference matters.
type ChangeKind string

const (
	// ChangeUnexpected means the product modified something it promised not to.
	ChangeUnexpected ChangeKind = "unexpected"
	// ChangeExpected means the difference is the plugin registration that the
	// install or uninstall step is supposed to cause, and that `plugin unlink`
	// reverses.
	ChangeExpected ChangeKind = "expected"
	// ChangeNote is information the reader may want, with no verdict attached.
	ChangeNote ChangeKind = "note"
)

// Change is one difference between two fingerprints.
type Change struct {
	Kind   ChangeKind `json:"kind"`
	Field  string     `json:"field"`
	Detail string     `json:"detail"`
}

// Compare reports what changed between two fingerprints.
//
// The rule for the plugin registry is the interesting part: the product *is*
// allowed to register itself there, because that is how herdr installs anything
// and `plugin unlink` takes it out again. What is not allowed is any other
// difference in that file — which is why the comparison looks at whether our
// registration is the only thing that moved, rather than waving the whole file
// through.
func Compare(before, after Fingerprint) []Change {
	var changes []Change

	// 1. The user's herdr configuration. Never ours to touch.
	if before.HerdrConfig.SHA256 != after.HerdrConfig.SHA256 ||
		before.HerdrConfig.Exists != after.HerdrConfig.Exists {
		changes = append(changes, Change{
			Kind:  ChangeUnexpected,
			Field: "herdr_config",
			Detail: fmt.Sprintf(
				"config.toml changed (%s → %s). Perch must never modify your herdr configuration",
				short(before.HerdrConfig.SHA256), short(after.HerdrConfig.SHA256),
			),
		})
	}

	// 2. The plugin registry: only our own registration may appear or disappear.
	if before.OurPluginRegistered != after.OurPluginRegistered {
		direction := "registered"
		if !after.OurPluginRegistered {
			direction = "unregistered"
		}
		changes = append(changes, Change{
			Kind:   ChangeExpected,
			Field:  "plugin_registration",
			Detail: "the Perch plugin was " + direction + " (this is what install/uninstall does, and `herdr plugin unlink` reverses it)",
		})
	} else if before.HerdrPlugins.SHA256 != after.HerdrPlugins.SHA256 {
		changes = append(changes, Change{
			Kind:  ChangeUnexpected,
			Field: "herdr_plugins",
			Detail: "the plugin registry changed in a way that is not Perch's own registration; " +
				"another plugin or an external edit is involved",
		})
	}

	// 3. Anything the plugins directory gained or lost, except our own directory.
	//
	// Removals count too: a check that only looked for new files would miss a
	// product that tidied away something the user never asked it to touch.
	for _, entry := range diffList(before.PluginsDir, after.PluginsDir) {
		if strings.HasPrefix(entry, PluginID) {
			changes = append(changes, Change{
				Kind:   ChangeExpected,
				Field:  "plugins_dir",
				Detail: "herdr's plugin directory gained " + entry + " for Perch",
			})
			continue
		}
		changes = append(changes, Change{
			Kind:   ChangeUnexpected,
			Field:  "plugins_dir",
			Detail: "herdr's plugin directory changed outside Perch's own entry: " + entry,
		})
	}
	for _, entry := range diffList(after.PluginsDir, before.PluginsDir) {
		if strings.HasPrefix(entry, PluginID) {
			changes = append(changes, Change{
				Kind:   ChangeExpected,
				Field:  "plugins_dir",
				Detail: "herdr's plugin directory lost " + entry + " for Perch",
			})
			continue
		}
		changes = append(changes, Change{
			Kind:   ChangeUnexpected,
			Field:  "plugins_dir",
			Detail: "something vanished from herdr's plugin directory: " + entry,
		})
	}

	return changes
}

// Unexpected returns only the changes that break the promise.
func Unexpected(changes []Change) []Change {
	var out []Change
	for _, c := range changes {
		if c.Kind == ChangeUnexpected {
			out = append(out, c)
		}
	}
	return out
}

// ---------------------------------------------------------------- internals

func stamp(path string) (FileStamp, error) {
	out := FileStamp{Path: path}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return out, err
	}
	out.Exists = true
	out.Size = info.Size()
	out.ModTime = info.ModTime().UTC().Format(time.RFC3339)

	f, err := os.Open(path)
	if err != nil {
		return out, err
	}
	defer func() { _ = f.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return out, err
	}
	out.SHA256 = hex.EncodeToString(hash.Sum(nil))
	return out, nil
}

func listDir(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Name())
	}
	sort.Strings(out)
	return out
}

func diffList(before, after []string) []string {
	seen := make(map[string]struct{}, len(before))
	for _, item := range before {
		seen[item] = struct{}{}
	}
	var out []string
	for _, item := range after {
		if _, ok := seen[item]; !ok {
			out = append(out, item)
		}
	}
	return out
}

// pluginsFileMentions reads the hash-matched registry content we already have on
// disk. It re-reads rather than caching because the fingerprint is taken
// immediately before or after the file is touched.
func pluginsFileMentions(stamp FileStamp, id string) bool {
	if !stamp.Exists {
		return false
	}
	data, err := os.ReadFile(stamp.Path)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), id)
}

func short(hash string) string {
	if len(hash) <= 12 {
		return hash
	}
	return hash[:12]
}
