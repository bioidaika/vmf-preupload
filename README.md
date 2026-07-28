# VMF Preupload

VMF Preupload is a Windows desktop utility for preparing existing movie and
TV files before a torrent is created. It scans MediaInfo, optionally looks up
identity metadata, and shows a complete old-path → new-path plan before any
filesystem change is made.

The first milestone is deliberately conservative:

- the default profile uses dot-separated tokens and falls back to `-NoGroup`;
- an existing, high-confidence grouped P2P release name is preserved exactly
  by default; disable **Preserve existing P2P releases** only when intentionally
  forcing the VMF renderer;
- service tokens are retained only when they are evidenced in the old filename
  or entered explicitly;
- `UHD` is retained only when the original video filename contains a
  standalone `UHD` or `Ultra HD` marker; resolution, source, release type,
  MediaInfo, and parent-folder names never create it;
- `WEB-DL`, `WEBRip`, `REMUX`, and `ENCODE` are naming modes (the app does not
  transcode or remux media);
- rebuilt names use release-specific video spelling: `AVC`/`HEVC` for REMUX,
  `H.264`/`H.265` for WEB-DL, and `x264`/`x265` for ENCODE/WEBRip only when the
  encoder library is evidenced by MediaInfo;
- apply is a two-phase rename transaction with a journal that can be undone.

## Included runtime

- Go backend and Wails v2 desktop shell
- React/TypeScript/Vite frontend
- Portable Windows MediaInfo CLI in `assets/mediainfo`
- TMDB movie search and TVDB v4 series search
- filename/folder preview, collision checks, apply, and undo

## Build a Windows package

Requirements:

- Windows 10/11 x64 with the Microsoft WebView2 runtime;
- Go 1.22 or newer;
- Node.js 20 or newer (the script invokes `npm.cmd` directly, avoiding the
  commonly blocked `npm.ps1` shim);
- internet access for the first Wails module download.

From the repository root:

```powershell
git clone https://github.com/bioidaika/vmf-preupload.git
cd vmf-preupload
.\scripts\build-windows.ps1
```

If the machine blocks all local `.ps1` files, run the same trusted workspace
script with a one-process override (this does not change the system policy):

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\scripts\build-windows.ps1
```

The script installs/builds the frontend, runs Wails v2.12.0, verifies the
bundled MediaInfo files, and copies all of them beside the executable. The
distributable is the whole `build\bin` directory:

```text
build\bin\vmf-preupload.exe
build\bin\assets\mediainfo\MediaInfo.exe
build\bin\assets\mediainfo\LIBCURL.DLL
build\bin\assets\mediainfo\LICENSE
```

Do not distribute the executable without its adjacent `assets\mediainfo`
directory. Portable Go/Node installations can be selected explicitly:

```powershell
.\scripts\build-windows.ps1 `
  -GoPath 'C:\Program Files\Go\bin\go.exe' `
  -NpmPath 'D:\tools\node\npm.cmd'
```

For a quick frontend-only check:

```powershell
cd frontend
npm ci
npm run build
cd ..
```

Backend checks do not require a running desktop window:

```powershell
go test ./...
go vet ./...
```

For the Wails development loop, use the same pinned CLI without installing a
global binary:

```powershell
go run github.com/wailsapp/wails/v2/cmd/wails@v2.12.0 dev
```

## Use the app

1. Choose a video file or a movie/season folder. Scan is read-only.
2. Select Movie or TV series, search TMDB/TVDB if desired, and confirm the
   identity fields.
3. Review the MediaInfo-derived technical fields and any filename hints.
4. Choose the release mode and build the rename plan.
5. Resolve any warning/collision, then select **Apply rename**. **Undo** uses
   the latest transaction journal.

For a selected TV season folder, the folder receives a season-pack name while
each episode keeps its own `SxxEyy` identity. When a selected series container
has direct children named `Season 1`/`S01`, `Season 2`/`S02`, and so on, the
container stays in place and every recognized season folder is planned as its
own P2P release. Nested content remains below the matching renamed season.

A flat series container holding files from several seasons also stays in
place; every file uses its own parsed season/episode instead of inheriting S01
from the first scan. The app does not create new season directories for this
flat layout yet. Subtitle/NFO/image sidecars are not independently renamed.

## Provider keys and local settings

Keys can be entered in the profile panel or supplied with `TMDB_API_KEY`,
`TVDB_API_KEY`, and `TVDB_PIN` during development. Environment values override
non-empty saved values. On Windows the UI settings file is normally:

```text
%AppData%\VMFPreupload\settings.json
```

The current milestone stores keys in that per-user JSON file. Before sharing
the app with other users, move secrets to Windows Credential Manager (or use a
separate per-machine secret mechanism).

MediaInfo is auto-detected from the packaged `assets\mediainfo` directory,
the working directory, or `PATH`. Set **MediaInfo executable** only when a
different binary should be used.

Movie names are requested from TMDB with `language=en-US`. TV series search
keeps all original-language results, prefers TVDB's `eng` translation, and
resolves the selected series through `/translations/eng`; the original title
is retained as metadata and used only when an English title is unavailable,
never appended as a second title in the release basename.

## Naming profile

The versioned profile and examples are documented in
[docs/VMF_NAMING_PROFILE.md](docs/VMF_NAMING_PROFILE.md). The renderer uses
normalized facts and does not invent a source, service, edition, or release
group from a codec alone. `upbrr` was consulted as a behavior reference; it is
not a runtime dependency.

The preservation classifier is intentionally conservative and order
independent: it requires a real trailing release group, resolution, a movie
year or TV marker, and a release-type-specific codec/source signature. A
preserved item is shown in the plan but creates no rename operation; Apply is
disabled when the complete plan contains no changes.

## Safety and current limits

- Applying a plan changes paths but never media bytes. `REMUX` and `ENCODE`
  describe the existing release; they do not launch FFmpeg.
- A rename journal is kept next to the plan root as a hidden
  `.vmf-rename-*.json` file. Keep it until you no longer need Undo.
- Do not rename a path that a torrent client is currently seeding unless the
  torrent is intentionally being rebuilt; a path change changes torrent
  metadata even when bytes are identical.
- TVDB currently searches series; episode title/number may need confirmation
  in the metadata panel.

## Third-party runtime

The repository includes MediaInfo CLI/MediaInfoLib v26.05 for the portable
Windows build. MediaInfo is provided by MediaArea.net under the BSD 2-Clause
license included at `assets/mediainfo/LICENSE`. The official archive URL,
SHA-256 checksums, MediaInfo notices, and the libcurl license are documented in
[`assets/mediainfo/README.md`](assets/mediainfo/README.md).

## Data provider attribution

<a href="https://www.themoviedb.org/"><img src="frontend/src/assets/tmdb.svg" alt="The Movie Database (TMDB)" width="180"></a>

This product uses the TMDB API but is not endorsed or certified by TMDB.

TV metadata is provided by [TheTVDB](https://thetvdb.com/). Please consider
adding missing information or subscribing.
