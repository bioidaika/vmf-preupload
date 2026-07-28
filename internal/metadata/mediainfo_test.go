package metadata

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestExtractUsesExplicitMediaInfoBinary(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the bundled fixture is Windows MediaInfo CLI")
	}
	root := t.TempDir()
	path := filepath.Join(root, "sample.mkv")
	if err := os.WriteFile(path, []byte("not a real media container"), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join("..", "..", "assets", "mediainfo", "MediaInfo.exe")
	info, warnings := Extract(context.Background(), path, bin)
	if info.RawJSON == "" {
		t.Fatalf("MediaInfo JSON is empty; warnings=%v", warnings)
	}
	if info.RawText == "" {
		t.Fatalf("MediaInfo text is empty; warnings=%v", warnings)
	}
}

func TestNormalizeAudioCodecUsesMPEGLayerEvidence(t *testing.T) {
	tests := []struct {
		name  string
		track map[string]any
		want  string
	}{
		{name: "profile layer 2", track: map[string]any{"Format": "MPEG Audio", "Format_Profile": "Layer 2"}, want: "MP2"},
		{name: "profile layer 3", track: map[string]any{"Format": "MPEG Audio", "Format_Profile_String": "Layer 3"}, want: "MP3"},
		{name: "roman profile layer II", track: map[string]any{"Format": "MPEG Audio", "Format_Profile": "Layer II"}, want: "MP2"},
		{name: "roman profile layer III", track: map[string]any{"Format": "MPEG Audio", "Format_Profile": "Layer III"}, want: "MP3"},
		{name: "codec id layer 2", track: map[string]any{"Format": "MPEG Audio", "CodecID": "A_MPEG/L2"}, want: "MP2"},
		{name: "compatible codec id layer 3", track: map[string]any{"Format": "MPEG Audio", "CodecID_Compatible": "A_MPEG/L3"}, want: "MP3"},
		{name: "explicit mp2 codec id", track: map[string]any{"Format": "Audio", "CodecID": "MP2"}, want: "MP2"},
		{name: "unknown layer is not guessed", track: map[string]any{"Format": "MPEG Audio"}, want: "MPEG Audio"},
		{name: "unrecognized profile is not guessed", track: map[string]any{"Format": "MPEG Audio", "Format_Profile": "Unknown"}, want: "MPEG Audio"},
		{name: "unrecognized codec id is not guessed", track: map[string]any{"Format": "MPEG Audio", "CodecID": "A_MPEG/UNKNOWN"}, want: "MPEG Audio"},
		{name: "existing AAC mapping", track: map[string]any{"Format": "AAC"}, want: "AAC"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeAudioCodec(tt.track); got != tt.want {
				t.Fatalf("normalizeAudioCodec()=%q want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeAudioCodecDistinguishesDTSHDProfiles(t *testing.T) {
	tests := []struct {
		name  string
		track map[string]any
		want  string
	}{
		{name: "master audio", track: map[string]any{"Format": "DTS", "Format_Commercial": "DTS-HD Master Audio"}, want: "DTS-HD.MA"},
		{name: "high resolution", track: map[string]any{"Format": "DTS-HD", "Format_Profile": "High Resolution Audio"}, want: "DTS-HD.HRA"},
		{name: "unspecified dts hd", track: map[string]any{"Format": "DTS-HD"}, want: "DTS-HD"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeAudioCodec(tt.track); got != tt.want {
				t.Fatalf("normalizeAudioCodec()=%q want %q", got, tt.want)
			}
		})
	}
}

func TestDetectAudioAtmosCombinesAllMediaInfoEvidence(t *testing.T) {
	tests := []map[string]any{
		{"Format_AdditionalFeatures": "JOC", "Format_Commercial": "Dolby Digital Plus"},
		{"Format_AdditionalFeatures": "16-ch", "Format_Commercial": "Dolby TrueHD with Dolby Atmos"},
		{"Title": "Vietnamese Dolby Atmos", "Format": "E-AC-3"},
	}
	for _, track := range tests {
		if !detectAudioAtmos(track) {
			t.Fatalf("Atmos evidence was missed: %#v", track)
		}
	}
	if detectAudioAtmos(map[string]any{"Format": "E-AC-3", "Title": "Main audio"}) {
		t.Fatal("Atmos was invented without evidence")
	}
}

func TestChannelsUsesCountInsteadOfLeakingSpeakerLayout(t *testing.T) {
	tests := []struct {
		name  string
		track map[string]any
		want  string
	}{
		{
			name:  "dts hd 5.1 layout",
			track: map[string]any{"Channels": "6", "ChannelLayout": "C L R Ls Rs LFE"},
			want:  "5.1",
		},
		{
			name:  "seven one layout",
			track: map[string]any{"Channels_Original": "8 channels", "ChannelLayout": "L R C LFE Ls Rs Lb Rb"},
			want:  "7.1",
		},
		{
			name:  "stereo",
			track: map[string]any{"Channels": "2", "ChannelLayout": "L R"},
			want:  "2.0",
		},
		{
			name:  "explicit notation",
			track: map[string]any{"Channels": "5.1"},
			want:  "5.1",
		},
		{
			name:  "layout alone is normalized",
			track: map[string]any{"ChannelLayout": "C L R Ls Rs LFE"},
			want:  "5.1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := channels(tt.track); got != tt.want {
				t.Fatalf("channels()=%q want %q", got, tt.want)
			}
			if got := channels(tt.track); strings.Contains(got, "LFE") || strings.Contains(got, "Ls") {
				t.Fatalf("speaker layout leaked from channels(): %q", got)
			}
		})
	}
}

func TestDetectEncodeRequiresEncoderLibraryEvidence(t *testing.T) {
	if got := detectEncode(map[string]any{"Format": "AVC"}); got != "" {
		t.Fatalf("codec family alone must not claim x264: %q", got)
	}
	if got := detectEncode(map[string]any{"Encoded_Library_Name": "x264 core 164"}); got != "x264" {
		t.Fatalf("x264 library was not detected: %q", got)
	}
	if got := detectEncode(map[string]any{"Encoded_Library_Name": "x265 3.5"}); got != "x265" {
		t.Fatalf("x265 library was not detected: %q", got)
	}
}

func TestVideoCodecUsesMPEGVersionEvidence(t *testing.T) {
	if got := videoCodec(map[string]any{"Format": "MPEG Video", "Format_Version": "Version 2"}); got != "MPEG-2" {
		t.Fatalf("videoCodec()=%q want MPEG-2", got)
	}
}
