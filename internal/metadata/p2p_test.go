package metadata

import (
	"path/filepath"
	"testing"
)

func TestIsP2PReleaseNameAcceptsReleaseTypeConventions(t *testing.T) {
	tests := []string{
		"Gotham.S01E01.Pilot.1080p.DTS-HD.MA.5.1.AVC.REMUX-FraMeSToR.mkv",
		"Example.Movie.2024.1080p.NF.WEB-DL.DDP5.1.H.264-FLUX.mkv",
		"Example.Show.S01E02.1080p.AMZN.WEBRip.DDP5.1.H.264-NTb.mkv",
		"Example.Show.S01E03.1080p.WEB.H264-ETHEL.mkv",
		"Example.Movie.2024.2160p.WEB.H265-NAISU.mkv",
		"Example.Movie.2024.1080p.BluRay.DTS.5.1.x264-DON.mkv",
		"Example.Movie.2024.2160p.TrueHD.7.1.HEVC.REMUX-FraMeSToR.mkv",
		"1917.2019.1080p.BluRay.DTS.x264-SPARKS.mkv",
		"300.2006.1080p.BluRay.DTS.x264-GROUP1.mkv",
		"24.S01E01.1080p.WEB.H264-ETHEL.mkv",
	}
	for _, filename := range tests {
		t.Run(filename, func(t *testing.T) {
			if !IsP2PReleaseName(filename) {
				t.Fatalf("IsP2PReleaseName(%q)=false, want true", filename)
			}
		})
	}
}

func TestIsP2PReleaseNameRejectsWeakOrPlaceholderNames(t *testing.T) {
	tests := []string{
		"Spider-Man.mkv",
		"Vacation.1080p-John.mkv",
		"Example.Movie.2024.1080p.AVC.REMUX-NoGroup.mkv",
		"Example.Movie.2024.1080p.WEB-DL.H.264.mkv",
		"Example.Movie.2024.1080p.H.264-Group.mkv",
		"Example.Movie.REMUX.AVC-Group.mkv",
		"1080p.AVC.REMUX-Group.mkv",
		"Example.Movie.2024.1080p.BluRay.H.264-Group.mkv",
		"Example.Movie.2024.1080p.WEB-DL.H.264-Unknown.mkv",
		"Example.Movie.2024.1080p.WEB-DL.H.264-FLUX.txt",
	}
	for _, filename := range tests {
		t.Run(filename, func(t *testing.T) {
			if IsP2PReleaseName(filename) {
				t.Fatalf("IsP2PReleaseName(%q)=true, want false", filename)
			}
		})
	}
}

func TestIsP2PReleaseNameOnlyInspectsBasename(t *testing.T) {
	filename := filepath.Join("Incoming Folder", "Gotham.S01E01.Pilot.1080p.DTS-HD.MA.5.1.AVC.REMUX-FraMeSToR.mkv")
	if !IsP2PReleaseName(filename) {
		t.Fatalf("IsP2PReleaseName(%q)=false, want true", filename)
	}
}

func TestIsP2PReleaseFolderName(t *testing.T) {
	name := "Gotham.S01.1080p.DTS-HD.MA.5.1.AVC.REMUX-FraMeSToR"
	if !IsP2PReleaseFolderName(name) {
		t.Fatalf("IsP2PReleaseFolderName(%q)=false, want true", name)
	}
	if IsP2PReleaseFolderName("Incoming") {
		t.Fatal("unstructured folder must not be preserved as a P2P release")
	}
}

func TestIsP2PReleaseNameRequiresMovieYearOrTVMarker(t *testing.T) {
	if IsP2PReleaseName("Vacation.1080p.WEB-DL.H.264-John.mkv") {
		t.Fatal("release without a movie year or TV marker is too weak to preserve")
	}
	if IsP2PReleaseName("Movie.2024.1080p.WEB-DL.H.264-DL.mkv") {
		t.Fatal("structural WEB-DL token must not become a release group")
	}
	if IsP2PReleaseName("Vacation.1080p.WEB-DL.BT.2020.H.265-John.mkv") {
		t.Fatal("BT.2020 after the resolution must not become movie-year evidence")
	}
}
