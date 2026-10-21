// Command import converts the 2013 InscriptionImages MySQL dump into JSON
// files under data/: one places.json listing every burial ground, and one
// stones/<slug>.json per place holding its monuments and inscriptions.
//
// Usage:
//
//	go run ./cmd/import -sql saferfif_InscriptionImages.sql [-photos InscriptionImages_new.zip] [-out data]
//
// The visitor search log (tbl_quick_search_request) holds IP addresses and is
// deliberately never read. The stonemason adverts (tbl_related_links) and the
// history timeline (tbl_context_event) are left out too.
package main

import (
	"archive/zip"
	"cmp"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/mmcnicol/kirkyards/internal/archive"
	"github.com/mmcnicol/kirkyards/internal/sqldump"
)

// The JSON shapes are shared with the web server.
type (
	Place       = archive.Place
	Stone       = archive.Stone
	Inscription = archive.Inscription
	Person      = archive.Person
	Event       = archive.Event
)

// resourcesPrefix is where the photos live inside the backup zip.
const resourcesPrefix = "InscriptionImages/resources/"

func main() {
	sqlPath := flag.String("sql", "", "path to saferfif_InscriptionImages.sql")
	photosZip := flag.String("photos", "", "optional path to InscriptionImages zip, to check photo references")
	outDir := flag.String("out", "data", "output directory")
	flag.Parse()
	if *sqlPath == "" {
		flag.Usage()
		os.Exit(2)
	}

	f, err := os.Open(*sqlPath)
	if err != nil {
		log.Fatal(err)
	}
	tables, err := sqldump.Parse(f)
	f.Close()
	if err != nil {
		log.Fatal(err)
	}

	places, stones := build(tables)

	if *photosZip != "" {
		if err := checkPhotos(*photosZip, places, stones); err != nil {
			log.Fatal(err)
		}
	}

	if err := os.MkdirAll(filepath.Join(*outDir, "stones"), 0o755); err != nil {
		log.Fatal(err)
	}
	writeJSON(filepath.Join(*outDir, "places.json"), places)
	for _, p := range places {
		writeJSON(filepath.Join(*outDir, "stones", p.Slug+".json"), stones[p.ID])
		fmt.Printf("%-32s %4d stones %4d inscriptions %4d transcribed\n", p.Slug, p.Stones, p.Inscriptions, p.Transcribed)
	}
}

func build(t sqldump.Tables) ([]*Place, map[int][]*Stone) {
	type location struct{ town, county, country string }
	locations := map[int]location{}
	for _, r := range t["tbl_location"] {
		locations[atoi(r.Str("Id"))] = location{r.Str("town"), r.Str("county"), r.Str("country")}
	}

	images := map[int]string{}
	for _, r := range t["tbl_image"] {
		images[atoi(r.Str("Id"))] = path.Join(r.Str("FilePath"), r.Str("FileName"))
	}

	people := map[int]*Person{}
	for _, r := range t["tbl_person"] {
		p := &Person{Firstname: r.Str("Firstname"), Surname: r.Str("Surname"), BirthSurname: r.Str("FamilySurname")}
		switch r.Str("GenderType") {
		case "1":
			p.Gender = "male"
		case "2":
			p.Gender = "female"
		}
		people[atoi(r.Str("Id"))] = p
	}
	for _, r := range t["tbl_person_event"] {
		p := people[atoi(r.Str("PersonId"))]
		if p == nil {
			log.Printf("warning: person event %s refers to missing person", r.Str("Id"))
			continue
		}
		e := &Event{
			Year:  atoi(r.Str("Date-Year")),
			Month: atoi(r.Str("Date-Month")),
			Day:   atoi(r.Str("Date-Day")),
			About: r.Str("Date-About") == "1",
			Age:   atoi(r.Str("AgeAtDeath")),
		}
		switch r.Str("EventTypeId") {
		case "1":
			p.Born = e
		case "2":
			p.Died = e
		}
	}

	inscPhotos := map[int][]string{}
	for _, r := range t["tbl_inscription_image"] {
		id := atoi(r.Str("InscriptionId"))
		img, ok := images[atoi(r.Str("ImageId"))]
		if !ok {
			log.Printf("warning: inscription %d refers to missing image %s", id, r.Str("ImageId"))
			continue
		}
		inscPhotos[id] = append(inscPhotos[id], img)
	}
	inscPeople := map[int][]Person{}
	for _, r := range t["tbl_inscription_person"] {
		if p := people[atoi(r.Str("PersonId"))]; p != nil {
			id := atoi(r.Str("InscriptionId"))
			inscPeople[id] = append(inscPeople[id], *p)
		}
	}

	stoneByID := map[int]*Stone{}
	stonePlace := map[int]int{}
	for _, r := range t["tbl_monument"] {
		id := atoi(r.Str("Id"))
		stoneByID[id] = &Stone{ID: id, Inscriptions: []Inscription{}}
		stonePlace[id] = atoi(r.Str("PlaceId"))
	}
	for _, r := range t["tbl_monument_inscription"] {
		s := stoneByID[atoi(r.Str("MonumentId"))]
		if s == nil {
			log.Printf("warning: inscription %s refers to missing monument %s", r.Str("Id"), r.Str("MonumentId"))
			continue
		}
		id := atoi(r.Str("Id"))
		s.Inscriptions = append(s.Inscriptions, Inscription{
			ID:      id,
			Face:    atoi(r.Str("FaceNumber")),
			Text:    cleanText(r.Str("InscriptionText")),
			Version: atoi(r.Str("Version")),
			Photos:  inscPhotos[id],
			People:  inscPeople[id],
		})
	}

	// Each place's photo directory is the common prefix of its images, e.g.
	// scotland/fife/tayport/auld_kirk_cemetery. Count candidates and keep the
	// most common in case of strays.
	dirVotes := map[int]map[string]int{}
	vote := func(placeID int, dir string) {
		if dirVotes[placeID] == nil {
			dirVotes[placeID] = map[string]int{}
		}
		dirVotes[placeID][dir]++
	}
	placePhotos := map[int][]string{}
	for _, r := range t["tbl_place_image"] {
		id := atoi(r.Str("PlaceId"))
		if img, ok := images[atoi(r.Str("ImageId"))]; ok {
			placePhotos[id] = append(placePhotos[id], img)
			if i := strings.LastIndex(img, "/image/"); i >= 0 {
				vote(id, img[:i])
			}
		}
	}
	for id, s := range stoneByID {
		for _, in := range s.Inscriptions {
			for _, img := range in.Photos {
				if i := strings.Index(img, "/monument/"); i >= 0 {
					vote(stonePlace[id], img[:i])
				}
			}
		}
	}

	var places []*Place
	stones := map[int][]*Stone{}
	for _, r := range t["tbl_place"] {
		id := atoi(r.Str("Id"))
		loc := locations[atoi(r.Str("locationId"))]
		p := &Place{
			ID:      id,
			Name:    r.Str("Name"),
			Type:    map[string]string{"1": "cemetery", "2": "church"}[r.Str("PlaceTypeId")],
			Town:    loc.town,
			County:  loc.county,
			Country: loc.country,
		}
		var best int
		for dir, n := range dirVotes[id] {
			if n > best || (n == best && dir < p.ImageDir) {
				p.ImageDir, best = dir, n
			}
		}
		if len(dirVotes[id]) > 1 {
			log.Printf("warning: place %d has photos in %d directories, using %s", id, len(dirVotes[id]), p.ImageDir)
		}
		if p.ImageDir == "" {
			log.Fatalf("place %d (%s) has no photos to locate its directory", id, p.Name)
		}
		p.Slug = strings.ReplaceAll(strings.ToLower(path.Base(path.Dir(p.ImageDir))+"-"+path.Base(p.ImageDir)), "_", "-")
		p.Photos = relativeTo(p.ImageDir, placePhotos[id])
		places = append(places, p)
	}
	slices.SortFunc(places, func(a, b *Place) int { return cmp.Compare(a.Slug, b.Slug) })

	placeByID := map[int]*Place{}
	for _, p := range places {
		placeByID[p.ID] = p
	}
	for id, s := range stoneByID {
		p := placeByID[stonePlace[id]]
		if p == nil {
			log.Printf("warning: monument %d has no place", id)
			continue
		}
		slices.SortFunc(s.Inscriptions, func(a, b Inscription) int {
			return cmp.Or(cmp.Compare(a.Face, b.Face), cmp.Compare(a.ID, b.ID))
		})
		for i := range s.Inscriptions {
			in := &s.Inscriptions[i]
			in.Photos = relativeTo(p.ImageDir, in.Photos)
			for _, ph := range in.Photos {
				// Larger cemeteries were photographed in sections, kept as
				// subdirectories: monument/image/<section>/<file>.
				if rest, ok := strings.CutPrefix(ph, "monument/image/"); ok {
					if dir := path.Dir(rest); dir != "." && s.Section == "" {
						s.Section = dir
					}
				}
			}
			p.Inscriptions++
			if in.Text != "" {
				p.Transcribed++
			}
		}
		p.Stones++
		stones[p.ID] = append(stones[p.ID], s)
	}
	for _, list := range stones {
		slices.SortFunc(list, func(a, b *Stone) int { return cmp.Compare(a.ID, b.ID) })
	}
	return places, stones
}

// checkPhotos reports photo references with no matching file in the backup
// zip, and monument photos in the zip that no inscription refers to.
func checkPhotos(zipPath string, places []*Place, stones map[int][]*Stone) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	inZip := map[string]bool{}
	for _, f := range zr.File {
		inZip[f.Name] = true
	}

	for _, p := range places {
		base := resourcesPrefix + p.ImageDir + "/"
		referenced := map[string]bool{}
		var missing []string
		check := func(ph string) {
			referenced[base+ph] = true
			if !inZip[base+ph] {
				missing = append(missing, ph)
			}
		}
		for _, ph := range p.Photos {
			check(ph)
		}
		for _, s := range stones[p.ID] {
			for _, in := range s.Inscriptions {
				for _, ph := range in.Photos {
					check(ph)
				}
			}
		}
		var unreferenced []string
		for name := range inZip {
			rest, ok := strings.CutPrefix(name, base+"monument/image/")
			if !ok || strings.HasPrefix(rest, "thumbs/") || strings.HasSuffix(name, "/") || !isPhoto(name) {
				continue
			}
			if !referenced[name] {
				unreferenced = append(unreferenced, strings.TrimPrefix(name, base))
			}
		}
		slices.Sort(unreferenced)
		if len(missing) > 0 || len(unreferenced) > 0 {
			fmt.Printf("%s: %d referenced photos missing from zip, %d monument photos not linked to an inscription\n",
				p.Slug, len(missing), len(unreferenced))
			for _, m := range head(missing, 5) {
				fmt.Println("  missing:", m)
			}
			for _, u := range head(unreferenced, 5) {
				fmt.Println("  unlinked:", u)
			}
		}
	}
	return nil
}

func isPhoto(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".jpg", ".jpeg", ".png":
		return true
	}
	return false
}

func head(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func relativeTo(dir string, imgs []string) []string {
	var out []string
	for _, img := range imgs {
		out = append(out, strings.TrimPrefix(strings.TrimPrefix(img, dir), "/"))
	}
	return out
}

// cleanText trims trailing spaces from each line and blank lines from the ends.
func cleanText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.Trim(strings.Join(lines, "\n"), "\n")
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func writeJSON(name string, v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(name, append(b, '\n'), 0o644); err != nil {
		log.Fatal(err)
	}
}
