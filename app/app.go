package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/bioidaika/vmf-preupload/internal/config"
	"github.com/bioidaika/vmf-preupload/internal/metadata"
	"github.com/bioidaika/vmf-preupload/internal/naming"
	"github.com/bioidaika/vmf-preupload/internal/providers"
	"github.com/bioidaika/vmf-preupload/internal/rename"
	"github.com/bioidaika/vmf-preupload/internal/scanner"
	"github.com/bioidaika/vmf-preupload/pkg/api"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the Wails-facing application service. All filesystem writes go
// through internal/rename; scan, provider lookup and preview remain read-only.
type App struct {
	ctx context.Context

	mu           sync.Mutex
	settings     api.Settings
	plan         rename.Plan
	planID       string
	planCanApply bool
	journal      *rename.Journal
	// journalAttention records a failed Apply/Undo outcome independently of
	// the last journal state we could persist or reload. In particular, the
	// in-memory state can already be "applied" when the final journal write
	// fails, and a failed Undo can leave the on-disk state looking "applied".
	journalAttention bool
}

func NewApp() *App {
	settings, err := config.Load()
	if err != nil {
		settings = config.DefaultSettings()
	}
	return &App{settings: settings}
}

func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *App) context() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func (a *App) GetSettings() Settings {
	a.mu.Lock()
	defer a.mu.Unlock()
	return settingsToDTO(a.settings)
}

func (a *App) SaveSettings(settings Settings) error {
	stored := settingsFromDTO(settings)
	if stored.ReleaseGroup == "" {
		stored.ReleaseGroup = "NoGroup"
	}
	if stored.Separator == "" {
		stored.Separator = "."
	}
	if stored.Profile == "" || strings.EqualFold(stored.Profile, "vmf@1") || strings.Contains(strings.ToLower(stored.Profile), "vmf compatible") {
		stored.Profile = "vmf@2"
	}
	if err := config.Save(stored); err != nil {
		return err
	}
	a.mu.Lock()
	a.settings = stored
	a.mu.Unlock()
	return nil
}

func (a *App) SelectFile() (string, error) {
	return runtime.OpenFileDialog(a.context(), runtime.OpenDialogOptions{
		Title: "Choose a media file",
		Filters: []runtime.FileFilter{{
			DisplayName: "Media files (*.mkv;*.mp4;*.m4v;*.ts;*.m2ts;*.avi;*.webm)",
			Pattern:     "*.mkv;*.mp4;*.m4v;*.ts;*.m2ts;*.avi;*.webm",
		}},
	})
}

func (a *App) SelectFolder() (string, error) {
	return runtime.OpenDirectoryDialog(a.context(), runtime.OpenDialogOptions{Title: "Choose a movie or TV folder"})
}

func (a *App) ScanPath(path string) (ScanResult, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return ScanResult{}, fmt.Errorf("path is empty")
	}
	settings := a.currentSettings()
	result, err := scanner.ScanPath(a.context(), path, settings.MediaInfoBin)
	if err != nil {
		return ScanResult{}, err
	}
	return a.toScanResult(result), nil
}

func (a *App) PreviewRename(request RenameRequest) (RenamePlan, error) {
	root := strings.TrimSpace(request.RootPath)
	if root == "" {
		return RenamePlan{}, fmt.Errorf("root path is empty")
	}
	scan, err := scanner.ScanPath(a.context(), root, a.currentSettings().MediaInfoBin)
	if err != nil {
		return RenamePlan{}, err
	}
	profile := naming.DefaultProfile()
	profile.Separator = request.Separator
	if profile.Separator == "" {
		profile.Separator = "."
	}
	baseMeta := toNamingMetadata(request.Metadata)
	if baseMeta.Group == "" {
		baseMeta.Group = "NoGroup"
	}
	metadataOverrides := makeMetadataOverrideSet(request.MetadataOverrides)
	preserveExistingP2P := true
	if request.PreserveExistingP2P != nil {
		preserveExistingP2P = *request.PreserveExistingP2P
	}

	if len(scan.Assets) == 0 {
		return RenamePlan{}, fmt.Errorf("no supported media files found")
	}
	// Do not trust a stale/legacy request flag. A folder carries UHD only when
	// every original video basename explicitly carries UHD/Ultra HD; each file
	// is classified independently below.
	baseMeta.UHD = allVideoAssetsHaveExplicitUHD(scan.Assets)
	rootInfo, err := os.Stat(root)
	if err != nil {
		return RenamePlan{}, err
	}
	rootIsDir := rootInfo.IsDir()
	parent := filepath.Dir(root)
	newRoot := root
	requests := make([]rename.RenameRequest, 0, len(scan.Assets)+1)
	displayItems := make([]RenameItem, 0, len(scan.Assets)+1)
	warnings := append([]string{}, scan.Warnings...)
	planErrors := []string{}
	seasonLayout := tvSeasonLayout{}
	if rootIsDir && baseMeta.Category == naming.TV {
		seasonLayout = analyzeTVSeasonLayout(root, scan.Assets)
		for _, message := range seasonLayout.ValidationErrors {
			appendUnique(&planErrors, "season layout: "+message)
		}
		for _, message := range tvSeasonLayoutWarnings(seasonLayout) {
			appendUnique(&warnings, message)
		}
	}

	// A series root is a container for multiple upload units, so keep it in
	// place and rename each recognized season directory below it. A selected
	// single-season folder remains one release unit and follows normal naming.
	if rootIsDir {
		if seasonLayout.SeriesRoot {
			displayItems = append(displayItems, RenameItem{OldPath: root, NewPath: root, Kind: "folder", Status: "same"})
		} else if preserveExistingP2P && metadata.IsP2PReleaseFolderName(filepath.Base(root)) {
			displayItems = append(displayItems, RenameItem{OldPath: root, NewPath: root, Kind: "folder", Status: "preserved"})
		} else {
			folderMeta := baseMeta
			if folderMeta.Category == naming.TV && len(seasonLayout.Seasons) == 1 {
				folderMeta.Season = seasonLayout.Seasons[0]
			}
			if folderMeta.Category == naming.TV && seasonLayout.VideoCount > 1 {
				folderMeta.Episode = ""
				folderMeta.EpisodeTitle = ""
			}
			folderName, nameWarnings := naming.Render(folderMeta, profile)
			warnings = appendNamingWarnings(warnings, nameWarnings)
			if folderName == "" {
				return RenamePlan{}, fmt.Errorf("could not render folder name")
			}
			newRoot = filepath.Join(parent, folderName)
			if filepath.Clean(newRoot) != filepath.Clean(root) {
				requests = append(requests, rename.RenameRequest{Source: root, Destination: newRoot})
			} else {
				displayItems = append(displayItems, RenameItem{OldPath: root, NewPath: newRoot, Kind: "folder", Status: "same"})
			}
		}
	}

	type seasonRenameTarget struct {
		directory       tvSeasonDirectory
		destination     string
		destinationBase string
		status          string
		create          bool
	}
	seasonDestinations := map[string]string{}
	seasonDestinationsBySeason := map[string]string{}
	seasonDestinationCounts := map[string]int{}
	seasonTargets := make([]seasonRenameTarget, 0, len(seasonLayout.Directories)+len(seasonLayout.Seasons))
	createDirectories := []string{}
	if rootIsDir && baseMeta.Category == naming.TV {
		directAssetsBySeason := map[string][]api.Asset{}
		for _, asset := range scan.Assets {
			assetKey := appPathKey(asset.Path)
			season := seasonLayout.AssetSeason[assetKey]
			if isVideoAsset(asset) && seasonLayout.AssetAtRoot[assetKey] && season != "" {
				directAssetsBySeason[season] = append(directAssetsBySeason[season], asset)
			}
		}
		existingSeasonCounts := map[string]int{}
		targetDirectories := append([]tvSeasonDirectory(nil), seasonLayout.Directories...)
		for index := range targetDirectories {
			directory := &targetDirectories[index]
			existingSeasonCounts[directory.Season]++
			directory.Assets = append(directory.Assets, directAssetsBySeason[directory.Season]...)
		}
		if seasonLayout.SeriesRoot {
			for _, season := range seasonLayout.Seasons {
				if existingSeasonCounts[season] == 0 && len(directAssetsBySeason[season]) > 0 {
					targetDirectories = append(targetDirectories, tvSeasonDirectory{Season: season, Assets: directAssetsBySeason[season]})
				}
			}
		}

		for _, seasonDirectory := range targetDirectories {
			seasonUHD := allVideoAssetsHaveExplicitUHD(seasonDirectory.Assets)
			if conflict := seasonTechnicalConflict(baseMeta, seasonDirectory.Season, seasonDirectory.Assets, seasonUHD, metadataOverrides); conflict != "" {
				appendUnique(&planErrors, conflict)
			}
			seasonMeta := baseMeta
			if len(seasonDirectory.Assets) > 0 {
				representative := seasonRepresentativeAsset(seasonDirectory.Assets)
				seasonMeta = mergeAssetMetadata(baseMeta, representative)
				seasonMeta = preferAssetTechnicalMetadata(seasonMeta, representative, metadataOverrides)
				seasonMeta = applyConservativeSeasonTags(seasonMeta, baseMeta, seasonDirectory.Assets, metadataOverrides)
			}
			seasonMeta.Season = seasonDirectory.Season
			seasonMeta.Episode = ""
			seasonMeta.EpisodeTitle = ""
			seasonMeta.UHD = seasonUHD

			create := strings.TrimSpace(seasonDirectory.Source) == ""
			sourceBase := ""
			if !create {
				sourceBase = filepath.Base(seasonDirectory.Source)
			}
			destinationBase := sourceBase
			status := "preserved"
			if create || !preserveExistingP2P || !metadata.IsP2PReleaseFolderName(sourceBase) {
				var nameWarnings []naming.Warning
				destinationBase, nameWarnings = naming.Render(seasonMeta, profile)
				warnings = appendNamingWarnings(warnings, nameWarnings)
				if create {
					status = "create"
				} else {
					status = "same"
				}
				if destinationBase == "" {
					appendUnique(&planErrors, "could not render season folder S"+seasonDirectory.Season)
					continue
				}
			}

			destination := filepath.Join(newRoot, destinationBase)
			if !create {
				seasonDestinations[appPathKey(seasonDirectory.Source)] = destination
			}
			if seasonDestinationsBySeason[seasonDirectory.Season] == "" {
				seasonDestinationsBySeason[seasonDirectory.Season] = destination
			}
			destinationKey := appPathKey(destination)
			seasonDestinationCounts[destinationKey]++
			seasonTargets = append(seasonTargets, seasonRenameTarget{
				directory:       seasonDirectory,
				destination:     destination,
				destinationBase: destinationBase,
				status:          status,
				create:          create,
			})
		}

		for _, target := range seasonTargets {
			destinationKey := appPathKey(target.destination)
			if seasonDestinationCounts[destinationKey] > 1 {
				appendUnique(&planErrors, "duplicate season destination: "+target.destination)
				oldPath := target.directory.Source
				if target.create {
					oldPath = "(new season folder)"
				}
				displayItems = append(displayItems, RenameItem{OldPath: oldPath, NewPath: target.destination, Kind: "folder", Status: "conflict"})
				continue
			}
			if target.create {
				createDirectories = append(createDirectories, target.destination)
				continue
			}
			sourceBase := filepath.Base(target.directory.Source)
			if target.status == "preserved" {
				displayItems = append(displayItems, RenameItem{OldPath: target.directory.Source, NewPath: target.destination, Kind: "folder", Status: target.status})
			} else if sourceBase == target.destinationBase {
				displayItems = append(displayItems, RenameItem{OldPath: target.directory.Source, NewPath: target.destination, Kind: "folder", Status: "same"})
			} else {
				requests = append(requests, rename.RenameRequest{Source: target.directory.Source, Destination: target.destination})
			}
		}
	}

	seenDest := map[string]bool{}
	conflictingFileDestinations := map[string]bool{}
	for _, asset := range scan.Assets {
		if !isVideoAsset(asset) {
			continue
		}
		dir := filepath.Dir(asset.Path)
		assetKey := appPathKey(asset.Path)
		if rootIsDir {
			dir = mappedAssetDirectory(asset, newRoot, seasonLayout, seasonDestinations, seasonDestinationsBySeason)
		}
		requiresExplicitMove := rootIsDir && seasonLayout.AssetAtRoot[assetKey] && appPathKey(dir) != appPathKey(newRoot)
		if preserveExistingP2P && metadata.IsP2PReleaseName(asset.Name) {
			destination := filepath.Join(dir, asset.Name)
			key := strings.ToLower(filepath.Clean(destination))
			if seenDest[key] {
				appendUnique(&planErrors, "duplicate destination: "+destination)
				conflictingFileDestinations[key] = true
				displayItems = append(displayItems, RenameItem{OldPath: asset.Path, NewPath: destination, Kind: "file", Status: "conflict"})
				continue
			}
			seenDest[key] = true
			if requiresExplicitMove {
				requests = append(requests, rename.RenameRequest{Source: asset.Path, Destination: destination})
			} else {
				displayItems = append(displayItems, RenameItem{OldPath: asset.Path, NewPath: destination, Kind: "file", Status: "preserved"})
			}
			continue
		}
		meta := mergeAssetMetadata(baseMeta, asset)
		if baseMeta.Category == naming.TV {
			if season := seasonLayout.AssetSeason[assetKey]; season != "" {
				meta.Season = season
			}
			if episode := seasonLayout.AssetEpisode[assetKey]; episode != "" {
				meta.Episode = episode
			}
			if seasonLayout.VideoCount > 1 {
				// Never reuse the first scanned episode for another asset. Missing
				// per-file identity remains missing and is surfaced as a collision or
				// layout error instead of silently becoming S01E01.
				if seasonLayout.AssetEpisode[assetKey] == "" {
					meta.Episode = ""
				}
				meta.EpisodeTitle = asset.Content.EpisodeTitle
				meta = preferAssetTechnicalMetadata(meta, asset, metadataOverrides)
			}
		}
		if baseMeta.Category == naming.TV && seasonLayout.VideoCount > 1 && strings.TrimSpace(meta.Episode) == "" {
			appendUnique(&planErrors, "season layout: could not determine an episode for "+asset.Name)
			displayItems = append(displayItems, RenameItem{OldPath: asset.Path, NewPath: asset.Path, Kind: "file", Status: "conflict"})
			continue
		}
		name, nameWarnings := naming.Render(meta, profile)
		warnings = appendNamingWarnings(warnings, nameWarnings)
		if name == "" {
			continue
		}
		destination := filepath.Join(dir, name+filepath.Ext(asset.Path))
		key := strings.ToLower(filepath.Clean(destination))
		if seenDest[key] {
			appendUnique(&planErrors, "duplicate destination: "+destination)
			conflictingFileDestinations[key] = true
			displayItems = append(displayItems, RenameItem{OldPath: asset.Path, NewPath: destination, Kind: "file", Status: "conflict"})
			continue
		}
		seenDest[key] = true
		if filepath.Base(asset.Path) == filepath.Base(destination) && !requiresExplicitMove {
			displayItems = append(displayItems, RenameItem{OldPath: asset.Path, NewPath: destination, Kind: "file", Status: "same"})
		} else {
			requests = append(requests, rename.RenameRequest{Source: asset.Path, Destination: destination})
		}
	}
	if len(requests) == 0 {
		// BuildPlan intentionally filters exact no-ops. Supplying one here gives
		// the GUI a stable plan ID while displayItems explains why no filesystem
		// operation is required.
		requests = append(requests, rename.RenameRequest{Source: root, Destination: root})
	}
	planRoot := parent
	if rootIsDir && seasonLayout.SeriesRoot {
		// The series container itself stays in place, so it is the narrowest
		// safety scope. This also keeps staging/journal files beside the selected
		// series instead of one directory above its media parent.
		planRoot = root
	}
	plan, err := rename.BuildPlan(requests, rename.PlanOptions{Root: planRoot, CreateDirectories: createDirectories})
	if err != nil {
		return RenamePlan{}, err
	}
	report := rename.Preflight(plan)
	for _, issue := range report.Issues {
		warnings = append(warnings, issue.Code+": "+issue.Message)
	}
	result := a.toRenamePlan(plan, warnings, report, displayItems)
	for index := range result.Items {
		item := &result.Items[index]
		if item.Kind == "file" && conflictingFileDestinations[appPathKey(item.NewPath)] {
			item.Status = "conflict"
		}
	}
	for _, message := range planErrors {
		appendUnique(&result.Errors, message)
	}
	result.CanApply = result.ChangeCount > 0 && len(result.Errors) == 0
	a.mu.Lock()
	a.plan = plan
	a.planID = result.ID
	a.planCanApply = result.CanApply
	a.mu.Unlock()
	return result, nil
}

func (a *App) ApplyRename(plan RenamePlan) error {
	a.mu.Lock()
	internalPlan := a.plan
	knownID := a.planID
	canApply := a.planCanApply
	previousJournal := a.journal
	previousJournalAttention := a.journalAttention
	a.mu.Unlock()
	if previousJournalAttention || journalNeedsAttention(previousJournal) {
		return fmt.Errorf("the previous transaction still requires Undo cleanup")
	}
	if plan.ID == "" || plan.ID != knownID || internalPlan.ID != knownID {
		return fmt.Errorf("rename plan is stale; refresh the preview")
	}
	if len(internalPlan.Operations) == 0 && len(internalPlan.Directories) == 0 {
		return fmt.Errorf("nothing to rename; existing names are already correct")
	}
	if !canApply {
		return fmt.Errorf("rename plan has unresolved errors; refresh the preview after fixing them")
	}
	journal, err := rename.Apply(a.context(), internalPlan, rename.ApplyOptions{})
	if journal != nil && journal.State != rename.JournalRolledBack {
		a.mu.Lock()
		a.journal = journal
		a.journalAttention = err != nil || journalNeedsAttention(journal)
		a.mu.Unlock()
	}
	if err != nil {
		return err
	}
	return nil
}

func (a *App) UndoRename() error {
	a.mu.Lock()
	journal := a.journal
	a.mu.Unlock()
	if journal == nil || journal.Path == "" {
		return fmt.Errorf("there is no completed rename transaction to undo")
	}
	if err := rename.Undo(a.context(), journal.Path); err != nil {
		activeJournal := journal
		if loaded, loadErr := rename.LoadJournal(journal.Path); loadErr == nil {
			activeJournal = loaded
		}
		a.mu.Lock()
		a.journal = activeJournal
		// The operation outcome is authoritative here. A journal reload can
		// legitimately still say "applied" when Undo failed before its next
		// durable state write.
		a.journalAttention = true
		a.mu.Unlock()
		return err
	}
	a.mu.Lock()
	a.journal = nil
	a.journalAttention = false
	a.mu.Unlock()
	return nil
}

// HasUndoJournal lets the GUI keep a retryable Undo visible after a partial
// cleanup error without guessing from an error string. Fully rolled-back
// journals need no action even though their audit file remains on disk.
func (a *App) HasUndoJournal() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.journal != nil && a.journal.Path != "" && (a.journalAttention || a.journal.State != rename.JournalRolledBack)
}

// UndoNeedsAttention distinguishes a normal applied journal from a failed or
// interrupted transaction which must not be overwritten by a new Apply.
func (a *App) UndoNeedsAttention() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.journalAttention || journalNeedsAttention(a.journal)
}

func journalNeedsAttention(journal *rename.Journal) bool {
	if journal == nil {
		return false
	}
	switch journal.State {
	case rename.JournalApplied, rename.JournalRolledBack:
		return false
	default:
		return true
	}
}

func (a *App) SearchMovie(query string) ([]SearchResult, error) {
	settings := a.currentSettings()
	results, err := providers.NewTMDBClient(settings.TMDBAPIKey).SearchMovies(a.context(), query, "")
	if err != nil {
		return nil, err
	}
	return providerResults(results), nil
}

func (a *App) SearchTV(query string) ([]SearchResult, error) {
	settings := a.currentSettings()
	results, err := providers.NewTVDBClient(settings.TVDBAPIKey, settings.TVDBPIN).SearchSeries(a.context(), query)
	if err != nil {
		return nil, err
	}
	return providerResults(results), nil
}

// ResolveTVSeries fetches one selected TVDB series' English translation. It
// is intentionally separate from SearchTV so a search does not issue one
// translation request per result and hit TVDB rate limits.
func (a *App) ResolveTVSeries(id string) (SearchResult, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return SearchResult{}, fmt.Errorf("TVDB series id is empty")
	}
	settings := a.currentSettings()
	result, err := providers.NewTVDBClient(settings.TVDBAPIKey, settings.TVDBPIN).Series(a.context(), id)
	if err != nil {
		return SearchResult{}, err
	}
	return SearchResult{ID: result.ID, Title: result.Title, OriginalTitle: result.Original, Year: result.Year, Overview: result.Overview, PosterURL: result.PosterPath}, nil
}

func (a *App) toScanResult(result api.ScanResult) ScanResult {
	fallbackGroup := strings.TrimSpace(a.currentSettings().ReleaseGroup)
	if fallbackGroup == "" {
		fallbackGroup = "NoGroup"
	}
	out := ScanResult{RootPath: result.Root, Files: []ScanFile{}, Seasons: []string{}, Warnings: []string{}, Metadata: TechnicalMetadata{MediaType: "movie", Group: fallbackGroup}}
	for _, warning := range result.Warnings {
		appendUnique(&out.Warnings, warning)
	}
	for _, asset := range result.Assets {
		kind := "video"
		out.Files = append(out.Files, ScanFile{Path: asset.Path, Kind: kind, Size: asset.Size})
		for _, warning := range asset.Warnings {
			// Include the asset basename so a season scan can identify which
			// file fell back to filename hints or failed MediaInfo extraction.
			message := warning
			if base := filepath.Base(asset.Path); base != "" {
				message = base + ": " + warning
			}
			appendUnique(&out.Warnings, message)
		}
		if out.Metadata.Title == "" {
			out.Metadata = contentToDTO(asset.Content, asset.Technical)
			// A detected third-party group is provenance, not permission to render
			// a different convention under that group's name. Exact preservation
			// retains it from the basename; VMF rendering uses the local fallback.
			out.Metadata.Group = fallbackGroup
			if asset.Technical.RawJSON != "" {
				var raw any
				if json.Unmarshal([]byte(asset.Technical.RawJSON), &raw) == nil {
					out.MediaInfoJSON = raw
				}
			}
		}
		if out.MediaInfoText == "" {
			if asset.Technical.RawText != "" {
				out.MediaInfoText = asset.Technical.RawText
			} else {
				out.MediaInfoText = asset.Technical.RawJSON
			}
		}
	}
	if out.Metadata.MediaType == "" {
		out.Metadata.MediaType = "movie"
	}
	out.MediaType = out.Metadata.MediaType
	layout := analyzeTVSeasonLayout(result.Root, result.Assets)
	if out.Metadata.MediaType == "tv" || len(layout.Seasons) > 0 {
		out.Metadata.MediaType = "tv"
		out.MediaType = "tv"
		out.Seasons = append(out.Seasons, layout.Seasons...)
		out.SeriesRoot = layout.SeriesRoot
		out.SeasonFolderCount = len(layout.Directories)
		chosenSeason, chosenEpisode, chosenEpisodeTitle := "", "", ""
		for _, asset := range result.Assets {
			assetKey := appPathKey(asset.Path)
			season, episode := layout.AssetSeason[assetKey], layout.AssetEpisode[assetKey]
			if season != "" && episode != "" {
				chosenSeason, chosenEpisode, chosenEpisodeTitle = season, episode, asset.Content.EpisodeTitle
				break
			}
			if chosenSeason == "" && chosenEpisode == "" && (season != "" || episode != "") {
				chosenSeason, chosenEpisode, chosenEpisodeTitle = season, episode, asset.Content.EpisodeTitle
			}
		}
		if chosenSeason != "" || chosenEpisode != "" {
			out.Metadata.Season = chosenSeason
			out.Metadata.Episode = chosenEpisode
			out.Metadata.EpisodeTitle = chosenEpisodeTitle
		}
		for _, message := range layout.ValidationErrors {
			appendUnique(&out.Warnings, "season layout: "+message)
		}
		for _, message := range tvSeasonLayoutWarnings(layout) {
			appendUnique(&out.Warnings, message)
		}
	}
	// Folder-level metadata drives the live suggested basename. Keep it aligned
	// with PreviewRename: a pack is UHD only when every source video basename
	// carries the explicit marker. A single selected file naturally follows
	// its own marker through the same rule.
	out.Metadata.UHD = allVideoAssetsHaveExplicitUHD(result.Assets)
	return out
}

func appendUnique(values *[]string, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	for _, existing := range *values {
		if existing == value {
			return
		}
	}
	*values = append(*values, value)
}

func (a *App) toRenamePlan(plan rename.Plan, warnings []string, report rename.ValidationReport, displayItems []RenameItem) RenamePlan {
	out := RenamePlan{ID: plan.ID, Items: []RenameItem{}, ChangeCount: len(plan.Operations) + len(plan.Directories), Warnings: append([]string{}, warnings...), Errors: []string{}}
	conflictingDirectories := map[string]bool{}
	for _, issue := range report.Issues {
		out.Errors = append(out.Errors, issue.Code+": "+issue.Message)
		if issue.Destination != "" {
			conflictingDirectories[appPathKey(issue.Destination)] = true
		}
	}
	for _, directory := range plan.Directories {
		status := "create"
		if conflictingDirectories[appPathKey(directory.Path)] {
			status = "conflict"
		}
		out.Items = append(out.Items, RenameItem{OldPath: "", NewPath: directory.Path, Kind: "folder", Status: status})
	}
	for _, op := range plan.Operations {
		kind := string(op.Kind)
		if op.Kind == rename.KindDir {
			kind = "folder"
		}
		out.Items = append(out.Items, RenameItem{OldPath: op.Source, NewPath: op.Destination, Kind: kind, Status: "ready"})
	}
	out.Items = append(out.Items, displayItems...)
	out.CanApply = out.ChangeCount > 0 && len(out.Errors) == 0
	return out
}

func toNamingMetadata(value TechnicalMetadata) naming.Metadata {
	year, _ := strconv.Atoi(strings.TrimSpace(value.Year))
	category := strings.ToUpper(strings.TrimSpace(value.MediaType))
	if category == "TV" {
		category = naming.TV
	} else {
		category = naming.Movie
	}
	tracks := []naming.AudioTrack{}
	for _, language := range strings.Split(value.Languages, ",") {
		language = strings.TrimSpace(language)
		if language != "" {
			tracks = append(tracks, naming.AudioTrack{Language: language})
		}
	}
	return naming.Metadata{Category: category, ReleaseType: strings.ToUpper(strings.TrimSpace(value.ReleaseType)), Title: value.Title, OriginalTitle: value.OriginalTitle, Year: year, Season: value.Season, Episode: value.Episode, EpisodeTitle: value.EpisodeTitle, Edition: value.Edition, Resolution: value.Resolution, Source: value.Source, Service: value.Service, HDR: value.HDR, VideoCodec: value.VideoCodec, VideoEncode: value.VideoEncode, Audio: value.Audio, AudioTracks: tracks, Group: value.Group}
}

func contentToDTO(content api.ContentInfo, technical api.TechnicalInfo) TechnicalMetadata {
	langs := []string{}
	for _, track := range technical.Tracks {
		if strings.EqualFold(track.Type, "Audio") && track.Language != "" {
			langs = append(langs, track.Language)
		}
	}
	return TechnicalMetadata{MediaType: strings.ToLower(content.Category), Title: content.Title, OriginalTitle: content.OriginalTitle, Year: content.Year, Season: content.Season, Episode: content.Episode, EpisodeTitle: content.EpisodeTitle, Edition: content.Edition, Resolution: content.Resolution, Source: content.Source, Service: content.Service, ReleaseType: normalizeDTOType(content.ReleaseType), VideoCodec: content.VideoCodec, VideoEncode: content.VideoEncode, HDR: content.HDR, Audio: content.Audio, Languages: strings.Join(langs, ","), Group: content.ReleaseGroup, UHD: strings.EqualFold(strings.TrimSpace(content.UHD), "UHD")}
}

func mergeAssetMetadata(base naming.Metadata, asset api.Asset) naming.Metadata {
	meta := base
	if meta.Title == "" {
		meta.Title = asset.Content.Title
	}
	if meta.Season == "" {
		meta.Season = asset.Content.Season
	}
	if meta.Episode == "" {
		meta.Episode = asset.Content.Episode
	}
	if meta.EpisodeTitle == "" {
		meta.EpisodeTitle = asset.Content.EpisodeTitle
	}
	if meta.Source == "" {
		meta.Source = asset.Content.Source
	}
	if meta.Service == "" {
		meta.Service = asset.Content.Service
	}
	if meta.ReleaseType == "" {
		meta.ReleaseType = asset.Content.ReleaseType
	}
	if meta.Resolution == "" {
		meta.Resolution = asset.Content.Resolution
	}
	if meta.VideoCodec == "" {
		meta.VideoCodec = asset.Content.VideoCodec
	}
	if meta.VideoEncode == "" {
		meta.VideoEncode = asset.Content.VideoEncode
	}
	if meta.HDR == "" {
		meta.HDR = asset.Content.HDR
	}
	if meta.Audio == "" {
		meta.Audio = asset.Content.Audio
	}
	// Override the folder/request value so a UHD marker from one episode cannot
	// leak into another episode that did not carry it in its original basename.
	meta.UHD = assetHasExplicitUHD(asset)
	meta.ExistingName = asset.Name
	for _, track := range asset.Technical.Tracks {
		if strings.EqualFold(track.Type, "Audio") {
			meta.AudioTracks = append(meta.AudioTracks, naming.AudioTrack{Language: track.Language, Title: track.Title, Codec: track.Codec, Channels: track.Channels, Main: track.Default, Atmos: track.Atmos})
		}
	}
	return meta
}

// preferAssetTechnicalMetadata prevents the first scanned episode from
// becoming the technical template for every later season. Identity/provider
// fields and the user's output group remain pack-level, while facts proven by
// each asset's filename or MediaInfo take precedence when available.
func preferAssetTechnicalMetadata(meta naming.Metadata, asset api.Asset, overrides map[string]bool) naming.Metadata {
	content := asset.Content
	scanSucceeded := strings.TrimSpace(asset.Technical.RawJSON) != ""
	if !overrides["source"] {
		// An empty value is meaningful provenance: without filename evidence,
		// do not reuse the source parsed from the first scanned episode.
		meta.Source = content.Source
	}
	// A service is filename evidence only. Clearing it here is intentional so
	// NF/AMZN from the first episode cannot leak into an untagged episode.
	if !overrides["service"] {
		meta.Service = content.Service
	}
	if !overrides["releasetype"] {
		// ParseFilename uses ENCODE as a generic fallback, so accept the parsed
		// type only when the basename actually contains release-type evidence.
		meta.ReleaseType = ""
		if hasReleaseTypeEvidence(asset.Name) && content.ReleaseType != "" {
			meta.ReleaseType = content.ReleaseType
		}
	}
	if !overrides["resolution"] && (scanSucceeded || content.Resolution != "") {
		meta.Resolution = content.Resolution
	}
	if !overrides["videocodec"] && (scanSucceeded || content.VideoCodec != "") {
		meta.VideoCodec = content.VideoCodec
	}
	if !overrides["videoencode"] {
		if overrides["videocodec"] {
			meta.VideoEncode = ""
		} else if scanSucceeded || content.VideoCodec != "" {
			meta.VideoEncode = content.VideoEncode
		}
	}
	if !overrides["hdr"] && (scanSucceeded || content.HDR != "") {
		meta.HDR = content.HDR
	}
	if !overrides["audio"] && (scanSucceeded || content.Audio != "") {
		meta.Audio = content.Audio
	}

	assetTracks := []naming.AudioTrack{}
	for _, track := range asset.Technical.Tracks {
		if strings.EqualFold(track.Type, "Audio") {
			assetTracks = append(assetTracks, naming.AudioTrack{Language: track.Language, Title: track.Title, Codec: track.Codec, Channels: track.Channels, Main: track.Default, Atmos: track.Atmos})
		}
	}
	if !overrides["languages"] && (scanSucceeded || len(assetTracks) > 0) {
		meta.AudioTracks = assetTracks
	} else if overrides["languages"] {
		requestedTracks := []naming.AudioTrack{}
		for _, track := range meta.AudioTracks {
			if track.Codec == "" && track.Channels == "" && track.Title == "" {
				requestedTracks = append(requestedTracks, track)
			}
		}
		meta.AudioTracks = requestedTracks
	}
	return meta
}

// seasonTechnicalConflict prevents a season pack from advertising only the
// first episode's technical signature when the files would render different
// folder names. Explicit UI overrides are honored before signatures are
// compared, and UHD uses the pack-level all-files rule for every candidate.
func seasonTechnicalConflict(base naming.Metadata, season string, assets []api.Asset, uhd bool, overrides map[string]bool) string {
	if len(assets) < 2 {
		return ""
	}
	type observedValue struct {
		value string
		asset string
	}
	observed := map[string]observedValue{}
	for _, asset := range assets {
		meta := mergeAssetMetadata(base, asset)
		meta = preferAssetTechnicalMetadata(meta, asset, overrides)
		meta.Season = season
		meta.Episode = ""
		meta.EpisodeTitle = ""
		meta.UHD = uhd
		assetName := filepath.Base(asset.Path)
		for _, field := range seasonTechnicalFields(meta) {
			if strings.TrimSpace(field.value) == "" {
				continue
			}
			key := technicalComparisonKey(field.value)
			if previous, ok := observed[field.name]; ok && previous.value != key {
				return fmt.Sprintf(
					"season layout: S%s contains mixed technical metadata (%s) between %s and %s; split the release or override that field",
					season,
					field.name,
					previous.asset,
					assetName,
				)
			}
			if _, ok := observed[field.name]; !ok {
				observed[field.name] = observedValue{value: key, asset: assetName}
			}
		}
	}
	return ""
}

type seasonTechnicalField struct {
	name  string
	value string
}

func seasonTechnicalFields(meta naming.Metadata) []seasonTechnicalField {
	return []seasonTechnicalField{
		{"release type", meta.ReleaseType},
		{"source", meta.Source},
		{"service", meta.Service},
		{"resolution", meta.Resolution},
		{"HDR", meta.HDR},
		{"video codec", meta.VideoCodec},
		{"video encoder", meta.VideoEncode},
		{"audio", meta.Audio},
	}
}

func technicalComparisonKey(value string) string {
	compact := strings.ToUpper(strings.NewReplacer(" ", "", ".", "", "-", "", "_", "").Replace(strings.TrimSpace(value)))
	switch compact {
	case "AVC", "H264", "X264":
		return "H264"
	case "HEVC", "H265", "X265":
		return "H265"
	}
	return compact
}

func seasonRepresentativeAsset(assets []api.Asset) api.Asset {
	best := assets[0]
	bestScore := seasonAssetEvidenceScore(best)
	for _, asset := range assets[1:] {
		score := seasonAssetEvidenceScore(asset)
		if score > bestScore || score == bestScore && appPathKey(asset.Path) < appPathKey(best.Path) {
			best, bestScore = asset, score
		}
	}
	return best
}

func seasonAssetEvidenceScore(asset api.Asset) int {
	score := 0
	for _, value := range []string{
		asset.Content.Source,
		asset.Content.Service,
		asset.Content.Resolution,
		asset.Content.VideoCodec,
		asset.Content.VideoEncode,
		asset.Content.HDR,
		asset.Content.Audio,
	} {
		if strings.TrimSpace(value) != "" {
			score++
		}
	}
	if hasReleaseTypeEvidence(asset.Name) {
		score += 2
	}
	if strings.TrimSpace(asset.Technical.RawJSON) != "" {
		score += 4
	}
	return score
}

// applyConservativeSeasonTags keeps pack-level optional tags only when every
// episode supports the same claim. A single NF/ViE/DUB episode must not label
// an otherwise untagged season; explicit UI overrides remain authoritative.
func applyConservativeSeasonTags(folder, base naming.Metadata, assets []api.Asset, overrides map[string]bool) naming.Metadata {
	if len(assets) == 0 {
		return folder
	}
	if !overrides["service"] {
		commonService := ""
		for index, asset := range assets {
			meta := preferAssetTechnicalMetadata(mergeAssetMetadata(base, asset), asset, overrides)
			service := strings.TrimSpace(meta.Service)
			if service == "" || index > 0 && !strings.EqualFold(service, commonService) {
				commonService = ""
				break
			}
			commonService = service
		}
		folder.Service = commonService
	}
	if !overrides["languages"] {
		commonTag := ""
		for index, asset := range assets {
			meta := preferAssetTechnicalMetadata(mergeAssetMetadata(base, asset), asset, overrides)
			tag := strings.TrimSpace(naming.VMFTag(meta))
			if tag == "" || index > 0 && !strings.EqualFold(tag, commonTag) {
				commonTag = ""
				break
			}
			commonTag = tag
		}
		if commonTag == "" {
			folder.AudioTracks = nil
			folder.ExistingName = ""
		}
	}
	return folder
}

func makeMetadataOverrideSet(values []string) map[string]bool {
	result := map[string]bool{}
	for _, value := range values {
		if value = strings.ToLower(strings.TrimSpace(value)); value != "" {
			result[value] = true
		}
	}
	return result
}

func hasReleaseTypeEvidence(filename string) bool {
	lower := strings.ToLower(filename)
	for _, marker := range []string{"web-dl", "webdl", "web.dl", "webrip", "web-rip", "remux", "bluray", "blu-ray", "bdrip", "brrip", "hdtv"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func assetHasExplicitUHD(asset api.Asset) bool {
	return strings.EqualFold(strings.TrimSpace(asset.Content.UHD), "UHD")
}

func allVideoAssetsHaveExplicitUHD(assets []api.Asset) bool {
	foundVideo := false
	for _, asset := range assets {
		if !isVideoAsset(asset) {
			continue
		}
		foundVideo = true
		if !assetHasExplicitUHD(asset) {
			return false
		}
	}
	return foundVideo
}

func appendNamingWarnings(values []string, warnings []naming.Warning) []string {
	for _, warning := range warnings {
		values = append(values, warning.Code+": "+warning.Message)
	}
	return values
}

func isVideoAsset(asset api.Asset) bool {
	ext := strings.ToLower(filepath.Ext(asset.Path))
	switch ext {
	case ".mkv", ".mp4", ".m4v", ".ts", ".m2ts", ".mov", ".avi", ".webm":
		return true
	}
	return ext == ""
}

func normalizeDTOType(value string) string {
	switch strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(value, "-", ""), " ", "")) {
	case "WEBDL":
		return "WEB-DL"
	case "WEBRIP":
		return "WEBRip"
	case "REMUX":
		return "REMUX"
	case "ENCODE":
		return "ENCODE"
	default:
		return value
	}
}

func providerResults(values []api.ProviderCandidate) []SearchResult {
	result := make([]SearchResult, 0, len(values))
	for _, value := range values {
		result = append(result, SearchResult{ID: value.ID, Title: value.Title, OriginalTitle: value.Original, Year: value.Year, Overview: value.Overview, PosterURL: value.PosterPath})
	}
	return result
}

func (a *App) currentSettings() api.Settings {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.settings
}

func settingsToDTO(value api.Settings) Settings {
	preserveExistingP2P := value.PreserveExistingP2P
	return Settings{
		Separator:           value.Separator,
		Group:               value.ReleaseGroup,
		PreserveExistingP2P: &preserveExistingP2P,
		IncludeUHD:          value.IncludeUHD,
		Profile:             value.Profile,
		TMDBAPIKey:          value.TMDBAPIKey,
		TVDBAPIKey:          value.TVDBAPIKey,
		TVDBPIN:             value.TVDBPIN,
		MediaInfoBin:        value.MediaInfoBin,
	}
}

func settingsFromDTO(value Settings) api.Settings {
	preserveExistingP2P := true
	if value.PreserveExistingP2P != nil {
		preserveExistingP2P = *value.PreserveExistingP2P
	}
	return api.Settings{
		Separator:           value.Separator,
		ReleaseGroup:        value.Group,
		PreserveExistingP2P: preserveExistingP2P,
		IncludeUHD:          value.IncludeUHD,
		Profile:             value.Profile,
		TMDBAPIKey:          value.TMDBAPIKey,
		TVDBAPIKey:          value.TVDBAPIKey,
		TVDBPIN:             value.TVDBPIN,
		MediaInfoBin:        value.MediaInfoBin,
	}
}
