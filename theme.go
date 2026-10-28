package main

import (
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

func ac(light, dark string) lipgloss.AdaptiveColor {
	return lipgloss.AdaptiveColor{Light: light, Dark: dark}
}

var (
	cFg     = ac("#1B1530", "#F4F0FF")
	cDim    = ac("#6B6580", "#A59FBF")
	cAccent = ac("#7C3AED", "#A78BFA")
	cPink   = ac("#DB2777", "#F472B6")
	cCyan   = ac("#0E7490", "#22D3EE")
	cGreen  = ac("#15803D", "#4ADE80")
	cAmber  = ac("#B45309", "#FBBF24")
	cRule   = ac("#CDBFEA", "#3A3160")
	cBorder = ac("#B9A3E8", "#5B4A9A")
	cSelBg  = ac("#EFE3FF", "#2E2057")
	cCardBg = ac("#F6F1FF", "#1B1630")
	cKeyBg  = ac("#E3D9F5", "#34295A")
	cCodeBg = ac("#1E1B3A", "#0B0914")
	white   = lipgloss.Color("#FFFFFF")

	sFg     = lipgloss.NewStyle().Foreground(cFg)
	sBold   = sFg.Bold(true)
	sDim    = lipgloss.NewStyle().Foreground(cDim)
	sAccent = lipgloss.NewStyle().Foreground(cAccent)
	sPink   = lipgloss.NewStyle().Foreground(cPink)
	sBranch = sPink.Bold(true)
	sCyan   = lipgloss.NewStyle().Foreground(cCyan)
	sGreen  = lipgloss.NewStyle().Foreground(cGreen)
	sAmber  = lipgloss.NewStyle().Foreground(cAmber)
	sRule   = lipgloss.NewStyle().Foreground(cRule)
	sBorder = lipgloss.NewStyle().Foreground(cBorder)
	sHit    = lipgloss.NewStyle().Background(lipgloss.Color("#FDE047")).Foreground(lipgloss.Color("#422006")).Bold(true)
	sTabOn  = lipgloss.NewStyle().Background(cAccent).Foreground(white).Bold(true)
	sKey    = lipgloss.NewStyle().Background(cKeyBg).Foreground(cFg).Bold(true)
	sKeyHot = lipgloss.NewStyle().Background(cPink).Foreground(white).Bold(true)
	sWt     = lipgloss.NewStyle().Background(lipgloss.Color("#FBBF24")).Foreground(lipgloss.Color("#451A03")).Bold(true)
	sCode   = lipgloss.NewStyle().Background(cCodeBg).Foreground(lipgloss.Color("#E9E4FF"))
	// The code block is dark in both themes, so its syntax colours use the bright (dark-theme) variants.
	sCodeCd  = sCode.Foreground(lipgloss.Color("#4ADE80")).Bold(true)
	sCodeCl  = sCode.Foreground(lipgloss.Color("#F472B6")).Bold(true)
	sCodeArg = sCode.Foreground(lipgloss.Color("#22D3EE"))
)

var chipFills = [][2]string{
	{"#7C3AED", "#FFFFFF"}, // violet
	{"#22D3EE", "#083344"}, // cyan
	{"#DB2777", "#FFFFFF"}, // pink
	{"#FBBF24", "#451A03"}, // amber
	{"#4ADE80", "#052E16"}, // green
}

func chipStyle(name string) lipgloss.Style {
	h := fnv.New32a()
	h.Write([]byte(name))
	c := chipFills[h.Sum32()%uint32(len(chipFills))]
	return lipgloss.NewStyle().Background(lipgloss.Color(c[0])).Foreground(lipgloss.Color(c[1])).Bold(true)
}

func init() {
	// Box-drawing and symbol glyphs are 1 cell in every terminal we target; keep runewidth in step with lipgloss.
	runewidth.DefaultCondition.EastAsianWidth = false
}

// span is a run of text in one style; line() lays spans into an exact number of cells.
type span struct {
	t  string
	st lipgloss.Style
}

func sp(t string, st lipgloss.Style) span { return span{t, st} }

func onBg(st lipgloss.Style, bg lipgloss.TerminalColor) lipgloss.Style {
	if bg == nil {
		return st
	}
	if _, unset := st.GetBackground().(lipgloss.NoColor); unset {
		return st.Background(bg)
	}
	return st
}

func spansWidth(ss []span) int {
	w := 0
	for _, s := range ss {
		w += runewidth.StringWidth(s.t)
	}
	return w
}

// fit renders spans into at most w cells, cutting the last visible span with "…".
func fit(ss []span, w int, bg lipgloss.TerminalColor) (string, int) {
	var b strings.Builder
	used := 0
	for _, s := range ss {
		sw := runewidth.StringWidth(s.t)
		if used+sw > w {
			room := w - used
			if room <= 0 {
				break
			}
			t := runewidth.Truncate(s.t, room, "…")
			b.WriteString(onBg(s.st, bg).Render(t))
			used += runewidth.StringWidth(t)
			break
		}
		if s.t != "" {
			b.WriteString(onBg(s.st, bg).Render(s.t))
		}
		used += sw
	}
	return b.String(), used
}

func pad(n int, bg lipgloss.TerminalColor) string {
	if n <= 0 {
		return ""
	}
	if bg == nil {
		return strings.Repeat(" ", n)
	}
	return lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", n))
}

// line is exactly w cells: left spans (truncated to leave room for right), filler, right spans.
func line(w int, bg lipgloss.TerminalColor, left, right []span) string {
	rw := spansWidth(right)
	if rw > w {
		rw = 0
		right = nil
	}
	lw := w - rw
	if rw > 0 {
		lw--
	}
	l, used := fit(left, max(lw, 0), bg)
	r, rused := fit(right, rw, bg)
	return l + pad(w-used-rused, bg) + r
}

func hexRGB(h string) [3]float64 {
	v, _ := strconv.ParseUint(strings.TrimPrefix(h, "#"), 16, 32)
	return [3]float64{float64(v >> 16 & 0xff), float64(v >> 8 & 0xff), float64(v & 0xff)}
}

func lerp3(stops []string, t float64) string {
	seg := t * float64(len(stops)-1)
	i := min(int(seg), len(stops)-2)
	f := seg - float64(i)
	a, b := hexRGB(stops[i]), hexRGB(stops[i+1])
	return fmt.Sprintf("#%02X%02X%02X", int(a[0]+(b[0]-a[0])*f+.5), int(a[1]+(b[1]-a[1])*f+.5), int(a[2]+(b[2]-a[2])*f+.5))
}

func wordmark(s string) string {
	light := []string{"#DB2777", "#7C3AED", "#0E7490"}
	dark := []string{"#F472B6", "#A78BFA", "#22D3EE"}
	rs := []rune(s)
	var b strings.Builder
	for i, r := range rs {
		t := 0.0
		if len(rs) > 1 {
			t = float64(i) / float64(len(rs)-1)
		}
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(ac(lerp3(light, t), lerp3(dark, t))).Render(string(r)))
	}
	return b.String()
}
