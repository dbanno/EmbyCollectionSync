package emby

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/dbanno/EmbyCollectionSync/internal/model"
)

func TestItemsPaginationProgress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/Items" || r.URL.Query().Get("Fields") != "ProviderIds" || r.Header.Get("X-Emby-Token") != "key" {
			t.Fatalf("unexpected request: %s", r.URL.String())
		}
		switch r.URL.Query().Get("StartIndex") {
		case "0":
			fmt.Fprint(w, `{"Items":[{"Id":"1","Type":"Movie"},{"Id":"2","Type":"Series"}],"TotalRecordCount":3}`)
		case "2":
			fmt.Fprint(w, `{"Items":[{"Id":"3","Type":"Movie"}],"TotalRecordCount":3}`)
		default:
			t.Fatalf("unexpected start index %q", r.URL.Query().Get("StartIndex"))
		}
	}))
	defer server.Close()
	c := Client{BaseURL: server.URL, APIKey: "key", HTTP: server.Client()}
	var seen [][2]int
	items, err := c.items(context.Background(), url.Values{"Fields": {"ProviderIds"}}, func(loaded, total int) { seen = append(seen, [2]int{loaded, total}) })
	if err != nil || len(items) != 3 || !reflect.DeepEqual(seen, [][2]int{{2, 3}, {3, 3}}) {
		t.Fatalf("items=%+v progress=%v err=%v", items, seen, err)
	}
}

func TestCreateCollectionRequiresSeedAndSendsParameters(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != "POST" || r.URL.Path != "/Collections" || r.URL.Query().Get("Name") != "MCU" || r.URL.Query().Get("IsLocked") != "false" || r.URL.Query().Get("Ids") != "123" {
			t.Fatalf("unexpected collection request: %s %s", r.Method, r.URL.String())
		}
		fmt.Fprint(w, `{"Id":"collection-1"}`)
	}))
	defer server.Close()
	c := Client{BaseURL: server.URL, APIKey: "secret", HTTP: server.Client()}
	if _, err := c.CreateCollection(context.Background(), "MCU", ""); err == nil || requests != 0 {
		t.Fatalf("empty seed: requests=%d err=%v", requests, err)
	}
	id, err := c.CreateCollection(context.Background(), "MCU", "123")
	if err != nil || id != "collection-1" || requests != 1 {
		t.Fatalf("id=%q requests=%d err=%v", id, requests, err)
	}
}

func TestCreateCollectionErrorHasContextWithoutKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) }))
	defer server.Close()
	c := Client{BaseURL: server.URL, APIKey: "very-secret-key", HTTP: server.Client()}
	_, err := c.CreateCollection(context.Background(), "MCU", "123")
	if err == nil || !strings.Contains(err.Error(), `collection "MCU"`) || !strings.Contains(err.Error(), "initial item 123") || !strings.Contains(err.Error(), "HTTP 500") || strings.Contains(err.Error(), "very-secret-key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProviderFilters(t *testing.T) {
	filters := ProviderFilters([]model.Item{
		{TMDbID: " 1726 ", IMDbID: "TT0371746", TVDbID: ""},
		{TMDbID: "1726", TVDbID: "12345"},
		{IMDbID: "tt0371746", TVDbID: "  "},
	})
	want := []string{"imdb.tt0371746", "tmdb.1726", "tvdb.12345"}
	if !reflect.DeepEqual(filters, want) {
		t.Fatalf("filters=%v want %v", filters, want)
	}
	if got := ProviderFilters([]model.Item{{IMDbID: "tt9"}, {TVDbID: "8"}}); !reflect.DeepEqual(got, []string{"imdb.tt9", "tvdb.8"}) {
		t.Fatalf("ID-only filters=%v", got)
	}
}

func TestLibraryMatchingBatchesAndDeduplicates(t *testing.T) {
	filters := make([]string, 51)
	for i := range filters {
		filters[i] = "tmdb." + strconv.Itoa(i+1)
	}
	queries := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries++
		q := r.URL.Query()
		if r.URL.Path != "/Items" || q.Get("Recursive") != "true" || q.Get("IncludeItemTypes") != "Movie,Series" || q.Get("Fields") != "ProviderIds" || q.Get("GroupItemsIntoCollections") != "false" || q.Get("StartIndex") != "0" {
			t.Errorf("unexpected query: %v", q)
		}
		count := len(strings.Split(q.Get("AnyProviderIdEquals"), ","))
		if queries == 1 && count != 50 || queries == 2 && count != 1 {
			t.Errorf("batch %d has %d filters", queries, count)
		}
		if queries == 1 {
			fmt.Fprint(w, `{"Items":[{"Id":"shared","Type":"Movie"},{"Id":"one","Type":"Movie"}],"TotalRecordCount":2}`)
		} else {
			fmt.Fprint(w, `{"Items":[{"Id":"shared","Type":"Movie"},{"Id":"two","Type":"Series"}],"TotalRecordCount":2}`)
		}
	}))
	defer server.Close()
	c := Client{BaseURL: server.URL, APIKey: "key", HTTP: server.Client()}
	items, batches, err := c.LibraryMatching(context.Background(), filters)
	if err != nil || batches != 2 || queries != 2 || len(items) != 3 {
		t.Fatalf("items=%+v batches=%d queries=%d err=%v", items, batches, queries, err)
	}
}

func TestLibraryMatchingFilterRejectionStops(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests++; w.WriteHeader(http.StatusBadRequest) }))
	defer server.Close()
	c := Client{BaseURL: server.URL, APIKey: "key", HTTP: server.Client()}
	items, batches, err := c.LibraryMatching(context.Background(), []string{"tmdb.1"})
	if err == nil || !strings.Contains(err.Error(), "AnyProviderIdEquals") || !strings.Contains(err.Error(), "HTTP 400") || items != nil || batches != 0 || requests != 1 {
		t.Fatalf("items=%v batches=%d requests=%d err=%v", items, batches, requests, err)
	}
}
