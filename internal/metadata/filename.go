package metadata

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bioidaika/vmf-preupload/pkg/api"
)

var (
	seasonEpisodePattern = regexp.MustCompile(`(?i)(?:^|[. _-])S(\d{1,2})(?:E(\d{1,3}))?(?:E(\d{1,3}))?`) // S01, S01E02, S01E02E03
	yearPattern          = regexp.MustCompile(`(?:^|[. _()\[\]-])((?:19|20)\d{2})(?:$|[. _()\[\]-])`)
	// Keep this list deliberately bounded and token-delimited.  A generic
	// "word after the resolution" heuristic would mistake title words such as
	// MAX or HBO for a service.  The aliases cover the common scene/Upload
	// Assistant spellings while preserving the canonical short token.
	servicePattern = regexp.MustCompile(`(?i)(?:^|[. _-])(NF|NETFLIX|AMZN|AMAZON|PRIME(?:VIDEO)?|PMTP|PMNP|PMNT|DSNP|DISNEY(?:PLUS)?|ATVP|APPLE(?:TV)?(?:PLUS)?|HMAX|HBOMAX|MAX|HULU|PCOK|CRAV|CRUNCHYROLL|STAN|IP|iP|BBC|HBO|FPT|FPTPLAY|IQIYI|VIKI|ROKU|PARAMOUNT(?:PLUS)?|PARA)(?:$|[. _-])`)
	// Release groups are the final hyphen-delimited component. Restricting the
	// component prevents a technical token such as "-DL.HDR.H.265-GRP" from
	// being swallowed as one giant group.
	groupPattern = regexp.MustCompile(`-([A-Za-z0-9][A-Za-z0-9_]*)$`)
	uhdPattern   = regexp.MustCompile(`(?i)(?:^|[. _-])(?:UHD|ULTRA[. _-]+HD)(?:$|[. _-])`)
)

// ParseFilename extracts hints only. It never treats a hint as authoritative;
// the UI can show and override every value before building a rename plan.
func ParseFilename(filename string) api.ContentInfo {
	base := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
	lower := strings.ToLower(base)
	info := api.ContentInfo{Category: "MOVIE", ReleaseGroup: "NoGroup"}
	if match := seasonEpisodePattern.FindStringSubmatch(base); match != nil {
		info.Category = "TV"
		info.Season = match[1]
		info.Episode = match[2]
	}
	if match := yearPattern.FindStringSubmatch(base); match != nil {
		info.Year = match[1]
	}
	if match := servicePattern.FindStringSubmatch(base); match != nil {
		info.Service = canonicalService(match[1])
	}
	info.ReleaseType, info.Source = detectReleaseTypeAndSource(lower)
	if uhdPattern.MatchString(base) {
		info.UHD = "UHD"
	}
	if match := groupPattern.FindStringSubmatch(base); match != nil && hasReleaseGroupEvidence(base[:matchStart(base, match[0])]) {
		candidate := strings.TrimSpace(match[1])
		if !isStructuralToken(candidate) {
			info.ReleaseGroup = candidate
		}
	}
	// Remove technical suffixes to produce a conservative title hint. The UI
	// and provider search remain authoritative for the final title.
	info.Title = titleHint(base)
	return info
}

func canonicalService(value string) string {
	compact := strings.ToUpper(strings.NewReplacer(" ", "", ".", "", "_", "", "-", "").Replace(strings.TrimSpace(value)))
	switch compact {
	case "NETFLIX", "NF":
		return "NF"
	case "AMAZON", "AMZN", "PRIME", "PRIMEVIDEO", "PMTP", "PMNP", "PMNT":
		// Keep the explicit Prime token when it was present; the PM* aliases
		// are already the canonical scene abbreviations.
		if strings.HasPrefix(compact, "PM") {
			return compact
		}
		return "AMZN"
	case "DISNEY", "DISNEYPLUS", "DSNP":
		return "DSNP"
	case "APPLETV", "APPLETVPLUS", "ATVP":
		return "ATVP"
	case "HBO", "HBOMAX", "HMAX":
		if compact == "HBO" {
			return "HBO"
		}
		return "HMAX"
	case "IP":
		return "iP"
	case "FPTPLAY", "FPT":
		return "FPT"
	case "CRUNCHYROLL":
		return "CR"
	case "PARAMOUNT", "PARAMOUNTPLUS", "PARA":
		return "PARA"
	default:
		return strings.TrimSpace(value)
	}
}

func detectReleaseTypeAndSource(lower string) (string, string) {
	switch {
	case strings.Contains(lower, "webrip") || strings.Contains(lower, "web-rip"):
		return "WEBRIP", "WEB"
	case strings.Contains(lower, "web-dl") || strings.Contains(lower, "webdl") || strings.Contains(lower, "web.dl"):
		return "WEBDL", "WEB"
	case strings.Contains(lower, "remux"):
		if strings.Contains(lower, "bluray") || strings.Contains(lower, "blu-ray") || strings.Contains(lower, "bdmv") {
			return "REMUX", "BluRay"
		}
		if strings.Contains(lower, "hddvd") {
			return "REMUX", "HDDVD"
		}
		return "REMUX", ""
	case strings.Contains(lower, "bluray") || strings.Contains(lower, "blu-ray"):
		return "ENCODE", "BluRay"
	case strings.Contains(lower, "hdtv"):
		return "ENCODE", "HDTV"
	default:
		return "ENCODE", ""
	}
}

func isStructuralToken(value string) bool {
	lower := strings.ToLower(value)
	for _, token := range []string{"web-dl", "webdl", "webrip", "remux", "x264", "x265", "h264", "h265", "nogroup", "nogrp", "unknown"} {
		if lower == token {
			return true
		}
	}
	return false
}

func titleHint(base string) string {
	value := base
	if match := groupPattern.FindStringSubmatch(value); match != nil && hasReleaseGroupEvidence(value[:matchStart(value, match[0])]) {
		value = value[:matchStart(value, match[0])]
	}
	value = seasonEpisodePattern.ReplaceAllString(value, "")
	value = yearPattern.ReplaceAllString(value, " ")
	// Service/UHD markers are useful hints but are not part of the identity
	// title. Remove only token-delimited forms so words such as "Max" inside a
	// normal title are not greedily stripped by a substring search.
	value = servicePattern.ReplaceAllString(value, " ")
	value = uhdPattern.ReplaceAllString(value, " ")
	// Stop at the first obvious technical marker. This deliberately leaves
	// ambiguous text for the provider search instead of silently discarding it.
	markers := regexp.MustCompile(`(?i)[. _-](2160p|1080p|720p|4320p|web-dl|webdl|webrip|remux|bluray|hdtv|uhd|ultra[. _-]+hd|x264|x265|h\.264|h\.265|hevc|avc)(?:[. _-]|$)`)
	if loc := markers.FindStringIndex(value); loc != nil {
		value = value[:loc[0]]
	}
	value = strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(value)
	return strings.Join(strings.Fields(value), " ")
}

func matchStart(value, match string) int {
	if index := strings.LastIndex(value, match); index >= 0 {
		return index
	}
	return len(value)
}

func hasReleaseGroupEvidence(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range []string{"web-dl", "webdl", "webrip", "remux", "bluray", "blu-ray", "bdmv", "hdtv", "x264", "x265", "h.264", "h.265", "2160p", "1080p", "720p"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return yearPattern.MatchString(value)
}
