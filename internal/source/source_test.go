package source

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/dbanno/EmbyCollectionSync/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(body string, header http.Header) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: header}
}

func TestMDBListMixedPagination(t *testing.T) {
	calls := 0
	c := Client{MDBListKey: "secret", HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "api.mdblist.com" || r.URL.Query().Get("apikey") != "secret" || r.URL.Query().Get("unified") != "true" {
			t.Fatalf("unexpected request: %s", r.URL.Host+r.URL.Path)
		}
		if calls == 1 {
			return response(`[{"title":"Film","mediatype":"movie","id":123}]`, http.Header{"X-Has-More": []string{"true"}}), nil
		}
		if r.URL.Query().Get("offset") != "1" {
			t.Fatalf("offset=%s", r.URL.Query().Get("offset"))
		}
		return response(`[{"title":"Show","mediatype":"show","tmdbid":"456","imdbid":"tt2"}]`, nil), nil
	})}}
	items, err := c.Fetch(context.Background(), config.Collection{Source: "mdblist", URL: "https://mdblist.com/lists/user/slug"})
	if err != nil || calls != 2 || len(items) != 2 || items[0].TMDbID != "123" || items[1].Type != "series" {
		t.Fatalf("items=%+v calls=%d err=%v", items, calls, err)
	}
}

func TestTraktMixedPagination(t *testing.T) {
	calls := 0
	c := Client{TraktClientID: "client", HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("trakt-api-key") != "client" || r.URL.Query().Get("limit") != "1000" {
			t.Fatal("missing Trakt headers or limit")
		}
		if calls == 1 {
			return response(`[{"type":"movie","movie":{"title":"Film","ids":{"tmdb":123}}}]`, http.Header{"X-Pagination-Page-Count": []string{"2"}}), nil
		}
		if r.URL.Query().Get("page") != "2" {
			t.Fatal("second page not requested")
		}
		return response(`[{"type":"show","show":{"title":"Show","ids":{"tvdb":42}}}]`, http.Header{"X-Pagination-Page-Count": []string{"2"}}), nil
	})}}
	items, err := c.Fetch(context.Background(), config.Collection{Source: "trakt", URL: "https://trakt.tv/users/user/lists/slug"})
	if err != nil || calls != 2 || len(items) != 2 || items[0].TMDbID != "123" || items[1].Type != "series" || items[1].TVDbID != "42" {
		t.Fatalf("items=%+v calls=%d err=%v", items, calls, err)
	}
}

func TestMDBListWrappedMixedItems(t *testing.T) {
	items, err := decodeMDBPage([]byte(`{"movies":[{"id":1}],"shows":[{"id":2}]}`))
	if err != nil || len(items) != 2 || items[0].MediaType != "movie" || items[1].MediaType != "show" {
		t.Fatalf("items=%+v err=%v", items, err)
	}
}
