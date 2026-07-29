package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bioidaika/vmf-preupload/internal/rename"
	"github.com/bioidaika/vmf-preupload/pkg/api"
)

const extrasDirectoryName = "Extras"

type extraMovePlan struct {
	Requests             []rename.RenameRequest
	CreateDirectories    []string
	DisplayItems         []RenameItem
	Errors               []string
	ConflictingPaths     map[string]bool
	MovedFileCount       int
	DestinationDirectory string
}

// buildExtraMovePlan isolates every non-payload file from the directories a
// tracker will receive. A multi-season series root is already a container, so
// Extras can live directly below it. A movie or single-season root is itself
// the upload unit; its Extras tree therefore lives in a namespaced sibling:
//
//	<parent>/Extras/<release folder name>/...
//
// Relative source paths are retained below that boundary. Besides avoiding
// basename collisions, this makes the move reversible without guessing which
// season an NFO, image, subtitle, or external-audio file belonged to.
func buildExtraMovePlan(root, newRoot string, seriesRoot bool, files []api.ExtraFile, occupiedDestinations map[string]bool, existingCreateDirectories []string) extraMovePlan {
	result := extraMovePlan{ConflictingPaths: map[string]bool{}}
	if len(files) == 0 {
		return result
	}

	extrasContainer := ""
	if seriesRoot {
		extrasContainer = filepath.Join(root, extrasDirectoryName)
		result.DestinationDirectory = extrasContainer
	} else {
		extrasContainer = filepath.Join(filepath.Dir(root), extrasDirectoryName)
		result.DestinationDirectory = filepath.Join(extrasContainer, filepath.Base(newRoot))
	}

	plannedDirectories := make(map[string]bool, len(existingCreateDirectories))
	for _, directory := range existingCreateDirectories {
		plannedDirectories[appPathKey(directory)] = true
	}

	ordered := append([]api.ExtraFile(nil), files...)
	sort.Slice(ordered, func(i, j int) bool { return appPathKey(ordered[i].Path) < appPathKey(ordered[j].Path) })
	for _, file := range ordered {
		relative, err := safeExtraRelativePath(root, file)
		if err != nil {
			result.Errors = appendUniqueString(result.Errors, fmt.Sprintf("extra file %q: %v", file.Path, err))
			result.DisplayItems = append(result.DisplayItems, RenameItem{OldPath: file.Path, NewPath: file.Path, Kind: "file", Status: "conflict"})
			continue
		}
		destination := filepath.Join(result.DestinationDirectory, relative)
		if !appPathWithin(destination, result.DestinationDirectory) {
			result.Errors = appendUniqueString(result.Errors, "extra destination escapes Extras: "+destination)
			result.DisplayItems = append(result.DisplayItems, RenameItem{OldPath: file.Path, NewPath: destination, Kind: "file", Status: "conflict"})
			continue
		}

		// A completed series-container plan inventories files already below its
		// top-level Extras folder. Map those files to themselves and omit the
		// no-op from preview so rebuilding a plan is idempotent.
		if appPathKey(file.Path) == appPathKey(destination) {
			occupiedDestinations[appPathKey(destination)] = true
			continue
		}

		destinationKey := appPathKey(destination)
		if occupiedDestinations[destinationKey] {
			result.Errors = appendUniqueString(result.Errors, "duplicate extra destination: "+destination)
			result.ConflictingPaths[destinationKey] = true
			result.DisplayItems = append(result.DisplayItems, RenameItem{OldPath: file.Path, NewPath: destination, Kind: "file", Status: "conflict"})
			continue
		}
		occupiedDestinations[destinationKey] = true

		missing, directoryErr := missingExtraDirectories(filepath.Dir(destination), result.DestinationDirectory, extrasContainer, plannedDirectories)
		if directoryErr != nil {
			result.Errors = appendUniqueString(result.Errors, fmt.Sprintf("extra destination %q: %v", destination, directoryErr))
			result.DisplayItems = append(result.DisplayItems, RenameItem{OldPath: file.Path, NewPath: destination, Kind: "file", Status: "conflict"})
			continue
		}
		result.CreateDirectories = append(result.CreateDirectories, missing...)
		result.Requests = append(result.Requests, rename.RenameRequest{Source: file.Path, Destination: destination})
		result.MovedFileCount++
	}
	return result
}

func safeExtraRelativePath(root string, file api.ExtraFile) (string, error) {
	relative, err := filepath.Rel(root, file.Path)
	if err != nil {
		return "", fmt.Errorf("resolve relative path: %w", err)
	}
	relative = filepath.Clean(relative)
	if relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid relative path %q", relative)
	}
	parts := splitRelativePath(relative)
	if len(parts) > 1 && strings.EqualFold(parts[0], extrasDirectoryName) {
		parts = parts[1:]
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("relative path is empty")
	}
	return filepath.Join(parts...), nil
}

func missingExtraDirectories(parent, boundary, container string, planned map[string]bool) ([]string, error) {
	parent = filepath.Clean(parent)
	boundary = filepath.Clean(boundary)
	container = filepath.Clean(container)
	if !appPathWithin(parent, boundary) {
		return nil, fmt.Errorf("directory %q is outside Extras", parent)
	}
	if !appPathWithin(boundary, container) {
		return nil, fmt.Errorf("Extras boundary %q is outside its container", boundary)
	}
	relative, err := filepath.Rel(container, parent)
	if err != nil || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("directory %q is outside Extras container", parent)
	}

	directories := []string{container}
	if relative != "." {
		current := container
		for _, part := range splitRelativePath(relative) {
			current = filepath.Join(current, part)
			directories = append(directories, current)
		}
	}

	missing := []string{}
	for _, current := range directories {
		info, statErr := os.Lstat(current)
		switch {
		case statErr == nil:
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return nil, fmt.Errorf("refuse to use Extras path which is not a real directory: %s", current)
			}
		case os.IsNotExist(statErr):
			key := appPathKey(current)
			if !planned[key] {
				missing = append(missing, current)
			}
		case statErr != nil:
			return nil, fmt.Errorf("inspect directory %q: %w", current, statErr)
		}
	}

	for _, directory := range missing {
		planned[appPathKey(directory)] = true
	}
	return missing, nil
}

func appPathWithin(path, root string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil || filepath.IsAbs(relative) {
		return false
	}
	return relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
