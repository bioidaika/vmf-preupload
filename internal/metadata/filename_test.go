package metadata

import "testing"

func TestParseFilenameExtractsOptionalTokens(t *testing.T) {
	got := ParseFilename("Example.Show.S01E02.2160p.NF.WEB-DL.HDR.H.265-GRP.mkv")
	if got.Category != "TV" || got.Season != "01" || got.Episode != "02" {
		t.Fatalf("episode parse = %#v", got)
	}
	if got.Service != "NF" || got.ReleaseType != "WEBDL" || got.ReleaseGroup != "GRP" {
		t.Fatalf("optional token parse = %#v", got)
	}
	// This input deliberately does not contain an explicit UHD token.
	if got.UHD != "" {
		t.Fatalf("unexpected UHD hint %q", got.UHD)
	}
}

func TestParseFilenamePreservesExplicitUHDAndNoGroupFallback(t *testing.T) {
	got := ParseFilename("Example.Movie.2026.UHD.BluRay.REMUX.2160p.mkv")
	if got.UHD != "UHD" || got.Source != "BluRay" || got.ReleaseType != "REMUX" {
		t.Fatalf("UHD/remux parse = %#v", got)
	}
	if got.ReleaseGroup != "NoGroup" {
		t.Fatalf("group fallback = %q", got.ReleaseGroup)
	}
}

func TestParseFilenameNormalizesServiceAliasesAndUltraHD(t *testing.T) {
	got := ParseFilename("Example.Movie.2026.Ultra.HD.BluRay.REMUX.2160p.Netflix-GRP.mkv")
	if got.UHD != "UHD" {
		t.Fatalf("Ultra HD marker was not preserved: %#v", got)
	}
	if got.Service != "NF" {
		t.Fatalf("Netflix alias was not normalized: %#v", got)
	}
	if got.ReleaseGroup != "GRP" {
		t.Fatalf("release group was not parsed: %#v", got)
	}
}

func TestParseFilenameDoesNotTreatTitleHyphenAsReleaseGroup(t *testing.T) {
	got := ParseFilename("Spider-Man.mkv")
	if got.ReleaseGroup != "NoGroup" {
		t.Fatalf("title hyphen was parsed as group: %#v", got)
	}
}

func TestParseFilenameDoesNotTreatWebDLAsReleaseGroup(t *testing.T) {
	got := ParseFilename("Example.Movie.2024.1080p.WEB-DL.mkv")
	if got.ReleaseGroup != "NoGroup" {
		t.Fatalf("WEB-DL suffix was parsed as group: %#v", got)
	}
}

func TestParseFilenameSeparatesSeriesAndEpisodeTitle(t *testing.T) {
	got := ParseFilename("Gotham.S01E01.Pilot.1080p.DTS-HD.MA.5.1.AVC.REMUX-FraMeSToR.mkv")
	if got.Category != "TV" || got.Season != "01" || got.Episode != "01" {
		t.Fatalf("episode identity parse = %#v", got)
	}
	if got.Title != "Gotham" || got.EpisodeTitle != "Pilot" {
		t.Fatalf("series/episode title parse = %#v", got)
	}
	if got.ReleaseType != "REMUX" || got.ReleaseGroup != "FraMeSToR" {
		t.Fatalf("release type/group parse = %#v", got)
	}
}

func TestParseFilenameDoesNotInventEpisodeTitleFromTechnicalSuffix(t *testing.T) {
	got := ParseFilename("Gotham.S01E02.1080p.WEB-DL.H.264-GRP.mkv")
	if got.Title != "Gotham" || got.EpisodeTitle != "" {
		t.Fatalf("technical suffix became episode title: %#v", got)
	}
}

func TestParseFilenameDoesNotTreatSeasonPackTextAsEpisodeTitle(t *testing.T) {
	got := ParseFilename("Gotham.S01.Complete.1080p.BluRay.x264-GRP.mkv")
	if got.Title != "Gotham" || got.EpisodeTitle != "" {
		t.Fatalf("season pack text became an episode title: %#v", got)
	}
}

func TestParseFilenameRequiresEpisodeTokenBoundary(t *testing.T) {
	for _, filename := range []string{"S1m0ne.2002.1080p.mkv", "Show.S01E01foo.1080p.mkv"} {
		if got := ParseFilename(filename); got.Category == "TV" {
			t.Fatalf("%q was misclassified as TV: %#v", filename, got)
		}
	}
}

func TestParseFilenameRetainsMultiEpisodeMarker(t *testing.T) {
	got := ParseFilename("Show.S01E01E02.Pilot.1080p.WEB-DL.mkv")
	if got.Season != "01" || got.Episode != "E01E02" || got.EpisodeTitle != "Pilot" {
		t.Fatalf("multi-episode parse = %#v", got)
	}
}

func TestParseFilenameDoesNotTreatServiceAsEpisodeTitle(t *testing.T) {
	got := ParseFilename("Show.S01E01.NF.WEB-DL.H.264.mkv")
	if got.EpisodeTitle != "" || got.Service != "NF" {
		t.Fatalf("service parse = %#v", got)
	}
}

func TestParseFilenameKeepsHyphenatedEpisodeTitle(t *testing.T) {
	got := ParseFilename("Show.S01E01.The-End.mkv")
	if got.EpisodeTitle != "The End" {
		t.Fatalf("hyphenated episode title was truncated: %#v", got)
	}
}

func TestParseFilenameStopsEpisodeTitleAtCompactAudio(t *testing.T) {
	for _, filename := range []string{"Show.S01E01.DD5.1.1080p.mkv", "Show.S01E02.AAC2.0.720p.mkv"} {
		if got := ParseFilename(filename); got.EpisodeTitle != "" {
			t.Fatalf("compact audio became episode title for %q: %#v", filename, got)
		}
	}
}
