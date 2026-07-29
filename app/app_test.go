package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bioidaika/vmf-preupload/internal/rename"
)

func TestFailedOutcomeKeepsAppliedJournalAttentionVisibleAndGuardsApply(t *testing.T) {
	application := NewApp()
	application.journal = &rename.Journal{
		Path:  filepath.Join(t.TempDir(), "applied.json"),
		State: rename.JournalApplied,
	}
	application.journalAttention = true

	if !application.HasUndoJournal() {
		t.Fatal("an applied journal from a failed operation outcome must keep Undo visible")
	}
	if !application.UndoNeedsAttention() {
		t.Fatal("an applied journal from a failed operation outcome must require attention")
	}
	if err := application.ApplyRename(RenamePlan{}); err == nil || !strings.Contains(strings.ToLower(err.Error()), "previous transaction") {
		t.Fatalf("a new Apply must be blocked until the failed outcome is resolved, got %v", err)
	}
}

func TestFailedOutcomeKeepsRolledBackJournalUndoVisible(t *testing.T) {
	application := NewApp()
	application.journal = &rename.Journal{
		Path:  filepath.Join(t.TempDir(), "rolled-back.json"),
		State: rename.JournalRolledBack,
	}
	application.journalAttention = true

	if !application.HasUndoJournal() {
		t.Fatal("a failed operation outcome must keep retryable Undo visible even when the last journal state says rolled back")
	}
	if !application.UndoNeedsAttention() {
		t.Fatal("a failed operation outcome must remain attention-worthy even when the last journal state says rolled back")
	}
}

func TestRenamePlanJSONIncludesEmptyCollections(t *testing.T) {
	data, err := json.Marshal(RenamePlan{
		ID:       "plan-1",
		Items:    []RenameItem{},
		Warnings: []string{},
		Errors:   []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"items", "warnings", "errors"} {
		items, ok := value[field].([]any)
		if !ok || len(items) != 0 {
			t.Fatalf("%s must be present as an empty JSON array: %s", field, data)
		}
	}
}

func TestPreviewApplyUndoSingleFile(t *testing.T) {
	root := t.TempDir()
	oldPath := filepath.Join(root, "old.mkv")
	if err := os.WriteFile(oldPath, []byte("synthetic-media"), 0o600); err != nil {
		t.Fatal(err)
	}

	application := NewApp()
	plan, err := application.PreviewRename(RenameRequest{
		RootPath: oldPath,
		Metadata: TechnicalMetadata{
			MediaType:   "movie",
			Title:       "Example Movie",
			Year:        "2026",
			Resolution:  "2160p",
			ReleaseType: "WEB-DL",
			Audio:       "DDP5.1",
			VideoCodec:  "H.265",
			Group:       "NoGroup",
		},
		Separator: ".",
	})
	if err != nil {
		t.Fatalf("PreviewRename: %v", err)
	}
	if len(plan.Items) != 1 {
		t.Fatalf("items=%d want=1: %#v", len(plan.Items), plan.Items)
	}
	newPath := plan.Items[0].NewPath
	wantBase := "Example.Movie.2026.2160p.WEB-DL.DDP5.1.H.265-NoGroup.mkv"
	if filepath.Base(newPath) != wantBase {
		t.Fatalf("new basename=%q want=%q", filepath.Base(newPath), wantBase)
	}
	if strings.Contains(filepath.Base(newPath), ".UHD.") {
		t.Fatalf("UHD must not be inferred for a 2160p WEB-DL: %q", newPath)
	}

	if err := application.ApplyRename(plan); err != nil {
		t.Fatalf("ApplyRename: %v", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("renamed file missing: %v", err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old path still exists or returned unexpected error: %v", err)
	}
	if err := application.UndoRename(); err != nil {
		t.Fatalf("UndoRename: %v", err)
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("old path not restored: %v", err)
	}
}

func TestPreviewFolderBuildsNestedFolderAndFilePlan(t *testing.T) {
	parent := t.TempDir()
	oldFolder := filepath.Join(parent, "unstructured")
	if err := os.Mkdir(oldFolder, 0o700); err != nil {
		t.Fatal(err)
	}
	oldFile := filepath.Join(oldFolder, "episode.mkv")
	if err := os.WriteFile(oldFile, []byte("synthetic-media"), 0o600); err != nil {
		t.Fatal(err)
	}

	application := NewApp()
	plan, err := application.PreviewRename(RenameRequest{
		RootPath: oldFolder,
		Metadata: TechnicalMetadata{
			MediaType:    "tv",
			Title:        "Example Show",
			Season:       "1",
			Episode:      "2",
			EpisodeTitle: "Pilot",
			Resolution:   "1080p",
			ReleaseType:  "WEBRip",
			Audio:        "AAC2.0",
			VideoEncode:  "x264",
			Group:        "NoGroup",
		},
		Separator: ".",
	})
	if err != nil {
		t.Fatalf("PreviewRename: %v", err)
	}
	if len(plan.Items) != 2 {
		t.Fatalf("items=%d want=2: %#v", len(plan.Items), plan.Items)
	}
	if len(plan.Errors) != 0 {
		t.Fatalf("unexpected preflight errors: %#v", plan.Errors)
	}
}

func TestPreviewSeasonFolderUsesEachParsedEpisode(t *testing.T) {
	parent := t.TempDir()
	oldFolder := filepath.Join(parent, "incoming")
	if err := os.Mkdir(oldFolder, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Example.Show.S01E01.1080p.WEB-DL.mkv", "Example.Show.S01E02.1080p.WEB-DL.mkv"} {
		if err := os.WriteFile(filepath.Join(oldFolder, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	application := NewApp()
	plan, err := application.PreviewRename(RenameRequest{
		RootPath:  oldFolder,
		Metadata:  TechnicalMetadata{MediaType: "tv", Title: "Example Show", Resolution: "1080p", ReleaseType: "WEB-DL", VideoCodec: "H.264", Group: "NoGroup"},
		Separator: ".",
	})
	if err != nil {
		t.Fatalf("PreviewRename: %v", err)
	}
	if len(plan.Items) != 3 {
		t.Fatalf("items=%d want folder plus two episodes: %#v", len(plan.Items), plan.Items)
	}
	seen := map[string]bool{}
	for _, item := range plan.Items {
		if strings.Contains(item.NewPath, "S01E01") {
			seen["01"] = true
		}
		if strings.Contains(item.NewPath, "S01E02") {
			seen["02"] = true
		}
	}
	if !seen["01"] || !seen["02"] {
		t.Fatalf("episode destinations did not preserve per-file identities: %#v", plan.Items)
	}
}

func TestPreviewPreservesExplicitUHDMarker(t *testing.T) {
	for _, filename := range []string{
		"Example.Movie.2026.UHD.BluRay.REMUX.2160p.mkv",
		"Example.Movie.2026.Ultra.HD.BluRay.REMUX.2160p.mkv",
	} {
		t.Run(filename, func(t *testing.T) {
			root := t.TempDir()
			oldPath := filepath.Join(root, filename)
			if err := os.WriteFile(oldPath, []byte("synthetic-media"), 0o600); err != nil {
				t.Fatal(err)
			}
			application := NewApp()
			plan, err := application.PreviewRename(RenameRequest{
				RootPath:  oldPath,
				Metadata:  TechnicalMetadata{MediaType: "movie", Title: "Example Movie", Year: "2026", Resolution: "2160p", Source: "BluRay", ReleaseType: "REMUX", VideoCodec: "H.265", Group: "NoGroup"},
				Separator: ".",
			})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(filepath.Base(plan.Items[0].NewPath), ".UHD.") {
				t.Fatalf("explicit UHD marker from the source filename should be preserved: %s", plan.Items[0].NewPath)
			}
		})
	}
}

func TestPreviewNeverInfersUHDFromResolutionSourceOrType(t *testing.T) {
	parent := t.TempDir()
	cases := []struct {
		name        string
		releaseType string
		source      string
	}{
		{name: "web-dl", releaseType: "WEB-DL", source: "WEB"},
		{name: "webrip", releaseType: "WEBRip", source: "WEB"},
		{name: "web encode", releaseType: "ENCODE", source: "WEB"},
		{name: "source-less encode", releaseType: "ENCODE"},
		{name: "blu-ray encode", releaseType: "ENCODE", source: "BluRay"},
		{name: "remux", releaseType: "REMUX", source: "BluRay"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(parent, tt.name+".mkv")
			if err := os.WriteFile(path, []byte("synthetic-media"), 0o600); err != nil {
				t.Fatal(err)
			}
			application := NewApp()
			plan, err := application.PreviewRename(RenameRequest{
				RootPath:  path,
				Metadata:  TechnicalMetadata{MediaType: "movie", Title: "Example Source", Year: "2026", Resolution: "2160p", Source: tt.source, ReleaseType: tt.releaseType, VideoCodec: "H.265", Group: "NoGroup"},
				Separator: ".",
			})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(filepath.Base(plan.Items[0].NewPath), ".UHD.") {
				t.Fatalf("UHD must not be inferred from 2160p/type/source: %s", plan.Items[0].NewPath)
			}
		})
	}
}

func TestPreviewDoesNotAddUHDToReported4KFilename(t *testing.T) {
	root := t.TempDir()
	oldPath := filepath.Join(root, "The Blood of Youth Quest of Heroic Hearts.2026.S01E01_4K.mkv")
	if err := os.WriteFile(oldPath, []byte("synthetic-media"), 0o600); err != nil {
		t.Fatal(err)
	}
	application := NewApp()
	plan, err := application.PreviewRename(RenameRequest{
		RootPath: oldPath,
		Metadata: TechnicalMetadata{
			MediaType: "tv", Title: "The Blood of Youth Quest of Heroic Hearts", Year: "2026",
			Season: "1", Episode: "1", Resolution: "2160p", ReleaseType: "ENCODE",
			VideoCodec: "H.265", Group: "NoGroup", UHD: true, // stale client value must be ignored
		},
		Separator: ".",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(filepath.Base(plan.Items[0].NewPath), ".UHD.") {
		t.Fatalf("4K/2160p without an original UHD marker must stay untagged: %s", plan.Items[0].NewPath)
	}
}

func TestPreviewUsesEnglishTitleAndNormalizedMPEGAudio(t *testing.T) {
	root := t.TempDir()
	oldPath := filepath.Join(root, "The Blood of Youth Quest of Heroic Hearts.2026.S01E18_4K.mkv")
	if err := os.WriteFile(oldPath, []byte("synthetic-media"), 0o600); err != nil {
		t.Fatal(err)
	}
	application := NewApp()
	plan, err := application.PreviewRename(RenameRequest{
		RootPath: oldPath,
		Metadata: TechnicalMetadata{
			MediaType: "tv", Title: "The Blood of Youth Quest of Heroic Hearts",
			OriginalTitle: "少年歌行之天下无双", Year: "2026", Season: "1", Episode: "18",
			Resolution: "2160p", ReleaseType: "ENCODE", VideoCodec: "H.265",
			Audio: "MP2.2.0", Languages: "vi", Group: "NoGroup",
		},
		Separator: ".",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "The.Blood.of.Youth.Quest.of.Heroic.Hearts.2026.S01E18.ViE.2160p.MP2.2.0.H.265-NoGroup.mkv"
	got := filepath.Base(plan.Items[0].NewPath)
	if got != want {
		t.Fatalf("new basename=%q want %q", got, want)
	}
	for _, forbidden := range []string{"少年歌行之天下无双", "MPEG.Audio", ".UHD."} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("new basename contains forbidden token %q: %s", forbidden, got)
		}
	}
}

func TestPreviewIgnoresUHDInParentFolderName(t *testing.T) {
	root := filepath.Join(t.TempDir(), "UHD Archive")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(root, "Example.Movie.2026.2160p.BluRay.REMUX.mkv")
	if err := os.WriteFile(oldPath, []byte("synthetic-media"), 0o600); err != nil {
		t.Fatal(err)
	}
	application := NewApp()
	scan, err := application.ScanPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if scan.Metadata.UHD {
		t.Fatal("a parent folder name must not become UHD filename evidence")
	}
	plan, err := application.PreviewRename(RenameRequest{
		RootPath:  root,
		Metadata:  TechnicalMetadata{MediaType: "movie", Title: "Example Movie", Year: "2026", Resolution: "2160p", Source: "BluRay", ReleaseType: "REMUX", VideoCodec: "H.265", Group: "NoGroup"},
		Separator: ".",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range plan.Items {
		if strings.Contains(filepath.Base(item.NewPath), ".UHD.") {
			t.Fatalf("parent folder leaked UHD into destination: %#v", item)
		}
	}
}

func TestPreviewKeepsUHDPerAssetInMixedSeasonFolder(t *testing.T) {
	root := filepath.Join(t.TempDir(), "incoming")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	withUHD := filepath.Join(root, "Example.Show.S01E01.UHD.2160p.BluRay.REMUX.mkv")
	withoutUHD := filepath.Join(root, "Example.Show.S01E02.2160p.BluRay.REMUX.mkv")
	for _, path := range []string{withUHD, withoutUHD} {
		if err := os.WriteFile(path, []byte("synthetic-media"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	application := NewApp()
	scanned, err := application.ScanPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if scanned.Metadata.UHD {
		t.Fatal("mixed season scan must not expose a folder-level UHD flag")
	}
	metadata := scanned.Metadata
	metadata.MediaType = "tv"
	metadata.Title = "Example Show"
	metadata.Resolution = "2160p"
	metadata.Source = "BluRay"
	metadata.ReleaseType = "REMUX"
	metadata.VideoCodec = "H.265"
	metadata.Group = "NoGroup"
	plan, err := application.PreviewRename(RenameRequest{RootPath: root, Metadata: metadata, Separator: "."})
	if err != nil {
		t.Fatal(err)
	}
	foundWith, foundWithout, foundFolder := false, false, false
	for _, item := range plan.Items {
		hasUHD := strings.Contains(filepath.Base(item.NewPath), ".UHD.")
		switch item.OldPath {
		case withUHD:
			foundWith = true
			if !hasUHD {
				t.Fatalf("explicit file marker was lost: %#v", item)
			}
		case withoutUHD:
			foundWithout = true
			if hasUHD {
				t.Fatalf("UHD leaked into untagged episode: %#v", item)
			}
		case root:
			foundFolder = true
			if hasUHD {
				t.Fatalf("mixed season folder must not be tagged UHD: %#v", item)
			}
		}
	}
	if !foundWith || !foundWithout || !foundFolder {
		t.Fatalf("incomplete mixed-season plan: %#v", plan.Items)
	}
}

func TestPreviewKeepsUHDWhenEverySeasonFileIsExplicit(t *testing.T) {
	root := filepath.Join(t.TempDir(), "incoming")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"Example.Show.S01E01.UHD.2160p.BluRay.REMUX.mkv",
		"Example.Show.S01E02.Ultra.HD.2160p.BluRay.REMUX.mkv",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("synthetic-media"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	application := NewApp()
	scanned, err := application.ScanPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if !scanned.Metadata.UHD {
		t.Fatal("an all-explicit season scan must expose folder-level UHD evidence")
	}
	metadata := scanned.Metadata
	metadata.MediaType = "tv"
	metadata.Title = "Example Show"
	metadata.Resolution = "2160p"
	metadata.Source = "BluRay"
	metadata.ReleaseType = "REMUX"
	metadata.VideoCodec = "H.265"
	metadata.Group = "NoGroup"
	plan, err := application.PreviewRename(RenameRequest{RootPath: root, Metadata: metadata, Separator: "."})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 3 {
		t.Fatalf("items=%d want folder plus two episodes: %#v", len(plan.Items), plan.Items)
	}
	for _, item := range plan.Items {
		if !strings.Contains(filepath.Base(item.NewPath), ".UHD.") {
			t.Fatalf("all-explicit season item lost UHD: %#v", item)
		}
	}
}

func TestScanPathPropagatesMediaInfoWarnings(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "movie.mkv")
	if err := os.WriteFile(path, []byte("not a real media container"), 0o600); err != nil {
		t.Fatal(err)
	}
	application := NewApp()
	application.settings.MediaInfoBin = filepath.Join(root, "missing-mediainfo.exe")
	result, err := application.ScanPath(path)
	if err != nil {
		t.Fatalf("ScanPath: %v", err)
	}
	if len(result.Warnings) == 0 {
		t.Fatalf("expected MediaInfo warning to cross the app DTO boundary: %#v", result)
	}
	found := false
	for _, warning := range result.Warnings {
		if strings.Contains(warning, "MediaInfo") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("unexpected warnings: %#v", result.Warnings)
	}
}

func TestPreviewPreservesExistingP2PGothamExactly(t *testing.T) {
	root := t.TempDir()
	originalName := "Gotham.S01E01.Pilot.1080p.DTS-HD.MA.5.1.AVC.REMUX-FraMeSToR.mkv"
	originalPath := filepath.Join(root, originalName)
	if err := os.WriteFile(originalPath, []byte("synthetic-media"), 0o600); err != nil {
		t.Fatal(err)
	}

	application := NewApp()
	plan, err := application.PreviewRename(RenameRequest{
		RootPath: originalPath,
		Metadata: TechnicalMetadata{
			MediaType:    "tv",
			Title:        "Gotham",
			Year:         "2014",
			Season:       "1",
			Episode:      "1",
			EpisodeTitle: "Pilot",
			Resolution:   "1080p",
			ReleaseType:  "REMUX",
			VideoCodec:   "H.264",
			Audio:        "DTS-HD.MA.5.1",
			Group:        "NoGroup",
		},
		Separator:           ".",
		PreserveExistingP2P: testBoolPointer(true),
	})
	if err != nil {
		t.Fatalf("PreviewRename: %v", err)
	}
	if len(plan.Items) != 1 {
		t.Fatalf("items=%d want=1: %#v", len(plan.Items), plan.Items)
	}
	item := plan.Items[0]
	if item.Status != "preserved" {
		t.Fatalf("status=%q want preserved: %#v", item.Status, item)
	}
	if item.OldPath != originalPath || item.NewPath != originalPath {
		t.Fatalf("protected release must remain exact: %#v", item)
	}
	if filepath.Base(item.NewPath) != originalName {
		t.Fatalf("basename=%q want exact %q", filepath.Base(item.NewPath), originalName)
	}
	if plan.ChangeCount != 0 || plan.CanApply {
		t.Fatalf("no-op plan changeCount=%d canApply=%t", plan.ChangeCount, plan.CanApply)
	}
	if err := application.ApplyRename(plan); err == nil || !strings.Contains(strings.ToLower(err.Error()), "nothing to rename") {
		t.Fatalf("ApplyRename error=%v want nothing-to-rename", err)
	}
	if application.journal != nil {
		t.Fatalf("no-op apply must not create a journal: %#v", application.journal)
	}
	if _, err := os.Stat(originalPath); err != nil {
		t.Fatalf("preserved source disappeared: %v", err)
	}
}

func TestPreviewCanForceVMFRenderForExistingP2P(t *testing.T) {
	root := t.TempDir()
	originalName := "Gotham.S01E01.Pilot.1080p.DTS-HD.MA.5.1.AVC.REMUX-FraMeSToR.mkv"
	originalPath := filepath.Join(root, originalName)
	if err := os.WriteFile(originalPath, []byte("synthetic-media"), 0o600); err != nil {
		t.Fatal(err)
	}

	application := NewApp()
	plan, err := application.PreviewRename(RenameRequest{
		RootPath: originalPath,
		Metadata: TechnicalMetadata{
			MediaType:    "tv",
			Title:        "Gotham",
			Year:         "2014",
			Season:       "1",
			Episode:      "1",
			EpisodeTitle: "Pilot",
			Resolution:   "1080p",
			ReleaseType:  "REMUX",
			VideoCodec:   "H.264",
			Audio:        "DTS-HD.MA.5.1",
			Group:        "NoGroup",
		},
		Separator:           ".",
		PreserveExistingP2P: testBoolPointer(false),
	})
	if err != nil {
		t.Fatalf("PreviewRename: %v", err)
	}
	if len(plan.Items) != 1 {
		t.Fatalf("items=%d want=1: %#v", len(plan.Items), plan.Items)
	}
	item := plan.Items[0]
	if item.Status != "ready" {
		t.Fatalf("status=%q want ready: %#v", item.Status, item)
	}
	want := "Gotham.2014.S01E01.Pilot.1080p.REMUX.AVC.DTS-HD.MA.5.1-NoGroup.mkv"
	if got := filepath.Base(item.NewPath); got != want {
		t.Fatalf("forced VMF basename=%q want %q", got, want)
	}
	if item.NewPath == originalPath {
		t.Fatalf("preserve=false unexpectedly kept the source name: %#v", item)
	}
	if plan.ChangeCount != 1 || !plan.CanApply {
		t.Fatalf("forced plan changeCount=%d canApply=%t", plan.ChangeCount, plan.CanApply)
	}
}

func TestPreviewGenericFolderCarriesProtectedChildAtRelativeSeasonPath(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "incoming")
	seasonDir := filepath.Join(root, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o700); err != nil {
		t.Fatal(err)
	}
	originalName := "Gotham.S01E01.Pilot.1080p.DTS-HD.MA.5.1.AVC.REMUX-FraMeSToR.mkv"
	originalPath := filepath.Join(seasonDir, originalName)
	if err := os.WriteFile(originalPath, []byte("synthetic-media"), 0o600); err != nil {
		t.Fatal(err)
	}

	application := NewApp()
	plan, err := application.PreviewRename(RenameRequest{
		RootPath: root,
		Metadata: TechnicalMetadata{
			MediaType:    "tv",
			Title:        "Gotham",
			Year:         "2014",
			Season:       "1",
			Episode:      "1",
			EpisodeTitle: "Pilot",
			Resolution:   "1080p",
			ReleaseType:  "REMUX",
			VideoCodec:   "H.264",
			Audio:        "DTS-HD.MA.5.1",
			Group:        "NoGroup",
		},
		Separator:           ".",
		PreserveExistingP2P: testBoolPointer(true),
	})
	if err != nil {
		t.Fatalf("PreviewRename: %v", err)
	}
	if plan.ChangeCount != 1 || !plan.CanApply {
		t.Fatalf("folder-only plan changeCount=%d canApply=%t items=%#v", plan.ChangeCount, plan.CanApply, plan.Items)
	}

	var rootItem, seasonItem, protectedItem *RenameItem
	for index := range plan.Items {
		item := &plan.Items[index]
		switch {
		case item.OldPath == root && item.Kind == "folder":
			rootItem = item
		case item.OldPath == seasonDir && item.Kind == "folder":
			seasonItem = item
		case item.Kind == "file" && item.Status == "preserved":
			protectedItem = item
		}
	}
	if rootItem == nil || seasonItem == nil || protectedItem == nil {
		t.Fatalf("want unchanged series root, renamed season and protected child: %#v", plan.Items)
	}
	if rootItem.NewPath != root || rootItem.Status != "same" {
		t.Fatalf("series container must remain in place: %#v", rootItem)
	}
	wantChildPath := filepath.Join(seasonItem.NewPath, originalName)
	if protectedItem.OldPath != originalPath || protectedItem.NewPath != wantChildPath {
		t.Fatalf("protected child relative path changed: got %#v want destination %q", protectedItem, wantChildPath)
	}
	if err := application.ApplyRename(plan); err != nil {
		t.Fatalf("ApplyRename: %v", err)
	}
	if _, err := os.Stat(wantChildPath); err != nil {
		t.Fatalf("protected child missing after parent rename: %v", err)
	}
}

func TestPreviewMixedFolderPreservesP2PAndRendersRawEpisode(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "incoming")
	seasonDir := filepath.Join(root, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o700); err != nil {
		t.Fatal(err)
	}
	protectedName := "Gotham.S01E01.Pilot.1080p.DTS-HD.MA.5.1.AVC.REMUX-FraMeSToR.mkv"
	protectedPath := filepath.Join(seasonDir, protectedName)
	rawPath := filepath.Join(seasonDir, "Gotham.S01E02.mkv")
	for _, path := range []string{protectedPath, rawPath} {
		if err := os.WriteFile(path, []byte("synthetic-media"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	application := NewApp()
	plan, err := application.PreviewRename(RenameRequest{
		RootPath: root,
		Metadata: TechnicalMetadata{
			MediaType:   "tv",
			Title:       "Gotham",
			Year:        "2014",
			Season:      "1",
			Resolution:  "1080p",
			ReleaseType: "REMUX",
			VideoCodec:  "H.264",
			Audio:       "DTS-HD.MA.5.1",
			Group:       "NoGroup",
		},
		Separator:           ".",
		PreserveExistingP2P: testBoolPointer(true),
	})
	if err != nil {
		t.Fatalf("PreviewRename: %v", err)
	}
	if plan.ChangeCount != 2 || !plan.CanApply {
		t.Fatalf("mixed plan changeCount=%d canApply=%t items=%#v", plan.ChangeCount, plan.CanApply, plan.Items)
	}

	seasonDestination := ""
	for _, item := range plan.Items {
		if item.OldPath == seasonDir && item.Kind == "folder" {
			seasonDestination = item.NewPath
		}
	}
	foundProtected, foundRawReady := false, false
	for _, item := range plan.Items {
		switch item.OldPath {
		case protectedPath:
			foundProtected = item.Status == "preserved" && filepath.Base(item.NewPath) == protectedName && filepath.Clean(filepath.Dir(item.NewPath)) == filepath.Clean(seasonDestination)
		case rawPath:
			foundRawReady = item.Status == "ready" && strings.Contains(filepath.Base(item.NewPath), ".S01E02.")
		}
	}
	if !foundProtected || !foundRawReady {
		t.Fatalf("preservation leaked or raw episode was not rendered: %#v", plan.Items)
	}
}

func TestPreviewBlocksPartialPlanWhenDestinationsCollide(t *testing.T) {
	root := filepath.Join(t.TempDir(), "incoming")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"raw-a.mkv", "raw-b.mkv"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	application := NewApp()
	plan, err := application.PreviewRename(RenameRequest{
		RootPath: root,
		Metadata: TechnicalMetadata{
			MediaType: "movie", Title: "Example Movie", Year: "2024", Resolution: "1080p",
			ReleaseType: "WEB-DL", VideoCodec: "H.264", Group: "NoGroup",
		},
		Separator:           ".",
		PreserveExistingP2P: testBoolPointer(true),
	})
	if err != nil {
		t.Fatalf("PreviewRename: %v", err)
	}
	if len(plan.Errors) == 0 || plan.CanApply {
		t.Fatalf("collision must block the complete plan: %#v", plan)
	}
	fileItems, conflictingFiles := 0, 0
	for _, item := range plan.Items {
		if item.Kind == "file" {
			fileItems++
			if item.Status == "conflict" {
				conflictingFiles++
			}
		}
	}
	if fileItems != 2 || conflictingFiles != fileItems {
		t.Fatalf("every colliding file must be visible as a conflict: %#v", plan.Items)
	}
	if err := application.ApplyRename(plan); err == nil || !strings.Contains(strings.ToLower(err.Error()), "unresolved") {
		t.Fatalf("backend must reject a partial collision plan: %v", err)
	}
	for _, name := range []string{"raw-a.mkv", "raw-b.mkv"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatalf("collision rejection changed %s: %v", name, err)
		}
	}
}

func TestScanDoesNotAdoptDetectedThirdPartyGroupForRendering(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Gotham.S01E01.Pilot.1080p.DTS-HD.MA.5.1.AVC.REMUX-FraMeSToR.mkv")
	if err := os.WriteFile(path, []byte("synthetic-media"), 0o600); err != nil {
		t.Fatal(err)
	}
	application := NewApp()
	application.settings.ReleaseGroup = "NoGroup"
	result, err := application.ScanPath(path)
	if err != nil {
		t.Fatalf("ScanPath: %v", err)
	}
	if result.Metadata.Group != "NoGroup" {
		t.Fatalf("detected third-party group leaked into output metadata: %#v", result.Metadata)
	}
	if result.MediaType != "tv" || result.Metadata.MediaType != "tv" {
		t.Fatalf("TV category did not cross both scan DTO fields: %#v", result)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"videoEncode":""`) {
		t.Fatalf("empty encoder proof must be explicit in scan JSON: %s", data)
	}
}

func TestSettingsDTOOmissionKeepsPreservationSafeDefault(t *testing.T) {
	if got := settingsFromDTO(Settings{}); !got.PreserveExistingP2P {
		t.Fatal("an older client omitting preserveExistingP2P must retain the safe default")
	}
	value := false
	if got := settingsFromDTO(Settings{PreserveExistingP2P: &value}); got.PreserveExistingP2P {
		t.Fatal("an explicit false preserveExistingP2P setting must be honored")
	}
}

func testBoolPointer(value bool) *bool {
	return &value
}
