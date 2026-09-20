package emby

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/dbanno/EmbyCollectionSync/internal/model"
)

type Client struct {
	BaseURL, APIKey string
	HTTP            *http.Client
}
type page struct {
	Items []model.EmbyItem `json:"Items"`
	Total int              `json:"TotalRecordCount"`
}

func (c Client) request(ctx context.Context, method, path string, q url.Values, result any) error {
	u := strings.TrimRight(c.BaseURL, "/") + "/" + strings.TrimLeft(path, "/")
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Emby-Token", c.APIKey)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("Emby request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Emby %s %s returned HTTP %d", method, path, resp.StatusCode)
	}
	if result != nil {
		if err := json.NewDecoder(io.LimitReader(resp.Body, 20<<20)).Decode(result); err != nil {
			return fmt.Errorf("decode Emby response: %w", err)
		}
	}
	return nil
}

func (c Client) Items(ctx context.Context, q url.Values) ([]model.EmbyItem, error) {
	return c.items(ctx, q, nil)
}

func (c Client) items(ctx context.Context, q url.Values, progress func(int, int)) ([]model.EmbyItem, error) {
	const limit = 500
	out := []model.EmbyItem{}
	for start := 0; ; {
		query := url.Values{}
		for k, v := range q {
			query[k] = append([]string(nil), v...)
		}
		query.Set("StartIndex", strconv.Itoa(start))
		query.Set("Limit", strconv.Itoa(limit))
		var p page
		if err := c.request(ctx, "GET", "Items", query, &p); err != nil {
			return nil, err
		}
		out = append(out, p.Items...)
		start += len(p.Items)
		if progress != nil {
			progress(start, p.Total)
		}
		if start >= p.Total {
			break
		}
		if len(p.Items) == 0 {
			return nil, fmt.Errorf("Emby returned an empty page before all items were fetched")
		}
	}
	return out, nil
}

func (c Client) Library(ctx context.Context, progress func(int, int)) ([]model.EmbyItem, error) {
	return c.items(ctx, url.Values{"Recursive": {"true"}, "IncludeItemTypes": {"Movie,Series"}, "Fields": {"ProviderIds"}, "GroupItemsIntoCollections": {"false"}}, progress)
}
func (c Client) Collections(ctx context.Context) ([]model.EmbyItem, error) {
	return c.Items(ctx, url.Values{"Recursive": {"true"}, "IncludeItemTypes": {"BoxSet"}})
}
func (c Client) CollectionItems(ctx context.Context, id string) ([]model.EmbyItem, error) {
	return c.Items(ctx, url.Values{"ParentId": {id}, "IncludeItemTypes": {"Movie,Series"}, "Recursive": {"false"}})
}
func (c Client) CreateCollection(ctx context.Context, name string) (string, error) {
	var v struct {
		ID string `json:"Id"`
	}
	if err := c.request(ctx, "POST", "Collections", url.Values{"Name": {name}}, &v); err != nil {
		return "", err
	}
	if v.ID == "" {
		return "", fmt.Errorf("Emby created collection without an ID")
	}
	return v.ID, nil
}
func (c Client) ChangeItems(ctx context.Context, collectionID string, ids []string, add bool) error {
	if len(ids) == 0 {
		return nil
	}
	path := "Collections/" + url.PathEscape(collectionID) + "/Items"
	method := "POST"
	if !add {
		method = "DELETE"
	}
	for i := 0; i < len(ids); i += 100 {
		end := i + 100
		if end > len(ids) {
			end = len(ids)
		}
		if err := c.request(ctx, method, path, url.Values{"Ids": {strings.Join(ids[i:end], ",")}}, nil); err != nil {
			return err
		}
	}
	return nil
}
