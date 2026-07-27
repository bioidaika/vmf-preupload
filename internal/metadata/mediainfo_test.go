package metadata

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
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
