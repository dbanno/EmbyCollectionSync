package emby

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
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
