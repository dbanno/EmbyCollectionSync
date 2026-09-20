package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dbanno/EmbyCollectionSync/internal/config"
	"github.com/dbanno/EmbyCollectionSync/internal/emby"
	"github.com/dbanno/EmbyCollectionSync/internal/model"
	"github.com/dbanno/EmbyCollectionSync/internal/source"
	"github.com/dbanno/EmbyCollectionSync/internal/syncer"
)

func main() {
	configPath := flag.String("config", "config.yaml", "YAML config path")
	dryRun := flag.Bool("dry-run", false, "report changes without modifying Emby")
	flag.Parse()
	log.SetFlags(0)
	if err := run(*configPath, *dryRun); err != nil {
		log.Print("Error: ", err)
		os.Exit(1)
	}
}

func run(configPath string, dryRun bool) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	statePath := filepath.Join(filepath.Dir(configPath), ".embycollectionsync-state.json")
	state, err := syncer.LoadState(statePath, strings.TrimRight(cfg.Emby.URL, "/"))
	if err != nil {
		return err
	}
	httpClient := &http.Client{Timeout: 30 * time.Second}
	ec := emby.Client{BaseURL: cfg.Emby.URL, APIKey: cfg.Emby.APIKey, HTTP: httpClient}
	sc := source.Client{HTTP: httpClient, MDBListKey: cfg.MDBList.APIKey, TraktClientID: cfg.Trakt.ClientID, TraktToken: cfg.Trakt.AccessToken}
	ctx := context.Background()
	var library, collections []model.EmbyItem
	for _, col := range cfg.Collections {
		if col.Enabled {
			log.Print("Loading Emby library...")
			library, err = ec.Library(ctx, func(loaded, total int) { log.Printf("Loaded %d/%d items...", loaded, total) })
			if err != nil {
				return fmt.Errorf("load Emby library: %w", err)
			}
			log.Print("Loading Emby collections...")
			collections, err = ec.Collections(ctx)
			if err != nil {
				return fmt.Errorf("load Emby collections: %w", err)
			}
			break
		}
	}
	byID := map[string]model.EmbyItem{}
	byName := map[string][]model.EmbyItem{}
	for _, v := range collections {
		byID[v.ID] = v
		byName[v.Name] = append(byName[v.Name], v)
	}
	failures := 0
	for _, col := range cfg.Collections {
		if !col.Enabled {
			continue
		}
		started := time.Now()
		if err := process(ctx, col, dryRun, sc, ec, library, byID, byName, &state, statePath, started); err != nil {
			failures++
			log.Printf("Sync failed: collection=%q: %v", col.Name, err)
		}
	}
	if failures > 0 {
		return fmt.Errorf("%d collection(s) failed", failures)
	}
	return nil
}

func process(ctx context.Context, col config.Collection, dryRun bool, sc source.Provider, ec emby.Client, library []model.EmbyItem, byID map[string]model.EmbyItem, byName map[string][]model.EmbyItem, state *syncer.State, statePath string, started time.Time) error {
	log.Printf("Fetching source: %s...", col.Name)
	entries, err := sc.Fetch(ctx, col)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return fmt.Errorf("source list is empty; refusing to remove collection items")
	}
	managed, known := state.Collections[col.Name]
	if known && (managed.Source != col.Source || managed.URL != col.URL) {
		return fmt.Errorf("source changed for managed collection; review state before proceeding")
	}
	id := managed.ID
	if col.EmbyID != "" {
		if known && id != col.EmbyID {
			return fmt.Errorf("configured emby_id differs from ownership state")
		}
		id = col.EmbyID
		item, ok := byID[id]
		if !ok || item.Name != col.Name {
			return fmt.Errorf("configured emby_id does not identify collection %q", col.Name)
		}
		if !known {
			known = true
		}
	}
	if known {
		item, ok := byID[id]
		if !ok {
			return fmt.Errorf("managed collection ID %s no longer exists", id)
		}
		if item.Name != col.Name {
			return fmt.Errorf("managed collection was renamed")
		}
	}
	if !known && len(byName[col.Name]) > 0 {
		return fmt.Errorf("collection name already exists but is not managed by this app")
	}
	var current []model.EmbyItem
	if known {
		current, err = ec.CollectionItems(ctx, id)
		if err != nil {
			return err
		}
	}
	plan := syncer.Reconcile(entries, library, current)
	for _, d := range plan.Duplicates {
		log.Printf("Duplicate match: type=%s %s=%s title=%q candidates=%v selected=%s", d.Type, d.Provider, d.ProviderID, d.Title, d.Candidates, d.Selected)
	}
	for _, v := range plan.Unmatched {
		log.Printf("Unmatched: collection=%q type=%s title=%q year=%d tmdb=%q imdb=%q tvdb=%q", col.Name, v.Type, v.Title, v.Year, v.TMDbID, v.IMDbID, v.TVDbID)
	}
	if !known && len(plan.Add) == 0 {
		return fmt.Errorf("cannot create collection %q: no source items matched the Emby library", col.Name)
	}
	if dryRun {
		if !known {
			log.Printf("Dry run: would create collection %q", col.Name)
		}
		logChanges(col.Name, plan)
		summary(col, plan, started, true)
		return nil
	}
	addIDs := plan.Add
	if !known {
		id, err = ec.CreateCollection(ctx, col.Name, plan.Add[0])
		if err != nil {
			return err
		}
		addIDs = plan.Add[1:]
		state.Collections[col.Name] = syncer.Managed{ID: id, Source: col.Source, URL: col.URL}
		if err := state.Save(statePath); err != nil {
			return fmt.Errorf("collection created but could not save ownership state: %w", err)
		}
		byID[id] = model.EmbyItem{ID: id, Name: col.Name}
		byName[col.Name] = append(byName[col.Name], byID[id])
	} else if _, recorded := state.Collections[col.Name]; !recorded {
		state.Collections[col.Name] = syncer.Managed{ID: id, Source: col.Source, URL: col.URL}
		if err := state.Save(statePath); err != nil {
			return fmt.Errorf("could not save ownership state: %w", err)
		}
	}
	if err := ec.ChangeItems(ctx, id, addIDs, true); err != nil {
		return err
	}
	if err := ec.ChangeItems(ctx, id, plan.Remove, false); err != nil {
		return err
	}
	summary(col, plan, started, false)
	return nil
}
func logChanges(name string, p syncer.Plan) {
	log.Printf("Changes: collection=%q add=%v remove=%v", name, p.Add, p.Remove)
}
func summary(c config.Collection, p syncer.Plan, start time.Time, dry bool) {
	prefix := "Sync complete"
	if dry {
		prefix = "Dry run complete"
	}
	log.Printf("%s: collection=%q source=%d matched=%d added=%d removed=%d unmatched=%d duration=%s", prefix, c.Name, p.Source, p.Matched, len(p.Add), len(p.Remove), len(p.Unmatched), time.Since(start).Round(time.Millisecond))
}
