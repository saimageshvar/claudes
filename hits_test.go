package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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

func TestWheelOverMatchesScrollsMatchesNotHead(t *testing.T) {
	ss := fixtures()
	m := newModel()
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	m.Update(loadedMsg(ss))
	var snips []Snippet
	for i := 0; i < 5; i++ {
		snips = append(snips, Snippet{User: i%2 == 0, Pre: "…said that ", Hit: "refresh token", Post: " is reused across both tabs at the same time and that is the whole problem here…"})
	}
	m.hits = map[string][]Snippet{ss[0].ID: snips}
	m.refilter()
	if !m.matchOn || m.match.AtBottom() {
		t.Fatalf("want a scrollable matches band, matchOn=%v height=%d", m.matchOn, m.match.Height)
	}
	headAt, matchAt := m.head.YOffset, m.match.YOffset
	y := bodyTop + 1 + len(m.pvTop) + 1 // first snippet row
	m.Update(tea.MouseMsg{X: 100, Y: y, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	if m.match.YOffset <= matchAt {
		t.Errorf("wheel over matches should scroll matches: offset %d -> %d", matchAt, m.match.YOffset)
	}
	if m.head.YOffset != headAt {
		t.Errorf("wheel over matches must not scroll how-it-started: offset %d -> %d", headAt, m.head.YOffset)
	}
}

func writeTranscript(t *testing.T, lines ...string) Session {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return Session{Path: path}
}

func TestTranscriptHitsFindsPromptsStartingWithAngleBracket(t *testing.T) {
	s := writeTranscript(t, `{"parentUuid":"a","type":"user","origin":{"kind":"human"},"message":{"content":"<Button onClick={save}>Save</Button> never fires"}}`)
	if got := transcriptHits([]Session{s}, "onclick", func() bool { return false })[0]; len(got) != 1 || !got[0].User {
		t.Errorf("a pasted JSX prompt should be searchable, got %+v", got)
	}
}

func TestTranscriptHitsPhraseSpansANewline(t *testing.T) {
	s := writeTranscript(t, `{"parentUuid":"a","type":"assistant","message":{"content":[{"type":"text","text":"the refresh\ntoken is shared"}]}}`)
	if got := transcriptHits([]Session{s}, "refresh token", func() bool { return false })[0]; len(got) != 1 {
		t.Errorf("words split by a newline should still match the phrase, got %+v", got)
	}
}

func TestTranscriptHitsNonASCIIPhrase(t *testing.T) {
	s := writeTranscript(t, `{"parentUuid":"a","type":"assistant","message":{"content":[{"type":"text","text":"Un CAFÉ très serré et 日本語のテスト"}]}}`)
	for _, q := range []string{"café", "日本語"} {
		if got := transcriptHits([]Session{s}, q, func() bool { return false })[0]; len(got) != 1 {
			t.Errorf("%q should match, got %+v", q, got)
		}
	}
}

func TestSnippetRowsFitAndKeepHitVisible(t *testing.T) {
	long := strings.Repeat("alpha beta gamma delta ", 8)
	cjk := strings.Repeat("日本語のテスト", 12)
	for _, sn := range []Snippet{
		{Pre: "…" + long, Hit: "needle", Post: " " + long + "…"},
		{Pre: "…" + cjk, Hit: "目的", Post: cjk + "…"},
		{User: true, Pre: "", Hit: "needle", Post: " short"},
	} {
		for _, tw := range []int{30, 48, 70} {
			rows := snippetRows(sn, tw)
			if len(rows) == 0 || len(rows) > 2 {
				t.Fatalf("tw=%d: want 1-2 rows, got %d", tw, len(rows))
			}
			joined := ""
			for _, r := range rows {
				if w := lipgloss.Width(r); w != tw+2 {
					t.Errorf("tw=%d: row is %d cells, want %d: %q", tw, w, tw+2, r)
				}
				joined += ansi.Strip(r)
			}
			if !strings.Contains(joined, sn.Hit) {
				t.Errorf("tw=%d: hit %q not visible in %q", tw, sn.Hit, joined)
			}
		}
	}
}

func TestHitsArrivingKeepTheSelection(t *testing.T) {
	ss := fixtures()
	m := newModel()
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m.Update(loadedMsg(ss))
	m.list.Select(2)
	want := m.selected().ID
	seq := m.seq.Add(1)
	m.Update(hitsMsg{seq: seq, ids: map[string][]Snippet{ss[4].ID: {{Hit: "x"}}}})
	if got := m.selected().ID; got != want {
		t.Errorf("selection jumped from %s to %s when hits arrived", want, got)
	}
}

func TestMatchesScrollSurvivesRebuild(t *testing.T) {
	ss := fixtures()
	m := newModel()
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	m.Update(loadedMsg(ss))
	var snips []Snippet
	for i := 0; i < 5; i++ {
		snips = append(snips, Snippet{Pre: "…said ", Hit: "token", Post: " is reused across both tabs at the same time and that is the whole problem here…"})
	}
	m.hits = map[string][]Snippet{ss[0].ID: snips}
	m.refilter()
	m.match.SetYOffset(2)
	m.refreshPreview() // what a late chat load triggers
	if m.match.YOffset != 2 {
		t.Errorf("matches scroll reset to %d after a rebuild", m.match.YOffset)
	}
}
