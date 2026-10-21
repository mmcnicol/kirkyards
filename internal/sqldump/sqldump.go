// Package sqldump reads the INSERT statements from a phpMyAdmin/mysqldump
// SQL file, which is all that is needed to recover the data from the 2013
// InscriptionImages MySQL database.
package sqldump

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// Row maps column names to values. A nil value is SQL NULL.
type Row map[string]*string

// Str returns the column value, or "" for NULL.
func (r Row) Str(col string) string {
	if v := r[col]; v != nil {
		return *v
	}
	return ""
}

// Tables maps table names to their rows, in dump order.
type Tables map[string][]Row

var insertRe = regexp.MustCompile("^INSERT INTO `([^`]+)` \\(([^)]*)\\) VALUES\\s*")

// Parse reads every INSERT statement in the dump.
func Parse(r io.Reader) (Tables, error) {
	tables := Tables{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	var (
		table string
		cols  []string
		stmt  strings.Builder
	)
	for sc.Scan() {
		line := sc.Text()
		if table == "" {
			m := insertRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			table = m[1]
			cols = nil
			for _, c := range strings.Split(m[2], ",") {
				cols = append(cols, strings.Trim(strings.TrimSpace(c), "`"))
			}
			stmt.Reset()
			line = line[len(m[0]):]
		}
		stmt.WriteString(line)
		stmt.WriteByte('\n')
		if done, err := statementComplete(stmt.String()); err != nil {
			return nil, err
		} else if !done {
			continue
		}
		rows, err := parseValues(stmt.String(), cols)
		if err != nil {
			return nil, fmt.Errorf("table %s: %w", table, err)
		}
		tables[table] = append(tables[table], rows...)
		table = ""
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if table != "" {
		return nil, fmt.Errorf("table %s: unterminated INSERT statement", table)
	}
	return tables, nil
}

// statementComplete reports whether s ends with a ';' outside a string literal.
func statementComplete(s string) (bool, error) {
	inStr := false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case inStr && c == '\\':
			i++
		case c == '\'':
			inStr = !inStr
		case !inStr && c == ';':
			return true, nil
		}
	}
	return false, nil
}

// parseValues parses "(v1, v2), (v1, v2);" into rows.
func parseValues(s string, cols []string) ([]Row, error) {
	p := &parser{s: s}
	var rows []Row
	for {
		p.skipSpace()
		if p.eof() {
			return nil, fmt.Errorf("unexpected end of statement")
		}
		if p.peek() == ';' {
			return rows, nil
		}
		if err := p.expect('('); err != nil {
			return nil, err
		}
		row := Row{}
		for i := 0; ; i++ {
			p.skipSpace()
			v, err := p.value()
			if err != nil {
				return nil, err
			}
			if i >= len(cols) {
				return nil, fmt.Errorf("row %d has more values than columns", len(rows)+1)
			}
			row[cols[i]] = v
			p.skipSpace()
			if p.peek() == ',' {
				p.pos++
				continue
			}
			if err := p.expect(')'); err != nil {
				return nil, err
			}
			if i != len(cols)-1 {
				return nil, fmt.Errorf("row %d has %d values, want %d", len(rows)+1, i+1, len(cols))
			}
			break
		}
		rows = append(rows, row)
		p.skipSpace()
		if p.peek() == ',' {
			p.pos++
		}
	}
}

type parser struct {
	s   string
	pos int
}

func (p *parser) eof() bool  { return p.pos >= len(p.s) }
func (p *parser) peek() byte { return p.s[p.pos] }

func (p *parser) skipSpace() {
	for !p.eof() && strings.IndexByte(" \t\r\n", p.peek()) >= 0 {
		p.pos++
	}
}

func (p *parser) expect(c byte) error {
	if p.eof() || p.peek() != c {
		return fmt.Errorf("expected %q at offset %d", c, p.pos)
	}
	p.pos++
	return nil
}

var mysqlEscapes = map[byte]string{
	'0': "\x00", 'b': "\b", 'n': "\n", 'r': "\r", 't': "\t", 'Z': "\x1a",
	'\\': "\\", '\'': "'", '"': "\"", '%': "\\%", '_': "\\_",
}

// value parses a quoted string, NULL, or bare number.
func (p *parser) value() (*string, error) {
	if p.eof() {
		return nil, fmt.Errorf("unexpected end of statement")
	}
	if p.peek() != '\'' {
		start := p.pos
		for !p.eof() && strings.IndexByte(",) \t\r\n", p.peek()) < 0 {
			p.pos++
		}
		tok := p.s[start:p.pos]
		if tok == "" {
			return nil, fmt.Errorf("empty value at offset %d", start)
		}
		if strings.EqualFold(tok, "NULL") {
			return nil, nil
		}
		return &tok, nil
	}
	p.pos++
	var b strings.Builder
	for {
		if p.eof() {
			return nil, fmt.Errorf("unterminated string")
		}
		c := p.peek()
		p.pos++
		switch {
		case c == '\\':
			if p.eof() {
				return nil, fmt.Errorf("unterminated string")
			}
			e := p.peek()
			p.pos++
			if r, ok := mysqlEscapes[e]; ok {
				b.WriteString(r)
			} else {
				b.WriteByte(e)
			}
		case c == '\'' && !p.eof() && p.peek() == '\'':
			b.WriteByte('\'')
			p.pos++
		case c == '\'':
			v := b.String()
			return &v, nil
		default:
			b.WriteByte(c)
		}
	}
}
