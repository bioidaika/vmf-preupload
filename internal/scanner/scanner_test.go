package scanner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bioidaika/vmf-preupload/pkg/api"
)

func TestScanPathInventoriesNonVideoFilesSeparately(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"movie.nfo":       "metadata",
		"poster.jpg":      "image",
		"Show.sample.mkv": "sample",
		filepath.Join("Season 1", "show.s01e01.vi.srt"):    "subtitle",
		filepath.Join("Season 1", "Extras", "notes.txt"):   "nested extras remains input",
		filepath.Join("Season 1", "Extras", "trailer.mkv"): "nested Extras video stays extra",
		filepath.Join(".cache", "preview.mkv"):             "dot-directory video stays extra",
		filepath.Join("Extras", "already-moved.nfo"):       "planner will make this a no-op",
		filepath.Join("Extras", "trailer.mkv"):             "video inside Extras stays extra",
	}
	for relative, contents := range files {
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := ScanPath(context.Background(), root, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Assets) != 0 {
		t.Fatalf("non-video files became media assets: %#v", result.Assets)
	}
	if len(result.ExtraFiles) != 9 {
		t.Fatalf("got %d extra files, want 9: %#v", len(result.ExtraFiles), result.ExtraFiles)
	}
	wantKinds := map[string]string{
		"movie.nfo":       "other",
		"poster.jpg":      "image",
		"Show.sample.mkv": "other",
		filepath.Join("Season 1", "show.s01e01.vi.srt"):    "subtitle",
		filepath.Join("Season 1", "Extras", "notes.txt"):   "other",
		filepath.Join("Season 1", "Extras", "trailer.mkv"): "other",
		filepath.Join(".cache", "preview.mkv"):             "other",
		filepath.Join("Extras", "already-moved.nfo"):       "other",
		filepath.Join("Extras", "trailer.mkv"):             "other",
	}
	for _, file := range result.ExtraFiles {
		if want := wantKinds[file.RelativePath]; file.Kind != want {
			t.Errorf("%s kind=%q want %q", file.RelativePath, file.Kind, want)
		}
		if file.Size == 0 {
			t.Errorf("%s size was not captured", file.RelativePath)
		}
	}
}

func TestScanPathDoesNotFollowSymlinkForExtraInventory(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "outside.nfo")
	if err := os.WriteFile(target, []byte("metadata"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked.nfo")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink creation is unavailable: %v", err)
	}

	result, err := ScanPath(context.Background(), root, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ExtraFiles) != 1 || result.ExtraFiles[0].Name != "outside.nfo" {
		t.Fatalf("symlink entered extra inventory: %#v", result.ExtraFiles)
	}
	if len(result.Warnings) == 0 || !strings.Contains(result.Warnings[0], "skipped non-regular") {
		t.Fatalf("skipped symlink was not surfaced: %#v", result.Warnings)
	}
	direct, err := ScanPath(context.Background(), link, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(direct.Assets) != 0 || len(direct.ExtraFiles) != 0 || len(direct.Warnings) == 0 {
		t.Fatalf("directly selected symlink must be skipped with a warning: %#v", direct)
	}
}

func TestSampleDetectionDoesNotConsumeTitlesContainingSample(t *testing.T) {
	root := t.TempDir()
	for _, relative := range []string{
		"The.Sample.2025.mkv",
		"Sampled.S01E01.mkv",
		"forced.!sample.mkv",
		"Show.S01E01.sample.mkv",
		filepath.Join("Sample", "clip.mkv"),
	} {
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(relative), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	result, err := ScanPath(context.Background(), root, "")
	if err != nil {
		t.Fatal(err)
	}
	assetNames := map[string]bool{}
	for _, asset := range result.Assets {
		assetNames[asset.Name] = true
	}
	for _, want := range []string{"The.Sample.2025.mkv", "Sampled.S01E01.mkv", "forced.!sample.mkv"} {
		if !assetNames[want] {
			t.Errorf("payload title was classified as sample: %s", want)
		}
	}
	extraNames := map[string]bool{}
	for _, extra := range result.ExtraFiles {
		extraNames[extra.RelativePath] = true
	}
	for _, want := range []string{"Show.S01E01.sample.mkv", filepath.Join("Sample", "clip.mkv")} {
		if !extraNames[want] {
			t.Errorf("sample was not classified as extra: %s", want)
		}
	}
}

func TestScanPathKeepsCommonVideoExtensionsInPayload(t *testing.T) {
	root := t.TempDir()
	for _, extension := range []string{".mpg", ".mpeg", ".mts", ".vob", ".wmv", ".mxf"} {
		path := filepath.Join(root, "Show.S01E01"+extension)
		if err := os.WriteFile(path, []byte(extension), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	result, err := ScanPath(context.Background(), root, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Assets) != 6 || len(result.ExtraFiles) != 0 {
		t.Fatalf("common video extensions were isolated as Extras: assets=%d extras=%#v", len(result.Assets), result.ExtraFiles)
	}
}

func TestPrimaryAudioKeepsMPEGCodecAndChannelsSeparate(t *testing.T) {
	for _, codec := range []string{"MP2", "MP3"} {
		t.Run(codec, func(t *testing.T) {
			got := primaryAudio([]api.Track{{Type: "Audio", Codec: codec, Channels: "2.0", Default: true}})
			want := codec + ".2.0"
			if got != want {
				t.Fatalf("primaryAudio()=%q want %q", got, want)
			}
		})
	}
}
