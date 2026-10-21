package site

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mmcnicol/kirkyards/internal/archive"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	a, err := archive.Load(fstest.MapFS{
		"places.json": {Data: []byte(`[{"id": 8, "slug": "tayport-auld-kirk-cemetery", "name": "Tayport Auld Kirk Cemetery",
			"town": "Tayport", "county": "Fife", "country": "Scotland", "imageDir": "scotland/fife/tayport/auld_kirk_cemetery",
			"stones": 2, "inscriptions": 2, "transcribed": 1}]`)},
		"stones/tayport-auld-kirk-cemetery.json": {Data: []byte(`[
			{"id": 1, "inscriptions": [{"id": 10, "face": 1, "text": "ERECTED\nBY\nWILLIAM SCOTT\nALSO JOHN SMITH, BREWER <b>\nDIED 8 AUGUST 1844", "photos": ["monument/image/S1.JPG"]}]},
			{"id": 2, "inscriptions": [{"id": 20, "face": 1, "text": ""}]}
		]`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(a, os.DirFS("../../web"), Config{
		BaseURL:      "https://kirkyards.scot",
		PhotoBaseURL: "https://photos.example",
		RepoURL:      "https://github.com/mmcnicol/kirkyards",
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func get(s *Server, path, accept string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	if accept != "" {
		r.Header.Set("Accept", accept)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func TestRoutes(t *testing.T) {
	s := newTestServer(t)
	browser := "text/html,application/xhtml+xml,*/*;q=0.8"
	tests := []struct {
		path, accept string
		status       int
		ctype        string
		contains     string
	}{
		{"/", browser, 200, "text/html", "Tayport Auld Kirk Cemetery"},
		{"/", "application/json", 200, "application/json", `"slug": "tayport-auld-kirk-cemetery"`},
		{"/tayport-auld-kirk-cemetery", browser, 200, "text/html", "William Scott"},
		{"/tayport-auld-kirk-cemetery.json", "", 200, "application/json", `"stoneList"`},
		{"/tayport-auld-kirk-cemetery.md", "", 200, "text/markdown", "- [William Scott]"},
		{"/tayport-auld-kirk-cemetery/", "", 301, "", ""},
		{"/tayport-auld-kirk-cemetery/1", browser, 200, "text/html", "https://photos.example/scotland/fife/tayport/auld_kirk_cemetery/monument/image/S1.JPG"},
		{"/tayport-auld-kirk-cemetery/1", "", 200, "text/html", "&lt;b&gt;"}, // inscription text is escaped
		{"/tayport-auld-kirk-cemetery/1", "application/json", 200, "application/json", `"title": "William Scott"`},
		{"/tayport-auld-kirk-cemetery/1", "text/markdown", 200, "text/markdown", "# William Scott (1844)"},
		{"/tayport-auld-kirk-cemetery/1.json", browser, 200, "application/json", `"years": "1844"`},
		{"/tayport-auld-kirk-cemetery/2", browser, 200, "text/html", "Not yet transcribed"},
		{"/tayport-auld-kirk-cemetery/1.xml", "", 404, "text/html", ""},
		{"/tayport-auld-kirk-cemetery/99", "", 404, "text/html", ""},
		{"/tayport-auld-kirk-cemetery/1/extra", "", 404, "text/html", ""},
		{"/nowhere", "", 404, "text/html", ""},
		{"/search?q=brewer", browser, 200, "text/html", "ALSO JOHN SMITH, BREWER"},
		{"/search?q=nobody", browser, 200, "text/html", "No stones found"},
		{"/api/search.json?q=smith", "", 200, "application/json", `"id": 1`},
		{"/llms.txt", "", 200, "text/plain", "https://kirkyards.scot/api/places.json"},
		{"/robots.txt", "", 200, "text/plain", "Sitemap: https://kirkyards.scot/sitemap.xml"},
		{"/sitemap.xml", "", 200, "application/xml", "<loc>https://kirkyards.scot/tayport-auld-kirk-cemetery/2</loc>"},
		{"/static/style.css", "", 200, "text/css", "--accent"},
	}
	for _, tt := range tests {
		w := get(s, tt.path, tt.accept)
		if w.Code != tt.status {
			t.Errorf("%s (Accept %q): status %d, want %d", tt.path, tt.accept, w.Code, tt.status)
			continue
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, tt.ctype) {
			t.Errorf("%s (Accept %q): Content-Type %q, want %q", tt.path, tt.accept, ct, tt.ctype)
		}
		if !strings.Contains(w.Body.String(), tt.contains) {
			t.Errorf("%s (Accept %q): body missing %q", tt.path, tt.accept, tt.contains)
		}
	}
}

func TestStoneJSONShape(t *testing.T) {
	s := newTestServer(t)
	var got apiStone
	if err := json.Unmarshal(get(s, "/tayport-auld-kirk-cemetery/1.json", "").Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.URL != "https://kirkyards.scot/tayport-auld-kirk-cemetery/1" || got.Place.Town != "Tayport" ||
		len(got.Inscriptions) != 1 || len(got.Inscriptions[0].Photos) != 1 {
		t.Errorf("unexpected stone JSON: %+v", got)
	}
}

func TestNoPhotoBase(t *testing.T) {
	s := newTestServer(t)
	s.cfg.PhotoBaseURL = ""
	body := get(s, "/tayport-auld-kirk-cemetery/1", "").Body.String()
	if !strings.Contains(body, "Photo coming soon") || strings.Contains(body, "<img src=\"/") {
		t.Error("expected a placeholder instead of a photo")
	}
}
