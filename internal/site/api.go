package site

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"

	"github.com/mmcnicol/kirkyards/internal/archive"
)

const licence = "Transcriptions CC BY 4.0 (https://creativecommons.org/licenses/by/4.0/), credit kirkyards.scot"

type apiPlace struct {
	Slug         string         `json:"slug"`
	Name         string         `json:"name"`
	Type         string         `json:"type"`
	Town         string         `json:"town"`
	County       string         `json:"county"`
	Country      string         `json:"country"`
	URL          string         `json:"url"`
	Stones       int            `json:"stones"`
	Inscriptions int            `json:"inscriptions"`
	Transcribed  int            `json:"transcribed"`
	StoneList    []apiStoneLink `json:"stoneList,omitempty"`
}

type apiStoneLink struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Years       string `json:"years,omitempty"`
	Transcribed bool   `json:"transcribed"`
	URL         string `json:"url"`
}

type apiStone struct {
	ID           int               `json:"id"`
	Title        string            `json:"title"`
	Years        string            `json:"years,omitempty"`
	URL          string            `json:"url"`
	Place        apiPlace          `json:"place"`
	Section      string            `json:"section,omitempty"`
	Inscriptions []apiInscription  `json:"inscriptions"`
	Licence      string            `json:"licence"`
	Notes        map[string]string `json:"notes"`
}

type apiInscription struct {
	Face   int              `json:"face"`
	Text   string           `json:"text"`
	Photos []string         `json:"photos,omitempty"`
	People []archive.Person `json:"people,omitempty"`
}

var stoneNotes = map[string]string{
	"title":     "Guessed from the first name in the inscription; may be wrong.",
	"years":     "Earliest and latest years appearing in the inscription.",
	"uncertain": "Square brackets mark uncertain readings, e.g. [1877?].",
}

func (s *Server) apiPlace(p *archive.Place, withStones bool) apiPlace {
	out := apiPlace{
		Slug: p.Slug, Name: p.Name, Type: p.Type, Town: p.Town, County: p.County, Country: p.Country,
		URL:    s.cfg.BaseURL + "/" + p.Slug,
		Stones: p.Stones, Inscriptions: p.Inscriptions, Transcribed: p.Transcribed,
	}
	if withStones {
		for _, st := range s.a.Stones(p.Slug) {
			out.StoneList = append(out.StoneList, s.stoneLink(st))
		}
	}
	return out
}

func (s *Server) stoneLink(st *archive.Stone) apiStoneLink {
	return apiStoneLink{ID: st.ID, Title: st.Title, Years: st.Years(), Transcribed: st.Transcribed(), URL: s.cfg.BaseURL + stoneURL(st)}
}

func (s *Server) apiStone(st *archive.Stone) apiStone {
	out := apiStone{
		ID: st.ID, Title: st.Title, Years: st.Years(), URL: s.cfg.BaseURL + stoneURL(st),
		Place: s.apiPlace(st.Place, false), Section: st.Section,
		Licence: licence, Notes: stoneNotes,
	}
	for _, in := range st.Inscriptions {
		ai := apiInscription{Face: in.Face, Text: in.Text, People: in.People}
		for _, ph := range in.Photos {
			if u := s.photoURL(st.Place, ph); u != "" {
				ai.Photos = append(ai.Photos, u)
			}
		}
		out.Inscriptions = append(out.Inscriptions, ai)
	}
	return out
}

func (s *Server) placesJSON(w http.ResponseWriter, r *http.Request) {
	var places []apiPlace
	for _, p := range s.a.Places {
		places = append(places, s.apiPlace(p, false))
	}
	writeJSON(w, map[string]any{"places": places, "licence": licence})
}

func (s *Server) searchJSON(w http.ResponseWriter, r *http.Request) {
	q, place := searchParams(r)
	results := []map[string]any{}
	for _, res := range s.a.Search(q, place, searchLimit) {
		results = append(results, map[string]any{
			"stone": s.stoneLink(res.Stone),
			"place": res.Stone.Place.Name,
			"lines": res.Lines,
		})
	}
	writeJSON(w, map[string]any{"query": q, "place": place, "limit": searchLimit, "results": results, "licence": licence})
}

func (s *Server) stoneMarkdown(st *archive.Stone) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s", st.Title)
	if y := st.Years(); y != "" {
		fmt.Fprintf(&b, " (%s)", y)
	}
	p := st.Place
	fmt.Fprintf(&b, "\n\n%s, %s, %s, %s. Stone %d.\n\n", p.Name, p.Town, p.County, p.Country, st.ID)
	fmt.Fprintf(&b, "Page: %s%s\n", s.cfg.BaseURL, stoneURL(st))
	for _, in := range st.Inscriptions {
		if len(st.Inscriptions) > 1 {
			fmt.Fprintf(&b, "\n## Face %d\n", in.Face)
		}
		if in.Text == "" {
			b.WriteString("\nNot yet transcribed.\n")
		} else {
			fmt.Fprintf(&b, "\n```text\n%s\n```\n", in.Text)
		}
		for _, ph := range in.Photos {
			if u := s.photoURL(p, ph); u != "" {
				fmt.Fprintf(&b, "\n![Photograph](%s)\n", u)
			}
		}
	}
	fmt.Fprintf(&b, "\nSquare brackets mark uncertain readings. The title is guessed from the inscription.\n\n%s.\n", licence)
	return b.String()
}

func (s *Server) placeMarkdown(p *archive.Place) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n%s, %s, %s. %d stones, %d of %d inscriptions transcribed.\n\n", p.Name, p.Town, p.County, p.Country, p.Stones, p.Transcribed, p.Inscriptions)
	for _, st := range s.a.Stones(p.Slug) {
		fmt.Fprintf(&b, "- [%s](%s%s.md)", st.Title, s.cfg.BaseURL, stoneURL(st))
		if y := st.Years(); y != "" {
			fmt.Fprintf(&b, " %s", y)
		}
		if !st.Transcribed() {
			b.WriteString(" (not transcribed)")
		}
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "\n%s.\n", licence)
	return b.String()
}

func (s *Server) llms(w http.ResponseWriter, r *http.Request) {
	t := s.totals()
	base := s.cfg.BaseURL
	var b strings.Builder
	fmt.Fprintf(&b, `# Kirkyards

> Photographs and transcriptions of %d gravestones (%d of %d inscriptions transcribed) in kirkyards and cemeteries around the River Tay: Tayport, Leuchars and Abdie in Fife, and Dundee. Photographed and transcribed in 2013.

Inscriptions are upper case, line by line as carved. Square brackets mark uncertain readings, e.g. [1877?]. Each stone's title is guessed from the first name in its inscription.

Every page is also available as JSON or Markdown: add .json or .md to a place or stone URL, or send an Accept header of application/json or text/markdown.

## API

- [All places](%[4]s/api/places.json): every burial ground with counts
- [Search](%[4]s/api/search.json?q=smith+tayport): stones whose inscriptions contain every word of q (prefix match); optional place=<slug>; up to %[5]d results
- Place: %[4]s/<place>.json lists its stones
- Stone: %[4]s/<place>/<id>.json has the full text, photo URLs and place

## Places

`, t.Stones, t.Transcribed, t.Inscriptions, base, searchLimit)
	for _, p := range s.a.Places {
		fmt.Fprintf(&b, "- [%s](%s/%s.md): %s, %s; %d stones\n", p.Name, base, p.Slug, p.Town, p.County, p.Stones)
	}
	fmt.Fprintf(&b, "\n## Licence\n\n%s. Source and corrections: %s\n", licence, s.cfg.RepoURL)
	writeText(w, "text/plain", b.String())
}

func (s *Server) robots(w http.ResponseWriter, r *http.Request) {
	writeText(w, "text/plain", "User-agent: *\nAllow: /\n\nSitemap: "+s.cfg.BaseURL+"/sitemap.xml\n")
}

func (s *Server) sitemap(w http.ResponseWriter, r *http.Request) {
	type loc struct {
		Loc string `xml:"loc"`
	}
	urls := []loc{{s.cfg.BaseURL + "/"}}
	for _, p := range s.a.Places {
		urls = append(urls, loc{s.cfg.BaseURL + "/" + p.Slug})
		for _, st := range s.a.Stones(p.Slug) {
			urls = append(urls, loc{s.cfg.BaseURL + stoneURL(st)})
		}
	}
	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write([]byte(xml.Header))
	enc := xml.NewEncoder(w)
	enc.Encode(struct {
		XMLName xml.Name `xml:"urlset"`
		NS      string   `xml:"xmlns,attr"`
		URLs    []loc    `xml:"url"`
	}{NS: "http://www.sitemaps.org/schemas/sitemap/0.9", URLs: urls})
}

// placeLD and stoneLD are schema.org structured data for search engines.
func (s *Server) placeLD(p *archive.Place) map[string]any {
	return map[string]any{
		"@context": "https://schema.org",
		"@type":    "Cemetery",
		"name":     p.Name,
		"url":      s.cfg.BaseURL + "/" + p.Slug,
		"address": map[string]any{
			"@type":           "PostalAddress",
			"addressLocality": p.Town,
			"addressRegion":   p.County,
			"addressCountry":  "GB",
		},
	}
}

func (s *Server) stoneLD(st *archive.Stone) map[string]any {
	ld := map[string]any{
		"@context": "https://schema.org",
		"@type":    "WebPage",
		"name":     st.Title + " — " + st.Place.Name,
		"url":      s.cfg.BaseURL + stoneURL(st),
		"about":    s.placeLD(st.Place),
		"license":  "https://creativecommons.org/licenses/by/4.0/",
	}
	if ph := st.Photos(); len(ph) > 0 && s.cfg.PhotoBaseURL != "" {
		ld["image"] = s.photoURL(st.Place, ph[0])
	}
	return ld
}
