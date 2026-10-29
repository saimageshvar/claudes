package main

import (
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

// cardRow is one card interior row: a cell of padding either side of tw cells of content, on the card background.
func cardRow(tw int, left []span) string {
	return pad(1, cCardBg) + line(tw, cCardBg, left, nil) + pad(1, cCardBg)
}

// cardRule is a section band: a filled label pill on a tinted full-width bar, so it cannot be mistaken for a
// message border; hint, when set, sits at the bar's right end ("↓ newer").
func cardRule(tw int, label string, st lipgloss.Style, hint string) string {
	pill := lipgloss.NewStyle().Background(st.GetForeground()).Foreground(white).Bold(true)
	var right []span
	if hint != "" {
		right = []span{sp(hint+" ", sDim)}
	}
	return line(tw+2, cKeyBg, []span{sp(" ", sDim), sp(" "+label+" ", pill)}, right)
}

// bubble draws one message as a rounded box: yours on the right, Claude's on the left, sized to its text.
func bubble(m Msg, tw int) []string {
	maxW := max(tw*72/100, 18)
	lines := wrap(clean(m.Text), maxW-4, 9)
	if len(lines) == 0 {
		return nil
	}
	who, whoSt, border, fill := "claude", sPink, sBorder, lipgloss.TerminalColor(nil)
	if m.User {
		who, whoSt, border, fill = "you", sAccent, sAccent, cSelBg
	}
	label := " " + who
	if !m.At.IsZero() {
		label += " · " + m.At.Local().Format("Jan 2 3:04 PM")
	}
	label += " "
	inner := runewidth.StringWidth(label) - 2
	for _, l := range lines {
		inner = max(inner, runewidth.StringWidth(l))
	}
	bw := inner + 4
	st := func(x lipgloss.Style) lipgloss.Style { return onBg(x, fill) }
	lead := ""
	if m.User {
		lead = strings.Repeat(" ", max(tw-bw, 0))
	}
	row := func(ss ...span) string { return cardRow(tw, append([]span{sp(lead, sFg)}, ss...)) }

	out := []string{row(
		sp("╭", st(border)), sp(label, st(whoSt.Bold(true))),
		sp(strings.Repeat("─", bw-2-runewidth.StringWidth(label))+"╮", st(border)))}
	for _, l := range lines {
		out = append(out, row(
			sp("│ ", st(border)), sp(l+strings.Repeat(" ", inner-runewidth.StringWidth(l)), st(sFg)), sp(" │", st(border))))
	}
	return append(out, row(sp("╰"+strings.Repeat("─", bw-2)+"╯", st(border))))
}

// chat is the bubbles of msgs one blank row apart, oldest first.
func chat(msgs []Msg, tw int) []string {
	var out []string
	for _, m := range msgs {
		if b := bubble(m, tw); len(b) > 0 {
			if len(out) > 0 {
				out = append(out, cardRow(tw, nil))
			}
			out = append(out, b...)
		}
	}
	return out
}

// previewTop is the card header: title (two rows), tags, folder and counts.
func previewTop(s *Session, tw int) []string {
	title, tst := wrap(clean(s.Title), tw, 2), sBold
	if len(title) == 0 {
		title, tst = []string{"(untitled)"}, sDim
	}
	for len(title) < 2 {
		title = append(title, "")
	}
	var out []string
	for _, t := range title {
		out = append(out, cardRow(tw, []span{sp(t, tst)}))
	}
	return append(out,
		cardRow(tw, tags(s)),
		cardRow(tw, []span{sp(short(s.Cwd), sDim)}),
		cardRow(tw, []span{sp(msgs(s.Msgs), sFg), sp(" · ", sDim), sp("started ", sDim), sp(s.Started.Local().Format("Mon Jan 2, 3:04 PM"), sFg)}))
}

// previewBottom pins the resume command to the card's foot.
func previewBottom(s *Session, tw int) []string {
	code := func(ss ...span) string {
		return pad(1, cCardBg) + line(tw, cCodeBg, append([]span{sp(" ", sCode)}, ss...), nil) + pad(1, cCardBg)
	}
	return []string{
		cardRule(tw, "resume", sGreen, ""),
		code(sp("cd", sCodeCd), sp(" "+short(s.Cwd), sCode)),
		code(sp("claude", sCodeCl), sp(" --resume", sCodeArg), sp(" "+s.ID, sCode)),
	}
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
