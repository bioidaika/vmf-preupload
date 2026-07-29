package rename

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildPlanAndPreflightRejectWindowsNamesAndCollision(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "old.mkv")
	if err := os.WriteFile(old, []byte("video"), 0600); err != nil {
		t.Fatal(err)
	}
	requests := []RenameRequest{{Source: old, Destination: filepath.Join(root, "CON.mkv")}}
	_, err := BuildPlan(requests, PlanOptions{Root: root})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "reserved") {
		t.Fatalf("expected reserved-name error, got %v", err)
	}

	destination := filepath.Join(root, "target.mkv")
	if err := os.WriteFile(destination, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan([]RenameRequest{{Source: old, Destination: destination}}, PlanOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	report := Preflight(plan)
	if report.Valid() {
		t.Fatal("expected destination collision")
	}
	if !hasIssue(report, "destination_exists") {
		t.Fatalf("unexpected issues: %#v", report.Issues)
	}
}

func TestBuildPlanSelectedFolderMayRenameToSibling(t *testing.T) {
	parent := t.TempDir()
	oldDir := filepath.Join(parent, "Old")
	newDir := filepath.Join(parent, "New")
	if err := os.Mkdir(oldDir, 0700); err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan([]RenameRequest{{Source: oldDir, Destination: newDir}}, PlanOptions{Root: oldDir})
	if err != nil {
		t.Fatal(err)
	}
	if pathKey(plan.Root) != pathKey(parent) {
		t.Fatalf("expected parent safety root, got %s", plan.Root)
	}
}

func TestApplyNestedFolderAndFileThenUndo(t *testing.T) {
	root := t.TempDir()
	oldDir := filepath.Join(root, "Old.Release")
	oldFile := filepath.Join(oldDir, "episode.mkv")
	newDir := filepath.Join(root, "New.Release")
	newFile := filepath.Join(newDir, "episode-renamed.mkv")
	if err := os.MkdirAll(oldDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldFile, []byte("media"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan([]RenameRequest{
		{Source: oldDir, Destination: newDir},
		{Source: oldFile, Destination: newFile},
	}, PlanOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(root, "rename-journal.json")
	journal, err := Apply(context.Background(), plan, ApplyOptions{JournalPath: journalPath})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if journal.State != JournalApplied {
		t.Fatalf("state = %s", journal.State)
	}
	if _, err := os.Stat(newFile); err != nil {
		t.Fatalf("new file missing: %v", err)
	}
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatalf("old directory still exists, err=%v", err)
	}

	if err := Undo(context.Background(), journalPath); err != nil {
		t.Fatalf("undo: %v", err)
	}
	if _, err := os.Stat(oldFile); err != nil {
		t.Fatalf("old file missing after undo: %v", err)
	}
	if _, err := os.Stat(newDir); !os.IsNotExist(err) {
		t.Fatalf("new directory still exists after undo, err=%v", err)
	}
}

func TestApplyCaseOnlyRenameAndJournalRoundTrip(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "Movie.mkv")
	destination := filepath.Join(root, "movie.mkv")
	if err := os.WriteFile(source, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan([]RenameRequest{{Source: source, Destination: destination}}, PlanOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	report := Preflight(plan)
	if !report.Valid() {
		t.Fatalf("case-only preflight: %#v", report.Issues)
	}
	jPath := filepath.Join(root, "journal.json")
	j, err := Apply(context.Background(), plan, ApplyOptions{JournalPath: jPath})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(destination); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadJournal(jPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.State != JournalApplied || loaded.ID != j.ID {
		t.Fatalf("loaded journal mismatch: %#v", loaded)
	}
}

func TestApplyFailureRollsBackWithoutOverwritingLateCollision(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.mkv")
	destination := filepath.Join(root, "destination.mkv")
	if err := os.WriteFile(source, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan([]RenameRequest{{Source: source, Destination: destination}}, PlanOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	journal, err := Apply(context.Background(), plan, ApplyOptions{
		JournalPath: filepath.Join(root, "failed-journal.json"),
		OnProgress: func(progress Progress) {
			if progress.Phase == "stage" {
				_ = os.WriteFile(destination, []byte("late collision"), 0600)
			}
		},
	})
	if err == nil {
		t.Fatal("expected late collision failure")
	}
	if journal == nil || journal.State != JournalRolledBack {
		t.Fatalf("expected automatic rollback journal, got %#v", journal)
	}
	data, readErr := os.ReadFile(source)
	if readErr != nil || string(data) != "original" {
		t.Fatalf("source was not restored: data=%q err=%v", data, readErr)
	}
	collision, readErr := os.ReadFile(destination)
	if readErr != nil || string(collision) != "late collision" {
		t.Fatalf("collision was overwritten: data=%q err=%v", collision, readErr)
	}
}

func TestApplySwapUsesTwoPhaseStaging(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "A.txt")
	b := filepath.Join(root, "B.txt")
	if err := os.WriteFile(a, []byte("A"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("B"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan([]RenameRequest{{Source: a, Destination: b}, {Source: b, Destination: a}}, PlanOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(context.Background(), plan, ApplyOptions{JournalPath: filepath.Join(root, "swap.json")}); err != nil {
		t.Fatal(err)
	}
	gotA, _ := os.ReadFile(a)
	gotB, _ := os.ReadFile(b)
	if string(gotA) != "B" || string(gotB) != "A" {
		t.Fatalf("swap failed: A=%q B=%q", gotA, gotB)
	}
}

func TestApplyCreatesDestinationDirectoriesAndUndoRemovesThem(t *testing.T) {
	root := t.TempDir()
	sourceOne := filepath.Join(root, "episode-one.mkv")
	sourceTwo := filepath.Join(root, "episode-two.mkv")
	seasonOne := filepath.Join(root, "S01")
	seasonTwo := filepath.Join(root, "S02")
	destinationOne := filepath.Join(seasonOne, "Show.S01E01.mkv")
	destinationTwo := filepath.Join(seasonTwo, "Show.S02E01.mkv")
	if err := os.WriteFile(sourceOne, []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourceTwo, []byte("two"), 0600); err != nil {
		t.Fatal(err)
	}

	plan, err := BuildPlan([]RenameRequest{
		{Source: sourceOne, Destination: destinationOne},
		{Source: sourceTwo, Destination: destinationTwo},
	}, PlanOptions{Root: root, CreateDirectories: []string{seasonOne, seasonTwo}})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Directories) != 2 {
		t.Fatalf("directories = %#v", plan.Directories)
	}
	if report := Preflight(plan); !report.Valid() {
		t.Fatalf("preflight: %#v", report.Issues)
	}

	journalPath := filepath.Join(root, "create-directories.json")
	journal, err := Apply(context.Background(), plan, ApplyOptions{JournalPath: journalPath})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(journal.Directories) != 2 || !journal.Directories[0].Created || !journal.Directories[1].Created {
		t.Fatalf("created directories were not journaled: %#v", journal.Directories)
	}
	if data, err := os.ReadFile(destinationOne); err != nil || string(data) != "one" {
		t.Fatalf("S01 destination data=%q err=%v", data, err)
	}
	if data, err := os.ReadFile(destinationTwo); err != nil || string(data) != "two" {
		t.Fatalf("S02 destination data=%q err=%v", data, err)
	}

	if err := Undo(context.Background(), journalPath); err != nil {
		t.Fatalf("undo: %v", err)
	}
	if data, err := os.ReadFile(sourceOne); err != nil || string(data) != "one" {
		t.Fatalf("restored S01 source data=%q err=%v", data, err)
	}
	if data, err := os.ReadFile(sourceTwo); err != nil || string(data) != "two" {
		t.Fatalf("restored S02 source data=%q err=%v", data, err)
	}
	for _, directory := range []string{seasonOne, seasonTwo} {
		if _, err := os.Stat(directory); !os.IsNotExist(err) {
			t.Fatalf("created directory remains after Undo: %s (err=%v)", directory, err)
		}
	}
	loaded, err := LoadJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Directories[0].Removed || !loaded.Directories[1].Removed {
		t.Fatalf("directory cleanup was not journaled: %#v", loaded.Directories)
	}
}

func TestPreflightRequiresExplicitMissingDestinationDirectory(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "episode.mkv")
	if err := os.WriteFile(source, []byte("video"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan([]RenameRequest{{
		Source:      source,
		Destination: filepath.Join(root, "S01", "Show.S01E01.mkv"),
	}}, PlanOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	report := Preflight(plan)
	if !hasIssue(report, "destination_parent") {
		t.Fatalf("missing destination directory must be explicit: %#v", report.Issues)
	}
}

func TestPreflightRequiresEveryCreatedDirectoryLevel(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "episode.mkv")
	season := filepath.Join(root, "S01")
	if err := os.WriteFile(source, []byte("video"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan([]RenameRequest{{
		Source:      source,
		Destination: filepath.Join(season, "Disc 1", "Show.S01E01.mkv"),
	}}, PlanOptions{Root: root, CreateDirectories: []string{season}})
	if err != nil {
		t.Fatal(err)
	}
	report := Preflight(plan)
	if !hasIssue(report, "destination_parent") {
		t.Fatalf("unplanned intermediate directory must fail preflight: %#v", report.Issues)
	}
}

func TestApplyLateDirectoryCollisionIsNotRemovedByRollback(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "episode.mkv")
	season := filepath.Join(root, "S01")
	destination := filepath.Join(season, "Show.S01E01.mkv")
	foreign := filepath.Join(season, "user.txt")
	if err := os.WriteFile(source, []byte("video"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan([]RenameRequest{{Source: source, Destination: destination}}, PlanOptions{
		Root: root, CreateDirectories: []string{season},
	})
	if err != nil {
		t.Fatal(err)
	}
	journal, err := Apply(context.Background(), plan, ApplyOptions{
		JournalPath: filepath.Join(root, "late-directory.json"),
		OnProgress: func(progress Progress) {
			if progress.Phase == "stage" {
				_ = os.Mkdir(season, 0700)
				_ = os.WriteFile(foreign, []byte("keep"), 0600)
			}
		},
	})
	if err == nil {
		t.Fatal("expected a late directory collision")
	}
	if journal == nil || journal.State != JournalRolledBack {
		t.Fatalf("journal = %#v", journal)
	}
	if data, readErr := os.ReadFile(source); readErr != nil || string(data) != "video" {
		t.Fatalf("source was not restored: data=%q err=%v", data, readErr)
	}
	if data, readErr := os.ReadFile(foreign); readErr != nil || string(data) != "keep" {
		t.Fatalf("foreign directory contents were removed: data=%q err=%v", data, readErr)
	}
	if journal.Directories[0].Created || journal.Directories[0].Removed {
		t.Fatalf("foreign directory was attributed to the app: %#v", journal.Directories[0])
	}
}

func TestUndoReportsNonEmptyCreatedDirectoryAndCanRetryCleanup(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "episode.mkv")
	season := filepath.Join(root, "S01")
	destination := filepath.Join(season, "Show.S01E01.mkv")
	foreign := filepath.Join(season, "user.txt")
	if err := os.WriteFile(source, []byte("video"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan([]RenameRequest{{Source: source, Destination: destination}}, PlanOptions{
		Root: root, CreateDirectories: []string{season},
	})
	if err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(root, "retain-directory.json")
	if _, err := Apply(context.Background(), plan, ApplyOptions{JournalPath: journalPath}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(foreign, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Undo(context.Background(), journalPath); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("undo must report retained non-empty directory, got %v", err)
	}
	if _, err := os.Stat(season); err != nil {
		t.Fatalf("non-empty created directory was not retained: %v", err)
	}
	if data, err := os.ReadFile(foreign); err != nil || string(data) != "keep" {
		t.Fatalf("foreign file data=%q err=%v", data, err)
	}
	failed, err := LoadJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if failed.State != JournalFailed || failed.Directories[0].Removed {
		t.Fatalf("non-empty cleanup must remain retryable: %#v", failed)
	}
	if err := os.Remove(foreign); err != nil {
		t.Fatal(err)
	}
	if err := Undo(context.Background(), journalPath); err != nil {
		t.Fatalf("retry cleanup: %v", err)
	}
	if _, err := os.Stat(season); !os.IsNotExist(err) {
		t.Fatalf("empty created directory remains after retry, err=%v", err)
	}
}

func TestApplyCreatesNestedDirectoriesParentFirst(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "episode.mkv")
	series := filepath.Join(root, "Series")
	season := filepath.Join(series, "S01")
	destination := filepath.Join(season, "Show.S01E01.mkv")
	if err := os.WriteFile(source, []byte("video"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan([]RenameRequest{{Source: source, Destination: destination}}, PlanOptions{
		Root: root, CreateDirectories: []string{season, series},
	})
	if err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(root, "nested-directories.json")
	if _, err := Apply(context.Background(), plan, ApplyOptions{JournalPath: journalPath}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if _, err := os.Stat(destination); err != nil {
		t.Fatal(err)
	}
	if err := Undo(context.Background(), journalPath); err != nil {
		t.Fatalf("undo: %v", err)
	}
	if _, err := os.Stat(series); !os.IsNotExist(err) {
		t.Fatalf("nested created directory tree remains, err=%v", err)
	}
}

func TestScanDoesNotFollowSymlinks(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "video.mkv"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := Scan(context.Background(), root, ScanOptions{Recursive: true, IncludeRoot: true, IncludeHidden: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) < 2 {
		t.Fatalf("expected root and file, got %#v", result.Entries)
	}
}

func hasIssue(report ValidationReport, code string) bool {
	for _, issue := range report.Issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}
