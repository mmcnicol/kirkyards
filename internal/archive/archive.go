// Package archive holds the transcribed stones and the places they stand in,
// loaded from the JSON written by cmd/import.
package archive

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

type Place struct {
	ID           int      `json:"id"`
	Slug         string   `json:"slug"`
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Town         string   `json:"town"`
	County       string   `json:"county"`
	Country      string   `json:"country"`
	ImageDir     string   `json:"imageDir"`
	Photos       []string `json:"photos,omitempty"`
	Stones       int      `json:"stones"`
	Inscriptions int      `json:"inscriptions"`
	Transcribed  int      `json:"transcribed"`
}

type Stone struct {
	ID           int           `json:"id"`
	Section      string        `json:"section,omitempty"`
	Inscriptions []Inscription `json:"inscriptions"`

	// Derived when loaded.
	Place    *Place   `json:"-"`
	Title    string   `json:"-"`
	FromYear int      `json:"-"`
	ToYear   int      `json:"-"`
	words    []string // upper-case words of all inscriptions, for search
}

type Inscription struct {
	ID      int      `json:"id"`
	Face    int      `json:"face"`
	Text    string   `json:"text"`
	Version int      `json:"version"`
	Photos  []string `json:"photos,omitempty"`
	People  []Person `json:"people,omitempty"`
}

type Person struct {
	Firstname    string `json:"firstname,omitempty"`
	Surname      string `json:"surname,omitempty"`
	BirthSurname string `json:"birthSurname,omitempty"`
	Gender       string `json:"gender,omitempty"`
	Born         *Event `json:"born,omitempty"`
	Died         *Event `json:"died,omitempty"`
}

type Event struct {
	Year  int  `json:"year,omitempty"`
	Month int  `json:"month,omitempty"`
	Day   int  `json:"day,omitempty"`
	About bool `json:"about,omitempty"`
	Age   int  `json:"age,omitempty"`
}

// Archive is every place and stone, read-only once loaded.
type Archive struct {
	Places []*Place
	places map[string]*Place
	stones map[string][]*Stone
	byID   map[string]map[int]*Stone
}

// Load reads places.json and stones/<slug>.json from fsys. Places with no
// stones (such as Abdie Church, which only has general photos) are dropped.
func Load(fsys fs.FS) (*Archive, error) {
	var places []*Place
	if err := readJSON(fsys, "places.json", &places); err != nil {
		return nil, err
	}
	a := &Archive{
		places: map[string]*Place{},
		stones: map[string][]*Stone{},
		byID:   map[string]map[int]*Stone{},
	}
	for _, p := range places {
		var stones []*Stone
		if err := readJSON(fsys, path.Join("stones", p.Slug+".json"), &stones); err != nil {
			return nil, err
		}
		if len(stones) == 0 {
			continue
		}
		a.Places = append(a.Places, p)
		a.places[p.Slug] = p
		a.byID[p.Slug] = map[int]*Stone{}
		for _, s := range stones {
			s.Place = p
			s.derive()
			a.byID[p.Slug][s.ID] = s
		}
		a.stones[p.Slug] = stones
	}
	slices.SortFunc(a.Places, func(x, y *Place) int {
		return cmp.Or(cmp.Compare(x.Town, y.Town), cmp.Compare(x.Name, y.Name))
	})
	return a, nil
}

func readJSON(fsys fs.FS, name string, v any) error {
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// Place returns the place with the given slug, or nil.
func (a *Archive) Place(slug string) *Place { return a.places[slug] }

// Stones returns a place's stones in ID order.
func (a *Archive) Stones(slug string) []*Stone { return a.stones[slug] }

// Stone returns one stone, or nil.
func (a *Archive) Stone(slug string, id int) *Stone { return a.byID[slug][id] }

// Transcribed reports whether any face of the stone has text.
func (s *Stone) Transcribed() bool {
	for _, in := range s.Inscriptions {
		if in.Text != "" {
			return true
		}
	}
	return false
}

// Photos returns every photo of the stone, relative to its place's ImageDir.
func (s *Stone) Photos() []string {
	var out []string
	for _, in := range s.Inscriptions {
		out = append(out, in.Photos...)
	}
	return out
}

// Text joins the text of every face.
func (s *Stone) Text() string {
	var parts []string
	for _, in := range s.Inscriptions {
		if in.Text != "" {
			parts = append(parts, in.Text)
		}
	}
	return strings.Join(parts, "\n\n")
}

// Years formats the span of years found in the inscription, e.g. "1844–1920".
func (s *Stone) Years() string {
	switch {
	case s.FromYear == 0:
		return ""
	case s.FromYear == s.ToYear:
		return strconv.Itoa(s.FromYear)
	}
	return fmt.Sprintf("%d–%d", s.FromYear, s.ToYear)
}

var yearRe = regexp.MustCompile(`\b(1[5-9]\d\d|20[0-2]\d)\b`)

func (s *Stone) derive() {
	text := s.Text()
	for _, m := range yearRe.FindAllString(text, -1) {
		y, _ := strconv.Atoi(m)
		if s.FromYear == 0 || y < s.FromYear {
			s.FromYear = y
		}
		s.ToYear = max(s.ToYear, y)
	}
	s.words = words(strings.ToUpper(text))
	s.Title = titleFromText(text)
	if s.Title == "" {
		s.Title = fmt.Sprintf("Stone %d", s.ID)
	}
}

// words splits text into letter/digit runs. Square brackets and question
// marks (used for uncertain readings) are separators.
func words(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '\''
	})
}

// formula holds the stock words of memorial inscriptions, which separate
// the names on a stone ("ERECTED BY WILLIAM SCOTT IN MEMORY OF HIS MOTHER").
var formula = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`A ABOVE AFFECTION AFFECTIONATE AGED ALSO AND
		BELOVED BORN BROTHER BURIED BY CHILD CHILDREN DAUGHTER DAUGHTERS DEAR
		DEARLY DIED ERECTED FAMILY FATHER FOND FROM GRANDFATHER GRANDMOTHER
		GRANDSON GRANDDAUGHTER HER HERE HIS HUSBAND IN INTERRED LIE LIES LOVE
		LOVED LOVING MEMORY MOTHER MY OF ON ONE OUR REMEMBRANCE RESPECTIVELY
		SACRED SISTER SON SONS SPOUSE THE THEIR THIS TO TRIBUTE WHO WIFE WIDOW
		WITH WAS YEARS`) {
		formula[w] = true
	}
}

// titleFromText guesses the first person named on a stone: the first run of
// two or more non-formula words, not all initials, on a line with no digits.
func titleFromText(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.ContainsAny(line, "0123456789") {
			continue
		}
		var run []string
		for _, w := range words(strings.ToUpper(line)) {
			if formula[w] {
				if len(run) >= 2 {
					break
				}
				run = nil
				continue
			}
			run = append(run, w)
		}
		if len(run) >= 2 && len(run) <= 5 && slices.ContainsFunc(run, func(w string) bool { return len(w) >= 3 }) {
			return titleCase(strings.Join(run, " "))
		}
	}
	return ""
}

// titleCase turns "MARJORY MCNICOL" into "Marjory McNicol".
func titleCase(s string) string {
	ws := strings.Fields(strings.ToLower(s))
	for i, w := range ws {
		r := []rune(w)
		r[0] = unicode.ToUpper(r[0])
		for j := 1; j < len(r); j++ {
			if r[j-1] == '\'' || (j == 2 && r[0] == 'M' && r[1] == 'c') {
				r[j] = unicode.ToUpper(r[j])
			}
		}
		ws[i] = string(r)
	}
	return strings.Join(ws, " ")
}
