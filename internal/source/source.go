package source

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/dbanno/EmbyCollectionSync/internal/config"
	"github.com/dbanno/EmbyCollectionSync/internal/model"
)

type Provider interface {
	Fetch(context.Context, config.Collection) ([]model.Item, error)
}
type Client struct {
	HTTP                                  *http.Client
	MDBListKey, TraktClientID, TraktToken string
}

func (c Client) Fetch(ctx context.Context, cfg config.Collection) ([]model.Item, error) {
	switch cfg.Source {
	case "mdblist":
		return c.mdblist(ctx, cfg.URL)
	case "trakt":
		return c.trakt(ctx, cfg.URL)
	}
	return nil, fmt.Errorf("unsupported source %q", cfg.Source)
}

func (c Client) get(ctx context.Context, endpoint string, headers map[string]string, target any) (http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("source request failed: %T", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("source returned HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 20<<20)).Decode(target); err != nil {
		return nil, fmt.Errorf("decode source response: %w", err)
	}
	return resp.Header, nil
}

func listPath(raw, host string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if (u.Scheme != "https" && u.Scheme != "http") || !strings.EqualFold(u.Hostname(), host) || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("expected a %s list URL", host)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 3 || parts[0] != "lists" || parts[1] == "" || parts[2] == "" {
		return "", fmt.Errorf("expected /lists/user/slug URL")
	}
	return strings.Join(parts[1:], "/"), nil
}

type flexID string

func (f *flexID) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*f = ""
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*f = flexID(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*f = flexID(n.String())
	return nil
}

type mdbItem struct {
	Title       string `json:"title"`
	Year        int    `json:"year"`
	ReleaseYear int    `json:"release_year"`
	MediaType   string `json:"mediatype"`
	Type        string `json:"type"`
	ID          flexID `json:"id"`
	TMDbID      flexID `json:"tmdbid"`
	IMDbID      string `json:"imdbid"`
	TVDbID      flexID `json:"tvdbid"`
}

func decodeMDBPage(raw json.RawMessage) ([]mdbItem, error) {
	var items []mdbItem
	if err := json.Unmarshal(raw, &items); err == nil {
		return items, nil
	}
	var wrapper struct {
		Items  []mdbItem `json:"items"`
		Movies []mdbItem `json:"movies"`
		Shows  []mdbItem `json:"shows"`
		Error  string    `json:"error"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, fmt.Errorf("invalid MDBList items response: %w", err)
	}
	if wrapper.Error != "" {
		return nil, fmt.Errorf("MDBList returned an error")
	}
	if wrapper.Items != nil {
		return wrapper.Items, nil
	}
	for i := range wrapper.Movies {
		if wrapper.Movies[i].MediaType == "" && wrapper.Movies[i].Type == "" {
			wrapper.Movies[i].MediaType = "movie"
		}
	}
	for i := range wrapper.Shows {
		if wrapper.Shows[i].MediaType == "" && wrapper.Shows[i].Type == "" {
			wrapper.Shows[i].MediaType = "show"
		}
	}
	if wrapper.Movies != nil || wrapper.Shows != nil {
		return append(wrapper.Movies, wrapper.Shows...), nil
	}
	return nil, fmt.Errorf("MDBList response has no items")
}

func (c Client) mdblist(ctx context.Context, raw string) ([]model.Item, error) {
	if c.MDBListKey == "" {
		return nil, fmt.Errorf("mdblist.api_key is required")
	}
	path, err := listPath(raw, "mdblist.com")
	if err != nil {
		path, err = listPath(raw, "www.mdblist.com")
		if err != nil {
			return nil, err
		}
	}
	endpoint := "https://api.mdblist.com/lists/" + path + "/items/"
	out := []model.Item{}
	for offset := 0; ; {
		q := url.Values{"limit": {"1000"}, "offset": {strconv.Itoa(offset)}, "unified": {"true"}, "apikey": {c.MDBListKey}}
		var rawPage json.RawMessage
		headers, err := c.get(ctx, endpoint+"?"+q.Encode(), nil, &rawPage)
		if err != nil {
			return nil, err
		}
		page, err := decodeMDBPage(rawPage)
		if err != nil {
			return nil, err
		}
		for _, v := range page {
			typ := strings.ToLower(v.MediaType)
			if typ == "" {
				typ = strings.ToLower(v.Type)
			}
			if typ == "show" || typ == "tv" {
				typ = "series"
			}
			if typ != "movie" && typ != "series" {
				return nil, fmt.Errorf("MDBList returned unknown media type %q", typ)
			}
			year := v.Year
			if year == 0 {
				year = v.ReleaseYear
			}
			tmdb := string(v.TMDbID)
			if tmdb == "" {
				tmdb = string(v.ID)
			}
			out = append(out, model.Item{Title: v.Title, Year: year, Type: typ, TMDbID: tmdb, IMDbID: v.IMDbID, TVDbID: string(v.TVDbID)})
		}
		more := strings.EqualFold(headers.Get("X-Has-More"), "true")
		if headers.Get("X-Has-More") == "" && len(page) == 1000 {
			return nil, fmt.Errorf("MDBList pagination header missing on full page")
		}
		if !more {
			break
		}
		if len(page) == 0 {
			return nil, fmt.Errorf("MDBList indicated more items but returned an empty page")
		}
		offset += len(page)
	}
	return out, nil
}

type traktIDs struct {
	TMDb flexID `json:"tmdb"`
	IMDb string `json:"imdb"`
	TVDb flexID `json:"tvdb"`
}
type traktMedia struct {
	Title string   `json:"title"`
	Year  int      `json:"year"`
	IDs   traktIDs `json:"ids"`
}
type traktItem struct {
	Type  string     `json:"type"`
	Movie traktMedia `json:"movie"`
	Show  traktMedia `json:"show"`
}

func (c Client) trakt(ctx context.Context, raw string) ([]model.Item, error) {
	if c.TraktClientID == "" {
		return nil, fmt.Errorf("trakt.client_id is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if (u.Scheme != "https" && u.Scheme != "http") || !strings.EqualFold(u.Hostname(), "trakt.tv") || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return nil, fmt.Errorf("expected a trakt.tv list URL")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 4 || parts[0] != "users" || parts[2] != "lists" || parts[1] == "" || parts[3] == "" {
		return nil, fmt.Errorf("expected /users/user/lists/slug URL")
	}
	endpoint := "https://api.trakt.tv/users/" + url.PathEscape(parts[1]) + "/lists/" + url.PathEscape(parts[3]) + "/items"
	headers := map[string]string{"trakt-api-key": c.TraktClientID, "trakt-api-version": "2", "Content-Type": "application/json"}
	if c.TraktToken != "" {
		headers["Authorization"] = "Bearer " + c.TraktToken
	}
	out := []model.Item{}
	for page := 1; ; page++ {
		q := url.Values{"page": {strconv.Itoa(page)}, "limit": {"1000"}}
		var items []traktItem
		h, err := c.get(ctx, endpoint+"?"+q.Encode(), headers, &items)
		if err != nil {
			return nil, err
		}
		for _, v := range items {
			var m traktMedia
			typ := v.Type
			switch typ {
			case "movie":
				m = v.Movie
			case "show":
				m = v.Show
				typ = "series"
			default:
				return nil, fmt.Errorf("Trakt returned unsupported media type %q", typ)
			}
			out = append(out, model.Item{Title: m.Title, Year: m.Year, Type: typ, TMDbID: string(m.IDs.TMDb), IMDbID: m.IDs.IMDb, TVDbID: string(m.IDs.TVDb)})
		}
		count, err := strconv.Atoi(h.Get("X-Pagination-Page-Count"))
		if err == nil && count > 0 {
			if page >= count {
				break
			}
			continue
		}
		if len(items) < 1000 {
			break
		}
		if len(items) == 0 {
			return nil, fmt.Errorf("Trakt returned an empty page before pagination completed")
		}
	}
	return out, nil
}
