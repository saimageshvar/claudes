package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

var update = flag.Bool("update", false, "rewrite testdata/*.golden")

var (
	fixedNow = time.Date(2026, 10, 28, 9, 14, 0, 0, time.UTC)
	realHome = os.Getenv("HOME")
)

func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.Ascii)
	lipgloss.SetHasDarkBackground(false)
	time.Local = time.UTC
	now = func() time.Time { return fixedNow }
	os.Setenv("HOME", "/home/u")
	os.Exit(m.Run())
}

func fixtures() []Session {
	ago := func(d time.Duration) time.Time { return fixedNow.Add(-d) }
	mk := func(id, title, cwd, branch string, g gitInfo, upd time.Duration, n int, prompt, reply string, files ...string) Session {
		return Session{ID: id, Title: title, Cwd: cwd, Branch: branch, gitInfo: g, Updated: ago(upd), Started: ago(upd + 2*time.Hour),
			Msgs: n, LastPrompt: prompt, LastReply: reply, Files: files}
	}
	return []Session{
		mk("7f3a1c9e-4d21-4b8a-9e0c-1a2b3c4d5e6f", "Fix token refresh race in auth middleware", "/home/u/code/chronus-api.worktrees/auth-refresh", "feat/auth-refresh",
			gitInfo{Repo: "chronus-api", Linked: "auth-refresh"}, 2*time.Minute, 48,
			"Why does the second request still send the old token?",
			"The refresh promise is not shared, so two callers race. I memoized it per session and added a regression test; all auth specs pass.",
			"/home/u/code/chronus-api.worktrees/auth-refresh/src/auth/middleware.ts", "/home/u/code/chronus-api.worktrees/auth-refresh/src/auth/refresh.ts", "/home/u/code/chronus-api.worktrees/auth-refresh/test/auth.spec.ts"),
		mk("11111111-2222-3333-4444-555555555555", "Migrate mentoring-match job to queue workers", "/home/u/code/chronus-api", "main", gitInfo{Repo: "chronus-api", Primary: true}, 41*time.Minute, 112, "Move it to the queue.", "Done."),
		mk("22222222-2222-3333-4444-555555555555", "Archie chat: streaming render jank", "/home/u/code/chronus-web.worktrees/chat-stream", "fix/chat-stream", gitInfo{Repo: "chronus-web", Linked: "chat-stream"}, 1*time.Hour+5*time.Minute, 27, "Why does it jank?", "Re-render per token."),
		mk("3d5e88b2-1c4f-4a6b-b9e0-4d5c6b7a8f90", "Write fuzzy-search TUI spec", "/home/u/scratch/claudes", "", gitInfo{Repo: "claudes", NoGit: true}, 3*time.Hour, 19, "Sketch the list and preview layout.", "Two-line rows with a preview pane and a help bar at the bottom. I wrote it up in SPEC.md.", "/home/u/scratch/claudes/SPEC.md"),
		mk("44444444-2222-3333-4444-555555555555", "Investigate flaky e2e: program-join.spec", "/home/u/code/chronus-web", "test/stabilize-e2e", gitInfo{Repo: "chronus-web", Primary: true}, 5*time.Hour, 64, "", ""),
		mk("55555555-2222-3333-4444-555555555555", "Terraform: bump RDS to 16", "/home/u/infra/prod", "main", gitInfo{Repo: "prod"}, 25*time.Hour, 33, "", ""),
		mk("66666666-2222-3333-4444-555555555555", "Explain webhook retry backoff", "/home/u/code/chronus-api", "main", gitInfo{Repo: "chronus-api", Primary: true}, 30*time.Hour, 1, "", ""),
		mk("77777777-2222-3333-4444-555555555555", "Review PR 2841: mentor availability with a deliberately very long title that must be truncated with an ellipsis", "/home/u/code/chronus-web.worktrees/pr-2841",
			"feat/availability-and-a-very-long-branch-name-that-keeps-going", gitInfo{Repo: "chronus-web", Linked: "pr-2841"}, 4*24*time.Hour, 41, "", ""),
		mk("88888888-2222-3333-4444-555555555555", "Old notes", "/home/u/notes", "", gitInfo{Repo: "notes", NoGit: true}, 40*24*time.Hour, 3, "", ""),
	}
}

func render(t *testing.T, w, h int, sessions []Session, q string, keys ...string) string {
	t.Helper()
	m := newModel()
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m.Update(loadedMsg(sessions))
	if q != "" {
		m.input.SetValue(q)
		m.refilter()
	}
	for _, k := range keys {
		switch k {
		case "ctrl+p":
			m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
		case "pgdown":
			m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
		case "enter":
			m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		}
	}
	out := ansi.Strip(m.View())
	lines := strings.Split(out, "\n")
	if len(lines) != h {
		t.Fatalf("%d lines, want %d", len(lines), h)
	}
	for i, l := range lines {
		if got := ansi.StringWidth(l); got != w {
			t.Fatalf("line %d is %d cells, want %d: %q", i, got, w, l)
		}
	}
	return out
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	p := filepath.Join("testdata", name+".golden")
	if *update {
		os.MkdirAll("testdata", 0o755)
		if err := os.WriteFile(p, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("%v (run go test -update)", err)
	}
	if string(want) != got {
		t.Errorf("%s differs from golden:\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func TestLayout(t *testing.T) {
	for _, c := range []struct {
		name  string
		w, h  int
		q     string
		keys  []string
		empty bool
	}{
		{name: "w80", w: 80, h: 30},
		{name: "w110", w: 110, h: 30},
		{name: "w140", w: 140, h: 30},
		{name: "w140_query", w: 140, h: 30, q: "auth tok"},
		{name: "w110_nomatch", w: 110, h: 30, q: "zzzz"},
		{name: "w80_nosessions", w: 80, h: 30, empty: true},
		{name: "w140_preview_off", w: 140, h: 30, keys: []string{"ctrl+p"}},
		{name: "w110_page2", w: 110, h: 30, keys: []string{"pgdown"}},
		{name: "w110_short", w: 110, h: 16},
		{name: "w140_resuming", w: 140, h: 30, keys: []string{"enter"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			ss := fixtures()
			if c.empty {
				ss = []Session{}
			}
			golden(t, c.name, render(t, c.w, c.h, ss, c.q, c.keys...))
		})
	}
}

func TestRankAllTokensMustMatch(t *testing.T) {
	var base []*Session
	ss := fixtures()
	for i := range ss {
		base = append(base, &ss[i])
	}
	got := rank(base, "auth tok")
	if len(got) != 1 || !strings.HasPrefix(got[0].s.Title, "Fix token refresh") {
		t.Fatalf("auth tok matched %d sessions", len(got))
	}
	if len(got[0].hl) == 0 {
		t.Fatal("no highlighted characters")
	}
	if n := len(rank(base, "chat-stream")); n != 1 { // worktree / branch name
		t.Fatalf("chat-stream matched %d", n)
	}
}

func TestWrapAndAgo(t *testing.T) {
	ls := wrap("one two three four five six seven", 9, 2)
	if len(ls) != 2 || !strings.HasSuffix(ls[1], "…") {
		t.Fatalf("wrap: %q", ls)
	}
	if got := ago(fixedNow.Add(-90*time.Second), fixedNow); got != "1m ago" {
		t.Fatal(got)
	}
	if got := msgs(1); got != "1 msg" {
		t.Fatal(got)
	}
}
