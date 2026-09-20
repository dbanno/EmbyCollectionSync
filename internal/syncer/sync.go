package syncer

import (
	"sort"
	"strings"

	"github.com/dbanno/EmbyCollectionSync/internal/model"
)

type index struct {
	byID map[string][]string
}

type DuplicateMatch struct {
	Type, Provider, ProviderID, Title string
	Candidates                        []string
	Selected                          string
}

func key(typ, provider, id string) string {
	return strings.ToLower(typ) + "|" + provider + "|" + strings.ToLower(strings.TrimSpace(id))
}
func NewIndex(items []model.EmbyItem) index {
	x := index{byID: map[string][]string{}}
	for _, v := range items {
		if v.ID == "" {
			continue
		}
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
			x.byID[k] = append(x.byID[k], v.ID)
		}
	}
	for k, candidates := range x.byID {
		sort.Slice(candidates, func(i, j int) bool { return lessEmbyID(candidates[i], candidates[j]) })
		unique := candidates[:0]
		for _, id := range candidates {
			if len(unique) == 0 || unique[len(unique)-1] != id {
				unique = append(unique, id)
			}
		}
		x.byID[k] = unique
	}
	return x
}

func lessEmbyID(a, b string) bool {
	aDigits, bDigits := digits(a), digits(b)
	if aDigits != bDigits {
		return aDigits
	}
	if aDigits && bDigits {
		an, bn := strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0")
		if len(an) != len(bn) {
			return len(an) < len(bn)
		}
		if an != bn {
			return an < bn
		}
	}
	return a < b
}

func digits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func (x index) Match(v model.Item) string {
	id, _ := x.match(v)
	return id
}

func (x index) match(v model.Item) (string, *DuplicateMatch) {
	for _, p := range []struct{ name, id string }{{"tmdb", v.TMDbID}, {"imdb", v.IMDbID}, {"tvdb", v.TVDbID}} {
		if p.id == "" {
			continue
		}
		k := key(v.Type, p.name, p.id)
		if candidates := x.byID[k]; len(candidates) > 0 {
			if len(candidates) > 1 {
				return candidates[0], &DuplicateMatch{Type: v.Type, Provider: p.name, ProviderID: p.id, Title: v.Title, Candidates: append([]string(nil), candidates...), Selected: candidates[0]}
			}
			return candidates[0], nil
		}
	}
	return "", nil
}

type Plan struct {
	Source, Matched int
	Add, Remove     []string
	Unmatched       []model.Item
	Duplicates      []DuplicateMatch
}

func Reconcile(source []model.Item, library []model.EmbyItem, current []model.EmbyItem) Plan {
	p := Plan{Source: len(source)}
	idx := NewIndex(library)
	desired := map[string]bool{}
	present := map[string]bool{}
	for _, v := range source {
		id, duplicate := idx.match(v)
		if duplicate != nil {
			p.Duplicates = append(p.Duplicates, *duplicate)
		}
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
