// Package site serves the archive as HTML pages for people, and as JSON and
// Markdown for agents, at the same URLs.
package site

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/mmcnicol/kirkyards/internal/archive"
)

type Config struct {
	// BaseURL is the public origin, e.g. https://kirkyards.scot, used for
	// canonical links, the sitemap and API responses.
	BaseURL string
	// PhotoBaseURL is where photos are served from, mirroring each place's
	// ImageDir, with thumbnails under thumbs/. Empty shows placeholders.
	PhotoBaseURL string
	// RepoURL is the GitHub repository, for correction requests.
	RepoURL string
}

type Server struct {
	a     *archive.Archive
	cfg   Config
	pages map[string]*template.Template
	mux   *http.ServeMux
}

const searchLimit = 100

func New(a *archive.Archive, web fs.FS, cfg Config) (*Server, error) {
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	cfg.PhotoBaseURL = strings.TrimRight(cfg.PhotoBaseURL, "/")
	s := &Server{a: a, cfg: cfg, pages: map[string]*template.Template{}, mux: http.NewServeMux()}

	funcs := template.FuncMap{
		"photo":    s.photoURL,
		"thumb":    s.thumbURL,
		"stoneURL": stoneURL,
		"lines":    func(t string) []string { return strings.Split(t, "\n") },
	}
	layout, err := template.New("layout.html").Funcs(funcs).ParseFS(web, "templates/layout.html")
	if err != nil {
		return nil, err
	}
	for _, name := range []string{"home", "place", "stone", "search", "notfound"} {
		t, err := template.Must(layout.Clone()).ParseFS(web, "templates/"+name+".html")
		if err != nil {
			return nil, err
		}
		s.pages[name] = t
	}
	static, err := fs.Sub(web, "static")
	if err != nil {
		return nil, err
	}

	m := s.mux
	m.HandleFunc("GET /{$}", s.home)
	m.HandleFunc("GET /search", s.search)
	m.HandleFunc("GET /api/places.json", s.placesJSON)
	m.HandleFunc("GET /api/search.json", s.searchJSON)
	m.HandleFunc("GET /llms.txt", s.llms)
	m.HandleFunc("GET /robots.txt", s.robots)
	m.HandleFunc("GET /sitemap.xml", s.sitemap)
	m.Handle("GET /static/{file}", cacheFor(86400, http.StripPrefix("/static/", http.FileServerFS(static))))
	m.HandleFunc("GET /{place}", s.place)
	m.HandleFunc("GET /{place}/{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/"+r.PathValue("place"), http.StatusMovedPermanently)
	})
	m.HandleFunc("GET /{stone...}", s.stone)
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	img := "'self'"
	if s.cfg.PhotoBaseURL != "" {
		img += " " + s.cfg.PhotoBaseURL
	}
	h.Set("Content-Security-Policy", "default-src 'self'; img-src "+img+"; style-src 'self'; form-action 'self'; frame-ancestors 'none'")
	s.mux.ServeHTTP(w, r)
}

func cacheFor(seconds int, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(seconds))
		h.ServeHTTP(w, r)
	})
}

// format picks the response format: an explicit extension wins, otherwise
// the Accept header, defaulting to HTML.
func format(r *http.Request, ext string) string {
	switch ext {
	case ".json":
		return "json"
	case ".md":
		return "md"
	case "":
	default:
		return "unknown"
	}
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "text/html") {
		return "html"
	}
	if strings.Contains(accept, "application/json") {
		return "json"
	}
	if strings.Contains(accept, "text/markdown") {
		return "md"
	}
	return "html"
}

func stoneURL(st *archive.Stone) string {
	return fmt.Sprintf("/%s/%d", st.Place.Slug, st.ID)
}

func (s *Server) photoURL(p *archive.Place, rel string) string {
	if s.cfg.PhotoBaseURL == "" {
		return ""
	}
	return s.cfg.PhotoBaseURL + "/" + p.ImageDir + "/" + rel
}

func (s *Server) thumbURL(p *archive.Place, rel string) string {
	if s.cfg.PhotoBaseURL == "" {
		return ""
	}
	return s.cfg.PhotoBaseURL + "/thumbs/" + p.ImageDir + "/" + rel
}

// page is the data every HTML template receives.
type page struct {
	Title       string
	Description string
	Path        string // canonical path
	Alternates  map[string]string
	JSONLD      any
	Query       string
	Place       *archive.Place
	Places      []*archive.Place
	Stones      []*archive.Stone
	Stone       *archive.Stone
	Prev, Next  *archive.Stone
	Results     []archive.Result
	Truncated   bool
	Totals      totals
	CorrectURL  string
	BaseURL     string
}

type totals struct{ Stones, Inscriptions, Transcribed int }

func (s *Server) totals() totals {
	var t totals
	for _, p := range s.a.Places {
		t.Stones += p.Stones
		t.Inscriptions += p.Inscriptions
		t.Transcribed += p.Transcribed
	}
	return t
}

func (s *Server) render(w http.ResponseWriter, status int, name string, p page) {
	p.BaseURL = s.cfg.BaseURL
	p.Places = s.a.Places
	var buf bytes.Buffer
	if err := s.pages[name].Execute(&buf, p); err != nil {
		slog.Error("render", "page", name, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Vary", "Accept")
	if status == http.StatusOK {
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusNotFound, "notfound", page{Title: "Not found", Path: r.URL.Path})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Vary", "Accept")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

func writeText(w http.ResponseWriter, contentType, body string) {
	w.Header().Set("Content-Type", contentType+"; charset=utf-8")
	w.Header().Set("Vary", "Accept")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	fmt.Fprint(w, body)
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	if format(r, "") == "json" {
		s.placesJSON(w, r)
		return
	}
	s.render(w, http.StatusOK, "home", page{
		Title:       "Kirkyards — gravestone inscriptions around the Tay",
		Description: "Searchable photographs and transcriptions of gravestones in Tayport, Leuchars, Abdie and Dundee kirkyards and cemeteries.",
		Path:        "/",
		Alternates:  map[string]string{"application/json": "/api/places.json", "text/plain": "/llms.txt"},
		Totals:      s.totals(),
	})
}

func (s *Server) place(w http.ResponseWriter, r *http.Request) {
	slug, ext := splitExt(r.PathValue("place"))
	p := s.a.Place(slug)
	if p == nil {
		s.notFound(w, r)
		return
	}
	switch format(r, ext) {
	case "json":
		writeJSON(w, s.apiPlace(p, true))
		return
	case "md":
		writeText(w, "text/markdown", s.placeMarkdown(p))
		return
	case "unknown":
		s.notFound(w, r)
		return
	}
	s.render(w, http.StatusOK, "place", page{
		Title:       p.Name,
		Description: fmt.Sprintf("Photographs and transcriptions of %d gravestones at %s, %s, %s.", p.Stones, p.Name, p.Town, p.County),
		Path:        "/" + p.Slug,
		Alternates:  map[string]string{"application/json": "/" + p.Slug + ".json", "text/markdown": "/" + p.Slug + ".md"},
		JSONLD:      s.placeLD(p),
		Place:       p,
		Stones:      s.a.Stones(p.Slug),
	})
}

func (s *Server) stone(w http.ResponseWriter, r *http.Request) {
	slug, rest, ok := strings.Cut(r.PathValue("stone"), "/")
	if !ok || strings.Contains(rest, "/") {
		s.notFound(w, r)
		return
	}
	idStr, ext := splitExt(rest)
	id, err := strconv.Atoi(idStr)
	st := s.a.Stone(slug, id)
	if err != nil || st == nil {
		s.notFound(w, r)
		return
	}
	switch format(r, ext) {
	case "json":
		writeJSON(w, s.apiStone(st))
		return
	case "md":
		writeText(w, "text/markdown", s.stoneMarkdown(st))
		return
	case "unknown":
		s.notFound(w, r)
		return
	}
	pg := page{
		Title:      st.Title + " — " + st.Place.Name,
		Path:       stoneURL(st),
		Alternates: map[string]string{"application/json": stoneURL(st) + ".json", "text/markdown": stoneURL(st) + ".md"},
		JSONLD:     s.stoneLD(st),
		Place:      st.Place,
		Stone:      st,
		CorrectURL: s.correctionURL(st),
	}
	pg.Description = describe(st)
	stones := s.a.Stones(slug)
	for i, x := range stones {
		if x == st {
			if i > 0 {
				pg.Prev = stones[i-1]
			}
			if i+1 < len(stones) {
				pg.Next = stones[i+1]
			}
		}
	}
	s.render(w, http.StatusOK, "stone", pg)
}

func describe(st *archive.Stone) string {
	if !st.Transcribed() {
		return fmt.Sprintf("Photograph of a gravestone at %s, %s. Not yet transcribed.", st.Place.Name, st.Place.Town)
	}
	text := strings.Join(strings.Fields(st.Text()), " ")
	if len(text) > 150 {
		text = text[:150]
		if i := strings.LastIndexByte(text, ' '); i > 0 {
			text = text[:i]
		}
		text += "…"
	}
	return fmt.Sprintf("%s, %s: %s", st.Place.Name, st.Place.Town, text)
}

func (s *Server) correctionURL(st *archive.Stone) string {
	if s.cfg.RepoURL == "" {
		return ""
	}
	q := url.Values{
		"title": {fmt.Sprintf("Correction: %s stone %d", st.Place.Name, st.ID)},
		"body":  {fmt.Sprintf("Stone: %s%s\n\nWhat should it say?\n\n", s.cfg.BaseURL, stoneURL(st))},
	}
	return s.cfg.RepoURL + "/issues/new?" + q.Encode()
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	q, place := searchParams(r)
	if format(r, "") == "json" {
		s.searchJSON(w, r)
		return
	}
	results := s.a.Search(q, place, searchLimit+1)
	pg := page{
		Title: "Search",
		Path:  "/search",
		Query: q,
		Place: s.a.Place(place),
	}
	if q != "" {
		pg.Title = "Search: " + q
		pg.Alternates = map[string]string{"application/json": "/api/search.json?" + r.URL.RawQuery}
	}
	if len(results) > searchLimit {
		results, pg.Truncated = results[:searchLimit], true
	}
	pg.Results = results
	s.render(w, http.StatusOK, "search", pg)
}

func searchParams(r *http.Request) (q, place string) {
	q = strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) > 100 {
		q = q[:100]
	}
	return q, r.URL.Query().Get("place")
}

func splitExt(s string) (base, ext string) {
	ext = path.Ext(s)
	return strings.TrimSuffix(s, ext), ext
}
