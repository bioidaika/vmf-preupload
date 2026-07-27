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
	".mkv": true, ".mp4": true, ".m4v": true, ".ts": true,
	".m2ts": true, ".mov": true, ".avi": true, ".webm": true,
}

func ScanPath(ctx context.Context, path, mediaInfoBin string) (api.ScanResult, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return api.ScanResult{}, err
	}
	stat, err := os.Stat(path)
	if err != nil {
		return api.ScanResult{}, err
	}
	result := api.ScanResult{Root: path, Assets: []api.Asset{}, Warnings: []string{}}
	if !stat.IsDir() {
		asset := scanFile(ctx, path, path, mediaInfoBin)
		result.Assets = append(result.Assets, asset)
		return result, nil
	}
	err = filepath.WalkDir(path, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: %v", current, walkErr))
			return nil
		}
		if entry.IsDir() {
			if current != path && strings.HasPrefix(entry.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if !mediaExtensions[strings.ToLower(filepath.Ext(entry.Name()))] {
			return nil
		}
		if strings.Contains(strings.ToLower(entry.Name()), "sample") && !strings.Contains(strings.ToLower(entry.Name()), "!sample") {
			return nil
		}
		result.Assets = append(result.Assets, scanFile(ctx, current, path, mediaInfoBin))
		return nil
	})
	if err != nil {
		return result, err
	}
	sort.Slice(result.Assets, func(i, j int) bool { return result.Assets[i].Path < result.Assets[j].Path })
	return result, nil
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
