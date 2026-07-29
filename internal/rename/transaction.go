package rename

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Apply executes a plan as a two-phase transaction and returns its journal.
// The returned journal is non-nil when execution fails after the journal was
// created; callers can inspect it or call Rollback/Undo after presenting the
// error to the user.  Any failed phase is rolled back automatically when
// possible.
func Apply(ctx context.Context, plan Plan, options ApplyOptions) (*Journal, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if plan.ID == "" {
		plan.ID = newID()
	}
	if plan.Version == 0 {
		plan.Version = planVersion
	}
	if plan.Root == "" {
		if len(plan.Operations) > 0 || len(plan.Directories) > 0 {
			paths := make([]string, 0, len(plan.Operations)*2+len(plan.Directories))
			for _, op := range plan.Operations {
				paths = append(paths, op.Source, op.Destination)
			}
			for _, directory := range plan.Directories {
				paths = append(paths, directory.Path)
			}
			plan.Root = commonAncestor(paths)
		} else {
			plan.Root, _ = absoluteClean(".", "")
		}
	}
	if err := Validate(plan); err != nil {
		return nil, fmt.Errorf("rename preflight failed: %w", err)
	}

	id := newID()
	journalPath, err := resolveJournalPath(plan, options.JournalPath, id)
	if err != nil {
		return nil, err
	}
	if exists, err := pathExists(journalPath); err != nil {
		return nil, fmt.Errorf("inspect journal path: %w", err)
	} else if exists {
		return nil, fmt.Errorf("journal path already exists: %s", journalPath)
	}
	if err := ensureJournalOutsideMedia(journalPath, plan); err != nil {
		return nil, err
	}

	j := &Journal{
		Version: planVersion, ID: id, PlanID: plan.ID, Root: plan.Root,
		Path: journalPath, State: JournalPrepared, CreatedAt: time.Now().UTC(),
		Operations:  make([]JournalOperation, len(plan.Operations)),
		Directories: make([]JournalDirectoryCreation, len(plan.Directories)),
	}
	for i, op := range plan.Operations {
		j.Operations[i] = JournalOperation{Operation: op}
	}
	for i, directory := range plan.Directories {
		j.Directories[i] = JournalDirectoryCreation{DirectoryCreation: directory}
	}
	if len(j.Operations) > 0 {
		stageRoot, err := makeStageRoot(plan, id)
		if err != nil {
			return nil, err
		}
		j.StageRoot = stageRoot
		for i := range j.Operations {
			j.Operations[i].Temp = filepath.Join(stageRoot, fmt.Sprintf("%03d-%s", i+1, tempBase(j.Operations[i].Source)))
		}
	}
	if err := persistJournal(j); err != nil {
		if j.StageRoot != "" {
			_ = os.Remove(j.StageRoot)
		}
		return nil, fmt.Errorf("create rename journal: %w", err)
	}

	if err := runJournal(ctx, j, options); err != nil {
		return j, err
	}
	return j, nil
}

// ApplyPlan is the desktop-friendly convenience form of Apply.
func ApplyPlan(plan Plan) (*Journal, error) {
	return Apply(context.Background(), plan, ApplyOptions{})
}

// LoadJournal reads a journal written by Apply.  The path argument is copied
// into Journal.Path even for journals produced by older versions which did
// not persist that field.
func LoadJournal(path string) (*Journal, error) {
	abs, err := absoluteClean(path, "")
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("read journal: %w", err)
	}
	var journal Journal
	if err := json.Unmarshal(data, &journal); err != nil {
		return nil, fmt.Errorf("decode journal: %w", err)
	}
	if journal.Version == 0 {
		journal.Version = planVersion
	}
	journal.Path = abs
	if journal.ID == "" {
		return nil, fmt.Errorf("journal has no id")
	}
	return &journal, nil
}

// Undo loads a journal and reverses an applied (or partially applied)
// transaction.  It is safe to call repeatedly: a rolled-back journal is a
// no-op.
func Undo(ctx context.Context, journalPath string) error {
	journal, err := LoadJournal(journalPath)
	if err != nil {
		return err
	}
	return Rollback(ctx, journal)
}

// Rollback reverses a journal in place.  For an applied journal it runs an
// inverse two-phase transaction; for an interrupted journal it uses the
// recorded temporary paths and completion flags.  The journal is retained so
// users have an audit trail.
func Rollback(ctx context.Context, journal *Journal) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if journal == nil {
		return fmt.Errorf("journal is nil")
	}
	if journal.Path == "" {
		return fmt.Errorf("journal path is empty")
	}
	switch journal.State {
	case JournalRolledBack:
		// A directory created by this transaction may have been retained because
		// a user added another file before Undo.  Repeated Undo is allowed to
		// finish that cleanup later, once the directory is empty.
		return combineErrors(nil, cleanupCreatedDirectories(journal))
	case JournalApplied:
		return undoApplied(ctx, journal)
	case JournalUndoing:
		return undoApplied(ctx, journal)
	case JournalPrepared:
		journal.State = JournalRolledBack
		journal.FinishedAt = time.Now().UTC()
		return persistJournal(journal)
	default:
		for _, op := range journal.Operations {
			if op.UndoTemp != "" || op.UndoPhase1Done || op.UndoPhase2Done {
				return undoApplied(ctx, journal)
			}
		}
		return rollbackPartial(ctx, journal)
	}
}

// runJournal performs the forward transaction.  It writes the journal before
// and after every operation; if a process is interrupted, the last durable
// flags tell Rollback which paths need attention.
func runJournal(ctx context.Context, journal *Journal, options ApplyOptions) error {
	journal.State = JournalApplying
	journal.StartedAt = time.Now().UTC()
	if err := persistJournal(journal); err != nil {
		return fmt.Errorf("start rename transaction: %w", err)
	}

	stageOrder := journalOperationOrder(journal.Operations, false, true)
	for completed, index := range stageOrder {
		if err := contextErr(ctx); err != nil {
			return failAndRollback(ctx, journal, err)
		}
		op := &journal.Operations[index]
		if op.Phase1Done {
			continue
		}
		if err := verifySourceSnapshot(*op); err != nil {
			return failAndRollback(ctx, journal, err)
		}
		if exists, err := pathExists(op.Temp); err != nil {
			return failAndRollback(ctx, journal, fmt.Errorf("inspect temporary path %q: %w", op.Temp, err))
		} else if exists {
			return failAndRollback(ctx, journal, fmt.Errorf("temporary path already exists: %s", op.Temp))
		}
		actual, found, err := findCaseInsensitivePath(op.Source)
		if err != nil {
			return failAndRollback(ctx, journal, fmt.Errorf("inspect source %q: %w", op.Source, err))
		}
		if !found {
			return failAndRollback(ctx, journal, fmt.Errorf("source disappeared before staging: %s", op.Source))
		}
		if err := os.Rename(actual, op.Temp); err != nil {
			return failAndRollback(ctx, journal, fmt.Errorf("stage %q: %w", op.Source, err))
		}
		op.Phase1Done = true
		emitProgress(options, Progress{JournalID: journal.ID, OperationID: op.ID, Phase: "stage", Source: op.Source, Destination: op.Destination, Completed: completed + 1, Total: len(stageOrder)})
		if err := persistJournal(journal); err != nil {
			return failAndRollback(ctx, journal, fmt.Errorf("record staged operation: %w", err))
		}
	}

	// Create every explicitly planned destination directory after all sources
	// are safely staged and before any destination is committed.  os.Mkdir is
	// intentional: every missing level must be present in the plan and journal,
	// so rollback never guesses which directories belong to the application.
	directoryOrder := journalDirectoryOrder(journal.Directories, false)
	for completed, index := range directoryOrder {
		if err := contextErr(ctx); err != nil {
			return failAndRollback(ctx, journal, err)
		}
		directory := &journal.Directories[index]
		if directory.Created {
			continue
		}
		if exists, err := pathExistsCaseInsensitive(directory.Path); err != nil {
			return failAndRollback(ctx, journal, fmt.Errorf("inspect directory creation target %q: %w", directory.Path, err))
		} else if exists {
			return failAndRollback(ctx, journal, fmt.Errorf("directory creation target appeared during transaction: %s", directory.Path))
		}
		parent, found, err := findCaseInsensitivePath(filepath.Dir(directory.Path))
		if err != nil {
			return failAndRollback(ctx, journal, fmt.Errorf("inspect directory parent for %q: %w", directory.Path, err))
		}
		if !found {
			return failAndRollback(ctx, journal, fmt.Errorf("directory parent disappeared: %s", filepath.Dir(directory.Path)))
		}
		parentInfo, err := os.Stat(parent)
		if err != nil {
			return failAndRollback(ctx, journal, fmt.Errorf("inspect directory parent for %q: %w", directory.Path, err))
		}
		if !parentInfo.IsDir() {
			return failAndRollback(ctx, journal, fmt.Errorf("directory parent is not a directory: %s", parent))
		}
		if err := os.Mkdir(directory.Path, 0700); err != nil {
			return failAndRollback(ctx, journal, fmt.Errorf("create destination directory %q: %w", directory.Path, err))
		}
		directory.Created = true
		emitProgress(options, Progress{JournalID: journal.ID, OperationID: directory.ID, Phase: "mkdir", Destination: directory.Path, Completed: completed + 1, Total: len(directoryOrder)})
		if err := persistJournal(journal); err != nil {
			return failAndRollback(ctx, journal, fmt.Errorf("record created directory: %w", err))
		}
	}

	commitOrder := journalOperationOrder(journal.Operations, true, false)
	for completed, index := range commitOrder {
		if err := contextErr(ctx); err != nil {
			return failAndRollback(ctx, journal, err)
		}
		op := &journal.Operations[index]
		if op.Phase2Done {
			continue
		}
		if exists, err := pathExistsCaseInsensitive(op.Destination); err != nil {
			return failAndRollback(ctx, journal, fmt.Errorf("inspect destination %q: %w", op.Destination, err))
		} else if exists {
			return failAndRollback(ctx, journal, fmt.Errorf("destination appeared during transaction: %s", op.Destination))
		}
		parent := filepath.Dir(op.Destination)
		if parentExists, err := pathExistsCaseInsensitive(parent); err != nil {
			return failAndRollback(ctx, journal, fmt.Errorf("inspect destination parent for %q: %w", op.Destination, err))
		} else if !parentExists {
			return failAndRollback(ctx, journal, fmt.Errorf("destination parent disappeared: %s", parent))
		}
		if err := os.Rename(op.Temp, op.Destination); err != nil {
			return failAndRollback(ctx, journal, fmt.Errorf("commit %q: %w", op.Destination, err))
		}
		op.Phase2Done = true
		emitProgress(options, Progress{JournalID: journal.ID, OperationID: op.ID, Phase: "commit", Source: op.Source, Destination: op.Destination, Completed: completed + 1, Total: len(commitOrder)})
		if err := persistJournal(journal); err != nil {
			return failAndRollback(ctx, journal, fmt.Errorf("record committed operation: %w", err))
		}
	}

	journal.State = JournalApplied
	journal.FinishedAt = time.Now().UTC()
	if err := persistJournal(journal); err != nil {
		return fmt.Errorf("record completed transaction: %w", err)
	}
	return nil
}

func failAndRollback(ctx context.Context, journal *Journal, cause error) error {
	journal.Error = cause.Error()
	journal.State = JournalFailed
	_ = persistJournal(journal)
	rollbackErr := rollbackPartial(ctx, journal)
	if rollbackErr != nil {
		return combineErrors(cause, []error{rollbackErr})
	}
	return cause
}

func rollbackPartial(ctx context.Context, journal *Journal) error {
	if err := contextErr(ctx); err != nil {
		// A rollback should still be attempted when the original context was
		// canceled, but avoid returning before the on-disk state is reconciled.
		_ = err
	}
	var rollbackErrors []error

	// First put destinations back into their temporary paths.  Descending
	// destination depth handles nested folders (child leaves before parent).
	indices := journalOperationOrder(journal.Operations, true, true)
	for _, index := range indices {
		op := &journal.Operations[index]
		if !op.Phase2Done {
			continue
		}
		actual, found, err := findCaseInsensitivePath(op.Destination)
		if err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("rollback inspect destination %q: %w", op.Destination, err))
			continue
		}
		if !found {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("rollback destination is missing: %s", op.Destination))
			continue
		}
		if exists, err := pathExists(op.Temp); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("rollback inspect temp %q: %w", op.Temp, err))
			continue
		} else if exists {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("rollback temp already exists: %s", op.Temp))
			continue
		}
		if err := os.Rename(actual, op.Temp); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("rollback commit %q: %w", op.Destination, err))
			continue
		}
		op.Phase2Done = false
		if err := persistJournal(journal); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("record rollback phase: %w", err))
		}
	}

	// Restore original source paths parent-first.  This is the inverse of the
	// staging order and is required when both a folder and its child appear in
	// one plan.
	indices = journalOperationOrder(journal.Operations, false, false)
	for _, index := range indices {
		op := &journal.Operations[index]
		if !op.Phase1Done {
			continue
		}
		if exists, err := pathExistsCaseInsensitive(op.Source); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("rollback inspect source %q: %w", op.Source, err))
			continue
		} else if exists {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("rollback source already exists: %s", op.Source))
			continue
		}
		actual, found, err := findCaseInsensitivePath(op.Temp)
		if err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("rollback inspect temp %q: %w", op.Temp, err))
			continue
		}
		if !found {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("rollback temp is missing: %s", op.Temp))
			continue
		}
		if err := os.Rename(actual, op.Source); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("rollback stage %q: %w", op.Source, err))
			continue
		}
		op.Phase1Done = false
		if err := persistJournal(journal); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("record rollback restore: %w", err))
		}
	}
	rollbackErrors = append(rollbackErrors, cleanupCreatedDirectories(journal)...)

	if len(rollbackErrors) != 0 {
		journal.State = JournalFailed
		journal.Error = combineErrors(fmt.Errorf("rollback failed"), rollbackErrors).Error()
		_ = persistJournal(journal)
		return combineErrors(nil, rollbackErrors)
	}
	journal.State = JournalRolledBack
	journal.FinishedAt = time.Now().UTC()
	// StageRoot should now be empty.  Remove only the directory itself; never
	// recursively delete user data if an unexpected item remains.
	if journal.StageRoot != "" {
		if err := removeIfEmpty(journal.StageRoot); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("remove empty staging directory: %w", err))
		}
	}
	if len(rollbackErrors) != 0 {
		return combineErrors(nil, rollbackErrors)
	}
	return persistJournal(journal)
}

func undoApplied(ctx context.Context, journal *Journal) error {
	journal.State = JournalUndoing
	journal.Error = ""
	if err := persistJournal(journal); err != nil {
		return err
	}
	if len(journal.Operations) > 0 {
		if journal.StageRoot == "" {
			stageRoot, err := makeStageRoot(Plan{Root: journal.Root}, journal.ID+"-undo")
			if err != nil {
				return err
			}
			journal.StageRoot = stageRoot
		} else if err := ensureDir(journal.StageRoot); err != nil {
			return fmt.Errorf("create undo staging directory: %w", err)
		}
	}

	// Reuse the journal operation list as an inverse transaction.  Original
	// Source/Destination are retained for audit; UndoTemp and undo flags carry
	// the inverse state.
	for i := range journal.Operations {
		if journal.Operations[i].UndoTemp == "" {
			journal.Operations[i].UndoTemp = filepath.Join(journal.StageRoot, fmt.Sprintf("undo-%03d-%s", i+1, tempBase(journal.Operations[i].Destination)))
		}
	}
	if err := persistJournal(journal); err != nil {
		return err
	}

	// Validate current destinations and ensure original sources are not
	// occupied by unrelated files.  A source which is another destination is a
	// legal swap and is staged before any commit.
	currentByKey := make(map[string]struct{}, len(journal.Operations))
	for _, op := range journal.Operations {
		if !op.UndoPhase1Done && !op.UndoPhase2Done {
			currentByKey[pathKey(op.Destination)] = struct{}{}
		}
	}
	for _, op := range journal.Operations {
		if op.UndoPhase2Done {
			if exists, err := pathExistsCaseInsensitive(op.Source); err != nil {
				return markUndoFailed(journal, fmt.Errorf("undo inspect restored source %q: %w", op.Source, err))
			} else if !exists {
				return markUndoFailed(journal, fmt.Errorf("restored source is missing: %s", op.Source))
			}
			continue
		}
		if op.UndoPhase1Done {
			if exists, err := pathExists(op.UndoTemp); err != nil {
				return markUndoFailed(journal, fmt.Errorf("undo inspect temporary path %q: %w", op.UndoTemp, err))
			} else if !exists {
				return markUndoFailed(journal, fmt.Errorf("undo temporary path is missing: %s", op.UndoTemp))
			}
		} else if exists, err := pathExistsCaseInsensitive(op.Destination); err != nil {
			return markUndoFailed(journal, fmt.Errorf("undo inspect %q: %w", op.Destination, err))
		} else if !exists {
			return markUndoFailed(journal, fmt.Errorf("undo source is missing: %s", op.Destination))
		}
		if exists, err := pathExistsCaseInsensitive(op.Source); err != nil {
			return fmt.Errorf("undo inspect target %q: %w", op.Source, err)
		} else if exists {
			if _, current := currentByKey[pathKey(op.Source)]; !current {
				return markUndoFailed(journal, fmt.Errorf("undo target already exists: %s", op.Source))
			}
		}
	}

	stageOrder := make([]int, len(journal.Operations))
	for i := range stageOrder {
		stageOrder[i] = i
	}
	sort.SliceStable(stageOrder, func(i, k int) bool {
		return pathDepth(journal.Operations[stageOrder[i]].Destination) > pathDepth(journal.Operations[stageOrder[k]].Destination)
	})
	for completed, index := range stageOrder {
		if err := contextErr(ctx); err != nil {
			return markUndoFailed(journal, err)
		}
		op := &journal.Operations[index]
		if op.UndoPhase1Done {
			continue
		}
		if exists, err := pathExists(op.UndoTemp); err != nil {
			return markUndoFailed(journal, err)
		} else if exists {
			return markUndoFailed(journal, fmt.Errorf("undo temporary path already exists: %s", op.UndoTemp))
		}
		actual, found, err := findCaseInsensitivePath(op.Destination)
		if err != nil || !found {
			if err == nil {
				err = fmt.Errorf("path missing")
			}
			return markUndoFailed(journal, fmt.Errorf("undo inspect %q: %w", op.Destination, err))
		}
		if err := os.Rename(actual, op.UndoTemp); err != nil {
			return markUndoFailed(journal, fmt.Errorf("undo stage %q: %w", op.Destination, err))
		}
		op.UndoPhase1Done = true
		emitProgress(ApplyOptions{}, Progress{JournalID: journal.ID, OperationID: op.ID, Phase: "undo-stage", Source: op.Destination, Destination: op.Source, Completed: completed + 1, Total: len(stageOrder)})
		if err := persistJournal(journal); err != nil {
			return err
		}
	}
	commitOrder := make([]int, len(journal.Operations))
	for i := range commitOrder {
		commitOrder[i] = i
	}
	sort.SliceStable(commitOrder, func(i, k int) bool {
		return pathDepth(journal.Operations[commitOrder[i]].Source) < pathDepth(journal.Operations[commitOrder[k]].Source)
	})
	for completed, index := range commitOrder {
		if err := contextErr(ctx); err != nil {
			return markUndoFailed(journal, err)
		}
		op := &journal.Operations[index]
		if op.UndoPhase2Done {
			continue
		}
		if exists, err := pathExistsCaseInsensitive(op.Source); err != nil {
			return markUndoFailed(journal, err)
		} else if exists {
			return markUndoFailed(journal, fmt.Errorf("undo destination appeared: %s", op.Source))
		}
		if err := os.Rename(op.UndoTemp, op.Source); err != nil {
			return markUndoFailed(journal, fmt.Errorf("undo commit %q: %w", op.Source, err))
		}
		op.UndoPhase2Done = true
		emitProgress(ApplyOptions{}, Progress{JournalID: journal.ID, OperationID: op.ID, Phase: "undo-commit", Source: op.Destination, Destination: op.Source, Completed: completed + 1, Total: len(commitOrder)})
		if err := persistJournal(journal); err != nil {
			return err
		}
	}
	if cleanupErrors := cleanupCreatedDirectories(journal); len(cleanupErrors) != 0 {
		return markUndoFailed(journal, combineErrors(fmt.Errorf("clean up created directories"), cleanupErrors))
	}

	journal.State = JournalRolledBack
	journal.FinishedAt = time.Now().UTC()
	if err := removeIfEmpty(journal.StageRoot); err != nil {
		return fmt.Errorf("remove undo staging directory: %w", err)
	}
	return persistJournal(journal)
}

func markUndoFailed(journal *Journal, cause error) error {
	journal.State = JournalFailed
	journal.Error = cause.Error()
	if err := persistJournal(journal); err != nil {
		return combineErrors(cause, []error{err})
	}
	return cause
}

func verifySourceSnapshot(op JournalOperation) error {
	info, err := os.Lstat(op.Source)
	if err != nil {
		return fmt.Errorf("source %q changed: %w", op.Source, err)
	}
	actual := snapshotFromInfo(info)
	if op.Kind != "" && actual.Kind != op.Kind {
		return fmt.Errorf("source %q kind changed from %s to %s", op.Source, op.Kind, actual.Kind)
	}
	if actual.Kind == KindFile && op.Snapshot.Exists && (actual.Size != op.Snapshot.Size || !op.Snapshot.ModTime.IsZero() && !actual.ModTime.Equal(op.Snapshot.ModTime)) {
		return fmt.Errorf("source %q changed since preview", op.Source)
	}
	return nil
}

func journalOperationOrder(ops []JournalOperation, byDestination, descending bool) []int {
	indices := make([]int, len(ops))
	for i := range ops {
		indices[i] = i
	}
	sort.SliceStable(indices, func(i, j int) bool {
		left, right := ops[indices[i]], ops[indices[j]]
		lp, rp := left.Source, right.Source
		if byDestination {
			lp, rp = left.Destination, right.Destination
		}
		ld, rd := pathDepth(lp), pathDepth(rp)
		if ld != rd {
			if descending {
				return ld > rd
			}
			return ld < rd
		}
		return pathKey(lp) < pathKey(rp)
	})
	return indices
}

func journalDirectoryOrder(directories []JournalDirectoryCreation, descending bool) []int {
	indices := make([]int, len(directories))
	for i := range directories {
		indices[i] = i
	}
	sort.SliceStable(indices, func(i, j int) bool {
		left, right := directories[indices[i]], directories[indices[j]]
		ld, rd := pathDepth(left.Path), pathDepth(right.Path)
		if ld != rd {
			if descending {
				return ld > rd
			}
			return ld < rd
		}
		return pathKey(left.Path) < pathKey(right.Path)
	})
	return indices
}

// cleanupCreatedDirectories removes only directories which this journal
// positively records as created, and only while they are still directories
// and empty.  A non-empty directory is retained and reported as an incomplete
// cleanup, keeping the journal retryable after the foreign contents have been
// removed.  Descending depth removes children before their parents.
func cleanupCreatedDirectories(journal *Journal) []error {
	if journal == nil {
		return []error{fmt.Errorf("journal is nil")}
	}
	var cleanupErrors []error
	for _, index := range journalDirectoryOrder(journal.Directories, true) {
		directory := &journal.Directories[index]
		if !directory.Created || directory.Removed {
			continue
		}
		info, err := os.Lstat(directory.Path)
		if os.IsNotExist(err) {
			directory.Removed = true
			if err := persistJournal(journal); err != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("record missing created directory %q: %w", directory.Path, err))
			}
			continue
		}
		if err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("inspect created directory %q: %w", directory.Path, err))
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("refuse to remove created-directory path which is no longer a directory: %s", directory.Path))
			continue
		}
		entries, err := os.ReadDir(directory.Path)
		if err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("inspect created directory contents %q: %w", directory.Path, err))
			continue
		}
		if len(entries) != 0 {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("created directory is not empty and was retained: %s", directory.Path))
			continue
		}
		if err := os.Remove(directory.Path); err != nil {
			if os.IsNotExist(err) {
				directory.Removed = true
			} else if current, readErr := os.ReadDir(directory.Path); readErr == nil && len(current) != 0 {
				// Another process populated the directory between ReadDir and
				// Remove.  Preserve it and leave cleanup retryable.
				cleanupErrors = append(cleanupErrors, fmt.Errorf("created directory became non-empty and was retained: %s", directory.Path))
				continue
			} else {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("remove empty created directory %q: %w", directory.Path, err))
				continue
			}
		} else {
			directory.Removed = true
		}
		if err := persistJournal(journal); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("record removed created directory %q: %w", directory.Path, err))
		}
	}
	return cleanupErrors
}

func emitProgress(options ApplyOptions, progress Progress) {
	if options.OnProgress != nil {
		options.OnProgress(progress)
	}
}

func tempBase(path string) string {
	return "." + safeTempBase(filepath.Base(path)) + ".vmf-tmp"
}

func safeTempBase(base string) string {
	base = strings.Map(func(r rune) rune {
		if r < 0x20 {
			return '_'
		}
		switch r {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			return '_'
		}
		return r
	}, base)
	base = strings.TrimRight(base, " .")
	if base == "" {
		base = "item"
	}
	if len([]rune(base)) > 80 {
		base = string([]rune(base)[:80])
	}
	return base
}

func resolveJournalPath(plan Plan, requested, id string) (string, error) {
	path := strings.TrimSpace(requested)
	if path == "" {
		root := plan.Root
		if root == "" {
			root, _ = absoluteClean(".", "")
		}
		parent := filepath.Dir(root)
		base := filepath.Base(root)
		if base == "." || base == string(filepath.Separator) || base == "" {
			base = "vmf-preupload"
		}
		path = filepath.Join(parent, "."+safeTempBase(base)+".vmf-rename-"+id+".json")
	}
	abs, err := absoluteClean(path, "")
	if err != nil {
		return "", fmt.Errorf("resolve journal path: %w", err)
	}
	if issue := validateWindowsPath(abs); issue != "" {
		return "", fmt.Errorf("invalid journal path: %s", issue)
	}
	return abs, nil
}

func ensureJournalOutsideMedia(journalPath string, plan Plan) error {
	for _, op := range plan.Operations {
		if pathKey(journalPath) == pathKey(op.Source) || pathKey(journalPath) == pathKey(op.Destination) {
			return fmt.Errorf("journal path overlaps a rename path: %s", journalPath)
		}
		if op.Kind == KindDir && (pathWithin(journalPath, op.Source) || pathWithin(journalPath, op.Destination)) {
			return fmt.Errorf("journal path must be outside renamed directories: %s", journalPath)
		}
	}
	for _, directory := range plan.Directories {
		if pathKey(journalPath) == pathKey(directory.Path) || pathWithin(journalPath, directory.Path) {
			return fmt.Errorf("journal path must be outside created directories: %s", journalPath)
		}
	}
	return nil
}

func makeStageRoot(plan Plan, id string) (string, error) {
	root := plan.Root
	if root == "" {
		root, _ = absoluteClean(".", "")
	}
	parent := filepath.Dir(root)
	base := filepath.Base(root)
	if base == "." || base == string(filepath.Separator) || base == "" {
		base = "vmf-preupload"
	}
	stage := filepath.Join(parent, "."+safeTempBase(base)+".vmf-stage-"+id)
	if issue := validateWindowsPath(stage); issue != "" {
		return "", fmt.Errorf("invalid staging path: %s", issue)
	}
	if exists, err := pathExists(stage); err != nil {
		return "", fmt.Errorf("inspect staging path: %w", err)
	} else if exists {
		return "", fmt.Errorf("staging path already exists: %s", stage)
	}
	if err := os.Mkdir(stage, 0700); err != nil {
		return "", fmt.Errorf("create staging directory: %w", err)
	}
	return stage, nil
}

func persistJournal(journal *Journal) error {
	if journal == nil || journal.Path == "" {
		return fmt.Errorf("journal path is empty")
	}
	if err := ensureDir(filepath.Dir(journal.Path)); err != nil {
		return fmt.Errorf("create journal directory: %w", err)
	}
	data, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return err
	}
	tmp := journal.Path + ".tmp-" + newID()
	file, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	writeErr := error(nil)
	if _, err := file.Write(data); err != nil {
		writeErr = err
	}
	if writeErr == nil {
		writeErr = file.Sync()
	}
	if closeErr := file.Close(); writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		_ = os.Remove(tmp)
		return writeErr
	}
	if err := os.Rename(tmp, journal.Path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
