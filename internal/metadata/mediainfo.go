package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/bioidaika/vmf-preupload/pkg/api"
)

type mediaDocument struct {
	Media struct {
		Track []map[string]any `json:"track"`
	} `json:"media"`
}

func Extract(ctx context.Context, path, binary string) (api.TechnicalInfo, []string) {
	info := api.TechnicalInfo{Container: strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")}
	warnings := []string{}
	bin := strings.TrimSpace(binary)
	if bin == "" {
		for _, candidate := range bundledBinaryCandidates() {
			if _, err := os.Stat(candidate); err == nil {
				bin = candidate
				break
			}
		}
	}
	if bin == "" {
		for _, candidate := range []string{"MediaInfo.exe", "mediainfo", "MediaInfo"} {
			if found, err := exec.LookPath(candidate); err == nil {
				bin = found
				break
			}
		}
	}
	if bin == "" {
		warnings = append(warnings, "MediaInfo executable not found; technical fields are filename hints only")
		return info, warnings
	}
	cmd := exec.CommandContext(ctx, bin, "--Output=JSON", path)
	out, err := cmd.Output()
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("MediaInfo failed: %v", err))
		return info, warnings
	}
	info.RawJSON = string(out)
	// Keep a human-readable copy for the GUI/upload workflow. Failure to obtain
	// the text view is non-fatal because the normalized JSON is still usable.
	textCmd := exec.CommandContext(ctx, bin, "--Output=Text", path)
	if textOut, textErr := textCmd.Output(); textErr == nil {
		info.RawText = string(textOut)
	}
	var doc mediaDocument
	if err := json.Unmarshal(out, &doc); err != nil {
		warnings = append(warnings, fmt.Sprintf("MediaInfo returned invalid JSON: %v", err))
		return info, warnings
	}
	var video map[string]any
	var audio []map[string]any
	for _, track := range doc.Media.Track {
		switch strings.ToLower(stringValue(track, "@type", "type")) {
		case "video":
			if video == nil {
				video = track
			}
		case "audio":
			audio = append(audio, track)
		}
	}
	if video != nil {
		info.Width = intValue(video, "Width", "width")
		info.Height = intValue(video, "Height", "height")
		info.Resolution = resolution(info.Width, info.Height, stringValue(video, "ScanType"))
		info.VideoCodec = videoCodec(video)
		info.VideoEncode = detectEncode(video)
		info.HDR = detectHDR(video)
	}
	for index, track := range audio {
		item := api.Track{
			Type:     "Audio",
			Index:    index,
			Language: stringValue(track, "Language", "Language_String3"),
			Title:    stringValue(track, "Title", "Title_Original"),
			Codec:    normalizeAudioCodec(track),
			Channels: channels(track),
			Default:  boolValue(track, "Default"),
			Forced:   boolValue(track, "Forced"),
		}
		item.Atmos = detectAudioAtmos(track)
		item.Commentary = strings.Contains(strings.ToLower(item.Title), "commentary")
		info.Tracks = append(info.Tracks, item)
	}
	return info, warnings
}

func bundledBinaryCandidates() []string {
	candidates := []string{}
	if executable, err := os.Executable(); err == nil {
		base := filepath.Dir(executable)
		candidates = append(candidates, filepath.Join(base, "assets", "mediainfo", "MediaInfo.exe"))
	}
	if working, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(working, "assets", "mediainfo", "MediaInfo.exe"))
	}
	return candidates
}

func stringValue(track map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := track[key]; ok && value != nil {
			return strings.TrimSpace(fmt.Sprint(value))
		}
	}
	return ""
}

func intValue(track map[string]any, keys ...string) int {
	value := stringValue(track, keys...)
	match := regexp.MustCompile(`\d+`).FindString(value)
	if match == "" {
		return 0
	}
	n, _ := strconv.Atoi(match)
	return n
}

func boolValue(track map[string]any, keys ...string) bool {
	value := strings.ToLower(stringValue(track, keys...))
	return value == "yes" || value == "true" || value == "1"
}

func resolution(width, height int, scan string) string {
	suffix := "p"
	if strings.Contains(strings.ToLower(scan), "interlace") {
		suffix = "i"
	}
	switch {
	case height >= 2000 || width >= 3800:
		return "2160" + suffix
	case height >= 900 || width >= 1900:
		return "1080" + suffix
	case height >= 600 || width >= 1200:
		return "720" + suffix
	case height >= 500:
		return "576" + suffix
	case height > 0:
		return "480" + suffix
	default:
		return ""
	}
}

func normalizeVideoCodec(value string) string {
	upper := strings.ToUpper(value)
	switch {
	case strings.Contains(upper, "HEVC") || strings.Contains(upper, "H.265"):
		return "H.265"
	case strings.Contains(upper, "AVC") || strings.Contains(upper, "H.264"):
		return "H.264"
	case strings.Contains(upper, "AV1"):
		return "AV1"
	case strings.Contains(upper, "VP9"):
		return "VP9"
	default:
		return strings.TrimSpace(value)
	}
}

func videoCodec(track map[string]any) string {
	format := stringValue(track, "Format", "CodecID")
	evidence := strings.ToUpper(strings.Join([]string{
		format,
		stringValue(track, "Format_Version"),
		stringValue(track, "CodecID"),
	}, " "))
	if strings.Contains(evidence, "MPEG VIDEO") && (strings.Contains(evidence, "VERSION 2") || strings.Contains(evidence, "MPEG-2") || strings.Contains(evidence, "MPEG2")) {
		return "MPEG-2"
	}
	return normalizeVideoCodec(format)
}

func detectEncode(track map[string]any) string {
	library := strings.ToLower(stringValue(track, "Encoded_Library_Name", "Encoded_Library", "Encoded_Library_Settings"))
	if strings.Contains(library, "x265") {
		return "x265"
	}
	if strings.Contains(library, "x264") {
		return "x264"
	}
	return ""
}

func detectHDR(track map[string]any) string {
	value := strings.ToLower(strings.Join([]string{stringValue(track, "HDR_Format"), stringValue(track, "HDR_Format_String"), stringValue(track, "Transfer_Characteristics"), stringValue(track, "ColourPrimaries")}, " "))
	parts := []string{}
	if strings.Contains(value, "dolby vision") {
		parts = append(parts, "DV")
	}
	if strings.Contains(value, "hdr10+") || strings.Contains(value, "st 2094") {
		parts = append(parts, "HDR10+")
	} else if strings.Contains(value, "hdr10") || strings.Contains(value, "pq") || strings.Contains(value, "bt.2020") {
		parts = append(parts, "HDR")
	}
	return strings.Join(unique(parts), ".")
}

func normalizeAudioCodec(track map[string]any) string {
	format := stringValue(track, "Format", "Format_String")
	commercial := strings.TrimSpace(strings.Join([]string{stringValue(track, "Format_Commercial"), stringValue(track, "Format_Commercial_IfAny")}, " "))
	profile := strings.TrimSpace(strings.Join([]string{stringValue(track, "Format_Profile"), stringValue(track, "Format_Profile_String")}, " "))
	codecID := strings.TrimSpace(strings.Join([]string{stringValue(track, "CodecID"), stringValue(track, "CodecID_Compatible")}, " "))
	if codec := normalizeMPEGAudioCodec(format, commercial, profile, codecID); codec != "" {
		return codec
	}
	value := strings.ToLower(strings.Join([]string{format, commercial, profile}, " "))
	switch {
	case strings.Contains(value, "truehd") || strings.Contains(value, "mlp fba"):
		return "TrueHD"
	case strings.Contains(value, "dts:x"):
		return "DTS:X"
	case strings.Contains(value, "dts-hd") && (strings.Contains(value, "master audio") || regexp.MustCompile(`(?:^|[^a-z])ma(?:$|[^a-z])`).MatchString(value)):
		return "DTS-HD.MA"
	case strings.Contains(value, "dts-hd") && (strings.Contains(value, "high resolution") || regexp.MustCompile(`(?:^|[^a-z])hra(?:$|[^a-z])`).MatchString(value)):
		return "DTS-HD.HRA"
	case strings.Contains(value, "dts-hd"):
		return "DTS-HD"
	case strings.Contains(value, "dts"):
		return "DTS"
	case strings.Contains(value, "e-ac-3") || strings.Contains(value, "dolby digital plus"):
		return "DDP"
	case strings.Contains(value, "ac-3") || strings.Contains(value, "dolby digital"):
		return "DD"
	case strings.Contains(value, "aac"):
		return "AAC"
	case strings.Contains(value, "flac"):
		return "FLAC"
	case strings.Contains(value, "opus"):
		return "Opus"
	default:
		return strings.TrimSpace(format)
	}
}

func detectAudioAtmos(track map[string]any) bool {
	evidence := strings.ToLower(strings.Join([]string{
		stringValue(track, "Format_AdditionalFeatures"),
		stringValue(track, "Title"),
		stringValue(track, "Title_Original"),
		stringValue(track, "Format_Commercial"),
		stringValue(track, "Format_Commercial_IfAny"),
		stringValue(track, "Format_Profile"),
		stringValue(track, "Format_Profile_String"),
	}, " "))
	return strings.Contains(evidence, "atmos") ||
		regexp.MustCompile(`(?:^|[^a-z0-9])joc(?:$|[^a-z0-9])`).MatchString(evidence) ||
		regexp.MustCompile(`(?:^|[^a-z0-9])16[ -]?ch(?:$|[^a-z0-9])`).MatchString(evidence)
}

func normalizeMPEGAudioCodec(format, commercial, profile, codecID string) string {
	family := strings.ToLower(strings.Join([]string{format, commercial, codecID}, " "))
	if !strings.Contains(family, "mpeg") &&
		!strings.EqualFold(strings.TrimSpace(format), "MP2") &&
		!strings.EqualFold(strings.TrimSpace(format), "MP3") &&
		!strings.EqualFold(strings.TrimSpace(codecID), "MP2") &&
		!strings.EqualFold(strings.TrimSpace(codecID), "MP3") {
		return ""
	}
	// Prefer the explicit MediaInfo profile, then fall back to the container
	// codec ID. If neither identifies a layer, do not guess from MPEG Audio.
	for _, value := range []string{profile, codecID, format, commercial} {
		compact := strings.ToLower(strings.NewReplacer(" ", "", "_", "", "-", "", ".", "").Replace(value))
		switch {
		case compact == "mp3", strings.Contains(compact, "layer3"), strings.Contains(compact, "layeriii"), strings.Contains(compact, "mpeg/l3"):
			return "MP3"
		case compact == "mp2", strings.Contains(compact, "layer2"), strings.Contains(compact, "layerii"), strings.Contains(compact, "mpeg/l2"):
			return "MP2"
		}
	}
	return ""
}

func channels(track map[string]any) string {
	// ChannelLayout is a list of speaker positions (for example
	// "C L R Ls Rs LFE"), not a release-name channel token. Prefer the
	// numeric MediaInfo fields and normalize the layout only as fallback
	// evidence; raw speaker labels must never enter a release name.
	value := stringValue(track, "Channels", "Channels_Original", "Channel_s_", "Channel_s__Original")
	layout := stringValue(track, "ChannelLayout", "ChannelLayout_Original", "ChannelPositions", "ChannelPositions_Original")
	if value == "" {
		return channelLayoutNotation(layout)
	}
	if notation := channelNotation(value); notation != "" {
		return notation
	}
	count := firstChannelCount(value)
	if count == 0 {
		return ""
	}
	if notation := channelLayoutNotation(layout); notation != "" {
		return notation
	}
	// MediaInfo commonly exposes only the total count. Its conventional
	// multichannel values include one LFE channel, matching tracker notation.
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
		return fmt.Sprintf("%d.0", count)
	}
}

func channelLayoutNotation(value string) string {
	bed, lfe, height := 0, 0, 0
	cleaned := strings.NewReplacer(",", " ", ":", " ", ";", " ", "/", " ").Replace(value)
	for _, field := range strings.Fields(cleaned) {
		switch strings.ToUpper(strings.Trim(field, "()[]{}")) {
		case "LFE", "LFE1":
			lfe++
		case "LFE2":
			lfe += 2
		case "TFL", "TFC", "TFR", "TBL", "TBC", "TBR", "TSL", "TSR", "VHL", "VHC", "VHR", "LH", "CH", "RH":
			height++
		case "L", "R", "C", "LS", "RS", "LB", "RB", "LC", "RC", "CS", "BC", "LW", "RW":
			bed++
		}
	}
	if bed == 0 && lfe == 0 && height == 0 {
		return ""
	}
	if height > 0 {
		return fmt.Sprintf("%d.%d.%d", bed, lfe, height)
	}
	return fmt.Sprintf("%d.%d", bed, lfe)
}

func channelNotation(value string) string {
	match := regexp.MustCompile(`(?:^|[^0-9])(\d{1,2})\.(\d{1,2})(?:\.(\d{1,2}))?(?:$|[^0-9])`).FindStringSubmatch(value)
	if match == nil {
		return ""
	}
	if match[3] != "" {
		return match[1] + "." + match[2] + "." + match[3]
	}
	return match[1] + "." + match[2]
}

func firstChannelCount(value string) int {
	match := regexp.MustCompile(`\d+`).FindString(value)
	if match == "" {
		return 0
	}
	count, _ := strconv.Atoi(match)
	return count
}

func unique(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}
