package archive

import (
	"testing"
	"testing/fstest"
)

func TestTitleFromText(t *testing.T) {
	tests := []struct{ text, want string }{
		{"ERECTED\nBY\nALEXANDER GREIG,\nIN MEMORY OF HIS WIFE", "Alexander Greig"},
		{"ERECTED BY WILLIAM SCOTT IN MEMORY OF HIS MOTHER\nMARJORY CORRIE", "William Scott"},
		{"[REERECTED?]\nIN MEMORY OF\nJANET SIMPSON\nWHO DIED 2 JUNE 1825", "Janet Simpson"},
		{"IN LOVING MEMORY OF\nJOHN MCNICOL", "John McNicol"},
		{"ERECTED\nBY\nWILLIAM [SMITH?]\nSHIPMASTER", "William Smith"},
		{"DIED 1844\nAGED 7 YEARS", ""},
		{"G B D S O\nIN MEMORY OF\nJOHN BROWN", "John Brown"},
	}
	for _, tt := range tests {
		if got := titleFromText(tt.text); got != tt.want {
			t.Errorf("titleFromText(%q) = %q, want %q", tt.text, got, tt.want)
		}
	}
}

var testFS = fstest.MapFS{
	"places.json": {Data: []byte(`[
		{"id": 8, "slug": "tayport-auld-kirk-cemetery", "name": "Tayport Auld Kirk Cemetery", "town": "Tayport", "imageDir": "scotland/fife/tayport/auld_kirk_cemetery"},
		{"id": 1, "slug": "abdie-church", "name": "Abdie Church", "town": "Abdie", "imageDir": "scotland/fife/abdie/church"}
	]`)},
	"stones/tayport-auld-kirk-cemetery.json": {Data: []byte(`[
		{"id": 1, "inscriptions": [{"id": 10, "face": 1, "text": "ERECTED\nBY\nWILLIAM SCOTT\nALSO JOHN SMITH, BREWER, TAYPORT\nDIED 8 AUGUST 1844", "photos": ["monument/image/S1.JPG"]}]},
		{"id": 2, "inscriptions": [{"id": 20, "face": 1, "text": "IN MEMORY OF\nJANET SMITHSON\nDIED 1825"}]},
		{"id": 3, "inscriptions": [{"id": 30, "face": 1, "text": ""}]}
	]`)},
	"stones/abdie-church.json": {Data: []byte(`[]`)},
}

func TestLoad(t *testing.T) {
	a, err := Load(testFS)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Places) != 1 {
		t.Fatalf("got %d places, want 1 (empty places dropped)", len(a.Places))
	}
	s := a.Stone("tayport-auld-kirk-cemetery", 1)
	if s == nil {
		t.Fatal("stone 1 not found")
	}
	if s.Title != "William Scott" || s.Years() != "1844" || s.Place.Town != "Tayport" {
		t.Errorf("stone 1 = %q %q %q", s.Title, s.Years(), s.Place.Town)
	}
	if got := a.Stone("tayport-auld-kirk-cemetery", 3); got.Transcribed() || got.Title != "Stone 3" {
		t.Errorf("untranscribed stone: transcribed=%v title=%q", got.Transcribed(), got.Title)
	}
}

func TestSearch(t *testing.T) {
	a, err := Load(testFS)
	if err != nil {
		t.Fatal(err)
	}
	ids := func(rs []Result) (out []int) {
		for _, r := range rs {
			out = append(out, r.Stone.ID)
		}
		return out
	}
	tests := []struct {
		q    string
		want []int
	}{
		{"smith", []int{1, 2}}, // whole word first, then prefix of SMITHSON
		{"Smith brewer", []int{1}},
		{"1825", []int{2}},
		{"nobody", nil},
		{"", nil},
	}
	for _, tt := range tests {
		got := ids(a.Search(tt.q, "", 0))
		if len(got) != len(tt.want) || (len(got) > 0 && got[0] != tt.want[0]) {
			t.Errorf("Search(%q) = %v, want %v", tt.q, got, tt.want)
		}
	}
	if rs := a.Search("brewer", "", 0); len(rs) != 1 || rs[0].Lines[0] != "ALSO JOHN SMITH, BREWER, TAYPORT" {
		t.Errorf("matching lines = %v", rs)
	}
}
