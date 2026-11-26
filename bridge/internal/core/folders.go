package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// folderEntryLimit caps one directory listing.
//
// A home directory can hold thousands of entries, and a phone dialog cannot use more than a screenful
// before scrolling becomes the whole task. The phone is told the list was cut short and can type the
// rest of the path instead, which is the same route it already has for a folder the list cannot show.
const folderEntryLimit = 500

// FolderEntry is one folder the phone may open.
type FolderEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// FoldersView is one directory, as the phone's folder picker sees it.
type FoldersView struct {
	Path      string        `json:"path"`
	Parent    string        `json:"parent"`
	Home      string        `json:"home"`
	Entries   []FolderEntry `json:"entries"`
	Truncated bool          `json:"truncated"`
}

// Folders lists the folders inside one directory on the computer.
//
// This is a read: it creates nothing and touches no herdr state. The picker starts at a folder the
// phone names (last used, or home), and every step up or down is another read of this endpoint.
func (c *Core) Folders(ctx context.Context, hostID, path string) (FoldersView, error) {
	machine, err := c.hostMachine(ctx, hostID)
	if err != nil {
		return FoldersView{}, err
	}
	if machine != "" {
		return FoldersView{}, fmt.Errorf("browsing folders on another computer is not supported yet")
	}
	return listFolders(path)
}

// listFolders resolves and validates one folder, then lists the folders inside it.
//
// Directories are the only entries: a file cannot be a working folder, and showing one in the picker
// would be a row that does nothing when tapped. Dot-directories are hidden because a home directory
// is mostly configuration and caches to a person choosing where a project lives; the manual path
// field can still reach one.
func listFolders(requested string) (FoldersView, error) {
	path, err := resolveFolder(requested)
	if err != nil {
		return FoldersView{}, err
	}
	read, err := os.ReadDir(path)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return FoldersView{}, fmt.Errorf("Perch is not allowed to read that folder: %s", path)
		}
		return FoldersView{}, fmt.Errorf("Perch could not read that folder: %w", err)
	}

	out := FoldersView{
		Path:    path,
		Entries: make([]FolderEntry, 0, len(read)),
	}
	if home := homeDir(); home != "" {
		out.Home = filepath.Clean(home)
	}
	for _, entry := range read {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		full := filepath.Join(path, name)
		if !isFolder(entry, full) {
			continue
		}
		if len(out.Entries) >= folderEntryLimit {
			out.Truncated = true
			break
		}
		out.Entries = append(out.Entries, FolderEntry{Name: name, Path: full})
	}
	sort.Slice(out.Entries, func(i, j int) bool {
		left, right := strings.ToLower(out.Entries[i].Name), strings.ToLower(out.Entries[j].Name)
		if left == right {
			return out.Entries[i].Name < out.Entries[j].Name
		}
		return left < right
	})
	if parent := filepath.Dir(path); parent != path {
		out.Parent = parent
	}
	return out, nil
}

// resolveFolder turns a requested path into one absolute folder that exists.
//
// It is shared by the picker and by project creation on purpose: the picker and the create call must
// agree about what "this folder" means, or the phone could show a folder and then create the project
// somewhere else. An absolute path does not need a home folder; only blank and "~" do, and saying
// "could not work out your home folder" for a perfectly valid absolute path would be a lie.
func resolveFolder(requested string) (string, error) {
	path, err := folderPath(requested, homeDir())
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("that folder does not exist on your computer: %s", path)
		}
		if errors.Is(err, os.ErrPermission) {
			return "", fmt.Errorf("Perch is not allowed to read that folder: %s", path)
		}
		return "", fmt.Errorf("Perch could not read that folder: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("that is a file, not a folder: %s", path)
	}
	return path, nil
}

// isFolder follows a symlink when deciding, because a symlinked project directory is a project
// directory as far as the person choosing it is concerned.
func isFolder(entry os.DirEntry, path string) bool {
	if entry.IsDir() {
		return true
	}
	if entry.Type()&os.ModeSymlink == 0 {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// folderPath turns whatever the phone sent into one clean absolute path.
//
// Blank means home, and "~" is expanded, because those are the two ways a person types a folder and
// neither one is a guess about which project they meant. A relative path is refused rather than
// resolved against whatever directory the bridge happens to have been started in: the phone's "here"
// and the daemon's "here" are not the same place, and silently picking one would create a project in
// a folder nobody chose.
func folderPath(requested, home string) (string, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" || requested == "~" {
		if home == "" {
			return "", errors.New("Perch could not work out your home folder")
		}
		return filepath.Clean(home), nil
	}
	if strings.HasPrefix(requested, "~/") || strings.HasPrefix(requested, `~\`) {
		if home == "" {
			return "", errors.New("Perch could not work out your home folder")
		}
		requested = filepath.Join(home, requested[2:])
	}
	if !filepath.IsAbs(requested) {
		example := "/home/you/projects"
		if home != "" {
			example = filepath.Join(home, "projects")
		}
		return "", fmt.Errorf("use a full folder path, like %s", example)
	}
	return filepath.Clean(requested), nil
}
