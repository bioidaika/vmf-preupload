// Package rename contains the filesystem-only part of the pre-upload
// workflow.  It deliberately knows nothing about TMDB/TVDB or naming rules:
// callers hand it a list of old and new paths and it performs a validated,
// journaled transaction.
package rename

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// EntryKind is the kind of an item returned by Scan or stored in an
// Operation.  Symlinks are never followed by Scan and are treated as a
// single filesystem item by the transaction.
type EntryKind string

const (
	KindFile    EntryKind = "file"
	KindDir     EntryKind = "directory"
	KindSymlink EntryKind = "symlink"
	KindOther   EntryKind = "other"
)

// Entry is a JSON-friendly filesystem snapshot.  ModTime is retained in UTC
// by encoding/json and is only advisory for directories (directory mtimes
// naturally change when children are changed).
type Entry struct {
	Path       string    `json:"path"`
	Relative   string    `json:"relative,omitempty"`
	Kind       EntryKind `json:"kind"`
	Size       int64     `json:"size"`
	Mode       uint32    `json:"mode"`
	ModTime    time.Time `json:"mod_time"`
	ReadOnly   bool      `json:"read_only,omitempty"`
	IsHidden   bool      `json:"hidden,omitempty"`
	LinkTarget string    `json:"link_target,omitempty"`
}

// ScanOptions controls Scan.  ScanPath supplies the desktop-friendly defaults
// (recursive, including root and hidden entries) when called without an
// option.  Scan itself honors the fields exactly.  Symlinks are intentionally
// not followed; this is a safety property for an app which can rename whole
// folders.
type ScanOptions struct {
	Recursive     bool `json:"recursive"`
	IncludeRoot   bool `json:"include_root"`
	IncludeHidden bool `json:"include_hidden"`
	MaxDepth      int  `json:"max_depth"` // 0 means unlimited
}

// ScanResult is the deterministic result of a read-only scan.
type ScanResult struct {
	Root    string  `json:"root"`
	Entries []Entry `json:"entries"`
}

// RenameRequest is the small input type used by BuildPlan.  Paths may be
// relative when PlanOptions.Root is supplied; absolute paths are preferred by
// callers and are always stored in the resulting Plan.
type RenameRequest struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
}

// Snapshot records the source state observed while building a plan.  It lets
// Apply refuse to rename a file which changed while the user was reviewing
// the preview.
type Snapshot struct {
	Exists  bool      `json:"exists"`
	Kind    EntryKind `json:"kind"`
	Size    int64     `json:"size"`
	Mode    uint32    `json:"mode"`
	ModTime time.Time `json:"mod_time"`
}

// Operation is one logical rename.  Destination is never overwritten by the
// transaction.  ID is stable within a plan and is useful for GUI progress
// and journal recovery.
type Operation struct {
	ID          string    `json:"id"`
	Source      string    `json:"source"`
	Destination string    `json:"destination"`
	Kind        EntryKind `json:"kind"`
	Snapshot    Snapshot  `json:"snapshot"`
}

// DirectoryCreation is an explicit directory which must be created before
// rename destinations are committed.  It is kept separate from Operation:
// a directory creation has no source to snapshot or stage, and pretending it
// is a rename would weaken the transaction's source-existence guarantees.
type DirectoryCreation struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

// Plan is immutable input to Apply.  Root is the common safety scope used to
// choose a staging directory and default journal path.  AllowOutsideRoot is
// recorded so a serialized plan has the same safety semantics when reopened.
type Plan struct {
	Version          int                 `json:"version"`
	ID               string              `json:"id"`
	Root             string              `json:"root"`
	AllowOutsideRoot bool                `json:"allow_outside_root,omitempty"`
	CreatedAt        time.Time           `json:"created_at"`
	Operations       []Operation         `json:"operations"`
	Directories      []DirectoryCreation `json:"directories,omitempty"`
}

// PlanOptions controls BuildPlan.  The default is conservative: paths are
// resolved against the current working directory (or Root), destinations
// must remain in Root when Root is supplied, and exact no-op requests are
// omitted.  Case-only renames are retained because they are common on
// Windows and are handled through the temporary phase. CreateDirectories is
// an explicit allow-list; BuildPlan never infers missing destination parents.
type PlanOptions struct {
	Root              string   `json:"root,omitempty"`
	AllowOutsideRoot  bool     `json:"allow_outside_root,omitempty"`
	IncludeNoop       bool     `json:"include_noop,omitempty"`
	CreateDirectories []string `json:"create_directories,omitempty"`
}

const planVersion = 1

// JournalState is persisted after every meaningful transaction step.
type JournalState string

const (
	JournalPrepared   JournalState = "prepared"
	JournalApplying   JournalState = "applying"
	JournalApplied    JournalState = "applied"
	JournalUndoing    JournalState = "undoing"
	JournalRolledBack JournalState = "rolled_back"
	JournalFailed     JournalState = "failed"
)

// JournalOperation extends an Operation with transaction bookkeeping.  Temp
// paths are in a staging directory on the same volume as the media, so a
// nested folder rename cannot invalidate another operation's temporary path.
type JournalOperation struct {
	Operation
	Temp           string `json:"temp,omitempty"`
	Phase1Done     bool   `json:"phase1_done,omitempty"`
	Phase2Done     bool   `json:"phase2_done,omitempty"`
	UndoTemp       string `json:"undo_temp,omitempty"`
	UndoPhase1Done bool   `json:"undo_phase1_done,omitempty"`
	UndoPhase2Done bool   `json:"undo_phase2_done,omitempty"`
}

// JournalDirectoryCreation records ownership of a directory created by this
// transaction.  Rollback and Undo only consider entries whose Created flag
// was set after a successful os.Mkdir, and remove them only while empty.
type JournalDirectoryCreation struct {
	DirectoryCreation
	Created bool `json:"created,omitempty"`
	Removed bool `json:"removed,omitempty"`
}

// Journal is an append-by-rewrite JSON journal.  Path is included in JSON so
// a GUI can pass the file to Undo after restarting the app.
type Journal struct {
	Version     int                        `json:"version"`
	ID          string                     `json:"id"`
	PlanID      string                     `json:"plan_id"`
	Root        string                     `json:"root"`
	StageRoot   string                     `json:"stage_root"`
	Path        string                     `json:"path"`
	State       JournalState               `json:"state"`
	CreatedAt   time.Time                  `json:"created_at"`
	StartedAt   time.Time                  `json:"started_at,omitempty"`
	FinishedAt  time.Time                  `json:"finished_at,omitempty"`
	Error       string                     `json:"error,omitempty"`
	Operations  []JournalOperation         `json:"operations"`
	Directories []JournalDirectoryCreation `json:"directories,omitempty"`
}

// Progress is emitted by Apply/Undo when OnProgress is set.
type Progress struct {
	JournalID   string `json:"journal_id"`
	OperationID string `json:"operation_id"`
	Phase       string `json:"phase"` // stage, mkdir, commit, undo-stage, undo-commit
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Completed   int    `json:"completed"`
	Total       int    `json:"total"`
}

// ApplyOptions controls execution.  JournalPath defaults to a hidden file
// next to the plan's common root.  Journals are kept by default so the GUI
// can offer Undo; callers may remove the file after their own retention
// policy has been applied.
type ApplyOptions struct {
	JournalPath string         `json:"journal_path,omitempty"`
	OnProgress  func(Progress) `json:"-"`
}

// ValidationIssue describes one preflight failure.  Code is stable enough
// for a GUI to localize while Message remains useful in logs.
type ValidationIssue struct {
	Code        string `json:"code"`
	Path        string `json:"path,omitempty"`
	Destination string `json:"destination,omitempty"`
	Message     string `json:"message"`
}

// ValidationReport is returned by Preflight.  It is deliberately a value
// type, making it straightforward to marshal through Wails.
type ValidationReport struct {
	Issues []ValidationIssue `json:"issues,omitempty"`
}

func (r ValidationReport) Valid() bool { return len(r.Issues) == 0 }

func (r ValidationReport) Error() string {
	if r.Valid() {
		return ""
	}
	parts := make([]string, 0, len(r.Issues))
	for _, issue := range r.Issues {
		if issue.Code == "" {
			parts = append(parts, issue.Message)
		} else {
			parts = append(parts, fmt.Sprintf("%s: %s", issue.Code, issue.Message))
		}
	}
	return strings.Join(parts, "; ")
}

// Validate is the error-returning convenience form of Preflight.
func Validate(plan Plan) error {
	report := Preflight(plan)
	if !report.Valid() {
		return report
	}
	return nil
}

// MarshalJSON keeps Plan/Journal JSON stable even if callers pass nil slices.
// The explicit methods are intentionally small; they also serve as compile
// time documentation for frontend consumers.
func (r ValidationReport) MarshalJSON() ([]byte, error) {
	type alias ValidationReport
	return json.Marshal(alias(r))
}

// sortOperations returns a copy sorted by path depth.  It is shared by the
// executor and tests and never mutates the caller's Plan.
func sortOperations(ops []Operation, byDestination bool, descending bool) []Operation {
	out := append([]Operation(nil), ops...)
	depth := func(op Operation) int {
		p := op.Source
		if byDestination {
			p = op.Destination
		}
		return pathDepth(p)
	}
	sort.SliceStable(out, func(i, j int) bool {
		di, dj := depth(out[i]), depth(out[j])
		if di != dj {
			if descending {
				return di > dj
			}
			return di < dj
		}
		pi, pj := out[i].Source, out[j].Source
		if byDestination {
			pi, pj = out[i].Destination, out[j].Destination
		}
		return pathKey(pi) < pathKey(pj)
	})
	return out
}

// contextErr is used by the scanner and executor to keep cancellation errors
// consistent across Go versions.
func contextErr(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
