# VMF Preupload frontend

The frontend is a Vite/React preview shell that can also be embedded in a
Wails v2 application. It intentionally does not import generated Wails
bindings. At runtime, `src/bridge.ts` discovers either:

```ts
window.go.app.App
// or, for a host/test harness:
window.__VMF_BRIDGE__
```

The bridge may expose these methods:

```text
ScanPath(path)
PreviewRename(request)
ApplyRename(plan)
UndoRename()
SearchMovie(query)
SearchTV(query)
ResolveTVSeries(id)
SelectFile()
SelectFolder()
GetSettings()
SaveSettings(settings)
```

Without a bridge the UI stays fully usable as a browser preview: scan,
metadata editing, preview generation, and Apply/Undo use synthetic data and
never write to the filesystem.

```powershell
npm install
npm run dev
```

For Wails, point the project `frontend` directory at this folder and use the
usual generated bindings in the host process; no frontend source changes are
required.
