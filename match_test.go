package main

import "testing"

func titled(titles ...string) []*Session {
	var out []*Session
	for _, t := range titles {
		out = append(out, &Session{Title: t})
	}
	return out
}

func titlesOf(ms []match) []string {
	var out []string
	for _, m := range ms {
		out = append(out, m.s.Title)
	}
	return out
}

func TestRankQuality(t *testing.T) {
	base := titled(
		"Colima ram 20GB",
		"Herdr 0.9.4 changelog",
		"cog-perf",
		"Brief chr-cog executor plan",
		"cog-babysit-pr",
		"Fix token refresh race in auth middleware",
		"Prs-by-author widget Jira and COG label",
		"Write fuzzy-search TUI spec",
	)
	cases := []struct {
		q     string
		first []string // must lead the results, in any order
		never []string // must not appear
	}{
		{"cog", []string{"cog-perf", "cog-babysit-pr", "Brief chr-cog executor plan", "Prs-by-author widget Jira and COG label"}, []string{"Colima ram 20GB", "Herdr 0.9.4 changelog"}},
		{"babys", []string{"cog-babysit-pr"}, nil},
		{"bbysit", []string{"cog-babysit-pr"}, nil},
		{"auth tok", []string{"Fix token refresh race in auth middleware"}, []string{"cog-perf"}},
		{"fuzzy", []string{"Write fuzzy-search TUI spec"}, nil},
		{"zzzz", nil, []string{"cog-perf", "Colima ram 20GB"}},
	}
	for _, c := range cases {
		got := titlesOf(rank(base, c.q))
		for i, want := range c.first {
			found := -1
			for j, g := range got {
				if g == want {
					found = j
				}
			}
			if found < 0 || found >= len(c.first) {
				t.Errorf("%q: %q should be in the top %d, got %q", c.q, want, len(c.first), got)
			}
			_ = i
		}
		for _, bad := range c.never {
			for _, g := range got {
				if g == bad {
					t.Errorf("%q should not match %q (got %q)", c.q, bad, got)
				}
			}
		}
	}
}

func TestRankWordStartBeatsMidWord(t *testing.T) {
	got := titlesOf(rank(titled("Decogify the module", "A cog in the machine"), "cog"))
	if len(got) != 2 || got[0] != "A cog in the machine" {
		t.Errorf("word start should lead, got %q", got)
	}
}
