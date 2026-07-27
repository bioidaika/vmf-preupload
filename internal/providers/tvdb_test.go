package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTVDBSearchAcceptsNumericAndObjectIDs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/login":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"token": "test-token"}})
		case "/search":
			if got := r.URL.Query().Get("language"); got != "" {
				t.Errorf("search language must not filter non-English primary series, got %q", got)
			}
			_, _ = w.Write([]byte(`{"data":[{"id":12345,"name":"Tên gốc","translations":{"eng":"Example Series"},"overviews":{"eng":"English overview"},"year":"2026","type":"series","image_url":"https://img.invalid/a.jpg"},{"objectID":"series-67890","name":"Another Series","type":"series"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewTVDBClient("api-key", "pin")
	client.Base = server.URL
	client.HTTP = server.Client()
	results, err := client.SearchSeries(context.Background(), "Example")
	if err != nil {
		t.Fatalf("SearchSeries: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results: %#v", len(results), results)
	}
	if results[0].ID != "12345" || results[0].PosterPath == "" {
		t.Fatalf("numeric id/image was not normalized: %#v", results[0])
	}
	if results[0].Title != "Example Series" || results[0].Overview != "English overview" {
		t.Fatalf("English search translation was not preferred: %#v", results[0])
	}
	if results[1].ID != "67890" {
		t.Fatalf("objectID fallback was not normalized: %#v", results[1])
	}
	if results[1].Title != "Another Series" {
		t.Fatalf("missing English translation should fall back to canonical name: %#v", results[1])
	}
}

func TestTVDBMissingEnglishTranslationFallsBackToOriginal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/login":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"token": "test-token"}})
		case "/series/123/extended":
			_, _ = w.Write([]byte(`{"data":{"id":123,"name":"Original Series","year":"2026"}}`))
		case "/series/123/episodes/default":
			_, _ = w.Write([]byte(`{"data":{"episodes":[{"id":456,"number":2,"seasonNumber":1,"name":"Original Episode"}]}}`))
		case "/series/123/translations/eng", "/episodes/456/translations/eng":
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewTVDBClient("api-key", "")
	client.Base = server.URL
	client.HTTP = server.Client()
	series, err := client.Series(context.Background(), "123")
	if err != nil || series.Title != "Original Series" {
		t.Fatalf("series fallback failed: result=%#v err=%v", series, err)
	}
	episode, err := client.Episode(context.Background(), "123", 1, 2)
	if err != nil || episode.Title != "Original Episode" {
		t.Fatalf("episode fallback failed: result=%#v err=%v", episode, err)
	}
}

func TestTVDBSeriesAndEpisodePreferEnglishTranslations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/login":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"token": "test-token"}})
		case "/series/123/extended":
			_, _ = w.Write([]byte(`{"data":{"id":123,"name":"Original Series","year":"2026","overview":"Original overview"}}`))
		case "/series/123/translations/eng":
			_, _ = w.Write([]byte(`{"data":{"language":"eng","name":"English Series","overview":"English overview"}}`))
		case "/series/123/episodes/default":
			if r.URL.Query().Get("season") != "1" || r.URL.Query().Get("episodeNumber") != "2" {
				t.Errorf("episode lookup did not constrain season/episode: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"data":{"episodes":[{"id":456,"number":2,"seasonNumber":1,"name":"Original Episode","overview":"Original episode overview"}]}}`))
		case "/episodes/456/translations/eng":
			_, _ = w.Write([]byte(`{"data":{"language":"eng","name":"English Episode","overview":"English episode overview"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewTVDBClient("api-key", "")
	client.Base = server.URL
	client.HTTP = server.Client()
	series, err := client.Series(context.Background(), "123")
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if series.Title != "English Series" || series.Overview != "English overview" {
		t.Fatalf("series English translation was not preferred: %#v", series)
	}
	episode, err := client.Episode(context.Background(), "123", 1, 2)
	if err != nil {
		t.Fatalf("Episode: %v", err)
	}
	if episode.Title != "English Episode" {
		t.Fatalf("translated episode endpoint was not used: %#v", episode)
	}
}

func TestTVDBSearchEmptyQueryDoesNotCallAPI(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewTVDBClient("api-key", "")
	client.Base = server.URL
	client.HTTP = server.Client()
	results, err := client.SearchSeries(context.Background(), "  ")
	if err != nil {
		t.Fatalf("SearchSeries empty query: %v", err)
	}
	if called || len(results) != 0 {
		t.Fatalf("empty query should be a no-op: called=%v results=%#v", called, results)
	}
}
