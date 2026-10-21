package archive

import (
	"cmp"
	"slices"
	"strings"
)

// Result is one stone matching a search.
type Result struct {
	Stone *Stone
	// Lines are the inscription lines containing a search term.
	Lines []string
	score int
}

// Search finds stones whose inscriptions contain every word in q. A query
// word matches any inscription word it starts with, so "SMITH" matches
// "SMITH," and "1844" matches "1844.". Whole-word matches rank higher.
// An optional place slug narrows the search to one place.
func (a *Archive) Search(q, place string, limit int) []Result {
	terms := words(strings.ToUpper(q))
	if len(terms) == 0 {
		return nil
	}
	var results []Result
	for _, p := range a.Places {
		if place != "" && p.Slug != place {
			continue
		}
		for _, s := range a.stones[p.Slug] {
			score := 0
			for _, t := range terms {
				best := 0
				for _, w := range s.words {
					if w == t {
						best = 2
						break
					}
					if strings.HasPrefix(w, t) {
						best = 1
					}
				}
				if best == 0 {
					score = 0
					break
				}
				score += best
			}
			if score > 0 {
				results = append(results, Result{Stone: s, Lines: matchingLines(s, terms), score: score})
			}
		}
	}
	slices.SortStableFunc(results, func(x, y Result) int { return cmp.Compare(y.score, x.score) })
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results
}

func matchingLines(s *Stone, terms []string) []string {
	var lines []string
	for _, line := range strings.Split(s.Text(), "\n") {
		for _, w := range words(strings.ToUpper(line)) {
			if slices.ContainsFunc(terms, func(t string) bool { return strings.HasPrefix(w, t) }) {
				lines = append(lines, line)
				break
			}
		}
		if len(lines) == 3 {
			break
		}
	}
	return lines
}
