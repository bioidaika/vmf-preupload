import { useRef, useState, type DragEvent } from 'react'
import { formatReleaseName, useRenamer } from './useRenamer'
import type { MediaType, RenameItem, RenamePlan, ScanResult, SearchResult, TechnicalMetadata } from './types'
import tmdbLogo from './assets/tmdb.svg'
import './styles.css'

type IconName =
  | 'archive'
  | 'folder'
  | 'file'
  | 'scan'
  | 'film'
  | 'tv'
  | 'sliders'
  | 'history'
  | 'search'
  | 'chevron'
  | 'check'
  | 'alert'
  | 'undo'
  | 'arrow'
  | 'spark'
  | 'external'

function Icon({ name, size = 18 }: { name: IconName; size?: number }) {
  const paths: Record<IconName, JSX.Element> = {
    archive: <><path d="M4 7.5h16"/><path d="M5.5 4h13l1.5 3.5h-16L5.5 4Z"/><path d="M6 7.5v11A1.5 1.5 0 0 0 7.5 20h9a1.5 1.5 0 0 0 1.5-1.5v-11"/><path d="M10 12h4"/></>,
    folder: <><path d="M3.5 6.5A1.5 1.5 0 0 1 5 5h4l2 2h8A1.5 1.5 0 0 1 20.5 8.5v8A1.5 1.5 0 0 1 19 18H5a1.5 1.5 0 0 1-1.5-1.5v-10Z"/><path d="M3.8 9h16.4"/></>,
    file: <><path d="M6 3.5h7l5 5V20H6z"/><path d="M13 3.5V9h5"/><path d="M9 13h6M9 16h4"/></>,
    scan: <><circle cx="10.5" cy="10.5" r="6.5"/><path d="m16 16 4 4"/><path d="M8 10.5h5M10.5 8v5"/></>,
    film: <><rect x="3.5" y="4.5" width="17" height="15" rx="2"/><path d="M8 4.5v15M16 4.5v15M3.5 9.5h4M16 9.5h4M3.5 14.5h4M16 14.5h4"/></>,
    tv: <><rect x="3.5" y="5" width="17" height="13" rx="2"/><path d="m9 2.5 3 2.5 3-2.5M8.5 21.5h7M12 18v3.5"/></>,
    sliders: <><path d="M4 6h16M4 12h16M4 18h16"/><circle cx="9" cy="6" r="2"/><circle cx="15" cy="12" r="2"/><circle cx="11" cy="18" r="2"/></>,
    history: <><path d="M4 11a8 8 0 1 0 2.3-5.6L4 7.7"/><path d="M4 4v3.7h3.7M12 8v4l2.5 1.5"/></>,
    search: <><circle cx="10.5" cy="10.5" r="6.5"/><path d="m16 16 4 4"/></>,
    chevron: <path d="m8 10 4 4 4-4"/>,
    check: <path d="m5 12 4 4L19 6"/>,
    alert: <><path d="M12 3.5 21 20H3l9-16.5Z"/><path d="M12 9v4.5M12 17h.01"/></>,
    undo: <><path d="M9 7 4 11.5 9 16"/><path d="M5 11.5h8a6 6 0 0 1 6 6v1"/></>,
    arrow: <><path d="M4 12h15"/><path d="m14 7 5 5-5 5"/></>,
    spark: <><path d="m12 3 1.5 5.5L19 10l-5.5 1.5L12 17l-1.5-5.5L5 10l5.5-1.5L12 3Z"/><path d="m19 16 .6 2.4L22 19l-2.4.6L19 22l-.6-2.4L16 19l2.4-.6L19 16Z"/></>,
    external: <><path d="M14 4h6v6M20 4l-9 9"/><path d="M18 13.5v4A1.5 1.5 0 0 1 16.5 19h-11A1.5 1.5 0 0 1 4 17.5v-11A1.5 1.5 0 0 1 5.5 5h4"/></>,
  }
  return <svg className="icon" width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">{paths[name]}</svg>
}

function Field({
  label,
  value,
  onChange,
  placeholder,
  hint,
  className = '',
  type = 'text',
}: {
  label: string
  value: string
  onChange: (value: string) => void
  placeholder?: string
  hint?: string
  className?: string
  type?: 'text' | 'password'
}) {
  return <label className={`field ${className}`}>
    <span className="field-label">{label}</span>
    <input type={type} value={value} onChange={(event) => onChange(event.target.value)} placeholder={placeholder} autoComplete="off" />
    {hint && <span className="field-hint">{hint}</span>}
  </label>
}

function StatusBadge({ status }: { status?: RenameItem['status'] }) {
  if (status === 'preserved') return <span className="status-badge preserved"><Icon name="archive" size={13} /> Preserved P2P</span>
  if (status === 'same') return <span className="status-badge neutral"><Icon name="check" size={13} /> Same</span>
  if (status === 'conflict') return <span className="status-badge danger"><Icon name="alert" size={13} /> Conflict</span>
  return <span className="status-badge ready"><Icon name="check" size={13} /> Ready</span>
}

function pathKey(path: string): string {
  return path.replace(/\//g, '\\').replace(/[\\]+$/, '').toLowerCase()
}

function parentLabel(path: string): string {
  const index = Math.max(path.lastIndexOf('\\'), path.lastIndexOf('/'))
  return index > 0 ? path.slice(0, index) : ''
}

function SearchBox({ mediaType, onSearch, onPick }: { mediaType: MediaType; onSearch: (query: string) => Promise<SearchResult[]>; onPick: (result: SearchResult) => void | Promise<void> }) {
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<SearchResult[]>([])
  const [searching, setSearching] = useState(false)

  const submit = async () => {
    if (!query.trim()) return
    setSearching(true)
    try {
      setResults(await onSearch(query))
    } finally {
      setSearching(false)
    }
  }

  return <div className="search-box">
    <div className="search-input-wrap">
      <Icon name="search" size={16} />
      <input value={query} onChange={(event) => setQuery(event.target.value)} onKeyDown={(event) => event.key === 'Enter' && void submit()} placeholder={`Search ${mediaType === 'movie' ? 'TMDB movies' : 'TVDB series'}…`} />
      <button className="search-submit" onClick={() => void submit()} disabled={searching}>{searching ? '…' : 'Search'}</button>
    </div>
    {results.length > 0 && <div className="search-results">
      {results.slice(0, 4).map((result) => <button key={result.id} className="search-result" onClick={() => { void onPick(result); setResults([]) }}>
        <span className="result-title">{result.title}</span><span className="result-year">{result.year ?? '—'}</span>
      </button>)}
    </div>}
  </div>
}

export default function App() {
  const renamer = useRenamer()
  const fileInput = useRef<HTMLInputElement>(null)
  const folderInput = useRef<HTMLInputElement>(null)
  const [activeTab, setActiveTab] = useState<'rename' | 'mediainfo' | 'history'>('rename')
  const [dragging, setDragging] = useState(false)

  const metadata = renamer.metadata
  const settings = renamer.settings
  const isSeriesContainer = metadata.mediaType === 'tv' && renamer.scan?.seriesRoot === true
  const selectedItem = renamer.plan?.items.find((item) => pathKey(item.oldPath) === pathKey(renamer.selectedPath))
  const liveName = selectedItem
    ? selectedItem.newPath.split(/[\\/]/).pop() ?? selectedItem.newPath
    : isSeriesContainer
      ? renamer.selectedPath.split(/[\\/]/).pop() ?? renamer.selectedPath
      : `${formatReleaseName(metadata, settings)}.mkv`
  const suggestedLabel = isSeriesContainer ? 'Series container (unchanged)' : 'Suggested basename'
  const planHasErrors = !!renamer.plan?.errors?.length
  const planHasPreservedItems = renamer.plan?.items.some((item) => item.status === 'preserved') ?? false

  const update = (patch: Partial<TechnicalMetadata>) => renamer.updateMetadata(patch)

  const pickFiles = (files: FileList | null) => {
    if (!files?.length) return
    const first = files[0]
    const path = (first as File & { webkitRelativePath?: string }).webkitRelativePath || first.name
    void renamer.scanPath(path)
  }

  const onDrop = (event: DragEvent<HTMLDivElement>) => {
    event.preventDefault()
    setDragging(false)
    pickFiles(event.dataTransfer.files)
  }

  const chooseResult = async (result: SearchResult) => {
    let selected = result
    if (metadata.mediaType === 'tv') {
      const resolved = await renamer.resolveTVSeries(result.id)
      if (resolved) selected = resolved
    }
    const year = selected.year ?? metadata.year
    update({ title: selected.title || selected.originalTitle || metadata.title, originalTitle: selected.originalTitle, year })
  }

  return <div className="app-shell">
    <header className="topbar">
      <div className="brand"><span className="brand-mark"><Icon name="archive" size={21} /></span><span>VMF <b>PREUPLOAD</b></span></div>
      <div className="topbar-meta"><span className={`bridge-pill ${renamer.bridgeConnected ? 'online' : ''}`}><i />{renamer.bridgeConnected ? 'Wails connected' : 'Browser preview'}</span><span className="version">v0.1.0</span></div>
    </header>

    <div className="workspace">
      <aside className="sidebar">
        <div className="sidebar-label">WORKSPACE</div>
        <button className={`nav-item ${activeTab === 'rename' ? 'active' : ''}`} onClick={() => setActiveTab('rename')}><Icon name="scan" /><span>Rename media</span><kbd>1</kbd></button>
        <button className={`nav-item ${activeTab === 'mediainfo' ? 'active' : ''}`} onClick={() => setActiveTab('mediainfo')}><Icon name="film" /><span>MediaInfo</span><kbd>2</kbd></button>
        <button className={`nav-item ${activeTab === 'history' ? 'active' : ''}`} onClick={() => setActiveTab('history')}><Icon name="history" /><span>History</span><kbd>3</kbd></button>
        <div className="sidebar-spacer" />
        <div className="profile-card"><span className="profile-dot">N</span><div><strong>Local profile</strong><small>{settings.group}</small></div><Icon name="chevron" size={14} /></div>
      </aside>

      <main className="main-content">
        {activeTab === 'mediainfo' ? <MediaInfoView scan={renamer.scan} /> : activeTab === 'history' ? <HistoryView plan={renamer.plan} /> : <>
          <div className="page-heading"><div><div className="eyebrow">RENAME WORKSPACE</div><h1>Prepare a release</h1><p>Match metadata, review technical details, then rename without touching your files until you approve.</p></div><button className="button ghost" onClick={() => void renamer.preview()} disabled={renamer.busy}><Icon name="spark" size={16} /> Refresh preview</button></div>

          <section className={`drop-zone ${dragging ? 'dragging' : ''}`} onDragOver={(event) => { event.preventDefault(); setDragging(true) }} onDragLeave={() => setDragging(false)} onDrop={onDrop}>
            <div className="drop-icon"><Icon name="folder" size={25} /></div>
            <div className="drop-copy"><strong>{renamer.scan?.rootPath ? 'Media selected' : 'Drop a movie or TV folder here'}</strong><span>{renamer.selectedPath || 'Choose a video file or a complete folder to start scanning.'}</span></div>
            <div className="drop-actions"><button className="button secondary" onClick={() => renamer.bridgeConnected ? void renamer.chooseFile() : fileInput.current?.click()}><Icon name="file" size={15} /> Choose file</button><button className="button secondary" onClick={() => renamer.bridgeConnected ? void renamer.chooseFolder() : folderInput.current?.click()}><Icon name="folder" size={15} /> Choose folder</button></div>
            <input ref={fileInput} className="visually-hidden" type="file" accept="video/*,.mkv,.mp4,.m4v,.ts" onChange={(event) => pickFiles(event.target.files)} />
            <input ref={folderInput} className="visually-hidden" type="file" /* Chromium supports directory selection via these non-standard attributes. */ {...{ webkitdirectory: '', directory: '' }} onChange={(event) => pickFiles(event.target.files)} />
          </section>

          <div className="content-grid">
            <div className="left-column">
              <section className="panel settings-panel"><div className="panel-heading"><div><span className="panel-kicker">PROFILE</span><h2>Naming profile</h2></div><span className="profile-version">{settings.profile}</span></div><div className="profile-line"><span className="profile-icon"><Icon name="sliders" size={17} /></span><div><strong>VMF compatible</strong><small>Dot-separated P2P naming · optional source tags</small></div><span className="active-label"><i /> Active</span></div><div className="settings-controls"><label className="field compact"><span className="field-label">Separator</span><select value={settings.separator} onChange={(event) => renamer.updateSettings({ separator: event.target.value })}><option value=".">Dot&nbsp; ·  Title.Year</option><option value=" ">Space&nbsp; ·  Title Year</option></select></label><Field label="Release group" value={settings.group} onChange={(value) => { renamer.updateSettings({ group: value }); update({ group: value }) }} hint="Fallback when no group is present in the filename" /></div><label className="preserve-policy"><input type="checkbox" checked={settings.preserveExistingP2P} onChange={(event) => renamer.updateSettings({ preserveExistingP2P: event.target.checked })} /><span><strong>Preserve existing P2P releases</strong><small>Keep a high-confidence grouped release basename exactly as published. Turn this off to force VMF rendering.</small></span></label><div className="uhd-policy"><strong>UHD requires an original tag</strong><small>UHD is preserved only when the original filename contains a standalone UHD or Ultra HD marker.</small></div><div className="provider-settings"><div className="provider-settings-heading"><strong>Provider and MediaInfo settings</strong><small>Keys stay local and are only used for search.</small></div><div className="field-grid"><Field label="TMDB API key" type="password" value={settings.tmdbApiKey} onChange={(value) => renamer.updateSettings({ tmdbApiKey: value })} /><Field label="TVDB API key" type="password" value={settings.tvdbApiKey} onChange={(value) => renamer.updateSettings({ tvdbApiKey: value })} /><Field label="TVDB PIN" type="password" value={settings.tvdbPin} onChange={(value) => renamer.updateSettings({ tvdbPin: value })} /><Field label="MediaInfo executable" value={settings.mediaInfoBin} onChange={(value) => renamer.updateSettings({ mediaInfoBin: value })} placeholder="Auto-detect or full path" /></div><button className="button secondary" onClick={() => void renamer.saveSettings()}>Save local settings</button><div className="provider-attribution"><a href="https://www.themoviedb.org/" target="_blank" rel="noreferrer" aria-label="The Movie Database"><img src={tmdbLogo} alt="The Movie Database (TMDB)" /></a><p>This product uses the TMDB API but is not endorsed or certified by TMDB.</p><p>TV metadata provided by <a href="https://thetvdb.com/" target="_blank" rel="noreferrer">TheTVDB</a>. Please consider adding missing information or subscribing.</p></div></div></section>

              <section className="panel metadata-panel"><div className="panel-heading"><div><span className="panel-kicker">IDENTITY</span><h2>Content metadata</h2></div><span className="source-chip"><Icon name="check" size={13} /> MediaInfo scanned</span></div><div className="segmented"><button className={metadata.mediaType === 'movie' ? 'selected' : ''} onClick={() => update({ mediaType: 'movie' })}><Icon name="film" size={15} /> Movie</button><button className={metadata.mediaType === 'tv' ? 'selected' : ''} onClick={() => update({ mediaType: 'tv' })}><Icon name="tv" size={15} /> TV series</button></div><SearchBox mediaType={metadata.mediaType} onSearch={metadata.mediaType === 'movie' ? renamer.searchMovie : renamer.searchTV} onPick={chooseResult} /><div className="field-grid"><Field label="Title" value={metadata.title} onChange={(value) => update({ title: value })} className="wide" /><Field label="Year" value={metadata.year} onChange={(value) => update({ year: value })} /><Field label="Edition" value={metadata.edition} onChange={(value) => update({ edition: value })} placeholder="Director's cut" />{metadata.mediaType === 'tv' && <><Field label="Season" value={metadata.season} onChange={(value) => update({ season: value })} /><Field label="Episode" value={metadata.episode} onChange={(value) => update({ episode: value })} /><Field label="Episode title" value={metadata.episodeTitle} onChange={(value) => update({ episodeTitle: value })} className="wide" /></>}</div></section>
            </div>

            <div className="right-column"><section className="panel technical-panel"><div className="panel-heading"><div><span className="panel-kicker">TECHNICAL</span><h2>Release details</h2></div><button className="icon-button" title="Read-only MediaInfo scan"><Icon name="external" size={15} /></button></div><div className="field-grid"><label className="field compact"><span className="field-label">Release type</span><select value={metadata.releaseType} onChange={(event) => update({ releaseType: event.target.value as TechnicalMetadata['releaseType'] })}><option>WEB-DL</option><option>WEBRip</option><option>REMUX</option><option>ENCODE</option></select></label><Field label="Resolution" value={metadata.resolution} onChange={(value) => update({ resolution: value })} placeholder="2160p" /><Field label="Source" value={metadata.source} onChange={(value) => update({ source: value })} placeholder="BluRay / WEB" /><Field label="Service" value={metadata.service} onChange={(value) => update({ service: value })} placeholder="NF, AMZN…" hint="Kept only when found in the original name" /><Field label="Video codec" value={metadata.videoCodec} onChange={(value) => update({ videoCodec: value })} /><Field label="HDR / Dolby Vision" value={metadata.hdr} onChange={(value) => update({ hdr: value })} placeholder="HDR10, DV" /><Field label="Main audio" value={metadata.audio} onChange={(value) => update({ audio: value })} /><Field label="Languages" value={metadata.languages} onChange={(value) => update({ languages: value })} placeholder="en, vi" /><Field label="Release group" value={metadata.group} onChange={(value) => update({ group: value })} /></div><div className="technical-foot"><span><i className="dot green" /> Parsed from MediaInfo</span><span><i className="dot amber" /> Confirm filename hints</span></div></section></div>
          </div>

          <section className="panel preview-panel"><div className="panel-heading preview-heading"><div><span className="panel-kicker">PLAN</span><h2>Rename preview</h2></div><div className="preview-actions"><span className="item-count">{renamer.plan?.items?.length ?? 0} item{(renamer.plan?.items?.length ?? 0) === 1 ? '' : 's'}</span><button className="button primary" onClick={() => void renamer.preview()} disabled={renamer.busy}><Icon name="scan" size={15} /> {renamer.busy ? 'Working…' : 'Build plan'}</button></div></div><div className="suggested-name"><span className="suggested-label"><Icon name="spark" size={14} /> {suggestedLabel}</span><code>{liveName}</code></div>{renamer.plan?.items?.length ? <div className="rename-list">{renamer.plan.items.map((item) => <RenameRow key={`${item.oldPath}-${item.newPath}`} item={item} />)}</div> : <div className="preview-empty"><Icon name="arrow" size={18} /><span>Build a plan to compare every old path with its new P2P name.</span></div>}{renamer.plan?.warnings?.map((warning) => <div className="inline-warning" key={warning}><Icon name="alert" size={15} />{warning}</div>)}{renamer.plan?.errors?.map((error) => <div className="inline-error" key={error}><Icon name="alert" size={15} />{error}</div>)}<div className="apply-bar"><div className="apply-copy">{renamer.applied ? <><span className="success-icon"><Icon name="check" size={14} /></span><span><strong>Rename applied</strong><small>Undo is available for the latest transaction.</small></span></> : planHasErrors ? <><span className="safe-icon"><Icon name="alert" size={14} /></span><span><strong>Resolve plan errors</strong><small>Apply stays disabled until every conflict is resolved.</small></span></> : renamer.plan && !renamer.plan.canApply && planHasPreservedItems ? <><span className="safe-icon"><Icon name="archive" size={14} /></span><span><strong>No rename needed</strong><small>Existing P2P basenames are preserved exactly.</small></span></> : renamer.plan && !renamer.plan.canApply ? <><span className="safe-icon"><Icon name="check" size={14} /></span><span><strong>Already matches</strong><small>The selected paths already match the VMF target.</small></span></> : <><span className="safe-icon"><Icon name="archive" size={14} /></span><span><strong>Safe by default</strong><small>Nothing changes until you apply this plan.</small></span></>}</div><div className="apply-actions"><button className="button ghost" onClick={() => void renamer.undo()} disabled={renamer.busy || !renamer.applied}><Icon name="undo" size={15} /> Undo</button><button className="button primary large" onClick={() => void renamer.apply()} disabled={renamer.busy || !renamer.plan?.canApply || renamer.applied}><Icon name="check" size={16} /> Apply rename</button></div></div></section>
          {(renamer.notice || renamer.error) && <div className={`toast ${renamer.error ? 'error' : ''}`}><Icon name={renamer.error ? 'alert' : 'check'} size={15} />{renamer.error || renamer.notice}</div>}
        </>}
      </main>
    </div>
  </div>
}

function RenameRow({ item }: { item: RenameItem }) {
  const oldName = item.oldPath.split(/[\\/]/).pop() ?? item.oldPath
  const newName = item.newPath.split(/[\\/]/).pop() ?? item.newPath
  const showParentMove = oldName === newName && pathKey(item.oldPath) !== pathKey(item.newPath)
  return <div className="rename-row"><div className="path-cell old"><span className="path-icon"><Icon name={item.kind === 'folder' ? 'folder' : 'file'} size={15} /></span><span className="path-copy" title={item.oldPath}><span>{oldName}</span>{showParentMove && <small>{parentLabel(item.oldPath)}</small>}</span></div><Icon name="arrow" size={15} /><div className="path-cell new"><span className="path-icon"><Icon name={item.kind === 'folder' ? 'folder' : 'file'} size={15} /></span><span className="path-copy" title={item.newPath}><span>{newName}</span>{showParentMove && <small>{parentLabel(item.newPath)}</small>}</span></div><StatusBadge status={item.status} /></div>
}

function MediaInfoView({ scan }: { scan: ScanResult | null }) {
  return <section className="panel full-workspace-panel"><div className="panel-heading"><div><span className="panel-kicker">MEDIAINFO</span><h2>Technical inspection</h2></div><span className="source-chip">Read-only</span></div>{scan?.warnings?.map((warning) => <div className="inline-warning" key={warning}><Icon name="alert" size={15} />{warning}</div>)}{scan?.mediaInfoText ? <pre className="mediainfo-output">{scan.mediaInfoText}</pre> : <div className="preview-empty"><Icon name="film" size={18} /><span>Choose a media file to extract MediaInfo JSON.</span></div>}</section>
}

function HistoryView({ plan }: { plan: RenamePlan | null }) {
  return <section className="panel full-workspace-panel"><div className="panel-heading"><div><span className="panel-kicker">HISTORY</span><h2>Latest rename plan</h2></div><span className="source-chip">Current session</span></div>{plan ? <div className="rename-list">{plan.items.map((item) => <RenameRow key={`${item.oldPath}-${item.newPath}`} item={item} />)}</div> : <div className="preview-empty"><Icon name="history" size={18} /><span>Build a rename plan to review it here.</span></div>}</section>
}
