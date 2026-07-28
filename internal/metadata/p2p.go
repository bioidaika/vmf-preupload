package metadata

import (
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

var (
	p2pGroupPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_]{0,47}$`)
	p2pResolution   = regexp.MustCompile(`(?i)(?:^|[. _-])(?:4320|2160|1440|1080|720|576|480)[pi](?:$|[. _-])`)
	p2pIdentityOnly = regexp.MustCompile(`(?i)^(?:(?:19|20)\d{2}|S\d{1,2}(?:E\d{1,3})*)$`)
	p2pIdentity     = regexp.MustCompile(`(?i)(?:^|[. _-])(?:(?:19|20)\d{2}|S\d{1,2}(?:E\d{1,3})*)(?:$|[. _-])`)

	p2pRemux     = regexp.MustCompile(`(?i)(?:^|[. _-])REMUX(?:$|[. _-])`)
	p2pWeb       = regexp.MustCompile(`(?i)(?:^|[. _-])WEB(?:[. _-]?(?:DL|RIP))?(?:$|[. _-])`)
	p2pEncodeSrc = regexp.MustCompile(`(?i)(?:^|[. _-])(?:BLU[. _-]?RAY|BDRIP|BRRIP|HDTV|DVDRIP)(?:$|[. _-])`)
	p2pBitstream = regexp.MustCompile(`(?i)(?:^|[. _-])(?:AVC|HEVC|VC[. _-]?1|MPEG[. _-]?2|H[. _-]?26[45])(?:$|[. _-])`)
	p2pWebVideo  = regexp.MustCompile(`(?i)(?:^|[. _-])(?:AVC|HEVC|AV1|VP9|H[. _-]?26[45]|X26[45])(?:$|[. _-])`)
	p2pEncoder   = regexp.MustCompile(`(?i)(?:^|[. _-])(?:X26[45]|AV1|XVID)(?:$|[. _-])`)
)

var p2pMediaExtensions = map[string]struct{}{
	".mkv": {}, ".mp4": {}, ".m4v": {}, ".ts": {}, ".m2ts": {},
	".mov": {}, ".avi": {}, ".webm": {},
}

// IsP2PReleaseName reports whether filename has enough structural evidence to
// be preserved as an existing P2P release name. It intentionally does not try
// to enforce one token order: REMUX, WEB and encode groups use different
// conventions. The detector is conservative because a false positive would
// prevent a filename the user expected the app to normalize from being
// changed.
//
// A match requires a supported media extension, a final non-placeholder
// release group, a title before the resolution, and a release-type-specific
// technical signature. The function only classifies the basename; it never
// validates that its claims agree with the media bytes.
func IsP2PReleaseName(filename string) bool {
	base := filepath.Base(strings.TrimSpace(filename))
	extension := strings.ToLower(filepath.Ext(base))
	if _, supported := p2pMediaExtensions[extension]; !supported {
		return false
	}

	stem := strings.TrimSuffix(base, filepath.Ext(base))
	return isP2PReleaseStem(stem)
}

// IsP2PReleaseFolderName applies the same evidence policy to a directory
// basename. A separate entry point is necessary because a dot-separated
// release folder has no extension, while filepath.Ext would treat its final
// technical tokens as one.
func IsP2PReleaseFolderName(folder string) bool {
	return isP2PReleaseStem(filepath.Base(strings.TrimSpace(folder)))
}

func isP2PReleaseStem(stem string) bool {
	groupSeparator := strings.LastIndex(stem, "-")
	if groupSeparator <= 0 || groupSeparator == len(stem)-1 {
		return false
	}
	body := stem[:groupSeparator]
	group := stem[groupSeparator+1:]
	if !validP2PGroup(group) {
		return false
	}

	resolution := p2pResolution.FindStringIndex(body)
	if resolution == nil {
		return false
	}
	identity := body[:resolution[0]]
	if !hasP2PTitle(identity) || !p2pIdentity.MatchString(identity) {
		return false
	}
	technical := body[resolution[0]:]

	switch {
	case p2pRemux.MatchString(technical):
		return p2pBitstream.MatchString(technical)
	case p2pWeb.MatchString(technical):
		return p2pWebVideo.MatchString(technical)
	default:
		return p2pEncodeSrc.MatchString(technical) && p2pEncoder.MatchString(technical)
	}
}

func validP2PGroup(group string) bool {
	if !p2pGroupPattern.MatchString(group) {
		return false
	}
	compact := strings.ToLower(strings.ReplaceAll(group, "_", ""))
	switch compact {
	case "nogroup", "nogrp", "unknown", "unk", "group", "remux", "web", "webdl", "webrip", "dl",
		"x264", "x265", "avc", "hevc", "h264", "h265", "hdr", "hd", "uhd", "dv":
		return false
	default:
		return true
	}
}

func hasP2PTitle(prefix string) bool {
	fields := strings.FieldsFunc(strings.Trim(prefix, ". _-"), func(r rune) bool {
		return r == '.' || r == ' ' || r == '_' || r == '-'
	})
	identityIndex := -1
	for index := len(fields) - 1; index >= 0; index-- {
		if p2pIdentityOnly.MatchString(fields[index]) {
			identityIndex = index
			break
		}
	}
	for index, field := range fields {
		if index == identityIndex {
			continue
		}
		for _, r := range field {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				return true
			}
		}
	}
	return false
}
