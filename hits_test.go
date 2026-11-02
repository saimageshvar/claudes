package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTranscriptHitsReturnsMessageText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	lines := []string{
		`{"parentUuid":"a","type":"user","origin":{"kind":"human"},"message":{"content":"Why does the refresh token race happen in auth?"}}`,
		`{"parentUuid":"b","type":"user","message":{"content":[{"type":"tool_result","content":"refresh token dump"}]}}`,
		`{"parentUuid":"c","type":"assistant","message":{"content":[{"type":"text","text":"The Refresh Token is shared via a memoized promise."}]}}`,
		`{"parentUuid":"d","type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"echo refresh token"}}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hits := transcriptHits([]Session{{Path: path}}, "refresh  TOKEN", func() bool { return false })
	got := hits[0]
	if len(got) != 2 {
		t.Fatalf("want 2 snippets (your prompt and Claude's text, not tool output), got %+v", got)
	}
	if !got[0].User || got[0].Hit != "refresh token" || got[0].Pre != "Why does the " || !strings.HasPrefix(got[0].Post, " race") {
		t.Errorf("user snippet wrong: %+v", got[0])
	}
	if got[1].User || got[1].Hit != "Refresh Token" {
		t.Errorf("claude snippet should keep the original case: %+v", got[1])
	}
}

func TestPreviewShowsMatchesBand(t *testing.T) {
	ss := fixtures()
	m := newModel()
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 44})
	m.Update(loadedMsg(ss))
	m.hits = map[string][]Snippet{ss[0].ID: {
		{User: true, Pre: "…why does the ", Hit: "refresh token", Post: " race happen in auth?"},
		{Pre: "The ", Hit: "refresh token", Post: " is shared…"},
	}}
	m.refilter()
	out := m.View()
	for _, want := range []string{"matches", "2 lines", "refresh token", "race happen"} {
		if !strings.Contains(out, want) {
			t.Errorf("preview missing %q:\n%s", want, out)
		}
	}
}
