package syncer

import (
	"sort"
	"strings"

	"github.com/dbanno/EmbyCollectionSync/internal/model"
)

type index struct {
	byID      map[string]string
	ambiguous map[string]bool
}

func key(typ, provider, id string) string {
	return strings.ToLower(typ) + "|" + provider + "|" + strings.ToLower(strings.TrimSpace(id))
}
func NewIndex(items []model.EmbyItem) index {
	x := index{byID: map[string]string{}, ambiguous: map[string]bool{}}
	for _, v := range items {
		typ := strings.ToLower(v.Type)
		if typ != "movie" && typ != "series" {
			continue
		}
		for p, id := range v.ProviderIDs {
			p = strings.ToLower(p)
			if p != "tmdb" && p != "imdb" && p != "tvdb" {
				continue
			}
			if strings.TrimSpace(id) == "" {
				continue
			}
			k := key(typ, p, id)
			if prior := x.byID[k]; prior != "" && prior != v.ID {
				x.ambiguous[k] = true
			}
			x.byID[k] = v.ID
		}
	}
	return x
}
func (x index) Match(v model.Item) string {
	for _, p := range []struct{ name, id string }{{"tmdb", v.TMDbID}, {"imdb", v.IMDbID}, {"tvdb", v.TVDbID}} {
		if p.id == "" {
			continue
		}
		k := key(v.Type, p.name, p.id)
		if x.ambiguous[k] {
			continue
		}
		if id := x.byID[k]; id != "" {
			return id
		}
	}
	return ""
}

type Plan struct {
	Source, Matched int
	Add, Remove     []string
	Unmatched       []model.Item
}

func Reconcile(source []model.Item, library []model.EmbyItem, current []model.EmbyItem) Plan {
	p := Plan{Source: len(source)}
	idx := NewIndex(library)
	desired := map[string]bool{}
	present := map[string]bool{}
	for _, v := range source {
		id := idx.Match(v)
		if id == "" {
			p.Unmatched = append(p.Unmatched, v)
			continue
		}
		p.Matched++
		desired[id] = true
	}
	for _, v := range current {
		present[v.ID] = true
		if !desired[v.ID] {
			p.Remove = append(p.Remove, v.ID)
		}
	}
	for id := range desired {
		if !present[id] {
			p.Add = append(p.Add, id)
		}
	}
	sort.Strings(p.Add)
	sort.Strings(p.Remove)
	return p
}
