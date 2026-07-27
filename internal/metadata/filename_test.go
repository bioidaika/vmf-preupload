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
