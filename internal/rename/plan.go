package rename

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf16"
)

// BuildPlan resolves requests, snapshots their sources and performs static
// checks.  Destination collisions which depend on the current filesystem are
// reported by Preflight (and are checked again by Apply immediately before
// execution).
func BuildPlan(requests []RenameRequest, options PlanOptions) (Plan, error) {
	if len(requests) == 0 {
		return Plan{}, fmt.Errorf("rename plan is empty")
	}

	root := ""
	if strings.TrimSpace(options.Root) != "" {
		var err error
		root, err = absoluteClean(options.Root, "")
		if err != nil {
			return Plan{}, fmt.Errorf("resolve plan root: %w", err)
		}
		if info, err := os.Lstat(root); err == nil && !info.IsDir() {
			root = filepath.Dir(root)
		}
	}

	ops := make([]Operation, 0, len(requests))
	seenSource := make(map[string]struct{}, len(requests))
	for i, request := range requests {
		source, err := absoluteClean(request.Source, root)
		if err != nil {
			return Plan{}, fmt.Errorf("request %d source: %w", i, err)
		}
		destination, err := absoluteClean(request.Destination, root)
		if err != nil {
			return Plan{}, fmt.Errorf("request %d destination: %w", i, err)
		}
		if source == destination && !options.IncludeNoop {
			continue
		}
		info, err := os.Lstat(source)
		if err != nil {
			return Plan{}, fmt.Errorf("request %d source %q: %w", i, source, err)
		}
		sourceKey := pathKey(source)
		if _, exists := seenSource[sourceKey]; exists {
			return Plan{}, fmt.Errorf("request %d: duplicate source %q", i, source)
		}
		seenSource[sourceKey] = struct{}{}
		op := Operation{
			ID:          fmt.Sprintf("op-%03d", len(ops)+1),
			Source:      source,
			Destination: destination,
			Kind:        kindFromFileInfo(info),
			Snapshot:    snapshotFromInfo(info),
		}
		ops = append(ops, op)
	}
	if len(ops) == 0 {
		// A plan with only exact no-ops is useful to the GUI (it can display a
		// successful dry-run), so retain a valid empty plan instead of failing.
		if root == "" {
			root, _ = absoluteClean(".", "")
		}
		return Plan{Version: planVersion, ID: newID(), Root: root,
			AllowOutsideRoot: options.AllowOutsideRoot, CreatedAt: time.Now().UTC()}, nil
	}

	if root == "" {
		paths := make([]string, 0, len(ops)*2)
		for _, op := range ops {
			paths = append(paths, op.Source, op.Destination)
		}
		root = commonAncestor(paths)
	} else if !options.AllowOutsideRoot {
		// A GUI commonly supplies the selected release folder as Root and also
		// asks to rename that folder to a sibling.  In that one well-defined
		// case, use the parent as the safety scope; otherwise an explicit Root
		// remains a hard boundary.
		for _, op := range ops {
			if pathKey(op.Source) == pathKey(root) && !pathWithin(op.Destination, root) {
				root = filepath.Dir(root)
				break
			}
		}
	}
	plan := Plan{
		Version:          planVersion,
		ID:               newID(),
		Root:             root,
		AllowOutsideRoot: options.AllowOutsideRoot,
		CreatedAt:        time.Now().UTC(),
		Operations:       ops,
	}
	// Static validation catches malformed names and duplicate paths before a
	// preview is shown.  Filesystem-dependent checks remain in Preflight.
	if report := staticValidation(plan); !report.Valid() {
		return Plan{}, report
	}
	return plan, nil
}

// NewPlan is an alias with a concise name for callers constructing requests
// from a GUI table.
func NewPlan(requests []RenameRequest, options PlanOptions) (Plan, error) {
	return BuildPlan(requests, options)
}

func staticValidation(plan Plan) ValidationReport {
	report := ValidationReport{}
	if plan.Version == 0 {
		// Plans built by older callers may omit Version; treat them as v1.
		plan.Version = planVersion
	}
	if plan.Root == "" {
		report.Issues = append(report.Issues, ValidationIssue{Code: "root_missing", Message: "plan root is empty"})
	}
	sources := make(map[string]int, len(plan.Operations))
	destinations := make(map[string]int, len(plan.Operations))
	for i, op := range plan.Operations {
		if strings.TrimSpace(op.Source) == "" || strings.TrimSpace(op.Destination) == "" {
			report.Issues = append(report.Issues, ValidationIssue{Code: "path_empty", Path: op.Source, Destination: op.Destination, Message: fmt.Sprintf("operation %d has an empty path", i)})
			continue
		}
		key := pathKey(op.Source)
		if previous, ok := sources[key]; ok {
			report.Issues = append(report.Issues, ValidationIssue{Code: "duplicate_source", Path: op.Source, Message: fmt.Sprintf("source is also used by operation %d", previous+1)})
		} else {
			sources[key] = i
		}
		destKey := pathKey(op.Destination)
		if previous, ok := destinations[destKey]; ok {
			report.Issues = append(report.Issues, ValidationIssue{Code: "duplicate_destination", Destination: op.Destination, Message: fmt.Sprintf("destination is also used by operation %d", previous+1)})
		} else {
			destinations[destKey] = i
		}
		if issue := validateWindowsPath(op.Destination); issue != "" {
			report.Issues = append(report.Issues, ValidationIssue{Code: "invalid_destination_name", Destination: op.Destination, Message: issue})
		}
		if !plan.AllowOutsideRoot && plan.Root != "" {
			if !pathWithin(op.Source, plan.Root) || !pathWithin(op.Destination, plan.Root) {
				report.Issues = append(report.Issues, ValidationIssue{Code: "outside_root", Path: op.Source, Destination: op.Destination, Message: "operation escapes the plan root"})
			}
		}
	}
	return report
}

// Preflight checks both plan structure and the current filesystem.  It never
// changes files.  Apply invokes it immediately before staging, because a
// preview can remain open while another process changes the directory.
func Preflight(plan Plan) ValidationReport {
	report := staticValidation(plan)
	if plan.Version == 0 {
		plan.Version = planVersion
	}
	if len(plan.Operations) == 0 {
		return report
	}

	sourceByKey := make(map[string]int, len(plan.Operations))
	destinationByKey := make(map[string]int, len(plan.Operations))
	destinationKinds := make(map[string]EntryKind, len(plan.Operations))
	for i, op := range plan.Operations {
		sourceByKey[pathKey(op.Source)] = i
		destinationByKey[pathKey(op.Destination)] = i
		destinationKinds[pathKey(op.Destination)] = op.Kind
	}

	for i, op := range plan.Operations {
		info, err := os.Lstat(op.Source)
		if err != nil {
			code := "source_missing"
			if !os.IsNotExist(err) {
				code = "source_unreadable"
			}
			report.Issues = append(report.Issues, ValidationIssue{Code: code, Path: op.Source, Message: fmt.Sprintf("cannot access source: %v", err)})
			continue
		}
		actual := snapshotFromInfo(info)
		if op.Kind != "" && op.Kind != actual.Kind {
			report.Issues = append(report.Issues, ValidationIssue{Code: "source_kind_changed", Path: op.Source, Message: fmt.Sprintf("source kind changed from %s to %s", op.Kind, actual.Kind)})
		}
		if op.Snapshot.Exists && actual.Kind == KindFile {
			if op.Snapshot.Size != actual.Size || !op.Snapshot.ModTime.IsZero() && !op.Snapshot.ModTime.Equal(actual.ModTime) {
				report.Issues = append(report.Issues, ValidationIssue{Code: "source_changed", Path: op.Source, Message: "source size or modification time changed since the plan was built"})
			}
		}

		// A destination may be an existing source of another operation (a
		// swap/chain); all sources are moved to staging before destinations are
		// committed.  Any other existing destination would be overwritten.
		if existing, err := pathExistsCaseInsensitive(op.Destination); err != nil {
			report.Issues = append(report.Issues, ValidationIssue{Code: "destination_unreadable", Destination: op.Destination, Message: fmt.Sprintf("cannot inspect destination: %v", err)})
		} else if existing {
			if _, isSource := sourceByKey[pathKey(op.Destination)]; !isSource {
				report.Issues = append(report.Issues, ValidationIssue{Code: "destination_exists", Destination: op.Destination, Message: "destination already exists"})
			}
		}

		parent := filepath.Dir(op.Destination)
		if issue := validateDestinationParent(parent, destinationByKey, destinationKinds); issue != "" {
			report.Issues = append(report.Issues, ValidationIssue{Code: "destination_parent", Destination: op.Destination, Message: issue})
		}

		// Moving a directory below itself can never be made safe by staging.
		if op.Kind == KindDir && pathWithin(op.Destination, op.Source) && pathKey(op.Destination) != pathKey(op.Source) {
			report.Issues = append(report.Issues, ValidationIssue{Code: "directory_into_itself", Path: op.Source, Destination: op.Destination, Message: "a directory cannot be moved into one of its own descendants"})
		}
		_ = i // retained for clear correspondence with source/destination maps
	}

	// A destination that is a descendant of a source belonging to a different
	// operation is usually a cycle.  Permit the common parent->new-parent plus
	// child->new-child shape, but reject direct source/destination nesting that
	// would strand a path during phase two.
	for i, left := range plan.Operations {
		for j, right := range plan.Operations {
			if i == j {
				continue
			}
			if left.Kind == KindDir && pathWithin(right.Destination, left.Source) && pathKey(right.Destination) != pathKey(left.Source) {
				// If the destination is also below the left operation's
				// destination, it is the supported nested rename shape.
				if !pathWithin(right.Destination, left.Destination) {
					report.Issues = append(report.Issues, ValidationIssue{Code: "nested_cycle", Path: right.Source, Destination: right.Destination, Message: "destination falls inside another source directory without a matching destination tree"})
				}
			}
		}
	}

	// os.Rename cannot cross volumes.  Detect that before a transaction has
	// staged half of a plan.  VolumeName is empty on Unix, which means all paths
	// are considered to be on the same volume there.
	for _, op := range plan.Operations {
		if sv, dv := strings.ToLower(filepath.VolumeName(op.Source)), strings.ToLower(filepath.VolumeName(op.Destination)); sv != dv {
			report.Issues = append(report.Issues, ValidationIssue{Code: "cross_volume", Path: op.Source, Destination: op.Destination, Message: "rename across volumes is not supported"})
		}
	}
	return report
}

func validateDestinationParent(parent string, destinationByKey map[string]int, destinationKinds map[string]EntryKind) string {
	for current := filepath.Clean(parent); ; current = filepath.Dir(current) {
		if exists, err := pathExistsCaseInsensitive(current); err == nil && exists {
			info, err := os.Stat(current)
			if err != nil {
				return fmt.Sprintf("cannot stat destination parent: %v", err)
			}
			if !info.IsDir() {
				return "destination parent is not a directory"
			}
			return ""
		}
		if kind, planned := destinationKinds[pathKey(current)]; planned {
			if kind != KindDir {
				return "destination parent is planned to be a file"
			}
			return ""
		}
		next := filepath.Dir(current)
		if next == current {
			return fmt.Sprintf("destination parent %q does not exist", parent)
		}
	}
}

func absoluteClean(path, root string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path is empty")
	}
	if !filepath.IsAbs(path) {
		if root != "" {
			path = filepath.Join(root, path)
		}
		var err error
		path, err = filepath.Abs(path)
		if err != nil {
			return "", err
		}
	}
	return filepath.Clean(path), nil
}

func commonAncestor(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	parts := func(path string) []string {
		volume := filepath.VolumeName(path)
		rest := strings.TrimPrefix(path, volume)
		rest = strings.Trim(rest, string(filepath.Separator))
		if rest == "" {
			return []string{volume + string(filepath.Separator)}
		}
		return append([]string{volume + string(filepath.Separator)}, strings.Split(rest, string(filepath.Separator))...)
	}
	common := parts(paths[0])
	for _, path := range paths[1:] {
		other := parts(path)
		limit := len(common)
		if len(other) < limit {
			limit = len(other)
		}
		i := 0
		for i < limit && strings.EqualFold(common[i], other[i]) {
			i++
		}
		common = common[:i]
		if len(common) == 0 {
			return filepath.VolumeName(paths[0]) + string(filepath.Separator)
		}
	}
	if len(common) == 1 {
		return common[0]
	}
	return filepath.Join(common...)
}

func pathDepth(path string) int {
	clean := filepath.Clean(path)
	volume := filepath.VolumeName(clean)
	clean = strings.TrimPrefix(clean, volume)
	clean = strings.Trim(clean, string(filepath.Separator))
	if clean == "" {
		return 0
	}
	return len(strings.Split(clean, string(filepath.Separator)))
}

// pathKey intentionally uses case-insensitive comparison even on Unix.  The
// application targets Windows and a plan prepared on Linux must not produce
// two names which collide when copied to an NTFS volume.
func pathKey(path string) string {
	clean := filepath.Clean(path)
	return strings.ToLower(clean)
}

func pathWithin(path, root string) bool {
	p := pathKey(path)
	r := pathKey(root)
	if p == r {
		return true
	}
	rel, err := filepath.Rel(r, p)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func kindFromFileInfo(info os.FileInfo) EntryKind {
	if info.Mode()&os.ModeSymlink != 0 {
		return KindSymlink
	}
	if info.IsDir() {
		return KindDir
	}
	if info.Mode().IsRegular() {
		return KindFile
	}
	return KindOther
}

func snapshotFromInfo(info os.FileInfo) Snapshot {
	return Snapshot{Exists: true, Kind: kindFromFileInfo(info), Size: info.Size(), Mode: uint32(info.Mode()), ModTime: info.ModTime().UTC()}
}

func validateWindowsPath(path string) string {
	// Validate every component, not just filepath.Base: folder renames also
	// become torrent paths and must be portable to Windows.
	normalized := strings.ReplaceAll(path, `\`, "/")
	components := strings.Split(normalized, "/")
	for index, component := range components {
		if component == "" || component == "." || component == ".." {
			continue
		}
		// A drive prefix (C:) is the one legal colon on Windows.
		if index == 0 && len(component) == 2 && component[1] == ':' && ((component[0] >= 'A' && component[0] <= 'Z') || (component[0] >= 'a' && component[0] <= 'z')) {
			continue
		}
		if issue := validateWindowsBasename(component); issue != "" {
			return issue
		}
	}
	if len([]rune(path)) > 32760 {
		return "path exceeds the Windows extended-path limit"
	}
	return ""
}

// ValidateWindowsPath validates all components of a proposed path against
// Windows filename rules without touching the filesystem.
func ValidateWindowsPath(path string) error {
	if issue := validateWindowsPath(path); issue != "" {
		return fmt.Errorf("%s", issue)
	}
	return nil
}

func validateWindowsBasename(name string) string {
	if name == "" || name == "." || name == ".." {
		return "name is empty or reserved"
	}
	for _, r := range name {
		if r < 0x20 {
			return "name contains a control character"
		}
		switch r {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			return fmt.Sprintf("name %q contains an invalid Windows character %q", name, r)
		}
	}
	if strings.HasSuffix(name, " ") || strings.HasSuffix(name, ".") {
		return "name cannot end with a space or period on Windows"
	}
	if len(utf16.Encode([]rune(name))) > 255 {
		return "name exceeds the Windows 255 UTF-16 code-unit component limit"
	}
	trimmed := strings.TrimRight(name, " .")
	base := trimmed
	if dot := strings.IndexByte(base, '.'); dot >= 0 {
		base = base[:dot]
	}
	switch strings.ToUpper(base) {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return fmt.Sprintf("name %q is reserved by Windows", name)
	}
	return ""
}

// ValidateWindowsBasename validates one file or directory name.  It rejects
// device names such as CON/NUL, trailing dots/spaces, control characters,
// invalid punctuation and overlong UTF-16 components.
func ValidateWindowsBasename(name string) error {
	if issue := validateWindowsBasename(name); issue != "" {
		return fmt.Errorf("%s", issue)
	}
	return nil
}

// sortedPaths is used by scanner and deterministic tests.
func sortedPaths(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool { return pathKey(entries[i].Path) < pathKey(entries[j].Path) })
}
