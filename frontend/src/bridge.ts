import type {
  RenamePlan,
  RenameRequest,
  ScanResult,
  SearchResult,
} from './types'

/**
 * A deliberately small adapter around the Wails bridge. Generated Wails
 * bindings are not imported here, so `npm run build` remains usable in a
 * normal browser. The adapter discovers the Wails-generated
 * `window.go.app.App` binding when running in
 * Wails and otherwise returns a clear error for the caller to fall back to its
 * preview data.
 */
export interface VmfBridge {
  ScanPath(path: string): Promise<ScanResult>
  PreviewRename(request: RenameRequest): Promise<RenamePlan>
  ApplyRename(plan: RenamePlan): Promise<void>
  UndoRename(): Promise<void>
  HasUndoJournal(): Promise<boolean>
  UndoNeedsAttention(): Promise<boolean>
  SearchMovie(query: string): Promise<SearchResult[]>
  SearchTV(query: string): Promise<SearchResult[]>
  ResolveTVSeries(id: string): Promise<SearchResult>
  SelectFile(): Promise<string>
  SelectFolder(): Promise<string>
  GetSettings(): Promise<import('./types').AppSettings>
  SaveSettings(settings: import('./types').AppSettings): Promise<void>
}

type UnknownBridge = Partial<VmfBridge>

declare global {
  interface Window {
    go?: {
      /** Wails uses the Go package name as the namespace (`app` here). */
      app?: {
        App?: UnknownBridge
      }
      /** Keep compatibility with older/manual hosts that used `main`. */
      main?: {
        App?: UnknownBridge
      }
    }
    /** Optional injection point useful to Wails hosts and automated tests. */
    __VMF_BRIDGE__?: UnknownBridge
  }
}

class BridgeUnavailable extends Error {
  constructor() {
    super('The Wails bridge is not available in browser preview mode.')
    this.name = 'BridgeUnavailable'
  }
}

function rawBridge(): UnknownBridge | undefined {
  if (typeof window === 'undefined') return undefined
  // Wails v2 generates bindings under `window.go.<package>.<Type>`. The
  // service is declared in the `app` package, so the packaged desktop app
  // exposes `window.go.app.App`; `main.App` is retained for older hosts and
  // test fixtures.
  return window.__VMF_BRIDGE__ ?? window.go?.app?.App ?? window.go?.main?.App
}

function method<K extends keyof VmfBridge>(name: K): VmfBridge[K] {
  return (async (...args: any[]) => {
    // Resolve lazily. Wails normally installs bindings before the frontend is
    // evaluated, while preview/test hosts may inject them afterwards.
    const candidate = rawBridge()
    const fn = candidate?.[name]
    if (typeof fn !== 'function') {
      throw new BridgeUnavailable()
    }
    // Wails methods can rely on their receiver in generated code. Keep the
    // cast at this adapter boundary so the public API remains strongly typed.
    const callable = fn as (...inner: any[]) => any
    return await callable.apply(candidate, args)
  }) as VmfBridge[K]
}

export const bridge: VmfBridge = {
  ScanPath: method('ScanPath'),
  PreviewRename: method('PreviewRename'),
  ApplyRename: method('ApplyRename'),
  UndoRename: method('UndoRename'),
  HasUndoJournal: method('HasUndoJournal'),
  UndoNeedsAttention: method('UndoNeedsAttention'),
  SearchMovie: method('SearchMovie'),
  SearchTV: method('SearchTV'),
  ResolveTVSeries: method('ResolveTVSeries'),
  SelectFile: method('SelectFile'),
  SelectFolder: method('SelectFolder'),
  GetSettings: method('GetSettings'),
  SaveSettings: method('SaveSettings'),
}

export function isBridgeAvailable(): boolean {
  const candidate = rawBridge()
  return Boolean(candidate && typeof candidate.ScanPath === 'function')
}

export function isBridgeError(error: unknown): boolean {
  return error instanceof BridgeUnavailable ||
    (error instanceof Error && error.name === 'BridgeUnavailable')
}
