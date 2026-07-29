export type MediaType = 'movie' | 'tv'
export type ReleaseType = 'WEB-DL' | 'WEBRip' | 'REMUX' | 'ENCODE'

export interface TechnicalMetadata {
  mediaType: MediaType
  title: string
  /** Retained metadata; used only when an English title is unavailable. */
  originalTitle?: string
  year: string
  season: string
  episode: string
  episodeTitle: string
  edition: string
  resolution: string
  source: string
  service: string
  releaseType: ReleaseType
  videoCodec: string
  /** Encoder library when MediaInfo proves it (for example x264/x265). */
  videoEncode?: string
  hdr: string
  audio: string
  languages: string
  group: string
  /** True only when the original filename contained UHD/Ultra HD. */
  uhd: boolean
}

export interface ScanFile {
  path: string
  kind: 'video' | 'audio' | 'subtitle' | 'image' | 'other'
  size?: number
}

export interface ScanResult {
  rootPath: string
  mediaType: MediaType
  files: ScanFile[]
  metadata: Partial<TechnicalMetadata>
  seasons?: string[]
  seriesRoot?: boolean
  seasonFolderCount?: number
  warnings?: string[]
  mediaInfoText?: string
  mediaInfoJson?: unknown
}

export interface RenameItem {
  oldPath: string
  newPath: string
  kind: 'file' | 'folder'
  status?: 'ready' | 'create' | 'same' | 'preserved' | 'conflict' | 'warning'
}

export interface RenamePlan {
  id: string
  items: RenameItem[]
  changeCount: number
  canApply: boolean
  warnings: string[]
  errors: string[]
}

export interface RenameRequest {
  rootPath: string
  metadata: TechnicalMetadata
  metadataOverrides?: string[]
  separator: string
  preserveExistingP2P: boolean
  /** Deprecated vmf@1 compatibility field; the backend ignores it. */
  includeUhd?: boolean
}

export interface SearchResult {
  id: string
  title: string
  originalTitle?: string
  year?: string
  overview?: string
  posterUrl?: string
}

export interface AppSettings {
  separator: string
  group: string
  preserveExistingP2P: boolean
  /** Deprecated vmf@1 compatibility field; ignored. */
  includeUhd?: boolean
  profile: string
  tmdbApiKey: string
  tvdbApiKey: string
  tvdbPin: string
  mediaInfoBin: string
}

export const defaultMetadata: TechnicalMetadata = {
  mediaType: 'movie',
  title: 'Example.Movie',
  originalTitle: '',
  year: '2026',
  season: '01',
  episode: '01',
  episodeTitle: '',
  edition: '',
  resolution: '2160p',
  source: 'WEB',
  service: '',
  releaseType: 'WEB-DL',
  videoCodec: 'H.265',
  hdr: 'HDR10',
  audio: 'DDP5.1',
  languages: 'en,vi',
  group: 'NoGroup',
  uhd: false,
}

export const defaultSettings: AppSettings = {
  separator: '.',
  group: 'NoGroup',
  preserveExistingP2P: true,
  profile: 'vmf@2',
  tmdbApiKey: '',
  tvdbApiKey: '',
  tvdbPin: '',
  mediaInfoBin: '',
}
