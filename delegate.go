package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	rowH    = 3 // header-or-blank slot + 2 text lines (query mode: 2 text lines + blank)
	perPage = 6
	timeW   = 7
)

type item struct {
	match
	group string // day label when this row starts a group (or a page)
}

func (item) FilterValue() string { return "" }

type delegate struct{ m *model }

func (delegate) Height() int                         { return rowH }
func (delegate) Spacing() int                        { return 0 }
func (delegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d delegate) Render(w io.Writer, l list.Model, i int, li list.Item) {
	it := li.(item)
	sel := i == l.Index()
	lines := rowLines(it, l.Width(), sel, now())
	if d.m.query() == "" {
		head := strings.Repeat(" ", l.Width())
		if it.group != "" || i%max(l.Paginator.PerPage, 1) == 0 {
			head = dayHeader(dayLabel(it.s.Updated, now()), l.Width())
		}
		lines = append([]string{head}, lines...)
	} else {
		lines = append(lines, strings.Repeat(" ", l.Width()))
	}
	fmt.Fprint(w, strings.Join(lines, "\n"))
}

func dayLabel(t, at time.Time) string {
	y, mo, d := at.Date()
	today := time.Date(y, mo, d, 0, 0, 0, 0, at.Location())
	switch {
	case !t.Before(today):
		return "Today"
	case !t.Before(today.AddDate(0, 0, -1)):
		return "Yesterday"
	case !t.Before(today.AddDate(0, 0, -6)):
		return "This week"
	}
	return "Older"
}

func dayHeader(label string, w int) string {
	st := map[string]lipgloss.Style{"Today": sCyan, "Yesterday": sAmber, "This week": sAccent}[label]
	if label == "Older" {
		st = sDim
	}
	st = st.Bold(true)
	fill := max(w-len(label)-3, 0)
	return line(w, nil, []span{sp(" "+label+" ", st), sp(strings.Repeat("─", fill), sRule)}, nil)
}

func ago(t, at time.Time) string {
	d := at.Sub(t)
	days := int(d.Hours() / 24)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", max(int(d.Minutes()), 1))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case days < 14:
		return fmt.Sprintf("%dd ago", days)
	case days < 60:
		return fmt.Sprintf("%dw ago", days/7)
	case days < 365:
		return fmt.Sprintf("%dmo ago", days/30)
	}
	return fmt.Sprintf("%dy ago", days/365)
}

func freshness(t, at time.Time) lipgloss.Style {
	switch {
	case at.Sub(t) < time.Hour:
		return sGreen
	case dayLabel(t, at) == "Today":
		return sCyan
	}
	return sDim
}

func msgs(n int) string {
	if n == 1 {
		return "1 msg"
	}
	return fmt.Sprintf("%d msgs", n)
}

// tags is the folder chip, branch and worktree tag shared by list rows and the preview card.
func tags(s *Session) []span {
	out := []span{sp(" "+s.Repo+" ", chipStyle(s.Repo))}
	if s.Branch != "" {
		out = append(out, sp(" ", sFg), sp(s.Branch, sBranch))
	}
	switch {
	case s.Linked != "":
		out = append(out, sp(" ", sFg), sp(" wt "+s.Linked+" ", sWt))
	case s.Primary:
		out = append(out, sp(" ", sFg), sp("[primary]", sDim))
	}
	return out
}

func titleSpans(title string, hl map[int]bool, base lipgloss.Style) []span {
	var out []span
	var run strings.Builder
	cur := false
	flush := func() {
		if run.Len() == 0 {
			return
		}
		st := base
		if cur {
			st = sHit
		}
		out = append(out, sp(run.String(), st))
		run.Reset()
	}
	for b, r := range title {
		if hl[b] != cur {
			flush()
			cur = hl[b]
		}
		run.WriteRune(r)
	}
	flush()
	return out
}

func rowLines(it item, w int, sel bool, at time.Time) []string {
	s := it.s
	var bg lipgloss.TerminalColor
	bar := sp(" ", sFg)
	if sel {
		bg = cSelBg
		bar = sp("▌", sAccent)
	}
	fresh := freshness(s.Updated, at)
	when := ago(s.Updated, at)
	when = strings.Repeat(" ", max(timeW-len(when), 0)) + when

	title := titleSpans(oneLine(s.Title, 300), it.hl, sBold)
	if s.Title == "" {
		title = []span{sp("(untitled)", sDim)}
	}
	l1 := append([]span{bar, sp(" ", sFg), sp("●", fresh), sp(" ", sFg)}, title...)
	r1 := []span{sp(when, fresh.Bold(at.Sub(s.Updated) < time.Hour)), sp(" ", sFg)}

	l2 := append([]span{bar, sp("   ", sFg)}, tags(s)...)
	if s.InTx {
		l2 = append(l2, sp("  ", sFg), sp("in transcript", sCyan))
	}
	r2 := []span{sp(msgs(s.Msgs), sDim), sp(" ", sFg)}
	return []string{line(w, bg, l1, r1), line(w, bg, l2, r2)}
}
