package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bioidaika/vmf-preupload/internal/naming"
	"github.com/bioidaika/vmf-preupload/pkg/api"
)

func TestPreviewApplyUndoNestedMultiSeasonLayout(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "Gotham (2014)")
	seasonOne := filepath.Join(root, "Season 1")
	seasonTwo := filepath.Join(root, "Season 2")
	if err := os.MkdirAll(seasonOne, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(seasonTwo, 0o700); err != nil {
		t.Fatal(err)
	}

	episodeOne := filepath.Join(seasonOne, "Gotham.S01E01.mkv")
	episodeTwo := filepath.Join(seasonTwo, "Gotham.S02E03.mkv")
	if err := os.WriteFile(episodeOne, []byte("season-one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(episodeTwo, []byte("season-two"), 0o600); err != nil {
		t.Fatal(err)
	}

	application := NewApp()
	application.settings.MediaInfoBin = filepath.Join(parent, "missing-mediainfo.exe")
	plan, err := application.PreviewRename(RenameRequest{
		RootPath:            root,
		Metadata:            seasonLayoutMetadata(),
		Separator:           ".",
		PreserveExistingP2P: testBoolPointer(false),
	})
	if err != nil {
		t.Fatalf("PreviewRename: %v", err)
	}
	if len(plan.Errors) != 0 || !plan.CanApply {
		t.Fatalf("nested multi-season plan must be applicable: %#v", plan)
	}

	rootItem := seasonLayoutItemBySource(t, plan, root, "folder")
	if seasonLayoutHasToken(filepath.Base(rootItem.NewPath), "S01") || seasonLayoutHasToken(filepath.Base(rootItem.NewPath), "S02") {
		t.Errorf("series-level root must not be named as one season: %#v", rootItem)
	}

	seasonOneItem := seasonLayoutItemBySource(t, plan, seasonOne, "folder")
	seasonTwoItem := seasonLayoutItemBySource(t, plan, seasonTwo, "folder")
	if !seasonLayoutHasToken(filepath.Base(seasonOneItem.NewPath), "S01") {
		t.Fatalf("Season 1 folder destination has no S01 identity: %#v", seasonOneItem)
	}
	if !seasonLayoutHasToken(filepath.Base(seasonTwoItem.NewPath), "S02") {
		t.Fatalf("Season 2 folder destination has no S02 identity: %#v", seasonTwoItem)
	}
	for _, item := range []RenameItem{seasonOneItem, seasonTwoItem} {
		if filepath.Clean(filepath.Dir(item.NewPath)) != filepath.Clean(rootItem.NewPath) {
			t.Fatalf("season folder escaped the renamed series root: root=%#v season=%#v", rootItem, item)
		}
	}

	episodeOneItem := seasonLayoutItemBySource(t, plan, episodeOne, "file")
	episodeTwoItem := seasonLayoutItemBySource(t, plan, episodeTwo, "file")
	if filepath.Clean(filepath.Dir(episodeOneItem.NewPath)) != filepath.Clean(seasonOneItem.NewPath) ||
		!seasonLayoutHasToken(filepath.Base(episodeOneItem.NewPath), "S01E01") {
		t.Fatalf("S01 episode must remain below the renamed S01 folder: folder=%#v file=%#v", seasonOneItem, episodeOneItem)
	}
	if filepath.Clean(filepath.Dir(episodeTwoItem.NewPath)) != filepath.Clean(seasonTwoItem.NewPath) ||
		!seasonLayoutHasToken(filepath.Base(episodeTwoItem.NewPath), "S02E03") {
		t.Fatalf("S02 episode must remain below the renamed S02 folder: folder=%#v file=%#v", seasonTwoItem, episodeTwoItem)
	}

	if err := application.ApplyRename(plan); err != nil {
		t.Fatalf("ApplyRename: %v", err)
	}
	seasonLayoutAssertFile(t, episodeOneItem.NewPath, "season-one")
	seasonLayoutAssertFile(t, episodeTwoItem.NewPath, "season-two")
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("series container should remain after apply: %v", err)
	}

	if err := application.UndoRename(); err != nil {
		t.Fatalf("UndoRename: %v", err)
	}
	seasonLayoutAssertFile(t, episodeOne, "season-one")
	seasonLayoutAssertFile(t, episodeTwo, "season-two")
	if filepath.Clean(rootItem.NewPath) != filepath.Clean(root) {
		t.Fatalf("series root moved unexpectedly: %#v", rootItem)
	}
}

func TestPreviewApplyUndoFlatMultiSeasonLayout(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "Gotham (2014)")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	episodeOne := filepath.Join(root, "Gotham.S01E01.mkv")
	episodeTwo := filepath.Join(root, "Gotham.S02E03.mkv")
	if err := os.WriteFile(episodeOne, []byte("season-one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(episodeTwo, []byte("season-two"), 0o600); err != nil {
		t.Fatal(err)
	}

	application := NewApp()
	application.settings.MediaInfoBin = filepath.Join(parent, "missing-mediainfo.exe")
	plan, err := application.PreviewRename(RenameRequest{
		RootPath:            root,
		Metadata:            seasonLayoutMetadata(),
		Separator:           ".",
		PreserveExistingP2P: testBoolPointer(false),
	})
	if err != nil {
		t.Fatalf("PreviewRename: %v", err)
	}
	if len(plan.Errors) != 0 || !plan.CanApply {
		t.Fatalf("flat multi-season plan must be applicable: %#v", plan)
	}

	rootItem := seasonLayoutItemBySource(t, plan, root, "folder")
	if seasonLayoutHasToken(filepath.Base(rootItem.NewPath), "S01") || seasonLayoutHasToken(filepath.Base(rootItem.NewPath), "S02") {
		t.Errorf("flat multi-season root must be series-level, not a season pack: %#v", rootItem)
	}
	episodeOneItem := seasonLayoutItemBySource(t, plan, episodeOne, "file")
	episodeTwoItem := seasonLayoutItemBySource(t, plan, episodeTwo, "file")
	if filepath.Clean(filepath.Dir(episodeOneItem.NewPath)) != filepath.Clean(rootItem.NewPath) ||
		!seasonLayoutHasToken(filepath.Base(episodeOneItem.NewPath), "S01E01") {
		t.Fatalf("flat S01 episode identity or location changed: root=%#v file=%#v", rootItem, episodeOneItem)
	}
	if filepath.Clean(filepath.Dir(episodeTwoItem.NewPath)) != filepath.Clean(rootItem.NewPath) ||
		!seasonLayoutHasToken(filepath.Base(episodeTwoItem.NewPath), "S02E03") {
		t.Fatalf("flat S02 episode identity or location changed: root=%#v file=%#v", rootItem, episodeTwoItem)
	}

	if err := application.ApplyRename(plan); err != nil {
		t.Fatalf("ApplyRename: %v", err)
	}
	seasonLayoutAssertFile(t, episodeOneItem.NewPath, "season-one")
	seasonLayoutAssertFile(t, episodeTwoItem.NewPath, "season-two")
	if err := application.UndoRename(); err != nil {
		t.Fatalf("UndoRename: %v", err)
	}
	seasonLayoutAssertFile(t, episodeOne, "season-one")
	seasonLayoutAssertFile(t, episodeTwo, "season-two")
}

func TestPreviewMixedSeasonFoldersAndDirectEpisodes(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "Gotham (2014)")
	seasonOne := filepath.Join(root, "Season 1")
	if err := os.MkdirAll(seasonOne, 0o700); err != nil {
		t.Fatal(err)
	}
	episodeOne := filepath.Join(seasonOne, "Gotham.S01E01.mkv")
	episodeTwo := filepath.Join(root, "Gotham.S02E03.mkv")
	for path, contents := range map[string]string{episodeOne: "season-one", episodeTwo: "season-two"} {
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	application := NewApp()
	application.settings.MediaInfoBin = filepath.Join(parent, "missing-mediainfo.exe")
	plan, err := application.PreviewRename(RenameRequest{
		RootPath: root, Metadata: seasonLayoutMetadata(), Separator: ".", PreserveExistingP2P: testBoolPointer(false),
	})
	if err != nil {
		t.Fatalf("PreviewRename: %v", err)
	}
	if len(plan.Errors) != 0 || !plan.CanApply {
		t.Fatalf("mixed nested/flat plan must be applicable: %#v", plan)
	}
	if !seasonLayoutContainsWarning(plan, "mixed season layout") {
		t.Fatalf("mixed nested/flat plan must explain direct episodes: %#v", plan.Warnings)
	}
	seasonOneFolder := seasonLayoutItemBySource(t, plan, seasonOne, "folder")
	seasonOneFile := seasonLayoutItemBySource(t, plan, episodeOne, "file")
	seasonTwoFile := seasonLayoutItemBySource(t, plan, episodeTwo, "file")
	if filepath.Clean(filepath.Dir(seasonOneFile.NewPath)) != filepath.Clean(seasonOneFolder.NewPath) {
		t.Fatalf("nested episode escaped its renamed season folder: folder=%#v file=%#v", seasonOneFolder, seasonOneFile)
	}
	if filepath.Clean(filepath.Dir(seasonTwoFile.NewPath)) != filepath.Clean(root) ||
		!seasonLayoutHasToken(filepath.Base(seasonTwoFile.NewPath), "S02E03") {
		t.Fatalf("direct S02 episode must remain in the series root: %#v", seasonTwoFile)
	}
}

func TestPreviewFlatSingleSeasonRootRemainsSeasonPack(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "Gotham (2014)")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Gotham.S01E01.mkv", "Gotham.S01E02.mkv"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	application := NewApp()
	application.settings.MediaInfoBin = filepath.Join(parent, "missing-mediainfo.exe")
	plan, err := application.PreviewRename(RenameRequest{
		RootPath:            root,
		Metadata:            seasonLayoutMetadata(),
		Separator:           ".",
		PreserveExistingP2P: testBoolPointer(false),
	})
	if err != nil {
		t.Fatalf("PreviewRename: %v", err)
	}
	if len(plan.Errors) != 0 || !plan.CanApply {
		t.Fatalf("single-season pack plan must be applicable: %#v", plan)
	}

	rootItem := seasonLayoutItemBySource(t, plan, root, "folder")
	if !seasonLayoutHasToken(filepath.Base(rootItem.NewPath), "S01") {
		t.Fatalf("flat single-season root must remain an S01 pack: %#v", rootItem)
	}
	for _, episode := range []string{"S01E01", "S01E02"} {
		found := false
		for _, item := range plan.Items {
			if item.Kind == "file" && seasonLayoutHasToken(filepath.Base(item.NewPath), episode) {
				found = true
				if filepath.Clean(filepath.Dir(item.NewPath)) != filepath.Clean(rootItem.NewPath) {
					t.Fatalf("flat %s episode escaped its S01 pack: root=%#v file=%#v", episode, rootItem, item)
				}
			}
		}
		if !found {
			t.Fatalf("single-season plan has no %s destination: %#v", episode, plan.Items)
		}
	}
}

func TestScanReportsSeriesTopologyAcrossAllSeasonFolders(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Gotham (2014)")
	for _, relative := range []string{
		filepath.Join("Season 1", "Gotham.S01E01.mkv"),
		filepath.Join("Season 2", "Gotham.S02E01.mkv"),
	} {
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(relative), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	application := NewApp()
	application.settings.MediaInfoBin = filepath.Join(root, "missing-mediainfo.exe")
	result, err := application.ScanPath(root)
	if err != nil {
		t.Fatalf("ScanPath: %v", err)
	}
	if !result.SeriesRoot || result.SeasonFolderCount != 2 || strings.Join(result.Seasons, ",") != "01,02" {
		t.Fatalf("multi-season topology did not cross the app DTO: %#v", result)
	}
}

func TestScanInfersIdentityInsideExplicitSeasonDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Example Show (2024)")
	path := filepath.Join(root, "Season 2", "Tập 3.mkv")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("episode-three"), 0o600); err != nil {
		t.Fatal(err)
	}
	application := NewApp()
	application.settings.MediaInfoBin = filepath.Join(root, "missing-mediainfo.exe")
	result, err := application.ScanPath(root)
	if err != nil {
		t.Fatalf("ScanPath: %v", err)
	}
	if result.MediaType != "tv" || result.Metadata.Season != "02" || result.Metadata.Episode != "3" {
		t.Fatalf("season-directory identity inference failed: %#v", result)
	}
}

func TestPreviewNestedMultiSeasonPreservesP2PBasenamesAndMapsParents(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "Gotham (2014)")
	seasonOne := filepath.Join(root, "Season 1")
	seasonTwo := filepath.Join(root, "Season 2")
	if err := os.MkdirAll(seasonOne, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(seasonTwo, 0o700); err != nil {
		t.Fatal(err)
	}

	seasonOneName := "Gotham.S01E01.Pilot.1080p.DTS-HD.MA.5.1.AVC.REMUX-FraMeSToR.mkv"
	seasonTwoName := "Gotham.S02E01.Damned.If.You.Do.1080p.DTS-HD.MA.5.1.AVC.REMUX-FraMeSToR.mkv"
	seasonOnePath := filepath.Join(seasonOne, seasonOneName)
	seasonTwoPath := filepath.Join(seasonTwo, seasonTwoName)
	if err := os.WriteFile(seasonOnePath, []byte("season-one-p2p"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(seasonTwoPath, []byte("season-two-p2p"), 0o600); err != nil {
		t.Fatal(err)
	}

	application := NewApp()
	application.settings.MediaInfoBin = filepath.Join(parent, "missing-mediainfo.exe")
	plan, err := application.PreviewRename(RenameRequest{
		RootPath:            root,
		Metadata:            seasonLayoutMetadata(),
		Separator:           ".",
		PreserveExistingP2P: testBoolPointer(true),
	})
	if err != nil {
		t.Fatalf("PreviewRename: %v", err)
	}
	if len(plan.Errors) != 0 || !plan.CanApply {
		t.Fatalf("nested P2P plan must be applicable: %#v", plan)
	}

	seasonOneFolder := seasonLayoutItemBySource(t, plan, seasonOne, "folder")
	seasonTwoFolder := seasonLayoutItemBySource(t, plan, seasonTwo, "folder")
	seasonOneFile := seasonLayoutItemBySource(t, plan, seasonOnePath, "file")
	seasonTwoFile := seasonLayoutItemBySource(t, plan, seasonTwoPath, "file")
	for _, item := range []RenameItem{seasonOneFile, seasonTwoFile} {
		if item.Status != "preserved" {
			t.Fatalf("recognized P2P episode was not marked preserved: %#v", item)
		}
	}
	if filepath.Base(seasonOneFile.NewPath) != seasonOneName || filepath.Clean(filepath.Dir(seasonOneFile.NewPath)) != filepath.Clean(seasonOneFolder.NewPath) {
		t.Fatalf("S01 P2P basename or mapped parent changed: folder=%#v file=%#v", seasonOneFolder, seasonOneFile)
	}
	if filepath.Base(seasonTwoFile.NewPath) != seasonTwoName || filepath.Clean(filepath.Dir(seasonTwoFile.NewPath)) != filepath.Clean(seasonTwoFolder.NewPath) {
		t.Fatalf("S02 P2P basename or mapped parent changed: folder=%#v file=%#v", seasonTwoFolder, seasonTwoFile)
	}

	if err := application.ApplyRename(plan); err != nil {
		t.Fatalf("ApplyRename: %v", err)
	}
	seasonLayoutAssertFile(t, seasonOneFile.NewPath, "season-one-p2p")
	seasonLayoutAssertFile(t, seasonTwoFile.NewPath, "season-two-p2p")
	if err := application.UndoRename(); err != nil {
		t.Fatalf("UndoRename: %v", err)
	}
	seasonLayoutAssertFile(t, seasonOnePath, "season-one-p2p")
	seasonLayoutAssertFile(t, seasonTwoPath, "season-two-p2p")
}

func TestPreviewBlocksSeasonFolderEpisodeMismatch(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "Gotham (2014)")
	seasonOne := filepath.Join(root, "Season 1")
	if err := os.MkdirAll(seasonOne, 0o700); err != nil {
		t.Fatal(err)
	}
	mismatchedEpisode := filepath.Join(seasonOne, "Gotham.S02E01.mkv")
	if err := os.WriteFile(mismatchedEpisode, []byte("mismatch"), 0o600); err != nil {
		t.Fatal(err)
	}

	application := NewApp()
	application.settings.MediaInfoBin = filepath.Join(parent, "missing-mediainfo.exe")
	plan, err := application.PreviewRename(RenameRequest{
		RootPath:            root,
		Metadata:            seasonLayoutMetadata(),
		Separator:           ".",
		PreserveExistingP2P: testBoolPointer(false),
	})
	if err != nil {
		t.Fatalf("PreviewRename: %v", err)
	}
	if plan.CanApply || !seasonLayoutContainsError(plan, "season folder") {
		t.Fatalf("Season 1 containing S02 must block Apply with a visible layout error: %#v", plan)
	}
	if err := application.ApplyRename(plan); err == nil || !strings.Contains(strings.ToLower(err.Error()), "unresolved") {
		t.Fatalf("backend accepted a season mismatch plan: %v", err)
	}
	if application.journal != nil {
		t.Fatalf("rejected mismatch plan created a journal: %#v", application.journal)
	}
	seasonLayoutAssertFile(t, mismatchedEpisode, "mismatch")
}

func TestPreviewBlocksEquivalentSeasonDirectoryCollision(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "Gotham (2014)")
	seasonWord := filepath.Join(root, "Season 1")
	seasonShort := filepath.Join(root, "S01")
	if err := os.MkdirAll(seasonWord, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(seasonShort, 0o700); err != nil {
		t.Fatal(err)
	}
	wordEpisode := filepath.Join(seasonWord, "Gotham.S01E01.mkv")
	shortEpisode := filepath.Join(seasonShort, "Gotham.S01E02.mkv")
	if err := os.WriteFile(wordEpisode, []byte("word-season"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shortEpisode, []byte("short-season"), 0o600); err != nil {
		t.Fatal(err)
	}

	application := NewApp()
	application.settings.MediaInfoBin = filepath.Join(parent, "missing-mediainfo.exe")
	plan, err := application.PreviewRename(RenameRequest{
		RootPath:            root,
		Metadata:            seasonLayoutMetadata(),
		Separator:           ".",
		PreserveExistingP2P: testBoolPointer(false),
	})
	if err != nil {
		t.Fatalf("PreviewRename: %v", err)
	}
	if plan.CanApply || !seasonLayoutContainsError(plan, "duplicate season destination") {
		t.Fatalf("equivalent Season 1/S01 destinations must block Apply: %#v", plan)
	}
	wordFolder := seasonLayoutItemBySource(t, plan, seasonWord, "folder")
	shortFolder := seasonLayoutItemBySource(t, plan, seasonShort, "folder")
	if filepath.Clean(wordFolder.NewPath) != filepath.Clean(shortFolder.NewPath) {
		t.Fatalf("collision regression did not produce the same canonical destination: word=%#v short=%#v", wordFolder, shortFolder)
	}
	if wordFolder.Status != "conflict" || shortFolder.Status != "conflict" {
		t.Fatalf("every duplicate season destination must be visible as a conflict: word=%#v short=%#v", wordFolder, shortFolder)
	}
	if err := application.ApplyRename(plan); err == nil || !strings.Contains(strings.ToLower(err.Error()), "unresolved") {
		t.Fatalf("backend accepted duplicate season destinations: %v", err)
	}
	seasonLayoutAssertFile(t, wordEpisode, "word-season")
	seasonLayoutAssertFile(t, shortEpisode, "short-season")
}

func TestPreviewBlocksUnidentifiedEpisodeInFlatMultiSeasonLayout(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Example Show (2024)")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Show.S01E01.mkv", "Show.S02E01.mkv", "unknown.mkv"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	application := NewApp()
	application.settings.MediaInfoBin = filepath.Join(root, "missing-mediainfo.exe")
	plan, err := application.PreviewRename(RenameRequest{
		RootPath: root, Metadata: seasonLayoutMetadata(), Separator: ".", PreserveExistingP2P: testBoolPointer(false),
	})
	if err != nil {
		t.Fatalf("PreviewRename: %v", err)
	}
	if plan.CanApply || !seasonLayoutContainsError(plan, "could not determine a season") {
		t.Fatalf("unidentified flat episode must block a multi-season plan: %#v", plan)
	}
}

func TestPreviewBlocksMissingEpisodeAcrossSeasonFolders(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Example Show (2024)")
	for _, season := range []string{"Season 1", "Season 2"} {
		path := filepath.Join(root, season, "video.mkv")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(season), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	application := NewApp()
	application.settings.MediaInfoBin = filepath.Join(root, "missing-mediainfo.exe")
	plan, err := application.PreviewRename(RenameRequest{
		RootPath: root, Metadata: seasonLayoutMetadata(), Separator: ".", PreserveExistingP2P: testBoolPointer(false),
	})
	if err != nil {
		t.Fatalf("PreviewRename: %v", err)
	}
	if plan.CanApply || !seasonLayoutContainsError(plan, "could not determine an episode") {
		t.Fatalf("missing episode identity must block a multi-file TV plan: %#v", plan)
	}
}

func TestScanKeepsSeasonAndEpisodeFromTheSameAsset(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Example Show (2024)")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Show.S01.mkv", "Show.S02E03.mkv"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	application := NewApp()
	application.settings.MediaInfoBin = filepath.Join(root, "missing-mediainfo.exe")
	result, err := application.ScanPath(root)
	if err != nil {
		t.Fatalf("ScanPath: %v", err)
	}
	if result.Metadata.Season != "02" || result.Metadata.Episode != "03" {
		t.Fatalf("scan combined identity from different files: %#v", result.Metadata)
	}
}

func TestRecognizesStructuredNoGroupSeasonDirectory(t *testing.T) {
	name := "Example.Show.2024.S01.1080p.WEB-DL.H.264-NoGroup"
	if season, ok := seasonFromDirectoryName(name); !ok || season != "01" {
		t.Fatalf("seasonFromDirectoryName(%q)=(%q,%t)", name, season, ok)
	}
}

func TestMultiFileTechnicalMetadataDoesNotLeakFromFirstEpisode(t *testing.T) {
	base := naming.Metadata{Service: "NF", Source: "WEB", ReleaseType: naming.WebDL, Resolution: "2160p", VideoCodec: "H.265", VideoEncode: "x265", HDR: "HDR"}
	asset := api.Asset{
		Name: "Show.S02E01.1080p.AMZN.WEB-DL.H.264.mkv",
		Content: api.ContentInfo{
			Service: "AMZN", Resolution: "1080p", ReleaseType: "WEBDL", VideoCodec: "H.264",
		},
		Technical: api.TechnicalInfo{RawJSON: `{}`},
	}
	got := preferAssetTechnicalMetadata(base, asset, nil)
	if got.Service != "AMZN" || got.Resolution != "1080p" || got.VideoCodec != "H.264" {
		t.Fatalf("asset technical facts did not override first-episode values: %#v", got)
	}
	if got.VideoEncode != "" || got.HDR != "" {
		t.Fatalf("stale encoder/HDR leaked from the first episode: %#v", got)
	}
	overridden := preferAssetTechnicalMetadata(base, asset, map[string]bool{"resolution": true, "hdr": true})
	if overridden.Resolution != "2160p" || overridden.HDR != "HDR" {
		t.Fatalf("explicit technical overrides were not honored: %#v", overridden)
	}

	untagged := api.Asset{Name: "Show.S02E02.mkv", Content: api.ContentInfo{ReleaseType: naming.Encode}}
	cleared := preferAssetTechnicalMetadata(base, untagged, nil)
	if cleared.Source != "" || cleared.ReleaseType != "" {
		t.Fatalf("source/release type leaked without filename evidence: %#v", cleared)
	}
	kept := preferAssetTechnicalMetadata(base, untagged, map[string]bool{"source": true, "releasetype": true})
	if kept.Source != "WEB" || kept.ReleaseType != naming.WebDL {
		t.Fatalf("explicit source/release-type overrides were not honored: %#v", kept)
	}
}

func seasonLayoutMetadata() TechnicalMetadata {
	// A folder scan exposes the first episode's season and episode in the UI.
	// PreviewRename must still classify a multi-season selection from all files.
	return TechnicalMetadata{
		MediaType:   "tv",
		Title:       "Gotham",
		Year:        "2014",
		Season:      "1",
		Episode:     "1",
		Resolution:  "1080p",
		ReleaseType: "WEB-DL",
		VideoCodec:  "H.264",
		Group:       "NoGroup",
	}
}

func seasonLayoutItemBySource(t *testing.T, plan RenamePlan, source, kind string) RenameItem {
	t.Helper()
	for _, item := range plan.Items {
		if filepath.Clean(item.OldPath) == filepath.Clean(source) && item.Kind == kind {
			return item
		}
	}
	t.Fatalf("plan has no %s item for %q: %#v", kind, source, plan.Items)
	return RenameItem{}
}

func seasonLayoutHasToken(name, token string) bool {
	normalized := strings.NewReplacer("-", ".", "_", ".", " ", ".").Replace(name)
	for _, component := range strings.Split(normalized, ".") {
		if strings.EqualFold(component, token) {
			return true
		}
	}
	return false
}

func seasonLayoutContainsError(plan RenamePlan, fragment string) bool {
	fragment = strings.ToLower(fragment)
	for _, message := range plan.Errors {
		if strings.Contains(strings.ToLower(message), fragment) {
			return true
		}
	}
	return false
}

func seasonLayoutContainsWarning(plan RenamePlan, fragment string) bool {
	fragment = strings.ToLower(fragment)
	for _, message := range plan.Warnings {
		if strings.Contains(strings.ToLower(message), fragment) {
			return true
		}
	}
	return false
}

func seasonLayoutAssertFile(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	if string(data) != want {
		t.Fatalf("contents of %q = %q, want %q", path, data, want)
	}
}
