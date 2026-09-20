package syncer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Managed struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	URL    string `json:"url"`
}
type State struct {
	EmbyURL     string             `json:"emby_url"`
	Collections map[string]Managed `json:"collections"`
}

func LoadState(path, embyURL string) (State, error) {
	s := State{EmbyURL: embyURL, Collections: map[string]Managed{}}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, fmt.Errorf("state file: %w", err)
	}
	if s.EmbyURL != embyURL {
		return s, fmt.Errorf("state file belongs to another Emby URL")
	}
	if s.Collections == nil {
		s.Collections = map[string]Managed{}
	}
	return s, nil
}
func (s State) Save(path string) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".embycollectionsync-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
