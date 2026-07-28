package app

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/bioidaika/vmf-preupload/internal/metadata"
	"github.com/bioidaika/vmf-preupload/pkg/api"
)

var (
	wordSeasonDirectory  = regexp.MustCompile(`(?i)^(?:season|mùa|mua)[. _-]+(\d{1,2})$`)
	shortSeasonDirectory = regexp.MustCompile(`(?i)^s[. _-]*(\d{1,2})$`)
	p2pSeasonToken       = regexp.MustCompile(`(?i)(?:^|[. _-])S(\d{1,2})(?:$|[. _-])`)
	seasonResolution     = regexp.MustCompile(`(?i)(?:^|[. _-])(?:4320|2160|1440|1080|720|576|480)[pi](?:$|[. _-])`)
	seasonGroupSuffix    = regexp.MustCompile(`-[A-Za-z0-9][A-Za-z0-9_]{0,47}$`)
	episodeOnlyName      = regexp.MustCompile(`(?i)^(?:e|ep|episode|tập|tap)[. _-]*(\d{1,3})$`)
	numericEpisodeName   = regexp.MustCompile(`^(\d{1,3})$`)
)

// tvSeasonLayout describes how TV episodes are arranged below the selected
// root. Season directories are intentionally limited to direct children with
// explicit names such as "Season 1"/"S01", or an already-compliant P2P
// season basename. Arbitrary subdirectories are never renamed by inference.
type tvSeasonLayout struct {
	VideoCount       int
	SeriesRoot       bool
	MultiSeason      bool
	Seasons          []string
	Directories      []tvSeasonDirectory
	AssetDirectory   map[string]string
	AssetSeason      map[string]string
	AssetEpisode     map[string]string
	DirectVideoCount int
	ValidationErrors []string
}

type tvSeasonDirectory struct {
	Source string
	Season string
	Assets []api.Asset
}

func analyzeTVSeasonLayout(root string, assets []api.Asset) tvSeasonLayout {
	layout := tvSeasonLayout{
		AssetDirectory: map[string]string{},
		AssetSeason:    map[string]string{},
		AssetEpisode:   map[string]string{},
	}
	seasonSet := map[string]bool{}
	directories := map[string]*tvSeasonDirectory{}

	for _, asset := range assets {
		if !isVideoAsset(asset) {
			continue
		}
		layout.VideoCount++
		assetKey := appPathKey(asset.Path)
		assetSeason := canonicalSeason(asset.Content.Season)
		if asset.Content.Episode != "" {
			layout.AssetEpisode[assetKey] = asset.Content.Episode
		}

		relative := filepath.Clean(asset.RelativePath)
		parts := splitRelativePath(relative)
		if len(parts) == 1 {
			layout.DirectVideoCount++
		}
		if len(parts) >= 2 {
			folderName := parts[0]
			if directorySeason, ok := seasonFromDirectoryName(folderName); ok {
				source := filepath.Join(root, folderName)
				sourceKey := appPathKey(source)
				directory := directories[sourceKey]
				if directory == nil {
					directory = &tvSeasonDirectory{Source: source, Season: directorySeason}
					directories[sourceKey] = directory
				}
				directory.Assets = append(directory.Assets, asset)
				layout.AssetDirectory[assetKey] = source
				if assetSeason == "" {
					assetSeason = directorySeason
				} else if assetSeason != directorySeason {
					layout.ValidationErrors = appendUniqueString(layout.ValidationErrors,
						fmt.Sprintf("season folder %q is S%s but contains %s", folderName, directorySeason, asset.Name))
				}
				seasonSet[directorySeason] = true
				if layout.AssetEpisode[assetKey] == "" {
					layout.AssetEpisode[assetKey] = episodeFromSeasonFilename(asset.Name)
				}
			}
		}

		if assetSeason != "" {
			layout.AssetSeason[assetKey] = assetSeason
			seasonSet[assetSeason] = true
		}
	}

	for _, directory := range directories {
		layout.Directories = append(layout.Directories, *directory)
	}
	if len(layout.Directories) == 0 {
		if rootSeason, ok := seasonFromDirectoryName(filepath.Base(root)); ok {
			seasonSet[rootSeason] = true
			for _, asset := range assets {
				if !isVideoAsset(asset) {
					continue
				}
				assetKey := appPathKey(asset.Path)
				assetSeason := layout.AssetSeason[assetKey]
				switch {
				case assetSeason == "":
					layout.AssetSeason[assetKey] = rootSeason
				case assetSeason != rootSeason:
					layout.ValidationErrors = appendUniqueString(layout.ValidationErrors,
						fmt.Sprintf("selected season folder is S%s but contains %s", rootSeason, asset.Name))
				}
				if layout.AssetEpisode[assetKey] == "" {
					layout.AssetEpisode[assetKey] = episodeFromSeasonFilename(asset.Name)
				}
			}
		}
	}
	sort.Slice(layout.Directories, func(i, j int) bool {
		return appPathKey(layout.Directories[i].Source) < appPathKey(layout.Directories[j].Source)
	})
	for season := range seasonSet {
		layout.Seasons = append(layout.Seasons, season)
	}
	sort.Strings(layout.Seasons)
	layout.MultiSeason = len(layout.Seasons) > 1
	layout.SeriesRoot = len(layout.Directories) > 0 || layout.MultiSeason
	if layout.MultiSeason {
		for _, asset := range assets {
			if isVideoAsset(asset) && layout.AssetSeason[appPathKey(asset.Path)] == "" {
				layout.ValidationErrors = appendUniqueString(layout.ValidationErrors,
					fmt.Sprintf("could not determine a season for %s in a multi-season selection", asset.Name))
			}
		}
	}
	return layout
}

func tvSeasonLayoutWarnings(layout tvSeasonLayout) []string {
	warnings := []string{}
	if layout.MultiSeason && len(layout.Directories) == 0 {
		warnings = append(warnings, "flat multi-season layout detected; episode files will remain in the series folder")
	}
	if layout.DirectVideoCount > 0 && len(layout.Directories) > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"mixed season layout detected; %d episode file(s) at the series root will remain there",
			layout.DirectVideoCount,
		))
	}
	return warnings
}

func mappedAssetDirectory(asset api.Asset, newRoot string, layout tvSeasonLayout, seasonDestinations map[string]string) string {
	sourceDirectory := layout.AssetDirectory[appPathKey(asset.Path)]
	if sourceDirectory != "" {
		if destinationDirectory := seasonDestinations[appPathKey(sourceDirectory)]; destinationDirectory != "" {
			remainder, err := filepath.Rel(sourceDirectory, filepath.Dir(asset.Path))
			if err == nil && remainder != "." && remainder != "" {
				return filepath.Join(destinationDirectory, remainder)
			}
			return destinationDirectory
		}
	}
	relativeDirectory := filepath.Dir(asset.RelativePath)
	if relativeDirectory == "." || relativeDirectory == "" {
		return newRoot
	}
	return filepath.Join(newRoot, relativeDirectory)
}

func seasonFromDirectoryName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	for _, pattern := range []*regexp.Regexp{wordSeasonDirectory, shortSeasonDirectory} {
		if match := pattern.FindStringSubmatch(name); match != nil {
			return canonicalSeason(match[1]), true
		}
	}
	if match := p2pSeasonToken.FindStringSubmatch(name); match != nil {
		if metadata.IsP2PReleaseFolderName(name) || seasonResolution.MatchString(name) && seasonGroupSuffix.MatchString(name) {
			return canonicalSeason(match[1]), true
		}
	}
	return "", false
}

func canonicalSeason(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimLeft(strings.TrimPrefix(strings.ToUpper(value), "S"), " ._-")
	if value == "" {
		return ""
	}
	number, err := strconv.Atoi(value)
	if err != nil || number < 0 || number > 99 {
		return ""
	}
	return fmt.Sprintf("%02d", number)
}

func episodeFromSeasonFilename(filename string) string {
	base := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
	for _, pattern := range []*regexp.Regexp{episodeOnlyName, numericEpisodeName} {
		if match := pattern.FindStringSubmatch(strings.TrimSpace(base)); match != nil {
			number, err := strconv.Atoi(match[1])
			if err == nil && number >= 0 && number <= 999 {
				return strconv.Itoa(number)
			}
		}
	}
	return ""
}

func splitRelativePath(relative string) []string {
	if relative == "" || relative == "." {
		return nil
	}
	parts := []string{}
	for _, part := range strings.FieldsFunc(relative, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part != "" && part != "." {
			parts = append(parts, part)
		}
	}
	return parts
}

func appPathKey(path string) string {
	return strings.ToLower(filepath.Clean(path))
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
