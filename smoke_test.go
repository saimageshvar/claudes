package main

import (
	"testing"
	"time"
)

func TestLoadAndSearch(t *testing.T) {
	st := time.Now()
	ss := loadSessions()
	t.Logf("load %v", time.Since(st))
	if len(ss) == 0 {
		t.Fatal("no sessions")
	}
	for _, s := range ss[:3] {
		t.Logf("%s | %s | %s | %s | msgs=%d wt=%q files=%d", s.ID[:8], s.Title, s.Cwd, s.Branch, s.Msgs, s.Worktree, len(s.Files))
	}
	st = time.Now()
	hits := transcriptHits(ss, []string{"json-render"}, func() bool { return false })
	t.Logf("%d sessions, %d hits in %v", len(ss), len(hits), time.Since(st))
}
