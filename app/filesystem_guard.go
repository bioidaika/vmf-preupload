package app

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// filesystemSnapshot is a lightweight, MediaInfo-free guard captured with a
// preview. Directory rename operations move every child implicitly, so the
// transaction must reject a tree that gained, lost, or changed a file after
// the preview was built.
type filesystemSnapshot struct {
	Root    string
	Entries map[string]filesystemEntry
}

type filesystemEntry struct {
	Kind    fs.FileMode
	Size    int64
	ModTime time.Time
}

func captureFilesystemSnapshot(root string) (filesystemSnapshot, error) {
	root = filepath.Clean(root)
	snapshot := filesystemSnapshot{Root: root, Entries: map[string]filesystemEntry{}}
	info, err := os.Lstat(root)
	if err != nil {
		return filesystemSnapshot{}, fmt.Errorf("inspect selected path for preview guard: %w", err)
	}
	add := func(path string, info fs.FileInfo) {
		snapshot.Entries[appPathKey(path)] = filesystemEntry{Kind: info.Mode(), Size: info.Size(), ModTime: info.ModTime()}
	}
	if !info.IsDir() {
		add(root, info)
		return snapshot, nil
	}
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("inspect %s: %w", path, walkErr)
		}
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("inspect %s: %w", path, err)
		}
		add(path, info)
		// WalkDir does not follow directory symlinks. Recording the link itself
		// still lets Apply notice if it is replaced before the transaction.
		_ = entry
		return nil
	}); err != nil {
		return filesystemSnapshot{}, err
	}
	return snapshot, nil
}

func (snapshot filesystemSnapshot) Equal(other filesystemSnapshot) bool {
	if appPathKey(snapshot.Root) != appPathKey(other.Root) || len(snapshot.Entries) != len(other.Entries) {
		return false
	}
	for path, expected := range snapshot.Entries {
		actual, ok := other.Entries[path]
		if !ok || expected.Kind != actual.Kind || expected.Size != actual.Size || !expected.ModTime.Equal(actual.ModTime) {
			return false
		}
	}
	return true
}
