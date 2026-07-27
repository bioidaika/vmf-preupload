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

Examples:

```text
Example.Movie.2026.ViE.2160p.NF.WEB-DL.DDP5.1.Atmos.DV.H.265-NoGroup.mkv
Example.Movie.2026.2160p.UHD.BluRay.REMUX.HDR.H.265.TrueHD.7.1-NoGroup.mkv
Example.Show.2026.S01E02.ViE.1080p.WEBRip.DDP5.1.x264-NoGroup.mkv
```

For a multi-file folder, each output file follows its own original basename.
The output folder carries `UHD` only when every source video filename carries
the explicit marker, preventing one episode from tagging the whole season.
