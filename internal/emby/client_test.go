package emby

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestLibraryPaginationProgress(t *testing.T) {
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
	items, err := c.Library(context.Background(), func(loaded, total int) { seen = append(seen, [2]int{loaded, total}) })
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
