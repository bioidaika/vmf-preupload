// Package naming contains the deterministic, tracker-oriented release-name
// renderer used by vmf-preupload.
//
// The package deliberately deals in normalized facts rather than trying to
// inspect files or call metadata providers.  The scanner/API layers can fill
// Metadata, and this package turns those facts into a basename.  It never
// guesses a streaming service from a resolution or codec: a service is only
// rendered when the caller supplies one.
package naming

import (
	"regexp"
	"strconv"
	"strings"
)

// The category and release-type constants are untyped on purpose.  This lets
// callers use them in JSON-backed structs whose fields are plain strings as
// well as in comparisons.
const (
	Movie = "MOVIE"
	TV    = "TV"

	WebDL  = "WEBDL"
	WebRip = "WEBRIP"
	Remux  = "REMUX"
	Encode = "ENCODE"
)

// Additional, descriptive aliases make call sites self-documenting without
// forcing Metadata fields to use a named string type.
const (
	CategoryMovie = Movie
	CategoryTV    = TV

	ReleaseWebDL  = WebDL
	ReleaseWebRip = WebRip
	ReleaseRemux  = Remux
	ReleaseEncode = Encode
)

// Profile controls the output convention.  A zero Profile is accepted by
// Render and is replaced with DefaultProfile.
type Profile struct {
	Name         string `json:"name,omitempty"`
	Separator    string `json:"separator,omitempty"`
	DefaultGroup string `json:"defaultGroup,omitempty"`
	// IncludeUHD is retained for JSON/API compatibility with vmf@1. It is no
	// longer consulted: only an explicit marker parsed from the original
	// filename may enable UHD, and the GUI exposes no global force toggle.
	IncludeUHD bool `json:"includeUHD,omitempty"`
}

// DefaultProfile returns the VMF-compatible baseline agreed for the app.
// Release names use dots between components and NoGroup when no release group
// was found in the source filename.
func DefaultProfile() Profile {
	return Profile{
		Name:         "vmf@2",
		Separator:    ".",
		DefaultGroup: "NoGroup",
	}
}

// Warning is a non-fatal issue found while rendering.  Render can still
// return a useful preview when warnings are present.
type Warning struct {
	Code    string `json:"code"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

// AudioTrack is the subset of a MediaInfo audio track relevant to naming and
// VMF Vietnamese-language tagging.  The extra aliases (Format and
// ChannelLayout) are useful when unmarshalling different MediaInfo adapters.
type AudioTrack struct {
	Language      string `json:"language,omitempty"`
	LanguageCode  string `json:"languageCode,omitempty"`
	Title         string `json:"title,omitempty"`
	Codec         string `json:"codec,omitempty"`
	Format        string `json:"format,omitempty"`
	Channels      string `json:"channels,omitempty"`
	ChannelLayout string `json:"channelLayout,omitempty"`
	Atmos         bool   `json:"atmos,omitempty"`
	Vietnamese    bool   `json:"vietnamese,omitempty"`
	Dub           bool   `json:"dub,omitempty"`
	Main          bool   `json:"main,omitempty"`
}

// Metadata is the normalized identity and technical information used to
// construct a release basename.  Fields are intentionally JSON-friendly so
// the Wails boundary can pass this structure without conversion.
type Metadata struct {
	Category    string `json:"category,omitempty"`
	MediaType   string `json:"mediaType,omitempty"`
	ReleaseType string `json:"releaseType,omitempty"`
	// Type is accepted as an alias for ReleaseType for scanners that mirror
	// MediaInfo/upbrr terminology.
	Type string `json:"type,omitempty"`

	Title         string `json:"title,omitempty"`
	OriginalTitle string `json:"originalTitle,omitempty"`
	// AltTitle is retained for compatibility/metadata display. Release names
	// emit one title only and never append an alternate title as a second token.
	AltTitle string `json:"altTitle,omitempty"`
	Year     int    `json:"year,omitempty"`

	Season       string `json:"season,omitempty"`
	Episode      string `json:"episode,omitempty"`
	EpisodeTitle string `json:"episodeTitle,omitempty"`
	Part         string `json:"part,omitempty"`

	Edition    string `json:"edition,omitempty"`
	Resolution string `json:"resolution,omitempty"`
	// UHD is explicit evidence supplied by the original-filename parser. It is
	// never inferred from resolution, release type, source, MediaInfo or path.
	UHD bool `json:"uhd,omitempty"`
	// UHDInferred is retained for compatibility with older callers and ignored.
	UHDInferred bool `json:"uhdInferred,omitempty"`
	// SourcePath is retained for compatibility/context but never supplies UHD.
	SourcePath string `json:"sourcePath,omitempty"`
	// Other mirrors release-parser facts but never supplies UHD.
	Other []string `json:"other,omitempty"`

	Source  string `json:"source,omitempty"`
	Service string `json:"service,omitempty"`

	HDR         string `json:"hdr,omitempty"`
	VideoCodec  string `json:"videoCodec,omitempty"`
	VideoEncode string `json:"videoEncode,omitempty"`
	// Video is a convenient alias used by a few MediaInfo adapters.
	Video string `json:"video,omitempty"`

	// Audio may contain an already-normalized composite (for example
	// "DDP5.1.Atmos").  When it is empty, the fields below and AudioTracks are
	// used to construct one.
	Audio          string       `json:"audio,omitempty"`
	AudioCodec     string       `json:"audioCodec,omitempty"`
	AudioChannels  string       `json:"audioChannels,omitempty"`
	AudioAtmos     bool         `json:"audioAtmos,omitempty"`
	AudioTracks    []AudioTrack `json:"audioTracks,omitempty"`
	AudioLanguages []string     `json:"audioLanguages,omitempty"`
	AudioTitles    []string     `json:"audioTitles,omitempty"`
	Vietnamese     bool         `json:"vietnamese,omitempty"`
	VietnameseDub  bool         `json:"vietnameseDub,omitempty"`

	Group string `json:"group,omitempty"`
	// The *Tag fields are aliases for parsers that retain the original token
	// name.  The first non-empty value wins.
	SourceTag  string `json:"sourceTag,omitempty"`
	ServiceTag string `json:"serviceTag,omitempty"`
	GroupTag   string `json:"groupTag,omitempty"`

	// ExistingName is optional context from the source filename. It is only
	// consulted for preserving an existing ViE/ViE.DUB tag when audio metadata
	// is incomplete; no other old-name token is copied blindly.
	ExistingName string `json:"existingName,omitempty"`
}

var (
	invalidFilenameRune  = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1F]`)
	multiDots            = regexp.MustCompile(`\.{2,}`)
	spaceLike            = regexp.MustCompile(`[\s_]+`)
	vmfExistingTag       = regexp.MustCompile(`(?i)(?:^|[. _-])vie(?:[. _-]dub)?(?:$|[. _-])`)
	vmfExistingDub       = regexp.MustCompile(`(?i)(?:^|[. _-])vie[. _-]dub(?:$|[. _-])`)
	wordTM               = regexp.MustCompile(`(?i)(^|[^[:alnum:]])tm([^[:alnum:]]|$)`)
	compactAudioPrefix   = regexp.MustCompile(`(?i)^(DDP|DD\+|DD|AAC)\.(\d+(?:\.\d+)?)(.*)$`)
	audioChannelNotation = regexp.MustCompile(`(?:^|[^0-9])(\d{1,2})\.(\d{1,2})(?:\.(\d{1,2}))?(?:$|[^0-9])`)
	audioChannelCount    = regexp.MustCompile(`^\s*(\d{1,2})(?:\s|channels?|ch)?\s*$`)
)

// Render builds a deterministic basename (without an extension) and returns
// non-fatal diagnostics.  The function is pure: it does not touch the
// filesystem, network, or global state.
func Render(m Metadata, profile Profile) (string, []Warning) {
	p := normalizeProfile(profile)
	warnings := make([]Warning, 0, 3)

	category, categoryKnown := normalizeCategory(firstNonEmpty(m.Category, m.MediaType))
	if !categoryKnown {
		if strings.TrimSpace(m.Season) != "" || strings.TrimSpace(m.Episode) != "" {
			category = TV
			warnings = append(warnings, Warning{Code: "inferred_category", Field: "category", Message: "category inferred as TV from season/episode fields"})
		} else {
			category = Movie
			if strings.TrimSpace(firstNonEmpty(m.Category, m.MediaType)) != "" {
				warnings = append(warnings, Warning{Code: "unknown_category", Field: "category", Message: "unknown category; rendered as MOVIE"})
			}
		}
	}

	typeValue, typeKnown := normalizeReleaseType(firstNonEmpty(m.ReleaseType, m.Type))
	if !typeKnown {
		if inferred := inferReleaseType(m.Source, m.Service, m.ExistingName); inferred != "" {
			typeValue = inferred
			warnings = append(warnings, Warning{Code: "inferred_release_type", Field: "releaseType", Message: "release type inferred from source filename metadata"})
		} else if strings.TrimSpace(firstNonEmpty(m.ReleaseType, m.Type)) != "" {
			warnings = append(warnings, Warning{Code: "unknown_release_type", Field: "releaseType", Message: "unknown release type; supplied technical order was used"})
		}
	}

	title := normalizePart(firstNonEmpty(m.Title, m.OriginalTitle))
	if title == "" {
		warnings = append(warnings, Warning{Code: "missing_title", Field: "title", Message: "a title is required to render a release name"})
		return "", warnings
	}

	parts := make([]string, 0, 20)
	seen := make(map[string]struct{})
	add := func(value string) {
		value = normalizePart(value)
		if value == "" {
			return
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		parts = append(parts, value)
	}

	// Emit exactly one identity title. Title is normally the English provider
	// translation; OriginalTitle is only a fallback when that translation is
	// unavailable. TV then appends its season/episode marker after the year.
	add(title)
	if category == TV {
		add(yearString(m.Year))
		seasonEpisode := normalizeSeasonEpisode(m.Season, m.Episode)
		if seasonEpisode == "" {
			if strings.TrimSpace(m.Season) == "" && strings.TrimSpace(m.Episode) == "" {
				warnings = append(warnings, Warning{Code: "missing_episode_id", Field: "season", Message: "TV name has no season/episode identifier"})
			}
		} else {
			add(seasonEpisode)
		}
		add(m.EpisodeTitle)
		add(m.Part)
	} else {
		add(yearString(m.Year))
	}

	add(m.Edition)
	if tag := VMFTag(m); tag != "" {
		// VMF places the Vietnamese marker immediately before the resolution
		// (or the first technical/source token when resolution is unavailable).
		add(tag)
	}
	resolution := normalizeResolution(m.Resolution)
	service := firstNonEmpty(m.Service, m.ServiceTag)
	source := firstNonEmpty(m.Source, m.SourceTag)
	add(resolution)
	if shouldIncludeUHD(m) {
		add("UHD")
	}

	audio := renderAudio(m)
	hdr := normalizePart(m.HDR)
	videoCodec := firstNonEmpty(m.VideoCodec, m.Video)
	videoEncode := firstNonEmpty(m.VideoEncode, m.Video)

	switch typeValue {
	case WebDL:
		// A WEB source is represented by the release type itself.  Service is
		// optional and is emitted only when supplied by the old filename/user.
		add(service)
		add("WEB-DL")
		add(audio)
		add(hdr)
		add(videoTokenForRelease(WebDL, videoCodec, videoEncode))
	case WebRip:
		add(service)
		add("WEBRip")
		add(audio)
		add(hdr)
		add(videoTokenForRelease(WebRip, videoCodec, videoEncode))
	case Remux:
		add(source)
		add("REMUX")
		add(hdr)
		add(videoTokenForRelease(Remux, videoCodec, videoEncode))
		add(audio)
	case Encode:
		add(source)
		// ENCODE is represented by the encoder token (x264/x265/AV1),
		// rather than an extra literal ENCODE component.
		add(audio)
		add(hdr)
		add(videoTokenForRelease(Encode, videoCodec, videoEncode))
	default:
		// Keep unknown/omitted types useful without inventing a source or
		// service.  If a caller supplied an unrecognized type, preserve it as
		// a normalized token and let the warning explain the ambiguity.
		add(typeValue)
		add(source)
		add(service)
		add(audio)
		add(hdr)
		add(firstNonEmpty(videoEncode, videoCodec))
	}

	group := normalizeGroup(firstNonEmpty(m.Group, m.GroupTag), p.DefaultGroup)
	if group == "" {
		group = "NoGroup"
	}

	name := strings.Join(parts, p.Separator)
	if name == "" {
		return "", warnings
	}
	return name + "-" + group, warnings
}

// BuildName is a descriptive alias for Render retained for callers that use
// naming terminology rather than rendering terminology.
func BuildName(m Metadata, profile Profile) (string, []Warning) {
	return Render(m, profile)
}

// RenderDefault renders with DefaultProfile.
func RenderDefault(m Metadata) (string, []Warning) {
	return Render(m, DefaultProfile())
}

// VMFTag determines the strongest Vietnamese audio marker.  ViE.DUB wins
// over ViE, and an empty string means no marker should be inserted.
func VMFTag(m Metadata) string {
	hasVie := m.Vietnamese
	hasDub := m.VietnameseDub

	for _, language := range m.AudioLanguages {
		if isVietnameseLanguage(language) {
			hasVie = true
		}
	}
	for _, title := range m.AudioTitles {
		vie, dub := classifyVietnameseTitle(title)
		hasVie = hasVie || vie
		hasDub = hasDub || dub
	}
	for _, track := range m.AudioTracks {
		if track.Vietnamese || isVietnameseLanguage(track.Language) || isVietnameseLanguage(track.LanguageCode) {
			hasVie = true
		}
		vie, dub := classifyVietnameseTitle(track.Title)
		hasVie = hasVie || vie
		hasDub = hasDub || dub || track.Dub
	}

	// Existing tags are a fallback only.  Fresh MediaInfo evidence always
	// wins, while an existing DUB marker is never downgraded to plain ViE.
	if existing := existingVMFTag(m.ExistingName); existing != "" {
		if existing == "ViE.DUB" {
			hasDub = true
			hasVie = true
		} else if !hasVie {
			hasVie = true
		}
	}

	if hasDub {
		return "ViE.DUB"
	}
	if hasVie {
		return "ViE"
	}
	return ""
}

func normalizeProfile(profile Profile) Profile {
	defaults := DefaultProfile()
	if strings.TrimSpace(profile.Name) == "" {
		profile.Name = defaults.Name
	}
	profile.Separator = strings.TrimSpace(profile.Separator)
	if profile.Separator == "" || strings.ContainsAny(profile.Separator, `/\\<>:"|?*`) {
		profile.Separator = defaults.Separator
	}
	if profile.DefaultGroup == "" {
		profile.DefaultGroup = defaults.DefaultGroup
	}
	return profile
}

func normalizeCategory(value string) (string, bool) {
	compact := strings.ToUpper(strings.TrimSpace(value))
	compact = strings.NewReplacer("-", "", "_", "", " ", "").Replace(compact)
	switch compact {
	case "MOVIE", "FILM", "MOVIES":
		return Movie, true
	case "TV", "SERIES", "SHOW", "EPISODE", "TELEVISION":
		return TV, true
	default:
		return "", false
	}
}

func normalizeReleaseType(value string) (string, bool) {
	compact := strings.ToUpper(strings.TrimSpace(value))
	compact = strings.NewReplacer(" ", "", "-", "", "_", "").Replace(compact)
	switch compact {
	case "WEBDL", "WEBDELIVERY":
		return WebDL, true
	case "WEBRIP", "WEBRIPPED":
		return WebRip, true
	case "REMUX", "REMASTEREDMUX":
		return Remux, true
	case "ENCODE", "ENCODED", "TRANSCODE":
		return Encode, true
	case "":
		return "", false
	default:
		return normalizePart(value), false
	}
}

func inferReleaseType(source, service, existing string) string {
	for _, value := range []string{source, existing} {
		compact := strings.ToUpper(strings.NewReplacer(" ", "", "-", "", "_", "", ".", "").Replace(value))
		switch {
		case strings.Contains(compact, "REMUX"):
			return Remux
		case strings.Contains(compact, "WEBRIP"):
			return WebRip
		case strings.Contains(compact, "WEBDL"):
			return WebDL
		}
	}
	_ = service // service alone is not enough evidence to infer a type.
	return ""
}

// videoTokenForRelease applies the codec spelling convention associated with
// the release type. AVC/HEVC describe the untouched bitstream in a REMUX,
// x264/x265 identify an encode (including WEBRip), while WEB-DL uses the
// H.264/H.265 delivery spelling. The input facts may use any equivalent alias.
func videoTokenForRelease(releaseType, codec, encode string) string {
	if releaseType == Encode || releaseType == WebRip {
		encoder := strings.ToUpper(strings.NewReplacer(" ", "", ".", "", "-", "", "_", "").Replace(strings.TrimSpace(encode)))
		switch encoder {
		case "X264":
			return "x264"
		case "X265":
			return "x265"
		case "AV1":
			return "AV1"
		}
	}
	value := firstNonEmpty(codec, encode)
	compact := strings.ToUpper(strings.NewReplacer(" ", "", ".", "", "-", "", "_", "").Replace(strings.TrimSpace(value)))
	switch compact {
	case "AVC", "H264", "X264":
		if releaseType == Remux {
			return "AVC"
		}
		return "H.264"
	case "HEVC", "H265", "X265":
		if releaseType == Remux {
			return "HEVC"
		}
		return "H.265"
	case "AV1":
		return "AV1"
	case "VP9":
		return "VP9"
	case "VC1":
		return "VC-1"
	case "MPEG2", "MPEGVIDEO":
		return "MPEG-2"
	default:
		return normalizePart(value)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func yearString(year int) string {
	if year <= 0 {
		return ""
	}
	return strconv.Itoa(year)
}

func normalizeSeasonEpisode(season, episode string) string {
	season = strings.TrimSpace(season)
	episode = strings.TrimSpace(episode)
	if season == "" && episode == "" {
		return ""
	}

	// A parser may already provide a combined marker in either field.
	combined := season
	if strings.Contains(strings.ToUpper(combined), "S") && strings.Contains(strings.ToUpper(combined), "E") {
		if episode != "" && !strings.Contains(strings.ToUpper(combined), strings.ToUpper(episode)) {
			combined += episode
		}
		return normalizePart(combined)
	}

	season = normalizeSeason(season)
	episode = normalizeEpisode(episode)
	return normalizePart(season + episode)
}

func normalizeSeason(value string) string {
	if value == "" {
		return ""
	}
	upper := strings.ToUpper(strings.TrimSpace(value))
	if strings.HasPrefix(upper, "S") {
		digits := strings.TrimLeft(upper[1:], " ._-S")
		if digits != "" {
			if n, err := strconv.Atoi(digits); err == nil {
				return "S" + padNumber(n, 2)
			}
			return "S" + digits
		}
	}
	if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
		return "S" + padNumber(n, 2)
	}
	return upper
}

func normalizeEpisode(value string) string {
	if value == "" {
		return ""
	}
	upper := strings.ToUpper(strings.TrimSpace(value))
	if strings.HasPrefix(upper, "E") {
		digits := strings.TrimLeft(upper[1:], " ._-E")
		if digits != "" {
			if n, err := strconv.Atoi(digits); err == nil {
				return "E" + padNumber(n, 2)
			}
			return "E" + digits
		}
	}
	if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
		return "E" + padNumber(n, 2)
	}
	return upper
}

func padNumber(value, width int) string {
	s := strconv.Itoa(value)
	for len(s) < width {
		s = "0" + s
	}
	return s
}

func normalizeResolution(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	compact := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(value, " ", ""), "_", ""))
	compact = strings.ReplaceAll(compact, "×", "x")
	// Common MediaInfo dimensions.  Keep an explicitly supplied p-suffix as
	// written after normalizing its case.
	if strings.Contains(compact, "x") {
		parts := strings.SplitN(compact, "x", 2)
		if len(parts) == 2 {
			heightText := strings.TrimSuffix(strings.TrimSpace(parts[1]), "p")
			if height, err := strconv.Atoi(heightText); err == nil && height > 0 {
				return strconv.Itoa(height) + "p"
			}
		}
	}
	if strings.HasSuffix(compact, "p") {
		if n, err := strconv.Atoi(strings.TrimSuffix(compact, "p")); err == nil && n > 0 {
			return strconv.Itoa(n) + "p"
		}
	}
	if n, err := strconv.Atoi(compact); err == nil && n > 0 {
		return strconv.Itoa(n) + "p"
	}
	return normalizePart(value)
}

// shouldIncludeUHD accepts only the explicit flag populated by the
// original-filename parser. Resolution and technical metadata are not proof
// that the original release name carried a UHD tag.
func shouldIncludeUHD(m Metadata) bool {
	return m.UHD
}

func renderAudio(m Metadata) string {
	if explicit := normalizePart(m.Audio); explicit != "" {
		return normalizeExplicitAudio(explicit)
	}

	track := chooseAudioTrack(m.AudioTracks)
	codec := firstNonEmpty(m.AudioCodec, track.Codec, track.Format)
	channels := normalizeAudioChannels(firstNonEmpty(m.AudioChannels, track.Channels, track.ChannelLayout))
	atmos := m.AudioAtmos || track.Atmos || containsAtmos(track.Title, codec)
	if codec == "" && channels == "" && !atmos {
		return ""
	}

	codec = normalizeAudioCodec(codec)
	parts := make([]string, 0, 3)
	if codec != "" {
		// Scene naming convention combines the short Dolby/AAC codec with
		// its channel count (DDP5.1, DD2.0, AAC2.0).  Longer lossless codec
		// names remain separate components (TrueHD.7.1, DTS-HD.MA.5.1).
		if channels != "" && isCompactAudioCodec(codec) && !strings.Contains(strings.ToLower(codec), strings.ToLower(channels)) {
			parts = append(parts, codec+channels)
		} else {
			parts = append(parts, codec)
			if channels != "" && !strings.Contains(strings.ToLower(codec), strings.ToLower(channels)) {
				parts = append(parts, channels)
			}
		}
	} else if channels != "" {
		parts = append(parts, channels)
	}
	if atmos && !containsFold(parts, "Atmos") {
		parts = append(parts, "Atmos")
	}
	return strings.Join(parts, ".")
}

func normalizeAudioChannels(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if match := audioChannelNotation.FindStringSubmatch(value); match != nil {
		if match[3] != "" {
			return match[1] + "." + match[2] + "." + match[3]
		}
		return match[1] + "." + match[2]
	}
	if match := audioChannelCount.FindStringSubmatch(value); match != nil {
		count, _ := strconv.Atoi(match[1])
		switch count {
		case 1:
			return "1.0"
		case 2:
			return "2.0"
		case 6:
			return "5.1"
		case 8:
			return "7.1"
		default:
			if count > 2 {
				return strconv.Itoa(count) + ".0"
			}
		}
	}

	bed, lfe, height := 0, 0, 0
	cleaned := strings.NewReplacer(",", " ", ":", " ", ";", " ", "/", " ").Replace(value)
	for _, field := range strings.Fields(cleaned) {
		switch strings.ToUpper(strings.Trim(field, "()[]{}")) {
		case "LFE", "LFE1", "LFE2":
			lfe++
		case "TFL", "TFC", "TFR", "TBL", "TBC", "TBR", "TSL", "TSR", "VHL", "VHC", "VHR", "LH", "CH", "RH":
			height++
		case "L", "R", "C", "LS", "RS", "LB", "RB", "LC", "RC", "CS", "BC", "LW", "RW":
			bed++
		}
	}
	if bed == 0 && lfe == 0 && height == 0 {
		// Unknown free-form layout text is never safe to emit as filename tags.
		return ""
	}
	if height > 0 {
		return strconv.Itoa(bed) + "." + strconv.Itoa(lfe) + "." + strconv.Itoa(height)
	}
	return strconv.Itoa(bed) + "." + strconv.Itoa(lfe)
}

func normalizeExplicitAudio(value string) string {
	match := compactAudioPrefix.FindStringSubmatch(value)
	if match == nil {
		return value
	}
	codec := strings.ToUpper(match[1])
	if codec == "DD+" {
		codec = "DDP"
	}
	return codec + match[2] + match[3]
}

func isCompactAudioCodec(codec string) bool {
	switch strings.ToUpper(codec) {
	case "DD", "DDP", "AAC":
		return true
	default:
		return false
	}
}

func chooseAudioTrack(tracks []AudioTrack) AudioTrack {
	for _, track := range tracks {
		if track.Main {
			return track
		}
	}
	if len(tracks) > 0 {
		return tracks[0]
	}
	return AudioTrack{}
}

func normalizeAudioCodec(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	compact := strings.ToLower(strings.NewReplacer(" ", "", "-", "", "_", "").Replace(value))
	switch compact {
	case "eac3", "dolbydigitalplus", "ddplus", "ddp":
		return "DDP"
	case "ac3", "dolbydigital", "dd":
		return "DD"
	case "dtshdmasteraudio", "dtshdma":
		return "DTS-HD.MA"
	case "dts-hd", "dtshd":
		return "DTS-HD"
	case "truehd", "dolbytruehd":
		return "TrueHD"
	case "flac":
		return "FLAC"
	case "aac":
		return "AAC"
	case "opus":
		return "Opus"
	case "pcm", "lpcm":
		return "PCM"
	}
	return normalizePart(value)
}

func containsAtmos(values ...string) bool {
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), "atmos") {
			return true
		}
	}
	return false
}

func containsFold(values []string, wanted string) bool {
	for _, value := range values {
		if strings.EqualFold(value, wanted) {
			return true
		}
	}
	return false
}

func normalizeGroup(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = strings.TrimSpace(fallback)
	}
	if value == "" {
		value = "NoGroup"
	}
	value = strings.Trim(value, " .-_\t\r\n")
	if value == "" {
		return "NoGroup"
	}
	compact := strings.ToLower(strings.NewReplacer(" ", "", ".", "", "_", "", "-", "").Replace(value))
	if compact == "nogrp" || compact == "nogroup" || compact == "unknown" {
		return "NoGroup"
	}
	return normalizePart(value)
}

func normalizePart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = invalidFilenameRune.ReplaceAllString(value, " ")
	value = strings.ReplaceAll(value, "\u00a0", " ")
	value = spaceLike.ReplaceAllString(value, ".")
	// Brackets are presentation punctuation in source names; preserve their
	// contents while avoiding filename characters that confuse tracker parsers.
	value = strings.NewReplacer("[", ".", "]", ".", "(", ".", ")", ".", "{", ".", "}", ".", ",", ".", ":", ".", ";", ".", "'", "", "\"", "").Replace(value)
	value = multiDots.ReplaceAllString(value, ".")
	value = strings.Trim(value, ". -_\t\r\n")
	return value
}

func isVietnameseLanguage(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return false
	}
	// Accept ISO-639-1/2 forms and common MediaInfo display values.
	if value == "vi" || value == "vie" || value == "vietnamese" || strings.HasPrefix(value, "vi-") || strings.HasPrefix(value, "vie-") {
		return true
	}
	return strings.Contains(value, "vietnamese")
}

func classifyVietnameseTitle(value string) (vie, dub bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return false, false
	}
	if strings.Contains(value, "lồng tiếng") || strings.Contains(value, "long tieng") || strings.Contains(value, "uslt") || strings.Contains(value, "vnlt") || strings.Contains(value, "dubbed") || strings.Contains(value, "dubbing") {
		return true, true
	}
	if strings.Contains(value, "thuyết minh") || strings.Contains(value, "thuyet minh") || wordTM.MatchString(value) {
		return true, false
	}
	return false, false
}

func existingVMFTag(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	if vmfExistingDub.MatchString(value) {
		return "ViE.DUB"
	}
	if vmfExistingTag.MatchString(value) {
		return "ViE"
	}
	return ""
}
