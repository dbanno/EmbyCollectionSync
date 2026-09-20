package syncer

import (
	"github.com/dbanno/EmbyCollectionSync/internal/model"
	"reflect"
	"testing"
)

func TestMatchPriorityAndType(t *testing.T) {
	idx := NewIndex([]model.EmbyItem{
		{ID: "m", Type: "Movie", ProviderIDs: map[string]string{"Tmdb": "1", "Imdb": "tt1"}},
		{ID: "s", Type: "Series", ProviderIDs: map[string]string{"Tmdb": "1", "Tvdb": "42"}},
	})
	for _, tc := range []struct {
		item model.Item
		want string
	}{
		{model.Item{Type: "movie", TMDbID: "1", IMDbID: "other"}, "m"},
		{model.Item{Type: "series", TMDbID: "1"}, "s"},
		{model.Item{Type: "movie", TMDbID: "missing", IMDbID: "tt1"}, "m"},
		{model.Item{Type: "series", TVDbID: "42"}, "s"},
		{model.Item{Type: "movie", Title: "Same title"}, ""},
	} {
		if got := idx.Match(tc.item); got != tc.want {
			t.Errorf("Match(%+v)=%q want %q", tc.item, got, tc.want)
		}
	}
}

func TestReconcileMixedAndUnmatched(t *testing.T) {
	lib := []model.EmbyItem{{ID: "a", Type: "Movie", ProviderIDs: map[string]string{"Tmdb": "1"}}, {ID: "b", Type: "Series", ProviderIDs: map[string]string{"Imdb": "tt2"}}}
	src := []model.Item{{Type: "movie", TMDbID: "1"}, {Type: "series", IMDbID: "tt2"}, {Type: "movie", Title: "Missing", TMDbID: "9"}}
	p := Reconcile(src, lib, []model.EmbyItem{{ID: "a"}, {ID: "old"}})
	if p.Source != 3 || p.Matched != 2 || len(p.Unmatched) != 1 || !reflect.DeepEqual(p.Add, []string{"b"}) || !reflect.DeepEqual(p.Remove, []string{"old"}) {
		t.Fatalf("unexpected plan: %+v", p)
	}
}

func TestAmbiguousID(t *testing.T) {
	idx := NewIndex([]model.EmbyItem{{ID: "a", Type: "Movie", ProviderIDs: map[string]string{"Tmdb": "1"}}, {ID: "b", Type: "Movie", ProviderIDs: map[string]string{"Tmdb": "1"}}})
	if got := idx.Match(model.Item{Type: "movie", TMDbID: "1"}); got != "" {
		t.Fatalf("ambiguous match = %q", got)
	}
}
