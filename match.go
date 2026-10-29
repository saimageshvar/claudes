package main

import (
	"sort"
	"strings"

	"github.com/sahilm/fuzzy"
)

type match struct {
	s     *Session
	score int
	hl    map[int]bool // byte offsets into s.Title
}

type fieldSet struct {
	pick func(*Session) string
	all  []*Session
}

func (f fieldSet) Len() int            { return len(f.all) }
func (f fieldSet) String(i int) string { return f.pick(f.all[i]) }

// fields are searched in this order of weight; only the title is highlighted.
var fields = []struct {
	pick   func(*Session) string
	weight int
}{
	{func(s *Session) string { return s.Title }, 300},
	{func(s *Session) string { return s.Repo }, 200},
	{func(s *Session) string { return s.Linked }, 150},
	{func(s *Session) string { return s.Branch }, 100},
}

func wordStart(s string, i int) bool {
	if i == 0 {
		return true
	}
	c := s[i-1]
	return !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 0x80)
}

// substringScore ranks a literal hit: a word start beats mid-word, earlier and shorter fields beat later and longer.
func substringScore(low string, idx, weight int) int {
	sc := 2000 + weight - min(idx, 100) - min(len(low)/8, 50)
	if wordStart(low, idx) {
		sc += 1500
	}
	return sc
}

// rank keeps sessions where every token matches one of title, folder, branch or worktree name, best score first
// and newest first on ties (base is already newest first). A token matches as a literal substring (word starts
// score highest), or failing that fuzzily, but only when its letters sit nearly together so "cog" does not match
// "Colima ram 20GB".
func rank(base []*Session, query string) []match {
	tokens := strings.Fields(strings.ToLower(query))
	if len(tokens) == 0 {
		out := make([]match, len(base))
		for i, s := range base {
			out[i] = match{s: s}
		}
		return out
	}
	total := make([]int, len(base))
	alive := make([]bool, len(base))
	hl := make([]map[int]bool, len(base))
	for i := range alive {
		alive[i] = true
	}
	for _, tok := range tokens {
		best := map[int]int{}
		mark := func(i, fi, from, to int) {
			if fi != 0 {
				return
			}
			if hl[i] == nil {
				hl[i] = map[int]bool{}
			}
			for b := from; b < to; b++ {
				hl[i][b] = true
			}
		}
		for fi, f := range fields {
			set := fieldSet{f.pick, base}
			literal := map[int]bool{}
			for i, s := range base {
				if !alive[i] {
					continue
				}
				val := f.pick(s)
				low := strings.ToLower(val)
				if len(low) != len(val) {
					continue
				}
				if idx := strings.Index(low, tok); idx >= 0 {
					if sc := substringScore(low, idx, f.weight); sc > best[i] {
						best[i] = sc
					}
					literal[i] = true
					mark(i, fi, idx, idx+len(tok))
				}
			}
			if len(tok) < 2 {
				continue
			}
			for _, r := range fuzzy.FindFrom(tok, set) {
				mi := r.MatchedIndexes
				if !alive[r.Index] || literal[r.Index] || mi[len(mi)-1]-mi[0] > len(tok)+2 {
					continue
				}
				if sc := 200 + f.weight/10 + min(max(r.Score, -100), 300); sc > best[r.Index] {
					best[r.Index] = sc
				}
				for _, b := range mi {
					mark(r.Index, fi, b, b+1)
				}
			}
		}
		for i := range alive {
			sc, ok := best[i]
			alive[i] = alive[i] && ok
			total[i] += sc
		}
	}
	var out []match
	for i, ok := range alive {
		if ok {
			out = append(out, match{s: base[i], score: total[i], hl: hl[i]})
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].score > out[b].score })
	return out
}
