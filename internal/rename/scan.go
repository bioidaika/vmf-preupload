package rename

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Scan performs a read-only filesystem walk.  It uses Lstat/WalkDir and does
// not follow symlinks, preventing a symlink inside a release from expanding
// into an unrelated tree.  Results are sorted by case-insensitive path for a
// stable GUI preview.
func Scan(ctx context.Context, root string, options ScanOptions) (ScanResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	absoluteRoot, err := absoluteClean(root, "")
	if err != nil {
		return ScanResult{}, err
	}
	info, err := os.Lstat(absoluteRoot)
	if err != nil {
		return ScanResult{}, fmt.Errorf("scan root: %w", err)
	}
	result := ScanResult{Root: absoluteRoot}
	includeRoot := options.IncludeRoot
	if options.Recursive {
		walkErr := filepath.WalkDir(absoluteRoot, func(path string, entry fs.DirEntry, walkErr error) error {
			if cancelErr := contextErr(ctx); cancelErr != nil {
				return cancelErr
			}
			if walkErr != nil {
				return walkErr
			}
			if path != absoluteRoot && !options.IncludeHidden && isHidden(filepath.Base(path)) {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if path != absoluteRoot && options.MaxDepth > 0 && pathDepthRelative(path, absoluteRoot) > options.MaxDepth {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if path == absoluteRoot && !includeRoot {
				return nil
			}
			e, err := entryToEntry(path, entry)
			if err != nil {
				return err
			}
			e.Relative, _ = filepath.Rel(absoluteRoot, path)
			if e.Relative == "." {
				e.Relative = ""
			}
			result.Entries = append(result.Entries, e)
			// WalkDir does not follow symlink directories by default.  Be
			// explicit for portability and to document the safety invariant.
			if entry.Type()&os.ModeSymlink != 0 && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		})
		if walkErr != nil {
			return ScanResult{}, fmt.Errorf("scan %q: %w", absoluteRoot, walkErr)
		}
	} else if info.IsDir() {
		entries, err := os.ReadDir(absoluteRoot)
		if err != nil {
			return ScanResult{}, fmt.Errorf("read scan root: %w", err)
		}
		if includeRoot {
			e, err := infoToEntry(absoluteRoot, info)
			if err != nil {
				return ScanResult{}, err
			}
			result.Entries = append(result.Entries, e)
		}
		for _, entry := range entries {
			if cancelErr := contextErr(ctx); cancelErr != nil {
				return ScanResult{}, cancelErr
			}
			if !options.IncludeHidden && isHidden(entry.Name()) {
				continue
			}
			path := filepath.Join(absoluteRoot, entry.Name())
			e, err := dirEntryToEntry(path, entry)
			if err != nil {
				return ScanResult{}, err
			}
			e.Relative = entry.Name()
			result.Entries = append(result.Entries, e)
		}
	} else {
		if includeRoot {
			e, err := infoToEntry(absoluteRoot, info)
			if err != nil {
				return ScanResult{}, err
			}
			result.Entries = append(result.Entries, e)
		}
	}
	sortedPaths(result.Entries)
	return result, nil
}

// ScanPath is a convenience wrapper for callers without a context.  With no
// option it recursively scans the complete selected path; accepting an
// optional value also keeps it convenient for a Wails bridge and tests.
func ScanPath(root string, options ...ScanOptions) (ScanResult, error) {
	settings := ScanOptions{Recursive: true, IncludeRoot: true, IncludeHidden: true}
	if len(options) > 0 {
		settings = options[0]
	}
	return Scan(context.Background(), root, settings)
}

func entryToEntry(path string, entry fs.DirEntry) (Entry, error) {
	info, err := entry.Info()
	if err != nil {
		return Entry{}, fmt.Errorf("stat %q: %w", path, err)
	}
	return infoToEntry(path, info)
}

func dirEntryToEntry(path string, entry os.DirEntry) (Entry, error) {
	return entryToEntry(path, entry)
}

func infoToEntry(path string, info os.FileInfo) (Entry, error) {
	e := Entry{Path: path, Kind: kindFromFileInfo(info), Size: info.Size(), Mode: uint32(info.Mode()), ModTime: info.ModTime().UTC(), ReadOnly: info.Mode().Perm()&0200 == 0, IsHidden: isHidden(filepath.Base(path))}
	if info.Mode()&os.ModeSymlink != 0 {
		if target, err := os.Readlink(path); err == nil {
			e.LinkTarget = target
		}
	}
	return e, nil
}

func isHidden(name string) bool {
	return strings.HasPrefix(name, ".")
}

func pathDepthRelative(path, root string) int {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return 0
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return 0
	}
	return len(strings.Split(rel, string(filepath.Separator)))
}
