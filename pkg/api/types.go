package api

import "time"

// Settings is the non-secret portion of the application configuration. API
// keys are kept here for the first local-only milestone; the settings service
// is the seam where an OS credential-store implementation can be added.
type Settings struct {
	TMDBAPIKey   string `json:"tmdbApiKey,omitempty"`
	TVDBAPIKey   string `json:"tvdbApiKey,omitempty"`
	TVDBPIN      string `json:"tvdbPin,omitempty"`
	ReleaseGroup string `json:"releaseGroup"`
	Separator    string `json:"separator"`
	// IncludeUHD is retained for old settings files and ignored. Only an
	// explicit UHD/Ultra HD marker in the original filename is preserved.
	IncludeUHD   bool   `json:"includeUhd,omitempty"`
	Profile      string `json:"profile"`
	MediaInfoBin string `json:"mediaInfoBin,omitempty"`
}

type ProviderCandidate struct {
	Provider   string `json:"provider"`
	ID         string `json:"id"`
	Title      string `json:"title"`
	Original   string `json:"original,omitempty"`
	Year       string `json:"year,omitempty"`
	Overview   string `json:"overview,omitempty"`
	PosterPath string `json:"posterPath,omitempty"`
	MediaType  string `json:"mediaType,omitempty"`
}

type Track struct {
	Type       string `json:"type"`
	Index      int    `json:"index"`
	Language   string `json:"language,omitempty"`
	Title      string `json:"title,omitempty"`
	Codec      string `json:"codec,omitempty"`
	Channels   string `json:"channels,omitempty"`
	Default    bool   `json:"default"`
	Forced     bool   `json:"forced"`
	Commentary bool   `json:"commentary"`
	Atmos      bool   `json:"atmos"`
}

type TechnicalInfo struct {
	Container       string  `json:"container,omitempty"`
	Resolution      string  `json:"resolution,omitempty"`
	Width           int     `json:"width,omitempty"`
	Height          int     `json:"height,omitempty"`
	Source          string  `json:"source,omitempty"`
	ReleaseType     string  `json:"releaseType,omitempty"`
	VideoCodec      string  `json:"videoCodec,omitempty"`
	VideoEncode     string  `json:"videoEncode,omitempty"`
	HDR             string  `json:"hdr,omitempty"`
	ExplicitUHD     bool    `json:"explicitUhd"`
	Bitrate         int64   `json:"bitrate,omitempty"`
	DurationSeconds float64 `json:"durationSeconds,omitempty"`
	Tracks          []Track `json:"tracks,omitempty"`
	RawJSON         string  `json:"rawJson,omitempty"`
	RawText         string  `json:"rawText,omitempty"`
}

type ContentInfo struct {
	Category      string  `json:"category"` // MOVIE or TV
	Title         string  `json:"title,omitempty"`
	OriginalTitle string  `json:"originalTitle,omitempty"`
	Year          string  `json:"year,omitempty"`
	Season        string  `json:"season,omitempty"`
	Episode       string  `json:"episode,omitempty"`
	EpisodeTitle  string  `json:"episodeTitle,omitempty"`
	TMDBID        string  `json:"tmdbId,omitempty"`
	TVDBID        string  `json:"tvdbId,omitempty"`
	IMDBID        string  `json:"imdbId,omitempty"`
	Service       string  `json:"service,omitempty"`
	Edition       string  `json:"edition,omitempty"`
	Hybrid        string  `json:"hybrid,omitempty"`
	Repack        string  `json:"repack,omitempty"`
	ReleaseGroup  string  `json:"releaseGroup,omitempty"`
	Audio         string  `json:"audio,omitempty"`
	VideoCodec    string  `json:"videoCodec,omitempty"`
	VideoEncode   string  `json:"videoEncode,omitempty"`
	HDR           string  `json:"hdr,omitempty"`
	Resolution    string  `json:"resolution,omitempty"`
	Source        string  `json:"source,omitempty"`
	ReleaseType   string  `json:"releaseType,omitempty"`
	UHD           string  `json:"uhd,omitempty"`
	AudioTracks   []Track `json:"audioTracks,omitempty"`
}

type Asset struct {
	Path         string        `json:"path"`
	RelativePath string        `json:"relativePath,omitempty"`
	Name         string        `json:"name"`
	IsDirectory  bool          `json:"isDirectory"`
	Size         int64         `json:"size,omitempty"`
	ModifiedAt   time.Time     `json:"modifiedAt,omitempty"`
	Content      ContentInfo   `json:"content"`
	Technical    TechnicalInfo `json:"technical"`
	Warnings     []string      `json:"warnings,omitempty"`
}

type ScanResult struct {
	Root     string   `json:"root"`
	Assets   []Asset  `json:"assets"`
	Warnings []string `json:"warnings,omitempty"`
}

type RenameEntry struct {
	OldPath string `json:"oldPath"`
	NewPath string `json:"newPath"`
	Kind    string `json:"kind"` // file or directory
	Size    int64  `json:"size,omitempty"`
}

type RenamePlan struct {
	ID        string        `json:"id"`
	Root      string        `json:"root"`
	Entries   []RenameEntry `json:"entries"`
	Warnings  []string      `json:"warnings,omitempty"`
	Ready     bool          `json:"ready"`
	CreatedAt time.Time     `json:"createdAt"`
}

type RenameRequest struct {
	Scan     ScanResult  `json:"scan"`
	Settings Settings    `json:"settings"`
	Content  ContentInfo `json:"content,omitempty"`
	Apply    bool        `json:"apply"`
}

type RenameResult struct {
	TransactionID string   `json:"transactionId,omitempty"`
	Applied       []string `json:"applied,omitempty"`
	RolledBack    bool     `json:"rolledBack"`
	Warnings      []string `json:"warnings,omitempty"`
}
