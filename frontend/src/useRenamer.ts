import { useCallback, useEffect, useMemo, useState } from 'react'
import { bridge, isBridgeAvailable, isBridgeError } from './bridge'
import {
  defaultMetadata,
  defaultSettings,
  type AppSettings,
  type RenamePlan,
  type RenameRequest,
  type ScanFile,
  type ScanResult,
  type SearchResult,
  type TechnicalMetadata,
} from './types'

const SYNTHETIC_VIDEO = 'Example.Movie.2026.2160p.WEB-DL.HDR10.H.265.mkv'

function token(value: string): string {
  return value
    .trim()
    .replace(/[\\/:*?"<>|]+/g, '')
    .replace(/[\s_]+/g, '.')
    .replace(/\.{2,}/g, '.')
    .replace(/^\.+|\.+$/g, '')
}

function titleFromPath(path: string): string {
  const leaf = path.split(/[\\/]/).pop() ?? ''
  const withoutExtension = leaf.replace(/\.[^.]+$/, '')
  return token(withoutExtension
    .replace(/\bS\d{1,2}(?:E\d{1,4})+\b/i, '')
    .replace(/\b(?:19|20)\d{2}\b/, '')
    .replace(/\b\d{3,4}p\b/i, '')
    .replace(/\b(?:UHD|ULTRA[. _-]+HD)\b/gi, '')
    .replace(/\b(?:WEB[- .]?DL|WEBRip|REMUX|ENCODE|BluRay|HDTV|WEB|NF|AMZN|DSNP|MAX|ATVP|HMAX|HULU|CRAV)\b/gi, '')
    .replace(/\b(?:HDR10\+?|HDR|DV|x264|x265|H\.264|H\.265|HEVC|AVC)\b/gi, '')
    .replace(/-[A-Za-z0-9][A-Za-z0-9._-]{1,30}$/, '')) || defaultMetadata.title
}

function hintsFromFilename(path: string): Partial<TechnicalMetadata> {
  const name = basename(path)
  const hints: Partial<TechnicalMetadata> = {
    title: titleFromPath(path),
  }
  if (/\bS\d{1,2}E\d{1,4}/i.test(name)) hints.mediaType = 'tv'
  const service = ['NF', 'AMZN', 'DSNP', 'MAX', 'ATVP', 'HMAX', 'HULU', 'CRAV'].find((candidate) => new RegExp(`(?:^|[. _-])${candidate}(?:$|[. _-])`, 'i').test(name))
  if (service) hints.service = service
  if (/\bWEB[- .]?DL\b/i.test(name)) hints.releaseType = 'WEB-DL'
  else if (/\bWEBRip\b/i.test(name)) hints.releaseType = 'WEBRip'
  else if (/\bREMUX\b/i.test(name)) hints.releaseType = 'REMUX'
  else if (/\b(?:ENCODE|BDRip|BRRip)\b/i.test(name)) hints.releaseType = 'ENCODE'
  const resolution = name.match(/\b(?:4320|2160|1440|1080|720|576|480)p\b/i)
  if (resolution) hints.resolution = resolution[0].toLowerCase()
  const source = ['BluRay', 'UHD.BluRay', 'HDTV', 'WEB'].find((candidate) => new RegExp(`(?:^|[. _-])${candidate.replace('.', '[.]')}(?:$|[. _-])`, 'i').test(name))
  if (source) hints.source = source
  const stem = name.replace(/\.[^.]+$/, '')
  if (/(?:^|[. _-])(?:UHD|ULTRA[. _-]+HD)(?:$|[. _-])/i.test(stem)) hints.uhd = true
  if (/\b(?:DV|Dolby.Vision)\b/i.test(name)) hints.hdr = 'DV'
  else if (/\b(?:HDR10\+?|HDR)\b/i.test(name)) hints.hdr = name.match(/\bHDR10\+?\b/i)?.[0] ?? 'HDR'
  if (/\b(?:x265|h\.265|hevc)\b/i.test(name)) hints.videoCodec = 'H.265'
  else if (/\b(?:x264|h\.264|avc)\b/i.test(name)) hints.videoCodec = 'H.264'
  const group = name.match(/-([A-Za-z0-9][A-Za-z0-9_]{0,30})(?:\.[^.]+)?$/)
  const hasReleaseEvidence = /(?:WEB[- .]?DL|WEBRip|REMUX|BluRay|HDTV|x264|x265|H\.26[45]|\b(?:19|20)\d{2}\b|\b\d{3,4}p\b)/i.test(name)
  if (group?.[1] && hasReleaseEvidence && !/^(?:DL|WEB|HDR|H|x26[45])$/i.test(group[1])) hints.group = group[1]
  return hints
}

function extension(path: string): string {
  const match = path.match(/(\.[^./\\]+)$/)
  return match?.[1] ?? '.mkv'
}

function basename(path: string): string {
  return path.split(/[\\/]/).pop() ?? path
}

function parentPath(path: string): string {
  const parts = path.split(/[\\/]/)
  parts.pop()
  return parts.join(path.includes('\\') ? '\\' : '/')
}

// The browser preview follows the same strict policy as the backend: only an
// explicit marker parsed from the original basename may supply UHD.
function shouldIncludeUhd(metadata: TechnicalMetadata): boolean {
  return metadata.uhd === true
}

export function formatReleaseName(metadata: TechnicalMetadata, settings: AppSettings): string {
  const parts: string[] = []
  const add = (value?: string) => {
    const normalized = token(value ?? '')
    if (normalized && !parts.includes(normalized)) parts.push(normalized)
  }

  add(metadata.title.trim() ? metadata.title : metadata.originalTitle)
  if (metadata.mediaType === 'tv') {
    add(metadata.year)
    const seasonNumber = metadata.season.replace(/^S/i, '').trim()
    const episodeNumber = metadata.episode.replace(/^E/i, '').trim()
    const season = seasonNumber ? `S${seasonNumber.padStart(2, '0')}` : ''
    const episode = episodeNumber ? `E${episodeNumber.padStart(2, '0')}` : ''
    add(`${season}${episode}`)
    add(metadata.episodeTitle)
  } else {
    add(metadata.year)
  }
  add(metadata.edition)
  if (metadata.languages.split(',').some((language) => /^(?:vi|vie|vietnamese)(?:-|$)/i.test(language.trim()))) add('ViE')
  add(metadata.resolution)
  if (shouldIncludeUhd(metadata)) add('UHD')

  switch (metadata.releaseType) {
    case 'WEB-DL':
    case 'WEBRip':
      add(metadata.service)
      add(metadata.releaseType)
      add(metadata.audio)
      add(metadata.hdr)
      add(metadata.videoCodec)
      break
    case 'REMUX':
      add(metadata.source)
      add('REMUX')
      add(metadata.hdr)
      add(metadata.videoCodec)
      add(metadata.audio)
      break
    case 'ENCODE':
      add(metadata.source)
      add(metadata.audio)
      add(metadata.hdr)
      add(metadata.videoCodec)
      break
  }

  const body = parts.join(settings.separator || '.')
  const group = token(metadata.group || settings.group || 'NoGroup') || 'NoGroup'
  return `${body}-${group}`
}

function syntheticScan(path: string): ScanResult {
  const root = path || `C:\\Media\\${SYNTHETIC_VIDEO}`
  const filePath = root.toLowerCase().endsWith('.mkv') || root.toLowerCase().endsWith('.mp4')
    ? root
    : `${root.replace(/[\\/]$/, '')}\\${SYNTHETIC_VIDEO}`
  const hints = hintsFromFilename(filePath)
  const metadata: Partial<TechnicalMetadata> = {
    ...defaultMetadata,
    ...hints,
  }
  const isFolder = !/\.[A-Za-z0-9]{2,5}$/.test(root)
  const files: ScanFile[] = [{ path: filePath, kind: 'video', size: 18_420_000_000 }]
  if (isFolder) files.unshift({ path: root, kind: 'other' })
  return { rootPath: root, mediaType: metadata.mediaType ?? 'movie', metadata, files }
}

function syntheticPlan(request: RenameRequest, scan: ScanResult | null): RenamePlan {
  const rootPath = request.rootPath || scan?.rootPath || `C:\\Media\\${SYNTHETIC_VIDEO}`
  const release = formatReleaseName(request.metadata, request.metadata ? {
    ...defaultSettings,
    separator: request.separator,
    group: request.metadata.group || 'NoGroup',
    profile: 'vmf@2',
  } : defaultSettings)
  const files = scan?.files?.length ? scan.files : [{ path: rootPath, kind: 'video' as const }]
  const items = files
    .filter((file) => file.kind === 'video' || file.kind === 'subtitle' || file.kind === 'audio' || file.path === rootPath)
    .map((file) => {
      const oldName = basename(file.path)
      const ext = extension(oldName)
      const isFolder = file.path === rootPath && file.kind === 'other'
      const targetName = isFolder ? release : `${release}${ext}`
      return {
        oldPath: file.path,
        newPath: `${parentPath(file.path)}${file.path.includes('\\') ? '\\' : '/'}${targetName}`,
        kind: isFolder ? 'folder' as const : 'file' as const,
        status: oldName === targetName ? 'same' as const : 'ready' as const,
      }
    })
  if (!items.length) {
    items.push({
      oldPath: rootPath,
      newPath: `${parentPath(rootPath)}${rootPath.includes('\\') ? '\\' : '/'}${release}${extension(rootPath)}`,
      kind: 'file' as const,
      status: 'ready',
    })
  }
  return {
    id: `preview-${Date.now()}`,
    items,
    warnings: request.metadata.service ? [] : ['Service tag was not found in the original filename and was omitted.'],
    errors: [],
  }
}

function normalizeRenamePlan(value: RenamePlan | null | undefined): RenamePlan {
  if (!value || typeof value.id !== 'string') {
    throw new Error('The backend returned an invalid rename plan.')
  }
  return {
    ...value,
    items: Array.isArray(value.items) ? value.items : [],
    warnings: Array.isArray(value.warnings) ? value.warnings : [],
    errors: Array.isArray(value.errors) ? value.errors : [],
  }
}

export interface RenamerState {
  settings: AppSettings
  metadata: TechnicalMetadata
  scan: ScanResult | null
  plan: RenamePlan | null
  selectedPath: string
  busy: boolean
  applied: boolean
  error: string
  notice: string
  bridgeConnected: boolean
  updateMetadata: (patch: Partial<TechnicalMetadata>) => void
  updateSettings: (patch: Partial<AppSettings>) => void
  scanPath: (path: string) => Promise<void>
  preview: () => Promise<void>
  apply: () => Promise<void>
  undo: () => Promise<void>
  searchMovie: (query: string) => Promise<SearchResult[]>
  searchTV: (query: string) => Promise<SearchResult[]>
  resolveTVSeries: (id: string) => Promise<SearchResult | null>
  chooseFile: () => Promise<void>
  chooseFolder: () => Promise<void>
  saveSettings: () => Promise<void>
}

export function useRenamer(): RenamerState {
  const [settings, setSettings] = useState(defaultSettings)
  const [metadata, setMetadata] = useState(defaultMetadata)
  const [scan, setScan] = useState<ScanResult | null>(() => syntheticScan(''))
  const [plan, setPlan] = useState<RenamePlan | null>(null)
  const [selectedPath, setSelectedPath] = useState('C:\\Media\\Example.Movie.2026.mkv')
  const [busy, setBusy] = useState(false)
  const [applied, setApplied] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')

  useEffect(() => {
    if (!isBridgeAvailable()) return
    void bridge.GetSettings()
      .then((stored) => {
        const next = { ...defaultSettings, ...stored }
        setSettings(next)
        setMetadata((current) => ({ ...current, group: next.group || current.group }))
      })
      .catch((cause) => setError(cause instanceof Error ? cause.message : 'Could not load settings.'))
  }, [])

  const updateMetadata = useCallback((patch: Partial<TechnicalMetadata>) => {
    setMetadata((current) => ({ ...current, ...patch }))
    setApplied(false)
  }, [])

  const updateSettings = useCallback((patch: Partial<AppSettings>) => {
    setSettings((current) => ({ ...current, ...patch }))
    setApplied(false)
  }, [])

  const scanPath = useCallback(async (path: string) => {
    setSelectedPath(path)
    setBusy(true)
    setError('')
    setNotice('')
    try {
      const result = await bridge.ScanPath(path)
      setScan(result)
      setMetadata((current) => ({ ...current, ...result.metadata, mediaType: result.mediaType, uhd: result.metadata.uhd === true }))
      const warningCount = result.warnings?.length ?? 0
      setNotice(`Scanned ${result.files.length} item${result.files.length === 1 ? '' : 's'}${warningCount ? ` with ${warningCount} warning${warningCount === 1 ? '' : 's'}` : ''}.`)
    } catch (cause) {
      if (isBridgeError(cause)) {
        // Browser-only hosts have no filesystem bridge. Keep the design-time
        // preview useful there, but never substitute synthetic data for a
        // real Wails/backend failure (for example a missing or unreadable
        // path), since that could lead to an unsafe rename plan.
        const result = syntheticScan(path)
        setScan(result)
        setMetadata((current) => ({ ...current, ...result.metadata, uhd: result.metadata.uhd === true }))
        setNotice('Browser preview: showing a synthetic MediaInfo result.')
      } else {
        setError(cause instanceof Error ? cause.message : 'Could not scan this path.')
        setPlan(null)
        setApplied(false)
      }
    } finally {
      setBusy(false)
    }
  }, [])

  const chooseFile = useCallback(async () => {
    try {
      const path = await bridge.SelectFile()
      if (path) await scanPath(path)
    } catch (cause) {
      if (!isBridgeError(cause)) setError(cause instanceof Error ? cause.message : 'Could not open the file picker.')
    }
  }, [scanPath])

  const chooseFolder = useCallback(async () => {
    try {
      const path = await bridge.SelectFolder()
      if (path) await scanPath(path)
    } catch (cause) {
      if (!isBridgeError(cause)) setError(cause instanceof Error ? cause.message : 'Could not open the folder picker.')
    }
  }, [scanPath])

  const saveSettings = useCallback(async () => {
    try {
      await bridge.SaveSettings(settings)
      setNotice('Settings saved locally.')
    } catch (cause) {
      if (!isBridgeError(cause)) setError(cause instanceof Error ? cause.message : 'Could not save settings.')
    }
  }, [settings])

  const request = useMemo<RenameRequest>(() => ({
    rootPath: selectedPath,
    metadata: { ...metadata, group: metadata.group || settings.group },
    separator: settings.separator,
  }), [metadata, selectedPath, settings])

  const preview = useCallback(async () => {
    setBusy(true)
    setError('')
    try {
      const next = await bridge.PreviewRename(request)
      // Older Wails responses may omit empty slices. Normalize at the bridge
      // boundary so rendering a successful plan can never dereference an
      // undefined items/warnings/errors collection.
      setPlan(normalizeRenamePlan(next))
      setApplied(false)
      setNotice('Preview refreshed.')
    } catch (cause) {
      if (isBridgeError(cause)) {
        setPlan(syntheticPlan(request, scan))
        setApplied(false)
        setNotice('Browser preview: rename plan generated locally.')
      } else {
        // A backend validation/preflight error must not be replaced with a
        // synthetic plan. Clear the old plan so Apply cannot accidentally
        // execute stale operations after the failure is shown.
        setError(cause instanceof Error ? cause.message : 'Could not build a rename plan.')
        setPlan(null)
        setApplied(false)
      }
    } finally {
      setBusy(false)
    }
  }, [request, scan])

  const apply = useCallback(async () => {
    if (!plan) return
    setBusy(true)
    setError('')
    try {
      await bridge.ApplyRename(plan)
      setApplied(true)
      setNotice('Rename applied. A journal entry is ready to undo.')
    } catch (cause) {
      setApplied(false)
      if (isBridgeError(cause)) {
        setNotice('Browser preview: no files were changed.')
      } else {
        // Do not mark a failed backend transaction as applied. Apply may have
        // rolled back automatically, but the UI must not claim success or
        // disable retry while exposing an undo action for a nonexistent
        // transaction.
        setError(cause instanceof Error ? cause.message : 'Could not apply the rename plan.')
        setNotice('Rename failed; no successful transaction was recorded.')
      }
    } finally {
      setBusy(false)
    }
  }, [plan])

  const undo = useCallback(async () => {
    setBusy(true)
    setError('')
    try {
      await bridge.UndoRename()
      setApplied(false)
      setNotice('The last rename was undone.')
    } catch (cause) {
      setApplied(false)
      if (isBridgeError(cause)) {
        setNotice('Browser preview: no filesystem changes to undo.')
      } else {
        setError(cause instanceof Error ? cause.message : 'Could not undo the last rename.')
        setNotice('Undo failed; inspect the latest journal before retrying.')
      }
    } finally {
      setBusy(false)
    }
  }, [])

  const searchMovie = useCallback(async (query: string) => {
    if (!query.trim()) return []
    try {
      return await bridge.SearchMovie(query)
    } catch (cause) {
      if (isBridgeError(cause)) {
        return [{ id: 'preview-movie', title: query.trim(), year: '2026', overview: 'Synthetic browser-preview result.' }]
      }
      setError(cause instanceof Error ? cause.message : 'Movie search failed.')
      return []
    }
  }, [])

  const searchTV = useCallback(async (query: string) => {
    if (!query.trim()) return []
    try {
      return await bridge.SearchTV(query)
    } catch (cause) {
      if (isBridgeError(cause)) {
        return [{ id: 'preview-tv', title: query.trim(), year: '2026', overview: 'Synthetic browser-preview result.' }]
      }
      setError(cause instanceof Error ? cause.message : 'TV search failed.')
      return []
    }
  }, [])

  const resolveTVSeries = useCallback(async (id: string) => {
    if (!id.trim()) return null
    try {
      return await bridge.ResolveTVSeries(id)
    } catch (cause) {
      // Browser preview has no provider bridge; keep the search result the
      // caller already has. A real TVDB error is shown but is also non-fatal
      // because the selected search title remains usable.
      if (!isBridgeError(cause)) setError(cause instanceof Error ? cause.message : 'TVDB series lookup failed.')
      return null
    }
  }, [])

  return {
    settings,
    metadata,
    scan,
    plan,
    selectedPath,
    busy,
    applied,
    error,
    notice,
    bridgeConnected: isBridgeAvailable(),
    updateMetadata,
    updateSettings,
    scanPath,
    preview,
    apply,
    undo,
    searchMovie,
    searchTV,
    resolveTVSeries,
    chooseFile,
    chooseFolder,
    saveSettings,
  }
}
