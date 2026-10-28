package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

func short(p string) string {
	if h := os.Getenv("HOME"); h != "" && strings.HasPrefix(p, h) {
		return "~" + strings.TrimPrefix(p, h)
	}
	return p
}

func clean(s string) string {
	s = strings.NewReplacer("**", "", "`", "", "__", "").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

// wrap word-wraps to w cells and at most n lines, ending the last one with "…" when text is left over.
func wrap(s string, w, n int) []string {
	if w < 1 || n < 1 {
		return nil
	}
	var out []string
	cur := ""
	for _, word := range strings.Fields(s) {
		for runewidth.StringWidth(word) > w { // a word wider than the line is hard-cut
			if cur != "" {
				out, cur = append(out, cur), ""
			}
			head := runewidth.Truncate(word, w, "")
			if head == "" {
				head = string([]rune(word)[:1])
			}
			out, word = append(out, head), word[len(head):]
		}
		switch {
		case cur == "":
			cur = word
		case runewidth.StringWidth(cur+" "+word) <= w:
			cur += " " + word
		default:
			out, cur = append(out, cur), word
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	if len(out) > n {
		out = out[:n]
		out[n-1] = runewidth.Truncate(out[n-1], w-1, "") + "…"
	}
	return out
}

type exchange struct {
	rule        string
	prompt, rep string
}

type pvBlock struct {
	label string
	st    lipgloss.Style
	text  []string // fully wrapped
	n     int      // lines granted
}

// conversation lays out how the session started and where it was left off in exactly budget rows,
// giving spare rows to the latest exchange first.
func conversation(s *Session, tw, budget int, row func([]span) string, rule func(string, lipgloss.Style) string) []string {
	ex := []exchange{{"conversation", s.LastPrompt, s.LastReply}}
	if s.FirstText != "" && clean(s.FirstText) != clean(s.LastPrompt) {
		ex = []exchange{{"how it started", s.FirstText, s.FirstReply}, {"where you left off", s.LastPrompt, s.LastReply}}
	}
	// A block costs its label row plus its granted lines; n == 0 hides it. The first exchange starts with just its
	// prompt so both exchanges fit a 30-row terminal.
	cost := func(b *pvBlock) int {
		if b.n == 0 {
			return 0
		}
		return 1 + b.n
	}
	build := func(ex []exchange) ([][]*pvBlock, int) {
		var groups [][]*pvBlock
		used := 0
		for ei, e := range ex {
			var g []*pvBlock
			for bi, b := range []struct {
				label, text string
				st          lipgloss.Style
			}{{"you", e.prompt, sCyan}, {"claude", e.rep, sPink}} {
				if t := clean(b.text); t != "" {
					n := 1
					if len(ex) == 2 && ei == 0 && bi == 1 {
						n = 0
					}
					g = append(g, &pvBlock{label: b.label, st: b.st, text: wrap(t, tw-2, 1<<20), n: n})
				}
			}
			used++ // rule
			for _, b := range g {
				used += cost(b)
			}
			groups = append(groups, g)
		}
		return groups, used
	}
	groups, used := build(ex)
	if used > budget && len(ex) == 2 {
		ex = ex[1:]
		groups, used = build(ex)
	}
	spare := budget - used
	// Round-robin, newest first: where you left off (claude, you), then how it started (you, claude).
	var order []*pvBlock
	for gi := len(groups) - 1; gi >= 0; gi-- {
		g := groups[gi]
		if gi == len(groups)-1 {
			for bi := len(g) - 1; bi >= 0; bi-- {
				order = append(order, g[bi])
			}
		} else {
			order = append(order, g...)
		}
	}
	for spare > 0 {
		gave := false
		for _, b := range order {
			need := 1
			if b.n == 0 {
				need = 2 // a hidden block also needs its label row
			}
			if spare >= need && b.n < len(b.text) {
				b.n++
				spare -= need
				gave = true
			}
		}
		if !gave {
			break
		}
	}
	var out []string
	for gi, e := range ex {
		out = append(out, rule(e.rule, sCyan))
		for _, b := range groups[gi] {
			if b.n == 0 {
				continue
			}
			out = append(out, row([]span{sp("▎ ", b.st), sp(b.label, b.st.Bold(true))}))
			ls := b.text[:min(b.n, len(b.text))]
			if b.n < len(b.text) {
				last := ls[len(ls)-1]
				ls = append(ls[:len(ls)-1:len(ls)-1], runewidth.Truncate(last, tw-3, "")+"…")
			}
			for _, t := range ls {
				out = append(out, row([]span{sp("▎ ", b.st), sp(t, sFg)}))
			}
		}
	}
	for len(out) < budget {
		out = append(out, row(nil))
	}
	return out[:max(min(len(out), budget), 0)]
}

// previewLines is the card interior: header at the top, files and the resume command pinned to the bottom,
// the conversation filling every row between; each line is exactly w cells on the card background.
func previewLines(s *Session, w, h int) []string {
	bg := cCardBg
	tw := w - 2
	row := func(left []span) string { return pad(1, bg) + line(tw, bg, left, nil) + pad(1, bg) }
	blank := row(nil)
	rule := func(label string, st lipgloss.Style) string {
		return row([]span{sp(label+" ", st.Bold(true)), sp(strings.Repeat("─", max(tw-len(label)-1, 0)), sRule)})
	}
	var out []string
	if s == nil {
		return out
	}

	title, tst := wrap(clean(s.Title), tw, 2), sBold
	if len(title) == 0 {
		title, tst = []string{"(untitled)"}, sDim
	}
	for len(title) < 2 {
		title = append(title, "")
	}
	for _, t := range title {
		out = append(out, row([]span{sp(t, tst)}))
	}
	out = append(out, row(tags(s)))
	out = append(out, row([]span{sp(short(s.Cwd), sDim)}))
	out = append(out, row([]span{sp(msgs(s.Msgs), sFg), sp(" · ", sDim), sp("started ", sDim), sp(s.Started.Local().Format("Mon Jan 2, 15:04"), sFg)}))
	out = append(out, blank)
	const bottomRows = 6 // blank, touched, blank, resume rule, 2 code lines
	out = append(out, conversation(s, tw, max(h-len(out)-bottomRows, 0), row, rule)...)
	out = append(out, blank)

	touched := []span{sp("touched ", sDim)}
	if len(s.Files) == 0 {
		touched = append(touched, sp("—", sDim))
	}
	for i, f := range s.Files {
		if i == 2 {
			touched = append(touched, sp(fmt.Sprintf(" +%d", len(s.Files)-2), sDim))
			break
		}
		if i > 0 {
			touched = append(touched, sp("  ", sFg))
		}
		touched = append(touched, sp(strings.TrimPrefix(short(f), short(s.Cwd)+"/"), sFg))
	}
	out = append(out, row(touched), blank, rule("resume", sGreen))

	code := func(ss ...span) string {
		return pad(1, bg) + line(tw, cCodeBg, append([]span{sp(" ", sCode)}, ss...), nil) + pad(1, bg)
	}
	out = append(out,
		code(sp("cd", sCodeCd), sp(" "+short(s.Cwd), sCode)),
		code(sp("claude", sCodeCl), sp(" --resume", sCodeArg), sp(" "+s.ID, sCode)))
	return out
}

// card wraps the interior lines in a rounded border with " preview " set into the top edge; exactly w×h cells.
func card(lines []string, w, h int) string {
	inner := w - 2
	label := " preview "
	top := sBorder.Render("╭─") + sDim.Render(label) + sBorder.Render(strings.Repeat("─", max(inner-1-len(label), 0))+"╮")
	bot := sBorder.Render("╰" + strings.Repeat("─", inner) + "╯")
	side := sBorder.Render("│")
	out := []string{top}
	for i := 0; i < h-2; i++ {
		l := pad(inner, cCardBg)
		if i < len(lines) {
			l = lines[i]
		}
		out = append(out, side+l+side)
	}
	return strings.Join(append(out, bot), "\n")
}
