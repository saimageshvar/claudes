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
	maxW := max(tw-4, 18) // nearly full width; the 4 spare cells keep the left/right chat feel
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

type snipCell struct {
	r   rune
	hit bool
}

func cellsWidth(cs []snipCell) int {
	w := 0
	for _, c := range cs {
		w += runewidth.RuneWidth(c.r)
	}
	return w
}

// shrink drops one rune from the outer end of an ellipsised context, keeping the ellipsis.
func shrink(p []rune, fromLeft bool) []rune {
	if len(p) <= 1 {
		return p
	}
	if fromLeft {
		if p[0] == '…' {
			return append(p[:1:1], p[2:]...)
		}
		return append([]rune{'…'}, p[2:]...)
	}
	return append(p[:len(p)-2:len(p)-2], '…')
}

// snippetRows lays one matching line out as at most two rows under a who-said-it tag, hit highlighted. The window
// is trimmed by cell width (before the hit first, then after it) to leave room for the word a wrap carries to row
// two, so the hit always stays on screen.
func snippetRows(sn Snippet, tw int) []string {
	const tagW = 8
	w := max(tw-tagW, 8)
	pre, hit, post := []rune(sn.Pre), []rune(sn.Hit), []rune(sn.Post)
	runesWidth := func(rs []rune) int { return runewidth.StringWidth(string(rs)) }
	for runesWidth(pre)+runesWidth(hit)+runesWidth(post) > w+w*3/4 {
		switch {
		case len(pre) > 1:
			pre = shrink(pre, true)
		case len(post) > 1:
			post = shrink(post, false)
		default:
			goto trimmed
		}
	}
trimmed:
	var cells []snipCell
	for _, part := range []struct {
		rs  []rune
		hit bool
	}{{pre, false}, {hit, true}, {post, false}} {
		for _, r := range part.rs {
			cells = append(cells, snipCell{r, part.hit})
		}
	}

	var first, rest []snipCell
	used := 0
	for i, c := range cells {
		rw := runewidth.RuneWidth(c.r)
		if used+rw > w {
			first, rest = cells[:i], cells[i:]
			for j := len(first) - 1; j > len(first)/2; j-- { // break at the last space when it is not too far back
				if first[j].r == ' ' {
					first, rest = cells[:j], cells[j+1:]
					break
				}
			}
			break
		}
		used += rw
	}
	if rest == nil {
		first = cells
	}
	if cellsWidth(rest) > w { // only reachable with very wide runes
		for len(rest) > 1 && cellsWidth(rest) > w-1 {
			rest = rest[:len(rest)-1]
		}
		rest = append(rest[:len(rest):len(rest)], snipCell{'…', false})
	}

	tag, tagSt := "claude", sPink.Bold(true)
	if sn.User {
		tag, tagSt = "you", sAccent.Bold(true)
	}
	var out []string
	for li, ln := range [][]snipCell{first, rest} {
		if li == 1 && len(ln) == 0 {
			break
		}
		lead := strings.Repeat(" ", tagW)
		if li == 0 {
			lead = tag + strings.Repeat(" ", tagW-len(tag))
		}
		spans := []span{sp(lead, tagSt)}
		for i := 0; i < len(ln); {
			j := i
			var b strings.Builder
			for j < len(ln) && ln[j].hit == ln[i].hit {
				b.WriteRune(ln[j].r)
				j++
			}
			st := sFg
			if ln[i].hit {
				st = sHit
			}
			spans = append(spans, sp(b.String(), st))
			i = j
		}
		out = append(out, cardRow(tw, spans))
	}
	return out
}
