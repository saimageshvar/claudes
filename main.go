package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/sahilm/fuzzy"
)

func ac(light, dark string) lipgloss.AdaptiveColor {
	return lipgloss.AdaptiveColor{Light: light, Dark: dark}
}

var (
	cAccent = ac("#c15f3c", "#e8896a")
	cText   = ac("#1f1f1f", "#ececec")
	cMuted  = ac("#6b6b6b", "#9a9a9a")
	cFaint  = ac("#a8a8a8", "#5c5c5c")
	cLine   = ac("#dcdcdc", "#3a3a3a")
	cBlue   = ac("#2f6fb3", "#7ab4ec")
	cViolet = ac("#7a4fb5", "#b79bea")

	sAccent = lipgloss.NewStyle().Foreground(cAccent)
	sBold   = lipgloss.NewStyle().Foreground(cText).Bold(true)
	sMuted  = lipgloss.NewStyle().Foreground(cMuted)
	sFaint  = lipgloss.NewStyle().Foreground(cFaint)
	sHit    = lipgloss.NewStyle().Foreground(cAccent).Bold(true).Underline(true)
	sBranch = lipgloss.NewStyle().Foreground(cViolet)
	sTx     = lipgloss.NewStyle().Foreground(cBlue)
	sPill   = lipgloss.NewStyle().Foreground(cViolet).Padding(0, 1).Background(ac("#efe9f9", "#2c2640"))
	sTabOn  = lipgloss.NewStyle().Foreground(ac("#ffffff", "#1a1a1a")).Background(cAccent).Bold(true).Padding(0, 1)
	sTabOff = lipgloss.NewStyle().Foreground(cMuted).Padding(0, 1)
	sBox    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cLine).Padding(0, 1)
	sCmd    = lipgloss.NewStyle().Foreground(cText).Background(ac("#f0ede6", "#262626")).Padding(0, 1)
)

var tabNames = []string{"All", "Today", "This week", "Worktrees"}

const (
	padX    = 2
	tabY    = 2
	listTop = 7
	rowH    = 3
)

type keyMap struct{ Up, Down, Tab, Open, Esc key.Binding }

func (k keyMap) ShortHelp() []key.Binding  { return []key.Binding{k.Up, k.Tab, k.Open, k.Esc} }
func (k keyMap) FullHelp() [][]key.Binding { return [][]key.Binding{k.ShortHelp()} }

var keys = keyMap{
	Up:   key.NewBinding(key.WithKeys("up", "ctrl+p"), key.WithHelp("↑↓", "navigate")),
	Down: key.NewBinding(key.WithKeys("down", "ctrl+n")),
	Tab:  key.NewBinding(key.WithKeys("tab", "shift+tab"), key.WithHelp("tab", "filter")),
	Open: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "resume")),
	Esc:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear / quit")),
}

type item struct {
	s  *Session
	hl map[int]bool
}

func (item) FilterValue() string { return "" }

type delegate struct{}

func (delegate) Height() int                         { return rowH }
func (delegate) Spacing() int                        { return 0 }
func (delegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (delegate) Render(w io.Writer, m list.Model, i int, it list.Item) {
	x := it.(item)
	s := x.s
	sel := i == m.Index()
	cw := m.Width() - 2

	bar := "  "
	if sel {
		bar = sAccent.Render("▎") + " "
	}

	ago := sFaint.Render(agoStr(s.Updated))
	var tb strings.Builder
	for n, r := range []rune(ansi.Truncate(s.Title, max(cw-lipgloss.Width(ago)-2, 6), "…")) {
		switch {
		case x.hl[n]:
			tb.WriteString(sHit.Render(string(r)))
		case sel:
			tb.WriteString(sBold.Render(string(r)))
		default:
			tb.WriteString(lipgloss.NewStyle().Foreground(cText).Render(string(r)))
		}
	}
	title := tb.String()
	gap := max(cw-lipgloss.Width(title)-lipgloss.Width(ago), 1)

	var right []string
	if s.InTx {
		right = append(right, sTx.Render("in transcript"))
	}
	if s.Worktree != "" {
		right = append(right, sPill.Render("worktree"))
	}
	right = append(right, sFaint.Render(fmt.Sprintf("%d msgs", s.Msgs)))
	rightS := strings.Join(right, "  ")

	left := sMuted.Render(folder(s.Cwd))
	if s.Branch != "" {
		left += sFaint.Render("  ·  ") + sBranch.Render(s.Branch)
	}
	left = ansi.Truncate(left, max(cw-lipgloss.Width(rightS)-3, 8), "…")
	pad := max(cw-lipgloss.Width(left)-lipgloss.Width(rightS), 1)

	fmt.Fprint(w, bar+title+strings.Repeat(" ", gap)+ago+"\n"+
		bar+left+strings.Repeat(" ", pad)+rightS+"\n")
}

func short(p string) string {
	if h := os.Getenv("HOME"); h != "" && strings.HasPrefix(p, h) {
		return "~" + strings.TrimPrefix(p, h)
	}
	return p
}

func folder(p string) string {
	p = short(p)
	parts := strings.Split(p, "/")
	if len(p) > 36 && len(parts) > 3 {
		return "…/" + strings.Join(parts[len(parts)-2:], "/")
	}
	return p
}

func agoStr(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", max(int(d.Minutes()), 1))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 14*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
	return fmt.Sprintf("%dw ago", int(d.Hours()/24/7))
}

type (
	loadedMsg   []Session
	debounceMsg int64
	hitsMsg     struct {
		seq int64
		ids map[string]bool
	}
)

type model struct {
	all       []Session
	tab       int
	input     textinput.Model
	list      list.Model
	vp        viewport.Model
	sp        spinner.Model
	help      help.Model
	w, h      int
	seq       atomic.Int64
	searching bool
	hits      map[string]bool
	tabX      [][2]int
	lastClick time.Time
	lastIdx   int
	chosen    *Session
}

func newModel() *model {
	in := textinput.New()
	in.Prompt = "❯ "
	in.PromptStyle = sAccent
	in.Placeholder = "search sessions — title, folder, branch, or anything said"
	in.PlaceholderStyle = sFaint
	in.Focus()
	l := list.New(nil, delegate{}, 0, 0)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowFilter(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(false)
	l.SetShowPagination(false)
	l.DisableQuitKeybindings()
	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	sp.Style = sAccent
	h := help.New()
	h.Styles.ShortKey = sMuted
	h.Styles.ShortDesc = sFaint
	h.Styles.ShortSeparator = sFaint
	return &model{input: in, list: l, vp: viewport.New(0, 0), sp: sp, help: h}
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, func() tea.Msg { return loadedMsg(loadSessions()) })
}

func (m *model) previewOn() bool { return m.w >= 100 }

func (m *model) leftW() int {
	if m.previewOn() {
		return m.w * 52 / 100
	}
	return m.w
}

func (m *model) layout() {
	lw := m.leftW()
	body := m.h - 2
	m.list.SetSize(lw-padX*2, max(body-listTop+1, rowH))
	m.input.Width = max(lw-padX*2-8, 10)
	m.vp.Width = max(m.w-lw-padX*2-1, 10)
	m.vp.Height = max(body-2, 2)
	m.help.Width = m.w
	m.refreshPreview()
}

func (m *model) inTab(s *Session, tab int) bool {
	switch tab {
	case 1:
		y, mo, d := time.Now().Date()
		return !s.Updated.Before(time.Date(y, mo, d, 0, 0, 0, 0, time.Local))
	case 2:
		return time.Since(s.Updated) < 7*24*time.Hour
	case 3:
		return s.Worktree != ""
	}
	return true
}

type haystack []*Session

func (h haystack) Len() int { return len(h) }
func (h haystack) String(i int) string {
	s := h[i]
	return s.Title + "\n" + s.Cwd + "\n" + s.Branch + "\n" + oneLine(s.LastPrompt, 200)
}

func (m *model) refilter() {
	var base haystack
	for i := range m.all {
		if m.inTab(&m.all[i], m.tab) {
			base = append(base, &m.all[i])
		}
	}
	terms := strings.Fields(strings.ToLower(m.input.Value()))
	var items []list.Item
	if len(terms) == 0 {
		for _, s := range base {
			items = append(items, item{s: s})
		}
	} else {
		score := map[int]int{}
		hl := map[int]map[int]bool{}
		for ti, t := range terms {
			next := map[int]int{}
			for _, r := range fuzzy.FindFrom(t, base) {
				if mi := r.MatchedIndexes; mi[len(mi)-1]-mi[0] > 2*len(t)+4 {
					continue
				}
				if ti > 0 {
					if _, ok := score[r.Index]; !ok {
						continue
					}
				}
				next[r.Index] = score[r.Index] + r.Score
				if hl[r.Index] == nil {
					hl[r.Index] = map[int]bool{}
				}
				for _, mi := range r.MatchedIndexes {
					if mi < len([]rune(base[r.Index].Title)) {
						hl[r.Index][mi] = true
					}
				}
			}
			score = next
		}
		idx := make([]int, 0, len(score))
		for i := range score {
			idx = append(idx, i)
		}
		sort.SliceStable(idx, func(a, b int) bool {
			if score[idx[a]] != score[idx[b]] {
				return score[idx[a]] > score[idx[b]]
			}
			return idx[a] < idx[b]
		})
		seen := map[*Session]bool{}
		for _, i := range idx {
			seen[base[i]] = true
			items = append(items, item{s: base[i], hl: hl[i]})
		}
		for _, s := range base {
			if m.hits[s.ID] && !seen[s] {
				c := *s
				c.InTx = true
				items = append(items, item{s: &c})
			}
		}
	}
	m.list.SetItems(items)
	m.list.ResetSelected()
	m.refreshPreview()
}

func (m *model) selected() *Session {
	if it, ok := m.list.SelectedItem().(item); ok {
		return it.s
	}
	return nil
}

func clip(s string, n int) string {
	s = strings.NewReplacer("**", "", "`", "", "__", "").Replace(s)
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	s = strings.Join(out, "\n")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func (m *model) kv(label, value string, lw int, valStyle lipgloss.Style) string {
	l := lipgloss.NewStyle().Width(lw).Foreground(cFaint).Render(label)
	v := valStyle.Width(m.vp.Width - lw).Render(value)
	return lipgloss.JoinHorizontal(lipgloss.Top, l, v)
}

func (m *model) rule(label string) string {
	t := " " + label + " "
	return sFaint.Render("──" + t + strings.Repeat("─", max(m.vp.Width-lipgloss.Width(t)-2, 0)))
}

func (m *model) exchange(prompt, reply string) string {
	txt := lipgloss.NewStyle().Foreground(cText)
	var b strings.Builder
	if prompt != "" {
		b.WriteString(m.kv("you", clip(prompt, 420), 8, txt) + "\n\n")
	}
	if reply != "" {
		b.WriteString(m.kv("claude", clip(reply, 560), 8, sMuted) + "\n")
	}
	return b.String()
}

func (m *model) refreshPreview() {
	s := m.selected()
	if s == nil {
		m.vp.SetContent("")
		return
	}
	var b strings.Builder
	b.WriteString(sBold.Width(m.vp.Width).Render(s.Title) + "\n\n")
	b.WriteString(m.kv("folder", short(s.Cwd), 10, sMuted) + "\n")
	if s.Branch != "" {
		b.WriteString(m.kv("branch", s.Branch, 10, sBranch) + "\n")
	}
	if s.Worktree != "" {
		b.WriteString(m.kv("worktree", s.Worktree, 10, sBranch) + "\n")
	}
	b.WriteString(m.kv("started", s.Started.Local().Format("Mon Jan 2, 15:04"), 10, sMuted) + "\n")
	b.WriteString(m.kv("activity", fmt.Sprintf("%d msgs · %s", s.Msgs, agoStr(s.Updated)), 10, sMuted) + "\n\n")

	if s.FirstText == s.LastPrompt {
		b.WriteString(m.rule("conversation") + "\n\n")
		b.WriteString(m.exchange(s.FirstText, s.LastReply) + "\n")
	} else {
		b.WriteString(m.rule("how it started") + "\n\n")
		b.WriteString(m.exchange(s.FirstText, s.FirstReply) + "\n")
		b.WriteString(m.rule("where you left off") + "\n\n")
		b.WriteString(m.exchange(s.LastPrompt, s.LastReply) + "\n")
	}
	if len(s.Files) > 0 {
		b.WriteString(m.rule("files touched") + "\n\n")
		for _, f := range s.Files {
			b.WriteString(sMuted.Render("  "+ansi.Truncate(strings.TrimPrefix(short(f), short(s.Cwd)+"/"), m.vp.Width-2, "…")) + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString(m.rule("resume") + "\n\n")
	b.WriteString(sCmd.Width(m.vp.Width).Render("cd "+short(s.Cwd)+"\nclaude --resume "+s.ID) + "\n")
	m.vp.SetContent(b.String())
	m.vp.GotoTop()
}

func (m *model) queryChanged() tea.Cmd {
	m.hits = nil
	m.refilter()
	seq := m.seq.Add(1)
	m.searching = false
	if len(strings.TrimSpace(m.input.Value())) < 2 {
		return nil
	}
	m.searching = true
	return tea.Batch(m.sp.Tick, tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg { return debounceMsg(seq) }))
}

func (m *model) search(seq int64) tea.Cmd {
	terms := []string{strings.Join(strings.Fields(m.input.Value()), " ")}
	all := m.all
	return func() tea.Msg {
		hits := transcriptHits(all, terms, func() bool { return m.seq.Load() != seq })
		ids := map[string]bool{}
		for i := range hits {
			ids[all[i].ID] = true
		}
		return hitsMsg{seq, ids}
	}
}

func (m *model) setTab(t int) {
	m.tab = (t + len(tabNames)) % len(tabNames)
	m.refilter()
}

func (m *model) resume() tea.Cmd {
	if s := m.selected(); s != nil {
		m.chosen = s
		return tea.Quit
	}
	return nil
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.layout()
		return m, nil
	case loadedMsg:
		m.all = msg
		m.refilter()
		return m, nil
	case spinner.TickMsg:
		if !m.searching {
			return m, nil
		}
		var c tea.Cmd
		m.sp, c = m.sp.Update(msg)
		return m, c
	case debounceMsg:
		if int64(msg) != m.seq.Load() {
			return m, nil
		}
		return m, m.search(int64(msg))
	case hitsMsg:
		if msg.seq == m.seq.Load() {
			m.searching = false
			m.hits = msg.ids
			m.refilter()
		}
		return m, nil
	case tea.MouseMsg:
		return m.mouse(msg)
	case tea.KeyMsg:
		switch {
		case msg.String() == "ctrl+c":
			return m, tea.Quit
		case key.Matches(msg, keys.Esc):
			if m.input.Value() != "" {
				m.input.SetValue("")
				return m, m.queryChanged()
			}
			return m, tea.Quit
		case key.Matches(msg, keys.Open):
			return m, m.resume()
		case key.Matches(msg, keys.Tab):
			d := 1
			if msg.String() == "shift+tab" {
				d = -1
			}
			m.setTab(m.tab + d)
			return m, nil
		case key.Matches(msg, keys.Up, keys.Down) || msg.String() == "pgup" || msg.String() == "pgdown":
			var c tea.Cmd
			m.list, c = m.list.Update(msg)
			m.refreshPreview()
			return m, c
		}
		before := m.input.Value()
		var c tea.Cmd
		m.input, c = m.input.Update(msg)
		if m.input.Value() != before {
			return m, tea.Batch(c, m.queryChanged())
		}
		return m, c
	}
	return m, nil
}

func (m *model) mouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.previewOn() && msg.X >= m.leftW() {
		var c tea.Cmd
		m.vp, c = m.vp.Update(msg)
		return m, c
	}
	switch {
	case msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown:
		var c tea.Cmd
		m.list, c = m.list.Update(msg)
		m.refreshPreview()
		return m, c
	case msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft:
		return m, nil
	case msg.Y == tabY:
		for i, r := range m.tabX {
			if msg.X >= r[0]+padX && msg.X < r[1]+padX {
				m.setTab(i)
			}
		}
	case msg.Y >= listTop:
		i := m.list.Paginator.Page*m.list.Paginator.PerPage + (msg.Y-listTop)/rowH
		if i >= len(m.list.Items()) {
			return m, nil
		}
		dbl := i == m.lastIdx && time.Since(m.lastClick) < 400*time.Millisecond
		m.lastIdx, m.lastClick = i, time.Now()
		m.list.Select(i)
		m.refreshPreview()
		if dbl {
			return m, m.resume()
		}
	}
	return m, nil
}

func (m *model) View() string {
	if m.w == 0 {
		return ""
	}
	lw := m.leftW()
	cw := lw - padX*2
	n := len(m.list.Items())

	pos := fmt.Sprintf("%d sessions", n)
	if n > 0 {
		pos = fmt.Sprintf("%d / %d", m.list.Index()+1, n)
	}
	brand := sAccent.Bold(true).Render("claudes")
	count := sFaint.Render(pos)
	header := brand + strings.Repeat(" ", max(cw-lipgloss.Width(brand)-lipgloss.Width(count), 1)) + count

	var tabs []string
	m.tabX = m.tabX[:0]
	x := 0
	for i, t := range tabNames {
		c := 0
		for j := range m.all {
			if m.inTab(&m.all[j], i) {
				c++
			}
		}
		st := sTabOff
		if i == m.tab {
			st = sTabOn
		}
		s := st.Render(fmt.Sprintf("%s %d", t, c))
		m.tabX = append(m.tabX, [2]int{x, x + lipgloss.Width(s) + 1})
		x += lipgloss.Width(s) + 1
		tabs = append(tabs, s)
	}

	searchInner := m.input.View()
	if m.searching {
		searchInner += " " + m.sp.View()
	}
	box := sBox.Width(cw - 2).Render(searchInner)

	body := m.list.View()
	if n == 0 && m.all != nil {
		body = sMuted.Render("  No sessions match “"+m.input.Value()+"”.") + "\n" + sFaint.Render("  esc to clear")
	}
	left := strings.Join([]string{"", header, strings.Join(tabs, " "), box, "", body}, "\n")
	left = lipgloss.NewStyle().PaddingLeft(padX).Width(lw).Height(m.h - 2).MaxHeight(m.h - 2).Render(left)

	out := left
	if m.previewOn() {
		right := "\n" + sFaint.Render("PREVIEW") + "\n\n" + m.vp.View()
		right = lipgloss.NewStyle().Width(m.w-lw).Height(m.h-2).MaxHeight(m.h-2).
			Border(lipgloss.NormalBorder(), false, false, false, true).BorderForeground(cLine).
			PaddingLeft(padX).PaddingRight(padX).Render(right)
		out = lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	}
	return out + "\n" + lipgloss.NewStyle().PaddingLeft(padX).Render(m.help.ShortHelpView(keys.ShortHelp()))
}

func main() {
	m := newModel()
	if _, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if m.chosen == nil {
		return
	}
	bin, err := exec.LookPath("claude")
	if err == nil {
		err = os.Chdir(m.chosen.Cwd)
	}
	if err == nil {
		err = syscall.Exec(bin, append([]string{"claude", "--resume", m.chosen.ID}, os.Args[1:]...), os.Environ())
	}
	fmt.Fprintln(os.Stderr, "claudes:", err)
	os.Exit(1)
}
