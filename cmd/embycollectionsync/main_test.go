package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dbanno/EmbyCollectionSync/internal/config"
	"github.com/dbanno/EmbyCollectionSync/internal/emby"
	"github.com/dbanno/EmbyCollectionSync/internal/model"
	"github.com/dbanno/EmbyCollectionSync/internal/syncer"
)

type fixedSource struct{ items []model.Item }

func (s fixedSource) Fetch(_ context.Context, _ config.Collection) ([]model.Item, error) {
	return s.items, nil
}

func testLibrary() []model.EmbyItem {
	return []model.EmbyItem{
		{ID: "20", Type: "Movie", ProviderIDs: map[string]string{"Tmdb": "2"}},
		{ID: "10", Type: "Movie", ProviderIDs: map[string]string{"Tmdb": "1"}},
	}
}

func TestNewCollectionSeedAndRemainingItems(t *testing.T) {
	for _, tc := range []struct {
		name        string
		source      []model.Item
		wantAdd     []string
		wantSummary string
	}{
		{"multiple", []model.Item{{Type: "movie", TMDbID: "2"}, {Type: "movie", TMDbID: "1"}}, []string{"20"}, "added=2"},
		{"single", []model.Item{{Type: "movie", TMDbID: "1"}}, nil, "added=1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var seed string
			var added []string
			creates := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == "POST" && r.URL.Path == "/Collections":
					creates++
					seed = r.URL.Query().Get("Ids")
					if r.URL.Query().Get("Name") != "MCU" || r.URL.Query().Get("IsLocked") != "false" {
						t.Fatalf("create query: %v", r.URL.Query())
					}
					fmt.Fprint(w, `{"Id":"new"}`)
				case r.Method == "POST" && r.URL.Path == "/Collections/new/Items":
					added = append(added, r.URL.Query().Get("Ids"))
				default:
					t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
				}
			}))
			defer server.Close()
			var logs bytes.Buffer
			prior := log.Writer()
			log.SetOutput(&logs)
			defer log.SetOutput(prior)
			state := syncer.State{EmbyURL: server.URL, Collections: map[string]syncer.Managed{}}
			path := filepath.Join(t.TempDir(), "state.json")
			c := emby.Client{BaseURL: server.URL, APIKey: "secret", HTTP: server.Client()}
			err := process(context.Background(), config.Collection{Name: "MCU", Source: "mdblist", URL: "https://mdblist.com/lists/u/mcu"}, false, fixedSource{tc.source}, c, testLibrary(), map[string]model.EmbyItem{}, map[string][]model.EmbyItem{}, &state, path, time.Now())
			if err != nil || creates != 1 || seed != "10" || !reflect.DeepEqual(added, tc.wantAdd) || !strings.Contains(logs.String(), tc.wantSummary) {
				t.Fatalf("err=%v creates=%d seed=%q added=%v logs=%s", err, creates, seed, added, logs.String())
			}
			saved, err := syncer.LoadState(path, server.URL)
			if err != nil || saved.Collections["MCU"].ID != "new" {
				t.Fatalf("ownership not saved: %+v err=%v", saved, err)
			}
		})
	}
}

func TestNewCollectionWithNoMatchIsNotCreated(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++; t.Fatalf("unexpected request: %s", r.URL) }))
	defer server.Close()
	state := syncer.State{EmbyURL: server.URL, Collections: map[string]syncer.Managed{}}
	c := emby.Client{BaseURL: server.URL, APIKey: "secret", HTTP: server.Client()}
	err := process(context.Background(), config.Collection{Name: "MCU"}, false, fixedSource{[]model.Item{{Type: "movie", TMDbID: "missing"}}}, c, testLibrary(), map[string]model.EmbyItem{}, map[string][]model.EmbyItem{}, &state, filepath.Join(t.TempDir(), "state.json"), time.Now())
	if err == nil || !strings.Contains(err.Error(), "no source items matched") || requests != 0 || len(state.Collections) != 0 {
		t.Fatalf("err=%v requests=%d state=%+v", err, requests, state)
	}
}

func TestExistingManagedCollectionUsesNormalChanges(t *testing.T) {
	var changes []string
	creates := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/Items":
			fmt.Fprint(w, `{"Items":[{"Id":"10"},{"Id":"30"}],"TotalRecordCount":2}`)
		case r.URL.Path == "/Collections/existing/Items":
			changes = append(changes, r.Method+":"+r.URL.Query().Get("Ids"))
		case r.Method == "POST" && r.URL.Path == "/Collections":
			creates++
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()
	col := config.Collection{Name: "MCU", Source: "mdblist", URL: "https://mdblist.com/lists/u/mcu"}
	state := syncer.State{EmbyURL: server.URL, Collections: map[string]syncer.Managed{"MCU": {ID: "existing", Source: col.Source, URL: col.URL}}}
	c := emby.Client{BaseURL: server.URL, APIKey: "secret", HTTP: server.Client()}
	err := process(context.Background(), col, false, fixedSource{[]model.Item{{Type: "movie", TMDbID: "1"}, {Type: "movie", TMDbID: "2"}}}, c, testLibrary(), map[string]model.EmbyItem{"existing": {ID: "existing", Name: "MCU"}}, map[string][]model.EmbyItem{"MCU": {{ID: "existing", Name: "MCU"}}}, &state, filepath.Join(t.TempDir(), "state.json"), time.Now())
	if err != nil || creates != 0 || !reflect.DeepEqual(changes, []string{"POST:20", "DELETE:30"}) {
		t.Fatalf("err=%v creates=%d changes=%v", err, creates, changes)
	}
}
