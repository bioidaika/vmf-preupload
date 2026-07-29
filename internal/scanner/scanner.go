package scanner

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bioidaika/vmf-preupload/internal/metadata"
	"github.com/bioidaika/vmf-preupload/pkg/api"
)

var mediaExtensions = map[string]bool{
	".3g2": true, ".3gp": true, ".asf": true, ".avi": true,
	".divx": true, ".f4v": true, ".flv": true, ".m2ts": true,
	".m4v": true, ".mkv": true, ".mov": true, ".mp4": true,
	".mpe": true, ".mpeg": true, ".mpg": true, ".mpv": true,
	".mts": true, ".mxf": true, ".ogv": true, ".rm": true,
	".rmvb": true, ".ts": true, ".vob": true, ".webm": true,
	".wmv": true,
}

var audioExtensions = map[string]bool{
	".aac": true, ".ac3": true, ".dts": true, ".eac3": true,
	".flac": true, ".m4a": true, ".mka": true, ".mp3": true,
	".ogg": true, ".opus": true, ".wav": true,
}

var subtitleExtensions = map[string]bool{
	".ass": true, ".idx": true, ".smi": true, ".srt": true,
	".ssa": true, ".sub": true, ".sup": true, ".vtt": true,
}

var imageExtensions = map[string]bool{
	".bmp": true, ".gif": true, ".jpeg": true, ".jpg": true,
	".png": true, ".tif": true, ".tiff": true, ".webp": true,
}

func ScanPath(ctx context.Context, path, mediaInfoBin string) (api.ScanResult, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return api.ScanResult{}, err
	}
	stat, err := os.Lstat(path)
	if err != nil {
		return api.ScanResult{}, err
	}
	result := api.ScanResult{Root: path, Assets: []api.Asset{}, ExtraFiles: []api.ExtraFile{}, Complete: true, Warnings: []string{}}
	if !stat.IsDir() {
		if !stat.Mode().IsRegular() {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: skipped non-regular filesystem entry", path))
			return result, nil
		}
		if isMediaFilename(filepath.Base(path)) && !isExcludedSample(filepath.Base(path)) {
			asset := scanFile(ctx, path, path, mediaInfoBin)
			result.Assets = append(result.Assets, asset)
		} else {
			result.ExtraFiles = append(result.ExtraFiles, extraFile(path, path, stat))
		}
		return result, nil
	}
	err = filepath.WalkDir(path, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			result.Complete = false
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: %v", current, walkErr))
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			result.Complete = false
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: %v", current, infoErr))
			return nil
		}
		// Do not follow or move symlinks, sockets, devices, or other special
		// entries. The Extras workflow is deliberately limited to regular files.
		if !info.Mode().IsRegular() {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: skipped non-regular filesystem entry", current))
			return nil
		}
		if underExtraDirectory(path, current) {
			// Extras and dot-directories are outside the upload payload at every
			// nesting level. Keep every regular file there in the extra inventory,
			// including video extensions, so a later scan cannot rename trailers,
			// samples, cache files, or content already organized by the app.
			result.ExtraFiles = append(result.ExtraFiles, extraFile(current, path, info))
			return nil
		}
		if !isMediaFilename(entry.Name()) {
			result.ExtraFiles = append(result.ExtraFiles, extraFile(current, path, info))
			return nil
		}
		if isExcludedSample(entry.Name()) {
			// Samples are videos, but they are not upload payload and therefore
			// belong in the same Extras inventory as NFO/images/subtitles.
			result.ExtraFiles = append(result.ExtraFiles, extraFile(current, path, info))
			return nil
		}
		result.Assets = append(result.Assets, scanFile(ctx, current, path, mediaInfoBin))
		return nil
	})
	if err != nil {
		return result, err
	}
	sort.Slice(result.Assets, func(i, j int) bool { return result.Assets[i].Path < result.Assets[j].Path })
	sort.Slice(result.ExtraFiles, func(i, j int) bool { return result.ExtraFiles[i].Path < result.ExtraFiles[j].Path })
	return result, nil
}

func isMediaFilename(name string) bool {
	return mediaExtensions[strings.ToLower(filepath.Ext(name))]
}

func isExcludedSample(name string) bool {
	lower := strings.ToLower(name)
	if strings.Contains(lower, "!sample") {
		return false
	}
	base := strings.TrimSuffix(lower, filepath.Ext(lower))
	tokens := strings.FieldsFunc(base, func(r rune) bool {
		switch r {
		case '.', ' ', '_', '-', '[', ']', '(', ')':
			return true
		}
		return false
	})
	return len(tokens) > 0 && tokens[len(tokens)-1] == "sample"
}

func underExtraDirectory(root, path string) bool {
	relativePath, err := filepath.Rel(root, path)
	if err != nil || relativePath == "." || filepath.IsAbs(relativePath) {
		return false
	}
	parts := strings.FieldsFunc(relativePath, func(r rune) bool { return r == '/' || r == '\\' })
	if len(parts) < 2 {
		return false
	}
	for _, directory := range parts[:len(parts)-1] {
		if strings.EqualFold(directory, "Extras") || strings.EqualFold(directory, "Sample") || strings.HasPrefix(directory, ".") && directory != "." && directory != ".." {
			return true
		}
	}
	return false
}

func extraFile(path, root string, stat fs.FileInfo) api.ExtraFile {
	return api.ExtraFile{
		Path:         path,
		RelativePath: relative(root, path),
		Name:         filepath.Base(path),
		Kind:         extraFileKind(filepath.Ext(path)),
		Size:         stat.Size(),
		ModifiedAt:   stat.ModTime(),
	}
}

func extraFileKind(extension string) string {
	extension = strings.ToLower(extension)
	switch {
	case audioExtensions[extension]:
		return "audio"
	case subtitleExtensions[extension]:
		return "subtitle"
	case imageExtensions[extension]:
		return "image"
	default:
		return "other"
	}
}

func scanFile(ctx context.Context, path, root, mediaInfoBin string) api.Asset {
	stat, statErr := os.Stat(path)
	content := metadata.ParseFilename(filepath.Base(path))
	technical, warnings := metadata.Extract(ctx, path, mediaInfoBin)
	technical.Source = content.Source
	technical.ReleaseType = content.ReleaseType
	if technical.Resolution != "" {
		content.Resolution = technical.Resolution
	}
	if technical.VideoCodec != "" {
		content.VideoCodec = technical.VideoCodec
		content.VideoEncode = technical.VideoEncode
	}
	if technical.HDR != "" {
		content.HDR = technical.HDR
	}
	content.Audio = primaryAudio(technical.Tracks)
	content.AudioTracks = audioTracks(technical.Tracks)
	content.ReleaseGroup = normalizeGroup(content.ReleaseGroup)
	// UHD is filename evidence only. MediaInfo and other technical facts must
	// never manufacture this release-name token.
	technical.ExplicitUHD = content.UHD != ""
	asset := api.Asset{Path: path, RelativePath: relative(root, path), Name: filepath.Base(path), Content: content, Technical: technical, Warnings: warnings}
	if statErr == nil {
		asset.Size = stat.Size()
		asset.ModifiedAt = stat.ModTime()
	} else {
		asset.Warnings = append(asset.Warnings, statErr.Error())
	}
	return asset
}

func relative(root, path string) string {
	value, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.Base(path)
	}
	return value
}

func normalizeGroup(value string) string {
	value = strings.TrimSpace(strings.TrimPrefix(value, "-"))
	if value == "" || strings.EqualFold(value, "nogrp") || strings.EqualFold(value, "unknown") {
		return "NoGroup"
	}
	return value
}

func audioTracks(tracks []api.Track) []api.Track {
	result := []api.Track{}
	for _, track := range tracks {
		if strings.EqualFold(track.Type, "Audio") {
			result = append(result, track)
		}
	}
	return result
}

func primaryAudio(tracks []api.Track) string {
	audio := audioTracks(tracks)
	if len(audio) == 0 {
		return ""
	}
	selected := audio[0]
	for _, track := range audio {
		if track.Default && !track.Commentary {
			selected = track
			break
		}
	}
	parts := []string{selected.Codec, selected.Channels}
	if selected.Atmos {
		parts = append(parts, "Atmos")
	}
	return strings.Join(nonEmpty(parts), ".")
}

func nonEmpty(values []string) []string {
	result := []string{}
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			result = append(result, strings.TrimSpace(value))
		}
	}
	return result
}
