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

// previewLines is the card interior, each line exactly w cells with the card background on every cell.
func previewLines(s *Session, w int) []string {
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
	out = append(out, blank, rule("conversation", sCyan))

	block := func(label string, st lipgloss.Style, text string, n int) {
		out = append(out, row([]span{sp("▎ ", st), sp(label, st.Bold(true))}))
		ls := wrap(clean(text), tw-2, n)
		for i := 0; i < n; i++ {
			t := ""
			if i < len(ls) {
				t = ls[i]
			}
			out = append(out, row([]span{sp("▎ ", st), sp(t, sFg)}))
		}
	}
	block("you", sCyan, s.LastPrompt, 2)
	block("claude", sPink, s.LastReply, 3)
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

	code := func(ss ...span) string { return pad(1, bg) + line(tw, cCodeBg, append([]span{sp(" ", sCode)}, ss...), nil) + pad(1, bg) }
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
