package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreviewApplyUndoMovesNestedSidecarsToSeriesExtras(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "Gotham (2014)")
	seasonOne := filepath.Join(root, "Season 1")
	seasonTwo := filepath.Join(root, "Season 2")
	for _, directory := range []string{seasonOne, seasonTwo, filepath.Join(root, "Extras")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	videoOne := filepath.Join(seasonOne, "Gotham.S01E01.1080p.WEB-DL.DDP5.1.H.264-TestGroup.mkv")
	videoTwo := filepath.Join(seasonTwo, "Gotham.S02E01.1080p.WEB-DL.DDP5.1.H.264-TestGroup.mkv")
	extraSources := map[string]string{
		filepath.Join(root, "tvshow.nfo"):                    "series-nfo",
		filepath.Join(seasonOne, "episode.nfo"):              "episode-nfo",
		filepath.Join(seasonOne, "poster.jpg"):               "season-one-poster",
		filepath.Join(seasonTwo, "poster.jpg"):               "season-two-poster",
		filepath.Join(seasonTwo, "subs", "episode.vi.srt"):   "subtitle",
		filepath.Join(seasonTwo, "audio", "commentary.flac"): "external-audio",
	}
	for path, contents := range map[string]string{
		videoOne: "season-one-video",
		videoTwo: "season-two-video",
	} {
		extrasWriteFile(t, path, contents)
	}
	for path, contents := range extraSources {
		extrasWriteFile(t, path, contents)
	}

	// This directory predates the transaction. The scanner may inventory its
	// content, but the planner must strip the leading Extras component and turn
	// the already-moved file into a no-op. Undo must never remove it.
	preexistingExtra := filepath.Join(root, "Extras", "keep.txt")
	extrasWriteFile(t, preexistingExtra, "keep")

	application := NewApp()
	application.settings.MediaInfoBin = filepath.Join(parent, "missing-mediainfo.exe")
	preserve := true
	request := RenameRequest{
		RootPath:            root,
		Metadata:            seasonLayoutMetadata(),
		Separator:           ".",
		PreserveExistingP2P: &preserve,
	}
	plan, err := application.PreviewRename(request)
	if err != nil {
		t.Fatalf("PreviewRename: %v", err)
	}
	if len(plan.Errors) != 0 || !plan.CanApply {
		t.Fatalf("series Extras plan must be applicable: %#v", plan)
	}
	if extrasHasMoveFrom(plan, preexistingExtra) {
		t.Fatalf("a file already below top-level Extras generated another move: %#v", plan.Items)
	}

	seasonOneFolder := seasonLayoutItemBySource(t, plan, seasonOne, "folder")
	seasonTwoFolder := seasonLayoutItemBySource(t, plan, seasonTwo, "folder")
	destinations := make(map[string]string, len(extraSources))
	for source := range extraSources {
		item := extrasItemBySource(t, plan, source)
		relative, err := filepath.Rel(root, source)
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(root, "Extras", relative)
		if filepath.Clean(item.NewPath) != filepath.Clean(want) {
			t.Fatalf("extra destination for %q = %q, want %q", source, item.NewPath, want)
		}
		if extrasPathWithin(item.NewPath, seasonOneFolder.NewPath) || extrasPathWithin(item.NewPath, seasonTwoFolder.NewPath) {
			t.Fatalf("extra remained inside a season torrent: %#v", item)
		}
		destinations[source] = item.NewPath
	}
	if destinations[filepath.Join(seasonOne, "poster.jpg")] == destinations[filepath.Join(seasonTwo, "poster.jpg")] {
		t.Fatal("relative season paths were flattened onto the same poster destination")
	}

	if err := application.ApplyRename(plan); err != nil {
		t.Fatalf("ApplyRename: %v", err)
	}
	for source, contents := range extraSources {
		if _, err := os.Stat(source); !os.IsNotExist(err) {
			t.Fatalf("extra source still exists after Apply: %s err=%v", source, err)
		}
		extrasAssertFile(t, destinations[source], contents)
	}
	extrasAssertFile(t, preexistingExtra, "keep")

	// A new application instance simulates a fresh scan. The scanner inventories
	// top-level Extras, then the planner strips that leading component so the
	// completed layout cannot generate Extras/Extras/... or any repeat move.
	secondApplication := NewApp()
	secondApplication.settings.MediaInfoBin = filepath.Join(parent, "missing-mediainfo.exe")
	secondPlan, err := secondApplication.PreviewRename(request)
	if err != nil {
		t.Fatalf("second PreviewRename: %v", err)
	}
	if secondPlan.ChangeCount != 0 || secondPlan.CanApply {
		t.Fatalf("completed Extras layout must be idempotent: %#v", secondPlan)
	}
	for _, item := range secondPlan.Items {
		if strings.Contains(strings.ToLower(filepath.Clean(item.NewPath)), strings.ToLower(filepath.Join("Extras", "Extras"))) {
			t.Fatalf("second preview nested Extras again: %#v", item)
		}
	}

	if err := application.UndoRename(); err != nil {
		t.Fatalf("UndoRename: %v", err)
	}
	for source, contents := range extraSources {
		extrasAssertFile(t, source, contents)
		if _, err := os.Stat(destinations[source]); !os.IsNotExist(err) {
			t.Fatalf("extra destination remains after Undo: %s err=%v", destinations[source], err)
		}
	}
	extrasAssertFile(t, preexistingExtra, "keep")
	for _, directory := range []string{
		filepath.Join(root, "Extras", "Season 1"),
		filepath.Join(root, "Extras", "Season 2"),
	} {
		if _, err := os.Stat(directory); !os.IsNotExist(err) {
			t.Fatalf("transaction-created Extras subdirectory remains after Undo: %s err=%v", directory, err)
		}
	}
}

func TestPreviewBlocksExistingExtraDestinationCollision(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "Gotham (2014)")
	seasonOne := filepath.Join(root, "Season 1")
	seasonTwo := filepath.Join(root, "Season 2")
	for _, directory := range []string{seasonOne, seasonTwo} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	extrasWriteFile(t, filepath.Join(seasonOne, "Gotham.S01E01.1080p.WEB-DL.H.264-TestGroup.mkv"), "one")
	extrasWriteFile(t, filepath.Join(seasonTwo, "Gotham.S02E01.1080p.WEB-DL.H.264-TestGroup.mkv"), "two")

	source := filepath.Join(seasonOne, "poster.jpg")
	destination := filepath.Join(root, "Extras", "Season 1", "poster.jpg")
	extrasWriteFile(t, source, "incoming")
	extrasWriteFile(t, destination, "existing")

	application := NewApp()
	application.settings.MediaInfoBin = filepath.Join(parent, "missing-mediainfo.exe")
	preserve := true
	plan, err := application.PreviewRename(RenameRequest{
		RootPath: root, Metadata: seasonLayoutMetadata(), Separator: ".", PreserveExistingP2P: &preserve,
	})
	if err != nil {
		t.Fatalf("PreviewRename: %v", err)
	}
	item := extrasItemBySource(t, plan, source)
	if filepath.Clean(item.NewPath) != filepath.Clean(destination) {
		t.Fatalf("collision destination = %q, want %q", item.NewPath, destination)
	}
	if plan.CanApply || !seasonLayoutContainsError(plan, "duplicate extra destination") {
		t.Fatalf("an existing Extras destination must block the whole plan: %#v", plan)
	}
	if err := application.ApplyRename(plan); err == nil {
		t.Fatal("ApplyRename succeeded despite an unresolved Extras collision")
	}
	extrasAssertFile(t, source, "incoming")
	extrasAssertFile(t, destination, "existing")
}

func TestSingleReleaseExtrasStayOutsideRenamedTorrentRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "incoming")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	video := filepath.Join(root, "Example.Movie.2026.1080p.WEB-DL.DDP5.1.H.264-TestGroup.mkv")
	extraSource := filepath.Join(root, "Artwork", "poster.jpg")
	extrasWriteFile(t, video, "video")
	extrasWriteFile(t, extraSource, "poster")

	application := NewApp()
	application.settings.MediaInfoBin = filepath.Join(parent, "missing-mediainfo.exe")
	preserve := true
	request := RenameRequest{
		RootPath: root,
		Metadata: TechnicalMetadata{
			MediaType: "movie", Title: "Example Movie", Year: "2026", Resolution: "1080p",
			ReleaseType: "WEB-DL", Audio: "DDP5.1", VideoCodec: "H.264", Group: "NoGroup",
		},
		Separator: ".", PreserveExistingP2P: &preserve,
	}
	plan, err := application.PreviewRename(request)
	if err != nil {
		t.Fatalf("PreviewRename: %v", err)
	}
	if len(plan.Errors) != 0 || !plan.CanApply {
		t.Fatalf("single-release Extras plan must be applicable: %#v", plan)
	}
	release := seasonLayoutItemBySource(t, plan, root, "folder")
	extra := extrasItemBySource(t, plan, extraSource)
	wantExtra := filepath.Join(parent, "Extras", filepath.Base(release.NewPath), "Artwork", "poster.jpg")
	if filepath.Clean(extra.NewPath) != filepath.Clean(wantExtra) {
		t.Fatalf("single-release extra destination = %q, want %q", extra.NewPath, wantExtra)
	}
	if extrasPathWithin(extra.NewPath, release.NewPath) {
		t.Fatalf("single-release extra is inside the torrent root: release=%#v extra=%#v", release, extra)
	}
	if !extrasPathWithin(extra.NewPath, parent) {
		t.Fatalf("single-release extra escaped the selected release parent: %#v", extra)
	}
	wantTail := filepath.Join("Artwork", "poster.jpg")
	if !strings.HasSuffix(strings.ToLower(filepath.Clean(extra.NewPath)), strings.ToLower(wantTail)) {
		t.Fatalf("single-release relative source path was not preserved: %#v", extra)
	}

	if err := application.ApplyRename(plan); err != nil {
		t.Fatalf("ApplyRename: %v", err)
	}
	extrasAssertFile(t, extra.NewPath, "poster")
	if _, err := os.Stat(filepath.Join(release.NewPath, "Artwork", "poster.jpg")); !os.IsNotExist(err) {
		t.Fatalf("poster remains in torrent root after Apply: err=%v", err)
	}

	secondApplication := NewApp()
	secondApplication.settings.MediaInfoBin = filepath.Join(parent, "missing-mediainfo.exe")
	request.RootPath = release.NewPath
	secondPlan, err := secondApplication.PreviewRename(request)
	if err != nil {
		t.Fatalf("second PreviewRename: %v", err)
	}
	if secondPlan.ChangeCount != 0 || secondPlan.CanApply {
		t.Fatalf("renamed single release must be idempotent: %#v", secondPlan)
	}

	if err := application.UndoRename(); err != nil {
		t.Fatalf("UndoRename: %v", err)
	}
	extrasAssertFile(t, video, "video")
	extrasAssertFile(t, extraSource, "poster")
	if _, err := os.Stat(filepath.Join(parent, "Extras")); !os.IsNotExist(err) {
		t.Fatalf("transaction-created single-release Extras tree remains after Undo: %v", err)
	}
}

func TestSingleReleaseExistingExtraDestinationIsShownAsConflict(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "incoming")
	video := filepath.Join(root, "Example.Movie.2026.1080p.WEB-DL.H.264-TestGroup.mkv")
	extraSource := filepath.Join(root, "poster.jpg")
	extrasWriteFile(t, video, "video")
	extrasWriteFile(t, extraSource, "incoming-poster")

	application := NewApp()
	application.settings.MediaInfoBin = filepath.Join(parent, "missing-mediainfo.exe")
	preserve := true
	request := RenameRequest{
		RootPath: root,
		Metadata: TechnicalMetadata{
			MediaType: "movie", Title: "Example Movie", Year: "2026", Resolution: "1080p",
			ReleaseType: "WEB-DL", VideoCodec: "H.264", Group: "NoGroup",
		},
		Separator: ".", PreserveExistingP2P: &preserve,
	}
	first, err := application.PreviewRename(request)
	if err != nil {
		t.Fatalf("first PreviewRename: %v", err)
	}
	destination := extrasItemBySource(t, first, extraSource).NewPath
	extrasWriteFile(t, destination, "existing-poster")

	second, err := application.PreviewRename(request)
	if err != nil {
		t.Fatalf("second PreviewRename: %v", err)
	}
	item := extrasItemBySource(t, second, extraSource)
	if second.CanApply || item.Status != "conflict" || !seasonLayoutContainsError(second, "destination_exists") {
		t.Fatalf("existing sibling Extras destination must be a visible conflict: plan=%#v item=%#v", second, item)
	}
	if err := application.ApplyRename(second); err == nil {
		t.Fatal("ApplyRename succeeded despite an existing sibling Extras destination")
	}
	extrasAssertFile(t, extraSource, "incoming-poster")
	extrasAssertFile(t, destination, "existing-poster")
}

func TestPreviewRejectsExtrasSymlinkInsteadOfMovingOutsideRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "Gotham (2014)")
	outside := filepath.Join(parent, "outside")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, contents := range map[string]string{
		"Gotham.S01E01.1080p.WEB-DL.H.264-TestGroup.mkv": "one",
		"Gotham.S02E01.1080p.WEB-DL.H.264-TestGroup.mkv": "two",
		"poster.jpg": "poster",
	} {
		extrasWriteFile(t, filepath.Join(root, name), contents)
	}
	if err := os.Symlink(outside, filepath.Join(root, "Extras")); err != nil {
		t.Skipf("directory symlink creation is unavailable: %v", err)
	}

	application := NewApp()
	application.settings.MediaInfoBin = filepath.Join(parent, "missing-mediainfo.exe")
	preserve := true
	plan, err := application.PreviewRename(RenameRequest{
		RootPath: root, Metadata: seasonLayoutMetadata(), Separator: ".", PreserveExistingP2P: &preserve,
	})
	if err != nil {
		t.Fatalf("PreviewRename: %v", err)
	}
	if plan.CanApply || !seasonLayoutContainsError(plan, "not a real directory") {
		t.Fatalf("an Extras symlink must block the whole plan: %#v", plan)
	}
	if err := application.ApplyRename(plan); err == nil {
		t.Fatal("ApplyRename succeeded through an Extras symlink")
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("files escaped through Extras symlink: %#v", entries)
	}
	extrasAssertFile(t, filepath.Join(root, "poster.jpg"), "poster")
}

func TestPreviewRejectsFileOccupyingExtrasDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Gotham (2014)")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, contents := range map[string]string{
		"Gotham.S01E01.1080p.WEB-DL.H.264-TestGroup.mkv": "one",
		"Gotham.S02E01.1080p.WEB-DL.H.264-TestGroup.mkv": "two",
		"poster.jpg": "poster",
		"Extras":     "not-a-directory",
	} {
		extrasWriteFile(t, filepath.Join(root, name), contents)
	}

	application := NewApp()
	application.settings.MediaInfoBin = filepath.Join(root, "missing-mediainfo.exe")
	preserve := true
	plan, err := application.PreviewRename(RenameRequest{
		RootPath: root, Metadata: seasonLayoutMetadata(), Separator: ".", PreserveExistingP2P: &preserve,
	})
	if err != nil {
		t.Fatalf("PreviewRename: %v", err)
	}
	if plan.CanApply || !seasonLayoutContainsError(plan, "not a real directory") {
		t.Fatalf("a file occupying Extras must block the whole plan: %#v", plan)
	}
	if err := application.ApplyRename(plan); err == nil {
		t.Fatal("ApplyRename succeeded with a file occupying Extras")
	}
	extrasAssertFile(t, filepath.Join(root, "poster.jpg"), "poster")
	extrasAssertFile(t, filepath.Join(root, "Extras"), "not-a-directory")
}

func TestApplyRejectsFilesAddedAfterPreviewGuard(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "incoming")
	video := filepath.Join(root, "Example.Movie.2026.1080p.WEB-DL.H.264-TestGroup.mkv")
	extrasWriteFile(t, video, "video")

	application := NewApp()
	application.settings.MediaInfoBin = filepath.Join(parent, "missing-mediainfo.exe")
	preserve := true
	plan, err := application.PreviewRename(RenameRequest{
		RootPath: root,
		Metadata: TechnicalMetadata{
			MediaType: "movie", Title: "Example Movie", Year: "2026", Resolution: "1080p",
			ReleaseType: "WEB-DL", VideoCodec: "H.264", Group: "NoGroup",
		},
		Separator: ".", PreserveExistingP2P: &preserve,
	})
	if err != nil || !plan.CanApply {
		t.Fatalf("initial plan must be applicable: plan=%#v err=%v", plan, err)
	}
	poster := filepath.Join(root, "poster.jpg")
	extrasWriteFile(t, poster, "added-after-preview")
	if err := application.ApplyRename(plan); err == nil || !strings.Contains(strings.ToLower(err.Error()), "changed since preview") {
		t.Fatalf("Apply must reject a changed tree, got %v", err)
	}
	extrasAssertFile(t, video, "video")
	extrasAssertFile(t, poster, "added-after-preview")
}

func extrasWriteFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func extrasItemBySource(t *testing.T, plan RenamePlan, source string) RenameItem {
	t.Helper()
	for _, item := range plan.Items {
		if item.Kind == "file" && filepath.Clean(item.OldPath) == filepath.Clean(source) {
			return item
		}
	}
	t.Fatalf("plan has no extra-file item for %q: %#v", source, plan.Items)
	return RenameItem{}
}

func extrasHasMoveFrom(plan RenamePlan, source string) bool {
	for _, item := range plan.Items {
		if filepath.Clean(item.OldPath) == filepath.Clean(source) && filepath.Clean(item.NewPath) != filepath.Clean(source) {
			return true
		}
	}
	return false
}

func extrasPathWithin(path, root string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return false
	}
	return relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func extrasAssertFile(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	if string(data) != want {
		t.Fatalf("contents of %q = %q, want %q", path, data, want)
	}
}
