package main

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var now = time.Now

const (
	wideAt  = 110
	listWW  = 62 // list column in wide mode
	gapW    = 2
	bodyTop = 6 // header, blank, 3-line search box, blank
	chrome  = 8 // bodyTop + blank + footer
)

var tabNames = []string{"All", "Today", "This week", "Worktrees"}
var tabCountStyle = []lipgloss.Style{sAccent, sCyan, sAmber, sPink}

type keyMap struct{ Nav, Open, Tab, Preview, Esc, Up, Down, PgUp, PgDown key.Binding }

var keys = keyMap{
	Nav:     key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "navigate")),
	Open:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "resume")),
	Tab:     key.NewBinding(key.WithKeys("tab", "shift+tab"), key.WithHelp("tab", "filter")),
	Preview: key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "preview")),
	Esc:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear / quit")),
	Up:      key.NewBinding(key.WithKeys("up", "ctrl+k")),
	Down:    key.NewBinding(key.WithKeys("down", "ctrl+n", "ctrl+j")),
	PgUp:    key.NewBinding(key.WithKeys("pgup")),
	PgDown:  key.NewBinding(key.WithKeys("pgdown")),
}

type (
	loadedMsg   []Session
	debounceMsg int64
	quitMsg     struct{}
	hitsMsg     struct {
		seq int64
		ids map[string]bool
	}
)

type model struct {
	all         []Session
	loaded      bool
	tab         int
	base        int // sessions in the current tab
	input       textinput.Model
	list        list.Model
	vp          viewport.Model
	sp          spinner.Model
	w, h        int
	showPreview bool
	seq         atomic.Int64
	searching   bool
	hits        map[string]bool
	tabX        [][2]int
	lastClick   time.Time
	lastIdx     int
	chosen      *Session
}

func newModel() *model {
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = "search title, folder, branch, or anything said"
	in.PlaceholderStyle = sDim
	in.TextStyle = sFg
	in.Focus()
	m := &model{input: in, vp: viewport.New(0, 0), showPreview: true}
	l := list.New(nil, delegate{m}, 0, 0)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowFilter(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(false)
	l.SetShowPagination(false)
	l.DisableQuitKeybindings()
	m.list = l
	m.sp = spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(sPink))
	return m
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, func() tea.Msg { return loadedMsg(loadSessions()) })
}

func (m *model) query() string { return strings.TrimSpace(m.input.Value()) }
func (m *model) wide() bool    { return m.w >= wideAt && m.showPreview }
func (m *model) bodyH() int    { return max(m.h-chrome, rowH) }

func (m *model) listW() int {
	if m.wide() {
		return listWW
	}
	return max(m.w-2, 20)
}

func (m *model) cardW() int { return m.w - 2 - listWW - gapW }

func (m *model) layout() {
	m.list.SetSize(m.listW(), max(m.bodyH()/rowH, 1)*rowH)
	m.vp.Width = max(m.cardW()-2, 1)
	m.vp.Height = max(m.bodyH()-2, 1)
	m.refreshPreview()
}

func (m *model) inTab(s *Session, tab int) bool {
	switch tab {
	case 1:
		return dayLabel(s.Updated, now()) == "Today"
	case 2:
		return dayLabel(s.Updated, now()) != "Older"
	case 3:
		return s.Linked != ""
	}
	return true
}

func (m *model) refilter() {
	var base []*Session
	for i := range m.all {
		if m.inTab(&m.all[i], m.tab) {
			base = append(base, &m.all[i])
		}
	}
	m.base = len(base)
	ranked := rank(base, m.query())
	seen := map[*Session]bool{}
	for _, r := range ranked {
		seen[r.s] = true
	}
	for _, s := range base {
		if m.hits[s.ID] && !seen[s] {
			c := *s
			c.InTx = true
			ranked = append(ranked, match{s: &c})
		}
	}
	items := make([]list.Item, len(ranked))
	prev := ""
	for i, r := range ranked {
		it := item{match: r}
		if lbl := dayLabel(r.s.Updated, now()); lbl != prev {
			it.group, prev = lbl, lbl
		}
		items[i] = it
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

func (m *model) refreshPreview() {
	lines := previewLines(m.selected(), m.vp.Width, m.vp.Height)
	for len(lines) < m.vp.Height {
		lines = append(lines, pad(m.vp.Width, cCardBg))
	}
	m.vp.SetContent(strings.Join(lines, "\n"))
	m.vp.GotoTop()
}

func (m *model) queryChanged() tea.Cmd {
	m.hits = nil
	m.refilter()
	seq := m.seq.Add(1)
	m.searching = false
	if len(m.query()) < 2 {
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
	s := m.selected()
	if s == nil || m.chosen != nil {
		return nil
	}
	m.chosen = s
	return tea.Tick(350*time.Millisecond, func(time.Time) tea.Msg { return quitMsg{} })
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.layout()
		return m, nil
	case loadedMsg:
		m.all, m.loaded = msg, true
		m.refilter()
		return m, nil
	case quitMsg:
		return m, tea.Quit
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
		if m.chosen != nil {
			return m, nil
		}
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
		case key.Matches(msg, keys.Preview):
			m.showPreview = !m.showPreview
			m.layout()
			return m, nil
		case key.Matches(msg, keys.Up):
			m.list.CursorUp()
			m.refreshPreview()
			return m, nil
		case key.Matches(msg, keys.Down):
			m.list.CursorDown()
			m.refreshPreview()
			return m, nil
		case key.Matches(msg, keys.PgUp, keys.PgDown):
			if key.Matches(msg, keys.PgUp) {
				m.list.PrevPage()
			} else {
				m.list.NextPage()
			}
			m.refreshPreview()
			return m, nil
		}
	}
	before := m.input.Value()
	var c tea.Cmd
	m.input, c = m.input.Update(msg)
	if m.input.Value() != before {
		return m, tea.Batch(c, m.queryChanged())
	}
	return m, c
}

func (m *model) mouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	inCard := m.wide() && msg.X >= 1+m.listW()+gapW
	switch {
	case msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown:
		if inCard {
			var c tea.Cmd
			m.vp, c = m.vp.Update(msg)
			return m, c
		}
		if msg.Button == tea.MouseButtonWheelUp {
			m.list.CursorUp()
		} else {
			m.list.CursorDown()
		}
		m.refreshPreview()
	case msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft || m.chosen != nil:
	case msg.Y == 0:
		for i, r := range m.tabX {
			if msg.X >= r[0] && msg.X < r[1] {
				m.setTab(i)
			}
		}
	case !inCard && msg.Y >= bodyTop && msg.Y < bodyTop+m.list.Paginator.PerPage*rowH:
		i := m.list.Paginator.Page*m.list.Paginator.PerPage + (msg.Y-bodyTop)/rowH
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

func (m *model) header() string {
	counts := make([]int, len(tabNames))
	for i := range m.all {
		for t := range tabNames {
			if m.inTab(&m.all[i], t) {
				counts[t]++
			}
		}
	}
	var b strings.Builder
	b.WriteString("  " + wordmark("✦ claudes") + " ")
	x := 2 + lipgloss.Width("✦ claudes") + 1
	m.tabX = m.tabX[:0]
	for i, name := range tabNames {
		var t string
		if i == m.tab {
			t = sTabOn.Render(fmt.Sprintf(" %s %d ", name, counts[i]))
		} else {
			t = " " + sDim.Render(name) + " " + tabCountStyle[i].Bold(true).Render(fmt.Sprint(counts[i])) + " "
		}
		b.WriteString(" " + t)
		w := lipgloss.Width(t)
		m.tabX = append(m.tabX, [2]int{x + 1, x + 1 + w})
		x += 1 + w
	}
	return ansi.Truncate(b.String(), m.w-1, "…")
}

func (m *model) searchBox() []string {
	inner := m.w - 4
	bd := sAccent
	count := sAccent.Bold(true).Render(fmt.Sprint(len(m.list.Items()))) + sDim.Render(" / "+fmt.Sprint(m.base))
	if m.searching {
		count = m.sp.View() + " " + count
	}
	room := inner - 2 - 2 - lipgloss.Width(count) - 2
	m.input.Width = max(room-1, 1)
	in := ansi.Truncate(m.input.View(), max(room, 0), "")
	mid := " " + sPink.Bold(true).Render("❯") + " " + in +
		strings.Repeat(" ", max(inner-3-lipgloss.Width(in)-lipgloss.Width(count)-1, 1)) + count + " "
	mid = ansi.Truncate(mid, inner, "")
	mid += strings.Repeat(" ", max(inner-lipgloss.Width(mid), 0))
	return []string{
		" " + bd.Render("╭"+strings.Repeat("─", inner)+"╮"),
		" " + bd.Render("│") + mid + bd.Render("│"),
		" " + bd.Render("╰"+strings.Repeat("─", inner)+"╯"),
	}
}

func (m *model) listColumn() []string {
	w, h := m.listW(), m.bodyH()
	var lines []string
	switch {
	case !m.loaded:
		lines = []string{line(w, nil, []span{sp(" Loading sessions…", sDim)}, nil)}
	case len(m.list.Items()) == 0 && m.query() != "":
		lines = []string{line(w, nil, []span{sp(fmt.Sprintf(" No sessions match %q. Press esc to clear.", m.query()), sDim)}, nil)}
	case len(m.list.Items()) == 0:
		lines = []string{line(w, nil, []span{sp(" No sessions here.", sDim)}, nil)}
	default:
		lines = strings.Split(m.list.View(), "\n")
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], w, "")
		lines[i] += strings.Repeat(" ", max(w-lipgloss.Width(lines[i]), 0))
	}
	for len(lines) < h {
		lines = append(lines, strings.Repeat(" ", w))
	}
	return lines
}

func keycap(b key.Binding, st lipgloss.Style) []span {
	h := b.Help()
	return []span{sp(" "+h.Key+" ", st), sp(" "+h.Desc, sDim)}
}

func (m *model) footer() string {
	w := m.w - 2
	if s := m.chosen; s != nil {
		return " " + line(w, nil, []span{sp("◐ resuming → cd "+short(s.Cwd)+" && claude --resume "+s.ID, sPink)}, nil) + " "
	}
	var left []span
	bs := []key.Binding{keys.Nav, keys.Open, keys.Tab}
	if m.w >= wideAt {
		bs = append(bs, keys.Preview)
	}
	bs = append(bs, keys.Esc)
	for i, b := range bs {
		if i > 0 {
			left = append(left, sp("  ", sFg))
		}
		st := sKey
		if i == 1 {
			st = sKeyHot
		}
		left = append(left, keycap(b, st)...)
	}
	var right []span
	if n := len(m.list.Items()); n > 0 {
		p := m.list.Paginator
		if p.TotalPages > 1 && p.TotalPages <= 8 {
			for i := 0; i < p.TotalPages; i++ {
				if i == p.Page {
					right = append(right, sp("● ", sAccent))
				} else {
					right = append(right, sp("○ ", sDim))
				}
			}
		}
		a := p.Page*p.PerPage + 1
		right = append(right, sp(fmt.Sprintf("%d–%d of %d", a, min(a+p.PerPage-1, n), n), sDim))
	}
	return " " + line(w, nil, left, right) + " "
}

func (m *model) View() string {
	if m.w == 0 || m.h == 0 {
		return ""
	}
	blank := strings.Repeat(" ", m.w)
	out := []string{fit1(m.header(), m.w), blank}
	out = append(out, m.searchBox()...)
	out = append(out, blank)
	left := m.listColumn()
	if m.wide() {
		right := strings.Split(card(strings.Split(m.vp.View(), "\n"), m.cardW(), m.bodyH()), "\n")
		for i := range left {
			out = append(out, " "+left[i]+strings.Repeat(" ", gapW)+right[i]+" ")
		}
	} else {
		for i := range left {
			out = append(out, " "+left[i]+" ")
		}
	}
	out = append(out, blank, m.footer())
	for i := range out {
		out[i] = fit1(out[i], m.w)
	}
	if len(out) > m.h {
		out = out[:m.h]
	}
	return strings.Join(out, "\n")
}

// fit1 makes a rendered line exactly w cells.
func fit1(s string, w int) string {
	if lipgloss.Width(s) > w {
		s = ansi.Truncate(s, w, "")
	}
	return s + strings.Repeat(" ", max(w-lipgloss.Width(s), 0))
}
