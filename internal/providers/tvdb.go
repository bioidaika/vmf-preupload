package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bioidaika/vmf-preupload/pkg/api"
)

type TVDBClient struct {
	APIKey string
	PIN    string
	HTTP   *http.Client
	Base   string

	mu      sync.Mutex
	token   string
	expires time.Time
}

const tvdbEnglishLanguage = "eng"

type tvdbTranslation struct {
	Language string `json:"language"`
	Name     string `json:"name"`
	Overview string `json:"overview"`
}

func NewTVDBClient(key, pin string) *TVDBClient {
	return &TVDBClient{APIKey: strings.TrimSpace(key), PIN: strings.TrimSpace(pin), HTTP: &http.Client{Timeout: 15 * time.Second}, Base: "https://api4.thetvdb.com/v4"}
}

func (c *TVDBClient) SearchSeries(ctx context.Context, query string) ([]api.ProviderCandidate, error) {
	if c == nil || c.APIKey == "" {
		return nil, fmt.Errorf("TVDB API key is not configured")
	}
	if strings.TrimSpace(query) == "" {
		return []api.ProviderCandidate{}, nil
	}
	values := url.Values{"query": {query}, "type": {"series"}, "limit": {"25"}}
	var payload struct {
		Data []struct {
			// TVDB has returned both `id` and `tvdb_id` over the lifetime of
			// the v4 search endpoint, and some deployments encode the value as
			// a JSON number while others use a string. RawMessage lets us accept
			// both without making a failed search look like an auth error.
			ID             json.RawMessage   `json:"id"`
			TVDBID         json.RawMessage   `json:"tvdb_id"`
			ObjectID       string            `json:"objectID"`
			Name           string            `json:"name"`
			NameTranslated string            `json:"name_translated"`
			Year           string            `json:"year"`
			Type           string            `json:"type"`
			Overview       string            `json:"overview"`
			Overviews      map[string]string `json:"overviews"`
			Translations   map[string]string `json:"translations"`
			ImageURL       string            `json:"image_url"`
			Image          string            `json:"image"`
			Thumbnail      string            `json:"thumbnail"`
		} `json:"data"`
	}
	if err := c.get(ctx, "/search", values, &payload); err != nil {
		return nil, err
	}
	result := make([]api.ProviderCandidate, 0, len(payload.Data))
	for _, item := range payload.Data {
		id := rawJSONText(item.ID)
		if id == "" {
			id = rawJSONText(item.TVDBID)
		}
		if id == "" {
			// `objectID` is commonly formatted as "series-12345".
			id = strings.TrimPrefix(strings.TrimSpace(item.ObjectID), "series-")
		}
		if id == "" {
			continue
		}
		mediaType := item.Type
		if mediaType == "" {
			mediaType = "series"
		}
		poster := firstNonEmpty(item.ImageURL, item.Image, item.Thumbnail)
		// TVDB search spans all aliases/translations. Do not send language=eng:
		// that parameter filters records whose *primary* language is English and
		// would hide many foreign series. Instead select the English translation
		// included in each search hit and fall back to the canonical name.
		title := firstNonEmpty(item.Translations[tvdbEnglishLanguage], item.Translations["en"], item.NameTranslated, item.Name)
		overview := firstNonEmpty(item.Overviews[tvdbEnglishLanguage], item.Overviews["en"], item.Overview)
		result = append(result, api.ProviderCandidate{Provider: "TVDB", ID: id, Title: title, Original: item.Name, Year: item.Year, Overview: overview, PosterPath: poster, MediaType: mediaType})
	}
	return result, nil
}

func rawJSONText(value json.RawMessage) string {
	if len(value) == 0 || string(value) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(value, &text) == nil {
		return strings.TrimSpace(text)
	}
	var number json.Number
	if json.Unmarshal(value, &number) == nil {
		return number.String()
	}
	return strings.Trim(strings.TrimSpace(string(value)), `"`)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (c *TVDBClient) Series(ctx context.Context, id string) (api.ProviderCandidate, error) {
	var payload struct {
		Data struct {
			ID         int    `json:"id"`
			Name       string `json:"name"`
			Year       string `json:"year"`
			FirstAired string `json:"firstAired"`
			Overview   string `json:"overview"`
			Image      string `json:"image"`
			RemoteIDs  []struct {
				ID string `json:"id"`
			} `json:"remoteIds"`
		} `json:"data"`
	}
	values := url.Values{"short": {"true"}}
	if err := c.get(ctx, "/series/"+url.PathEscape(id)+"/extended", values, &payload); err != nil {
		return api.ProviderCandidate{}, err
	}
	item := payload.Data
	year := item.Year
	if year == "" && len(item.FirstAired) >= 4 {
		year = item.FirstAired[:4]
	}
	translation := tvdbTranslation{}
	var translatedPayload struct {
		Data tvdbTranslation `json:"data"`
	}
	if err := c.get(ctx, "/series/"+url.PathEscape(id)+"/translations/"+tvdbEnglishLanguage, nil, &translatedPayload); err == nil {
		translation = translatedPayload.Data
	}
	return api.ProviderCandidate{Provider: "TVDB", ID: strconv.Itoa(item.ID), Title: firstNonEmpty(translation.Name, item.Name), Original: item.Name, Year: year, Overview: firstNonEmpty(translation.Overview, item.Overview), PosterPath: item.Image, MediaType: "series"}, nil
}

func (c *TVDBClient) Episode(ctx context.Context, seriesID string, season, episode int) (api.ProviderCandidate, error) {
	values := url.Values{"season": {strconv.Itoa(season)}, "episodeNumber": {strconv.Itoa(episode)}, "page": {"0"}}
	var payload struct {
		Data struct {
			Episodes []struct {
				ID       int    `json:"id"`
				Number   int    `json:"number"`
				Season   int    `json:"seasonNumber"`
				Name     string `json:"name"`
				Overview string `json:"overview"`
			} `json:"episodes"`
		} `json:"data"`
	}
	if err := c.get(ctx, "/series/"+url.PathEscape(seriesID)+"/episodes/default", values, &payload); err != nil {
		return api.ProviderCandidate{}, err
	}
	for _, item := range payload.Data.Episodes {
		if item.Season == season && item.Number == episode {
			translation := tvdbTranslation{}
			var translatedPayload struct {
				Data tvdbTranslation `json:"data"`
			}
			if err := c.get(ctx, "/episodes/"+strconv.Itoa(item.ID)+"/translations/"+tvdbEnglishLanguage, nil, &translatedPayload); err == nil {
				translation = translatedPayload.Data
			}
			return api.ProviderCandidate{Provider: "TVDB", ID: strconv.Itoa(item.ID), Title: firstNonEmpty(translation.Name, item.Name), Original: item.Name, Overview: firstNonEmpty(translation.Overview, item.Overview), MediaType: "episode"}, nil
		}
	}
	return api.ProviderCandidate{}, fmt.Errorf("TVDB episode S%02dE%02d not found", season, episode)
}

func (c *TVDBClient) get(ctx context.Context, endpoint string, values url.Values, target any) error {
	token, err := c.auth(ctx)
	if err != nil {
		return err
	}
	u := strings.TrimRight(c.Base, "/") + endpoint
	if len(values) > 0 {
		u += "?" + values.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		c.invalidate()
		return fmt.Errorf("TVDB token expired; retry the request")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("TVDB request failed: HTTP %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func (c *TVDBClient) auth(ctx context.Context) (string, error) {
	c.mu.Lock()
	if c.token != "" && time.Now().Before(c.expires) {
		token := c.token
		c.mu.Unlock()
		return token, nil
	}
	c.mu.Unlock()
	if c.APIKey == "" {
		return "", fmt.Errorf("TVDB API key is not configured")
	}
	payload := map[string]string{"apikey": c.APIKey}
	if c.PIN != "" {
		payload["pin"] = c.PIN
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.Base, "/")+"/login", strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("TVDB login failed: HTTP %s", resp.Status)
	}
	var result struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.Data.Token == "" {
		return "", fmt.Errorf("TVDB login returned no token")
	}
	c.mu.Lock()
	c.token = result.Data.Token
	c.expires = time.Now().Add(24 * time.Hour)
	c.mu.Unlock()
	return result.Data.Token, nil
}

func (c *TVDBClient) invalidate() {
	c.mu.Lock()
	c.token = ""
	c.expires = time.Time{}
	c.mu.Unlock()
}
