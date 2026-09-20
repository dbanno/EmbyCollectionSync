package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Credentials struct {
	APIKey string `yaml:"api_key"`
	URL    string `yaml:"url"`
}
type Trakt struct {
	ClientID    string `yaml:"client_id"`
	AccessToken string `yaml:"access_token"`
}
type Collection struct {
	Name    string `yaml:"name"`
	Source  string `yaml:"source"`
	URL     string `yaml:"url"`
	Enabled bool   `yaml:"enabled"`
	EmbyID  string `yaml:"emby_id"`
}
type Config struct {
	Emby        Credentials  `yaml:"emby"`
	MDBList     Credentials  `yaml:"mdblist"`
	Trakt       Trakt        `yaml:"trakt"`
	Collections []Collection `yaml:"collections"`
}

func Load(path string) (Config, error) {
	var c Config
	data, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return c, fmt.Errorf("parse config: %w", err)
	}
	c.Emby.URL = os.ExpandEnv(c.Emby.URL)
	c.Emby.APIKey = os.ExpandEnv(c.Emby.APIKey)
	c.MDBList.APIKey = os.ExpandEnv(c.MDBList.APIKey)
	c.Trakt.ClientID = os.ExpandEnv(c.Trakt.ClientID)
	c.Trakt.AccessToken = os.ExpandEnv(c.Trakt.AccessToken)
	if c.Emby.URL == "" || c.Emby.APIKey == "" {
		return c, fmt.Errorf("emby.url and emby.api_key are required")
	}
	if err := validURL(c.Emby.URL); err != nil {
		return c, fmt.Errorf("emby.url: %w", err)
	}
	names := map[string]bool{}
	for i, col := range c.Collections {
		if !col.Enabled {
			continue
		}
		if col.Name == "" || col.URL == "" {
			return c, fmt.Errorf("collection %d: name and url required", i+1)
		}
		if names[col.Name] {
			return c, fmt.Errorf("duplicate collection name %q", col.Name)
		}
		names[col.Name] = true
		if col.Source != "mdblist" && col.Source != "trakt" {
			return c, fmt.Errorf("collection %q: invalid source %q", col.Name, col.Source)
		}
		if err := validURL(col.URL); err != nil {
			return c, fmt.Errorf("collection %q: %w", col.Name, err)
		}
	}
	return c, nil
}

func validURL(raw string) error {
	u, e := url.Parse(raw)
	if e != nil {
		return e
	}
	if (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("expected an http(s) URL without credentials, query, or fragment")
	}
	return nil
}
