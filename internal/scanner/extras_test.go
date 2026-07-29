package scanner

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestScanPathInventoriesTopLevelExtrasForPlannerIdempotence(t *testing.T) {
	root := t.TempDir()
	paths := map[string]string{
		"Show.S01E01.mkv": "video",
		"root.nfo":        "root-extra",
		filepath.Join("eXtRaS", "already-moved.nfo"):       "ignored",
		filepath.Join("eXtRaS", "nested", "poster.jpg"):    "ignored-too",
		filepath.Join("Season 1", "Extras", "sidecar.srt"): "nested-extra",
	}
	for relative, contents := range paths {
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	result, err := ScanPath(context.Background(), root, filepath.Join(root, "missing-mediainfo"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Assets) != 1 {
		t.Fatalf("assets = %#v, want the one video", result.Assets)
	}
	got := make([]string, 0, len(result.ExtraFiles))
	for _, extra := range result.ExtraFiles {
		got = append(got, extra.RelativePath)
	}
	sort.Strings(got)
	want := []string{
		filepath.Join("Season 1", "Extras", "sidecar.srt"),
		filepath.Join("eXtRaS", "already-moved.nfo"),
		filepath.Join("eXtRaS", "nested", "poster.jpg"),
		"root.nfo",
	}
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("extra relative paths = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("extra relative paths = %#v, want %#v", got, want)
		}
	}
}
