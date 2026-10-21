package sqldump

import (
	"strings"
	"testing"
)

const sample = "-- comment\n" +
	"CREATE TABLE `t` (`Id` int);\n" +
	"INSERT INTO `t` (`Id`, `Text`, `Other`) VALUES\n" +
	"(1, 'IN MEMORY OF\\nJANE', NULL),\n" +
	"(2, 'Alexander''s death; \\'quoted\\'', '3.5'),\n" +
	"(3, 'multi\nline', -7);\n" +
	"INSERT INTO `t` (`Id`, `Text`, `Other`) VALUES\n" +
	"(4, '', 0);\n"

func TestParse(t *testing.T) {
	tables, err := Parse(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	rows := tables["t"]
	if len(rows) != 4 {
		t.Fatalf("got %d rows, want 4", len(rows))
	}
	tests := []struct {
		row       int
		col, want string
		null      bool
	}{
		{0, "Text", "IN MEMORY OF\nJANE", false},
		{0, "Other", "", true},
		{1, "Text", "Alexander's death; 'quoted'", false},
		{1, "Other", "3.5", false},
		{2, "Text", "multi\nline", false},
		{2, "Other", "-7", false},
		{3, "Id", "4", false},
		{3, "Text", "", false},
	}
	for _, tt := range tests {
		v := rows[tt.row][tt.col]
		if tt.null {
			if v != nil {
				t.Errorf("row %d %s = %q, want NULL", tt.row, tt.col, *v)
			}
			continue
		}
		if v == nil || *v != tt.want {
			t.Errorf("row %d %s = %v, want %q", tt.row, tt.col, v, tt.want)
		}
	}
}

func TestParseColumnMismatch(t *testing.T) {
	_, err := Parse(strings.NewReader("INSERT INTO `t` (`a`, `b`) VALUES\n(1);\n"))
	if err == nil {
		t.Fatal("expected error for short row")
	}
}
