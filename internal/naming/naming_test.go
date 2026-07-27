package naming

import (
	"strings"
	"testing"
)

func TestRenderCategoryAndReleaseTypeMatrix(t *testing.T) {
	tests := []struct {
		name        string
		category    string
		releaseType string
		season      string
		episode     string
		want        string
	}{
		{name: "movie webdl", category: Movie, releaseType: WebDL, want: "Sample.Title.2024.1080p.NF.WEB-DL.AAC2.0.HDR.x264-Team"},
		{name: "movie webrip", category: Movie, releaseType: WebRip, want: "Sample.Title.2024.1080p.NF.WEBRip.AAC2.0.HDR.x264-Team"},
		{name: "movie remux", category: Movie, releaseType: Remux, want: "Sample.Title.2024.1080p.BluRay.REMUX.HDR.H.264.AAC2.0-Team"},
		{name: "movie encode", category: Movie, releaseType: Encode, want: "Sample.Title.2024.1080p.BluRay.AAC2.0.HDR.x264-Team"},
		{name: "tv webdl", category: TV, releaseType: WebDL, season: "1", episode: "2", want: "Sample.Title.2024.S01E02.1080p.NF.WEB-DL.AAC2.0.HDR.x264-Team"},
		{name: "tv webrip", category: TV, releaseType: WebRip, season: "1", episode: "2", want: "Sample.Title.2024.S01E02.1080p.NF.WEBRip.AAC2.0.HDR.x264-Team"},
		{name: "tv remux", category: TV, releaseType: Remux, season: "1", episode: "2", want: "Sample.Title.2024.S01E02.1080p.BluRay.REMUX.HDR.H.264.AAC2.0-Team"},
		{name: "tv encode", category: TV, releaseType: Encode, season: "1", episode: "2", want: "Sample.Title.2024.S01E02.1080p.BluRay.AAC2.0.HDR.x264-Team"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, warnings := Render(Metadata{
				Category:      tt.category,
				ReleaseType:   tt.releaseType,
				Title:         "Sample Title",
				Year:          2024,
				Season:        tt.season,
				Episode:       tt.episode,
				Resolution:    "1080p",
				Source:        "BluRay",
				Service:       "NF",
				HDR:           "HDR",
				VideoCodec:    "H.264",
				VideoEncode:   "x264",
				AudioCodec:    "AAC",
				AudioChannels: "2.0",
				Group:         "Team",
			}, DefaultProfile())
			if len(warnings) != 0 {
				t.Fatalf("unexpected warnings: %#v", warnings)
			}
			if got != tt.want {
				t.Fatalf("Render() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderMovieWebDLUsesDotsAndNoImplicitUHD(t *testing.T) {
	got, warnings := Render(Metadata{
		Category:    Movie,
		ReleaseType: WebDL,
		Title:       "Example Movie",
		Year:        2026,
		Resolution:  "3840x2160",
		Service:     "NF",
		Audio:       "DDP5.1.Atmos",
		HDR:         "DV.HDR",
		VideoCodec:  "H.265",
	}, DefaultProfile())
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %#v", warnings)
	}
	want := "Example.Movie.2026.2160p.NF.WEB-DL.DDP5.1.Atmos.DV.HDR.H.265-NoGroup"
	if got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
}

func TestRenderMovieWebRipOmitsMissingService(t *testing.T) {
	got, _ := Render(Metadata{
		Category:      Movie,
		ReleaseType:   WebRip,
		Title:         "Synthetic Film",
		Year:          2025,
		Resolution:    "1080p",
		AudioCodec:    "E-AC-3",
		AudioChannels: "5.1",
		VideoEncode:   "x264",
		Group:         "TestGroup",
	}, DefaultProfile())
	want := "Synthetic.Film.2025.1080p.WEBRip.DDP5.1.x264-TestGroup"
	if got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
}

func TestRenderEmitsEnglishTitleOnceAndKeepsMPEGAudioCompact(t *testing.T) {
	got, _ := Render(Metadata{
		Category:      TV,
		ReleaseType:   Encode,
		Title:         "The Blood of Youth Quest of Heroic Hearts",
		OriginalTitle: "少年歌行之天下无双",
		Year:          2026,
		Season:        "1",
		Resolution:    "2160p",
		Audio:         "MP2.2.0",
		AudioTracks:   []AudioTrack{{Language: "vi"}},
		VideoCodec:    "H.265",
	}, DefaultProfile())
	want := "The.Blood.of.Youth.Quest.of.Heroic.Hearts.2026.S01.ViE.2160p.MP2.2.0.H.265-NoGroup"
	if got != want {
		t.Fatalf("Render()=%q want %q", got, want)
	}
}

func TestRenderUsesOriginalTitleOnlyAsFallback(t *testing.T) {
	for _, category := range []string{Movie, TV} {
		t.Run(category, func(t *testing.T) {
			metadata := Metadata{
				Category:      category,
				ReleaseType:   WebDL,
				Title:         "   ",
				OriginalTitle: "少年歌行之天下无双",
				Year:          2026,
				Resolution:    "1080p",
				VideoCodec:    "H.265",
			}
			if category == TV {
				metadata.Season = "1"
			}
			got, _ := Render(metadata, DefaultProfile())
			want := "少年歌行之天下无双.2026"
			if category == TV {
				want += ".S01"
			}
			want += ".1080p.WEB-DL.H.265-NoGroup"
			if got != want {
				t.Fatalf("Render()=%q want %q", got, want)
			}
		})
	}
}

func TestRenderTVRemuxPlacesEpisodeAndTechnicalFields(t *testing.T) {
	got, _ := Render(Metadata{
		Category:     TV,
		ReleaseType:  Remux,
		Title:        "Example Show",
		Year:         2024,
		Season:       "1",
		Episode:      "2",
		EpisodeTitle: "The Beginning",
		Resolution:   "2160p",
		Source:       "BluRay",
		HDR:          "HDR",
		VideoCodec:   "H.265",
		Audio:        "TrueHD.7.1.Atmos",
	}, DefaultProfile())
	want := "Example.Show.2024.S01E02.The.Beginning.2160p.BluRay.REMUX.HDR.H.265.TrueHD.7.1.Atmos-NoGroup"
	if got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
}

func TestRenderTVEncodeAddsVietnameseDubBeforeResolution(t *testing.T) {
	got, _ := Render(Metadata{
		Category:    TV,
		ReleaseType: Encode,
		Title:       "Example Series",
		Year:        2023,
		Season:      "S2",
		Episode:     "E3",
		Resolution:  "1920x1080",
		Source:      "WEB",
		AudioTracks: []AudioTrack{{Language: "vi-VN", Title: "Lồng tiếng", Codec: "AAC", Channels: "2.0"}},
		VideoEncode: "x265",
		Group:       "",
	}, DefaultProfile())
	want := "Example.Series.2023.S02E03.ViE.DUB.1080p.WEB.AAC2.0.x265-NoGroup"
	if got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
}

func TestRenderNormalizesScannerAudioComposite(t *testing.T) {
	got, _ := Render(Metadata{
		Category:    Movie,
		ReleaseType: WebDL,
		Title:       "Audio Sample",
		Year:        2024,
		Resolution:  "1080p",
		Audio:       "DDP.5.1.Atmos",
		VideoCodec:  "H.264",
	}, DefaultProfile())
	want := "Audio.Sample.2024.1080p.WEB-DL.DDP5.1.Atmos.H.264-NoGroup"
	if got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
}

func TestRenderIncludesUHDOnlyFromExplicitFilenameEvidence(t *testing.T) {
	tests := []struct {
		name        string
		releaseType string
		source      string
		resolution  string
		sourcePath  string
		other       []string
		explicitUHD bool
		inferredUHD bool
		wantUHD     bool
	}{
		{name: "web dl 2160p", releaseType: WebDL, source: "WEB", resolution: "2160p"},
		{name: "web rip 2160p", releaseType: WebRip, source: "WEB", resolution: "2160p"},
		{name: "web encode 2160p", releaseType: Encode, source: "WEB", resolution: "2160p"},
		{name: "blu-ray encode 2160p", releaseType: Encode, source: "BluRay", resolution: "2160p"},
		{name: "source-less encode 2160p", releaseType: Encode, resolution: "2160p"},
		{name: "remux 2160p", releaseType: Remux, source: "BluRay", resolution: "2160p"},
		{name: "parent path is not evidence", releaseType: WebRip, source: "WEB", resolution: "2160p", sourcePath: "C:/UHD Archive/Example.mkv"},
		{name: "ultra source is not evidence", releaseType: WebDL, source: "Ultra HDTV", resolution: "1080p"},
		{name: "release other is not evidence", releaseType: WebDL, source: "WEB", resolution: "1080p", other: []string{"Ultra HD"}},
		{name: "1080p remux", releaseType: Remux, source: "BluRay", resolution: "1080p"},
		{name: "legacy inferred flag ignored", releaseType: Remux, source: "BluRay", resolution: "2160p", inferredUHD: true},
		{name: "explicit filename flag", releaseType: WebDL, source: "WEB", resolution: "1080p", explicitUHD: true, wantUHD: true},
		{name: "legacy force flag ignored", releaseType: WebDL, source: "WEB", resolution: "2160p"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := Render(Metadata{
				Category: Movie, ReleaseType: tt.releaseType, Title: "Example Source", Year: 2022,
				Resolution: tt.resolution, Source: tt.source, SourcePath: tt.sourcePath, Other: tt.other,
				VideoCodec: "H.265", UHD: tt.explicitUHD, UHDInferred: tt.inferredUHD,
			}, func() Profile {
				profile := DefaultProfile()
				if tt.name == "legacy force flag ignored" {
					profile.IncludeUHD = true
				}
				return profile
			}())
			hasUHD := strings.Contains(got, ".UHD.")
			if hasUHD != tt.wantUHD {
				t.Fatalf("UHD presence=%t in %q, want %t", hasUHD, got, tt.wantUHD)
			}
		})
	}
}

func TestVMFTagStrongestAndExistingFallback(t *testing.T) {
	if got := VMFTag(Metadata{AudioTracks: []AudioTrack{{Language: "Vietnamese", Title: "Thuyết minh"}}}); got != "ViE" {
		t.Fatalf("VMFTag(thuyet minh) = %q, want ViE", got)
	}
	if got := VMFTag(Metadata{AudioTracks: []AudioTrack{{Language: "vi", Title: "Lồng tiếng"}}}); got != "ViE.DUB" {
		t.Fatalf("VMFTag(dub) = %q, want ViE.DUB", got)
	}
	if got := VMFTag(Metadata{ExistingName: "Show.S01E01.ViE.DUB.1080p.WEBRip-NoGroup"}); got != "ViE.DUB" {
		t.Fatalf("VMFTag(existing) = %q, want ViE.DUB", got)
	}
}

func TestRenderWarningsForMissingTitleAndUnknownType(t *testing.T) {
	got, warnings := Render(Metadata{Category: Movie, ReleaseType: "mystery", Year: 2026}, DefaultProfile())
	if got != "" {
		t.Fatalf("missing title Render() = %q, want empty", got)
	}
	if len(warnings) < 2 {
		t.Fatalf("warnings = %#v, want missing title and unknown type", warnings)
	}
}
