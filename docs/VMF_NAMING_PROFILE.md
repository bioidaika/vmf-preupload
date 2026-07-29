# VMF profile (`vmf@2`)

The renderer is deterministic and works on normalized facts. It does not
invent a service, source, edition, or release group from a codec alone.

## Defaults

- separator: `.`
- group fallback: `NoGroup`
- title: emit the English provider title once. Keep the original-language title
  as metadata and use it only when an English title is unavailable.
- `UHD`: retain only a standalone `UHD`/`Ultra HD` marker from the original
  video filename. Never infer it from 4K/2160p, release type, source,
  MediaInfo, a parent-folder name, or a legacy force flag.
- service: optional; retain only when found in the old name or entered by the
  user
- existing P2P: preserve the basename exactly by default when a real group,
  identity, resolution, and release signature are all present. This protection
  is evaluated per file and can be disabled to force VMF rendering.

## Token order

Movie and TV identity fields are followed by the technical fields below.
`ViE`/`ViE.DUB` is inserted immediately before resolution.

```text
WEB-DL:  Title.Year.Edition.ViE.Resolution.Service.WEB-DL.Audio.HDR.Video-Group
WEBRip:  Title.Year.Edition.ViE.Resolution.Service.WEBRip.Audio.HDR.Video-Group
REMUX:   Title.Year.Edition.ViE.Resolution.Source.REMUX.HDR.Video.Audio-Group
ENCODE:  Title.Year.Edition.ViE.Resolution.Source.Audio.HDR.Video-Group
```

TV episodes put `SxxEyy` and the episode title after the series identity. A
season folder omits the individual episode marker while its files retain it.

MediaInfo `MPEG Audio` is resolved with `Format_Profile` and `CodecID`: Layer 2
becomes `MP2`, Layer 3 becomes `MP3`, and the channel token remains separate
(`MP2.2.0`/`MP3.2.0`). An unknown MPEG layer is never guessed.

Video spelling follows the release mode:

- REMUX uses bitstream names (`AVC` / `HEVC`);
- WEB-DL uses delivery codec names (`H.264` / `H.265`);
- ENCODE and WEBRip use `x264` / `x265` only when MediaInfo identifies that
  encoder library; otherwise they retain the codec name and do not guess.

Examples:

```text
Example.Movie.2026.ViE.2160p.NF.WEB-DL.DDP5.1.Atmos.DV.H.265-NoGroup.mkv
Example.Movie.2026.2160p.UHD.BluRay.REMUX.HDR.HEVC.TrueHD.7.1-NoGroup.mkv
Example.Show.2026.S01E02.ViE.1080p.WEBRip.DDP5.1.x264-NoGroup.mkv
```

For a multi-file folder, each output file follows its own original basename.
The output folder carries `UHD` only when every source video filename carries
the explicit marker, preventing one episode from tagging the whole season.

## TV directory topology

- A selected single-season folder is one release unit and is rendered with its
  `Sxx` marker.
- A series container with direct `Season N`/`SNN` children is not renamed.
  Every recognized season child is rendered independently, and episode paths
  are mapped through that season folder's destination.
- A flat container with files from multiple seasons is kept as the series
  root. The transaction creates one P2P folder per season, then moves files
  into the matching folder using their own `SxxEyy` facts. Undo restores the
  flat layout and removes only transaction-created folders that are empty.
- If a season folder conflicts with a filename (`Season 1` containing S02), or
  two siblings resolve to the same season destination, the whole plan is
  blocked before Apply.
- A season bucket with different technical naming signatures (for example a
  WEB-DL 1080p episode and a REMUX 2160p episode) is also blocked instead of
  being labeled from the first file.
- Optional pack tags such as service and `ViE`/`DUB` are emitted only when all
  episodes in that season support the same tag, unless the user overrides it.
- Regular files which are not upload video payloads are separated from season
  torrents. Series containers use `<series>/Extras/<original relative path>`;
  selected movie/single-season release roots use
  `<parent>/Extras/<release name>/<original relative path>` so Extras remains
  outside the folder submitted to the tracker.
- Excluded sample videos (a final `sample` token or a `Sample` directory) and
  files below an existing `Extras` or hidden directory follow the same Extras
  policy. A leading, series-level `Extras` component is stripped when mapping
  destinations, making repeated previews idempotent rather than producing
  `Extras/Extras`.
- Extras moves use the same transaction journal as release renames. Undo puts
  every file back and removes only empty directories created by that journal;
  symlinks and special filesystem entries are never moved automatically.
- The preview records a lightweight filesystem snapshot (without rerunning
  MediaInfo). Apply is blocked if the selected tree changed or traversal was
  incomplete, preventing a newly arrived sidecar from being carried into a
  renamed torrent folder invisibly.
