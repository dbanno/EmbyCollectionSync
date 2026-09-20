package model

type Item struct {
	Title  string
	Year   int
	Type   string // movie or series
	TMDbID string
	IMDbID string
	TVDbID string
}

type EmbyItem struct {
	ID          string            `json:"Id"`
	Name        string            `json:"Name"`
	Type        string            `json:"Type"`
	ProviderIDs map[string]string `json:"ProviderIds"`
}
