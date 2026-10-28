package main

import (
	"testing"
	"time"
)

// Smoke test against the real ~/.claude/projects; skipped where there are no sessions.
func TestLoadAndSearch(t *testing.T) {
	t.Setenv("HOME", realHome)
	defer func(f func() time.Time) { now = f }(now)
	now = time.Now
	st := time.Now()
	ss := loadSessions()
	t.Logf("load %v", time.Since(st))
	if len(ss) == 0 {
		t.Skip("no sessions on this machine")
	}
	for _, s := range ss[:min(3, len(ss))] {
		t.Logf("%s | %s | repo=%s branch=%s linked=%q primary=%v msgs=%d files=%d", s.ID[:8], s.Title, s.Repo, s.Branch, s.Linked, s.Primary, s.Msgs, len(s.Files))
	}
	st = time.Now()
	hits := transcriptHits(ss, []string{"json-render"}, func() bool { return false })
	t.Logf("%d sessions, %d hits in %v", len(ss), len(hits), time.Since(st))
}
