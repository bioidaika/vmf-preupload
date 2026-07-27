package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bioidaika/vmf-preupload/pkg/api"
)

type TMDBClient struct {
	APIKey string
	HTTP   *http.Client
	Base   string
}

const tmdbEnglishLanguage = "en-US"

func NewTMDBClient(key string) *TMDBClient {
	return &TMDBClient{APIKey: strings.TrimSpace(key), HTTP: &http.Client{Timeout: 15 * time.Second}, Base: "https://api.themoviedb.org/3"}
}

func (c *TMDBClient) SearchMovies(ctx context.Context, query, year string) ([]api.ProviderCandidate, error) {
	if c == nil || c.APIKey == "" {
		return nil, fmt.Errorf("TMDB API key is not configured")
	}
	values := url.Values{"api_key": {c.APIKey}, "query": {query}, "include_adult": {"false"}, "language": {tmdbEnglishLanguage}}
	if year != "" {
		values.Set("year", year)
	}
	var payload struct {
		Results []struct {
			ID          int    `json:"id"`
			Title       string `json:"title"`
			Original    string `json:"original_title"`
			ReleaseDate string `json:"release_date"`
			Overview    string `json:"overview"`
			PosterPath  string `json:"poster_path"`
		} `json:"results"`
	}
	if err := c.get(ctx, "/search/movie", values, &payload); err != nil {
		return nil, err
	}
	result := make([]api.ProviderCandidate, 0, len(payload.Results))
	for _, item := range payload.Results {
		result = append(result, api.ProviderCandidate{Provider: "TMDB", ID: strconv.Itoa(item.ID), Title: firstNonEmpty(item.Title, item.Original), Original: item.Original, Year: yearFromDate(item.ReleaseDate), Overview: item.Overview, PosterPath: item.PosterPath, MediaType: "movie"})
	}
	return result, nil
}

func (c *TMDBClient) Movie(ctx context.Context, id string) (api.ProviderCandidate, error) {
	if c == nil || c.APIKey == "" {
		return api.ProviderCandidate{}, fmt.Errorf("TMDB API key is not configured")
	}
	values := url.Values{"api_key": {c.APIKey}, "append_to_response": {"external_ids"}, "language": {tmdbEnglishLanguage}}
	var item struct {
		ID          int    `json:"id"`
		Title       string `json:"title"`
		Original    string `json:"original_title"`
		ReleaseDate string `json:"release_date"`
		Overview    string `json:"overview"`
		PosterPath  string `json:"poster_path"`
		ExternalIDs struct {
			IMDB string `json:"imdb_id"`
		} `json:"external_ids"`
	}
	if err := c.get(ctx, "/movie/"+url.PathEscape(id), values, &item); err != nil {
		return api.ProviderCandidate{}, err
	}
	return api.ProviderCandidate{Provider: "TMDB", ID: strconv.Itoa(item.ID), Title: firstNonEmpty(item.Title, item.Original), Original: item.Original, Year: yearFromDate(item.ReleaseDate), Overview: item.Overview, PosterPath: item.PosterPath, MediaType: "movie"}, nil
}

func (c *TMDBClient) get(ctx context.Context, endpoint string, values url.Values, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.Base, "/")+endpoint+"?"+values.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("TMDB request failed: HTTP %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func yearFromDate(value string) string {
	if len(value) >= 4 {
		return value[:4]
	}
	return ""
}
