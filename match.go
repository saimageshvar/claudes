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

var fields = []func(*Session) string{
	func(s *Session) string { return s.Title },
	func(s *Session) string { return s.Repo },
	func(s *Session) string { return s.Branch },
	func(s *Session) string { return s.Linked },
}

// rank keeps sessions where every token fuzzy-matches one of title, folder, branch or worktree name,
// best score first and newest first on ties; base is already newest first.
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
		for fi, pick := range fields {
			for _, r := range fuzzy.FindFrom(tok, fieldSet{pick, base}) {
				// Drop matches whose letters are scattered across a long string ("tok" in "…long-branch-…-keeps").
				if mi := r.MatchedIndexes; !alive[r.Index] || mi[len(mi)-1]-mi[0] > 3*len(tok)+4 {
					continue
				}
				if old, ok := best[r.Index]; !ok || r.Score > old {
					best[r.Index] = r.Score
				}
				if fi == 0 {
					if hl[r.Index] == nil {
						hl[r.Index] = map[int]bool{}
					}
					for _, b := range r.MatchedIndexes {
						hl[r.Index][b] = true
					}
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
