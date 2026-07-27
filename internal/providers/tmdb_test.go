package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTMDBMovieRequestsAndUsesEnglishTitle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if got := r.URL.Query().Get("language"); got != tmdbEnglishLanguage {
			t.Errorf("language=%q want %q", got, tmdbEnglishLanguage)
		}
		switch r.URL.Path {
		case "/search/movie":
			_, _ = w.Write([]byte(`{"results":[{"id":42,"title":"English Movie","original_title":"Tên gốc","release_date":"2026-01-02","overview":"English overview"}]}`))
		case "/movie/42":
			_, _ = w.Write([]byte(`{"id":42,"title":"English Movie","original_title":"Tên gốc","release_date":"2026-01-02","overview":"English overview"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewTMDBClient("api-key")
	client.Base = server.URL
	client.HTTP = server.Client()
	results, err := client.SearchMovies(context.Background(), "Example", "2026")
	if err != nil {
		t.Fatalf("SearchMovies: %v", err)
	}
	if len(results) != 1 || results[0].Title != "English Movie" {
		t.Fatalf("English search title was not used: %#v", results)
	}
	detail, err := client.Movie(context.Background(), "42")
	if err != nil {
		t.Fatalf("Movie: %v", err)
	}
	if detail.Title != "English Movie" {
		t.Fatalf("English detail title was not used: %#v", detail)
	}
}

func TestTMDBMovieFallsBackToOriginalWhenEnglishTitleMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"id":42,"title":"","original_title":"Original Title"}]}`))
	}))
	defer server.Close()

	client := NewTMDBClient("api-key")
	client.Base = server.URL
	client.HTTP = server.Client()
	results, err := client.SearchMovies(context.Background(), "Example", "")
	if err != nil {
		t.Fatalf("SearchMovies: %v", err)
	}
	if len(results) != 1 || results[0].Title != "Original Title" {
		t.Fatalf("original-title fallback failed: %#v", results)
	}
}
