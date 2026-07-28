package app

// These DTOs intentionally mirror the small browser-facing contract in
// frontend/src/types.ts. Keeping the Wails boundary stable lets the frontend
// run in a browser preview without importing generated bindings.

type TechnicalMetadata struct {
	MediaType     string `json:"mediaType"`
	Title         string `json:"title"`
	OriginalTitle string `json:"originalTitle,omitempty"`
	Year          string `json:"year"`
	Season        string `json:"season"`
	Episode       string `json:"episode"`
	EpisodeTitle  string `json:"episodeTitle"`
	Edition       string `json:"edition"`
	Resolution    string `json:"resolution"`
	Source        string `json:"source"`
	Service       string `json:"service"`
	ReleaseType   string `json:"releaseType"`
	VideoCodec    string `json:"videoCodec"`
	// Always cross the Wails boundary, including as an empty string, so a new
	// scan cannot inherit encoder proof from the previously selected file.
	VideoEncode string `json:"videoEncode"`
	HDR         string `json:"hdr"`
	Audio       string `json:"audio"`
	Languages   string `json:"languages"`
	Group       string `json:"group"`
	UHD         bool   `json:"uhd"`
}

type ScanFile struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
	Size int64  `json:"size,omitempty"`
}

type ScanResult struct {
	RootPath  string            `json:"rootPath"`
	MediaType string            `json:"mediaType"`
	Files     []ScanFile        `json:"files"`
	Metadata  TechnicalMetadata `json:"metadata"`
	// Warnings contains non-fatal scanner/MediaInfo diagnostics. Keeping these
	// at the Wails boundary lets the UI distinguish a complete technical scan
	// from filename-only fallback data.
	Warnings      []string `json:"warnings,omitempty"`
	MediaInfoText string   `json:"mediaInfoText,omitempty"`
	MediaInfoJSON any      `json:"mediaInfoJson,omitempty"`
}

type RenameItem struct {
	OldPath string `json:"oldPath"`
	NewPath string `json:"newPath"`
	Kind    string `json:"kind"`
	Status  string `json:"status,omitempty"`
}

type RenamePlan struct {
	ID          string       `json:"id"`
	Items       []RenameItem `json:"items"`
	ChangeCount int          `json:"changeCount"`
	CanApply    bool         `json:"canApply"`
	// Keep empty collections in the Wails response. The frontend renders these
	// fields immediately after a plan is built, so omitting an empty slice would
	// turn it into undefined in JavaScript.
	Warnings []string `json:"warnings"`
	Errors   []string `json:"errors"`
}

type RenameRequest struct {
	RootPath  string            `json:"rootPath"`
	Metadata  TechnicalMetadata `json:"metadata"`
	Separator string            `json:"separator"`
	// A pointer keeps old clients safe: an omitted value defaults to true.
	PreserveExistingP2P *bool `json:"preserveExistingP2P,omitempty"`
	// IncludeUHD is accepted for vmf@1 client compatibility but deliberately
	// ignored; only an explicit marker in the original filename enables UHD.
	IncludeUHD bool `json:"includeUhd,omitempty"`
}

type Settings struct {
	Separator           string `json:"separator"`
	Group               string `json:"group"`
	PreserveExistingP2P *bool  `json:"preserveExistingP2P,omitempty"`
	// IncludeUHD is a deprecated compatibility field and is not honored.
	IncludeUHD   bool   `json:"includeUhd,omitempty"`
	Profile      string `json:"profile"`
	TMDBAPIKey   string `json:"tmdbApiKey,omitempty"`
	TVDBAPIKey   string `json:"tvdbApiKey,omitempty"`
	TVDBPIN      string `json:"tvdbPin,omitempty"`
	MediaInfoBin string `json:"mediaInfoBin,omitempty"`
}

type SearchResult struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	OriginalTitle string `json:"originalTitle,omitempty"`
	Year          string `json:"year,omitempty"`
	Overview      string `json:"overview,omitempty"`
	PosterURL     string `json:"posterUrl,omitempty"`
}
