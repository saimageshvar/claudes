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
	maxPaneMsgs = 400 // most messages either preview pane keeps
	wideAt      = 110
	listWW      = 62 // list column in wide mode
	gapW        = 2
	bodyTop     = 2 // tabs and key hints, then the search line with its page position
	chrome      = 2
)

var tabNames = []string{"All", "Today", "This week", "Worktrees"}
var tabCountStyle = []lipgloss.Style{sAccent, sCyan, sAmber, sPink}

type keyMap struct{ Nav, Open, Tab, Preview, Esc, Up, Down, PgUp, PgDown, ChatUp, ChatDown key.Binding }

var keys = keyMap{
	Nav:      key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "navigate")),
	Open:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "resume")),
	Tab:      key.NewBinding(key.WithKeys("tab", "shift+tab"), key.WithHelp("tab", "filter")),
	Preview:  key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "preview")),
	Esc:      key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear / quit")),
	Up:       key.NewBinding(key.WithKeys("up", "ctrl+k")),
	Down:     key.NewBinding(key.WithKeys("down", "ctrl+n", "ctrl+j")),
	PgUp:     key.NewBinding(key.WithKeys("pgup")),
	PgDown:   key.NewBinding(key.WithKeys("pgdown")),
	ChatUp:   key.NewBinding(key.WithKeys("shift+up")),
	ChatDown: key.NewBinding(key.WithKeys("shift+down")),
}

type (
	loadedMsg   []Session
	debounceMsg int64
	quitMsg     struct{}
	chatDueMsg  string // selection settled on this session; load its full chat
	chatMsg     struct {
		key  string
		msgs []Msg
	}
	hitsMsg struct {
		seq int64
		ids map[string][]Snippet
	}
)

type model struct {
	all         []Session
	loaded      bool
	tab         int
	base        int // sessions in the current tab
	input       textinput.Model
	list        list.Model
	head, tail  viewport.Model // how it started (scrolls down to newer) and the latest messages (scrolls up to older)
	pvTop       []string
	match       viewport.Model // the matches band: transcript lines hitting the query, scrolls when they don't all fit
	matchOn     bool
	matchLines  int
	matchKey    string // session and hit count the matches pane was last built for, so a rebuild keeps its scroll
	headOn      bool
	tailOn      bool
	chats       map[string][]Msg // full transcripts of sessions viewed, by chatKey
	chatWanted  map[string]bool  // loads already scheduled or running
	sp          spinner.Model
	w, h        int
	showPreview bool
	seq         atomic.Int64
	searching   bool
	hits        map[string][]Snippet // transcript lines matching the query, by session id
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
	m := &model{input: in, head: viewport.New(0, 0), tail: viewport.New(0, 0), match: viewport.New(0, 0), showPreview: true,
		chats: map[string][]Msg{}, chatWanted: map[string]bool{}}
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
		if len(m.hits[s.ID]) > 0 && !seen[s] {
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

func (m *model) pvW() int { return max(m.cardW()-2, 1) }

// refreshPreview rebuilds the card for the selected session and splits the rows between the matches band (when the
// query hit its transcript) and the head and tail chat panes.
func (m *model) refreshPreview() {
	prevMatchY := m.match.YOffset
	m.pvTop, m.matchOn, m.headOn, m.tailOn = nil, false, false, false
	m.head.Height, m.tail.Height, m.match.Height = 0, 0, 0
	s := m.selected()
	if s == nil {
		return
	}
	w, h := m.pvW(), max(m.bodyH()-2, 1)
	tw := w - 2
	m.head.Width, m.tail.Width = w, w
	m.pvTop = previewTop(s, tw)
	headMsgs, tailMsgs := s.Head, s.Tail
	if all, ok := m.chats[chatKey(s)]; ok {
		headMsgs, tailMsgs = all[:min(len(all), maxPaneMsgs)], nil
		if len(all) > shortChat {
			tailMsgs = all[max(len(all)-maxPaneMsgs, 0):]
		}
	}
	head, tail := chat(headMsgs, tw), chat(tailMsgs, tw)
	m.headOn, m.tailOn = len(head) > 0, len(tail) > 0

	rows := h - len(m.pvTop)
	for _, on := range []bool{m.headOn, m.tailOn} {
		if on {
			rows-- // its rule
		}
	}
	rows = max(rows, 0)
	if snips := m.hits[s.ID]; len(snips) > 0 && rows >= 4 {
		var all []string
		for _, sn := range snips {
			all = append(all, snippetRows(sn, tw)...)
		}
		m.match.Width, m.match.Height = w, min(len(all), max(rows*2/5-1, 2))
		m.match.SetContent(strings.Join(all, "\n"))
		if key := fmt.Sprintf("%s|%d", s.ID, len(snips)); key == m.matchKey {
			m.match.SetYOffset(prevMatchY)
		} else {
			m.match.GotoTop()
			m.matchKey = key
		}
		m.matchOn, m.matchLines = true, len(snips)
		rows -= 1 + m.match.Height
	}
	switch {
	case m.headOn && m.tailOn:
		m.head.Height = rows * 2 / 5
		if rows >= 6 {
			m.head.Height = max(m.head.Height, 3)
		}
		m.tail.Height = rows - m.head.Height
	case m.headOn:
		m.head.Height = rows
	case m.tailOn:
		m.tail.Height = rows
	}
	blank := cardRow(tw, nil)
	fill := func(rows []string, n int, top bool) string {
		var gap []string
		for i := len(rows); i < n; i++ {
			gap = append(gap, blank)
		}
		if top {
			return strings.Join(append(gap, rows...), "\n")
		}
		return strings.Join(append(rows, gap...), "\n")
	}
	m.head.SetContent(fill(head, m.head.Height, false))
	m.tail.SetContent(fill(tail, m.tail.Height, true))
	m.head.GotoTop()
	m.tail.GotoBottom()
}

// cardLines is the card interior: the header, then the matches band, how-it-started and latest panes, each under its
// rule, padded to the card's height.
func (m *model) cardLines() []string {
	tw := m.pvW() - 2
	lines := append([]string{}, m.pvTop...)
	if m.matchOn {
		hint := fmt.Sprintf("%d lines", m.matchLines)
		if m.matchLines == 1 {
			hint = "1 line"
		}
		switch {
		case !m.match.AtBottom():
			hint += " ↓"
		case !m.match.AtTop():
			hint = "↑ " + hint
		}
		lines = append(lines, cardRule(tw, "matches", sAmber, hint))
		lines = append(lines, strings.Split(m.match.View(), "\n")...)
	}
	if m.headOn && m.head.Height > 0 {
		label, hint := "conversation", ""
		if m.tailOn {
			label = "how it started"
		}
		if !m.head.AtBottom() {
			hint = "↓ newer"
		}
		lines = append(lines, cardRule(tw, label, sCyan, hint))
		lines = append(lines, strings.Split(m.head.View(), "\n")...)
	}
	if m.tailOn && m.tail.Height > 0 {
		hint := ""
		if !m.tail.AtTop() {
			hint = "↑ older"
		}
		lines = append(lines, cardRule(tw, "latest", sPink, hint))
		lines = append(lines, strings.Split(m.tail.View(), "\n")...)
	}
	if len(m.pvTop) > 0 && !m.headOn && !m.tailOn {
		lines = append(lines, cardRow(tw, []span{sp("No chat text captured for this session.", sDim)}))
	}
	blank := cardRow(tw, nil)
	for len(lines) < m.bodyH()-2 {
		lines = append(lines, blank)
	}
	return lines
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
	query := m.input.Value()
	all := m.all
	return func() tea.Msg {
		hits := transcriptHits(all, query, func() bool { return m.seq.Load() != seq })
		ids := map[string][]Snippet{}
		for i, sn := range hits {
			ids[all[i].ID] = sn
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

func chatKey(s *Session) string { return fmt.Sprintf("%s|%d", s.ID, s.Updated.UnixNano()) }

// wantChat schedules the full transcript of the selected session once the selection has rested on it.
func (m *model) wantChat() tea.Cmd {
	s := m.selected()
	if s == nil || m.w == 0 || !m.wide() {
		return nil
	}
	key := chatKey(s)
	if _, ok := m.chats[key]; ok || m.chatWanted[key] {
		return nil
	}
	m.chatWanted[key] = true
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return chatDueMsg(key) })
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	mm, c := m.update(msg)
	return mm, tea.Batch(c, m.wantChat())
}

func (m *model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case chatDueMsg:
		key := string(msg)
		s := m.selected()
		if s == nil || chatKey(s) != key {
			delete(m.chatWanted, key)
			return m, nil
		}
		path := s.Path
		return m, func() tea.Msg { return chatMsg{key, loadChat(path)} }
	case chatMsg:
		m.chats[msg.key], m.chatWanted[msg.key] = msg.msgs, false
		if s := m.selected(); s != nil && chatKey(s) == msg.key {
			m.refreshPreview()
		}
		return m, nil
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
			keep := ""
			if s := m.selected(); s != nil {
				keep = s.ID
			}
			m.refilter()
			m.selectID(keep) // the new rows must not yank the selection back to the top
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

// paneAt is the scrollable pane under card row r: matches, then how-it-started, else latest.
// selectID moves the selection to the session with that id, if it is still listed.
func (m *model) selectID(id string) {
	for i, it := range m.list.Items() {
		if it.(item).s.ID == id {
			m.list.Select(i)
			m.refreshPreview()
			return
		}
	}
}

func (m *model) paneAt(r int) *viewport.Model {
	r -= len(m.pvTop)
	if m.matchOn && r >= 0 {
		if r < 1+m.match.Height {
			return &m.match
		}
		r -= 1 + m.match.Height
	}
	if m.headOn && r < 1+m.head.Height { // the card header counts as the first pane
		return &m.head
	}
	return &m.tail
}

func (m *model) mouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	inCard := m.wide() && msg.X >= 1+m.listW()+gapW
	switch {
	case msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown:
		if inCard {
			var c tea.Cmd
			pane := m.paneAt(msg.Y - bodyTop - 1)
			*pane, c = pane.Update(msg)
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

// searchLine is the one-row search field with the page position on its right; while resuming it names the command.
func (m *model) searchLine() string {
	w := m.w - 2
	if c := m.chosen; c != nil {
		return " " + line(w, nil, []span{sp("◐ resuming → cd "+short(c.Cwd)+" && claude --resume "+c.ID, sPink)}, nil) + " "
	}
	right, rw := fit(m.pages(), w/2, nil)
	lead := sPink.Bold(true).Render("❯") + " "
	if m.searching {
		lead += m.sp.View() + " "
	}
	room := max(w-lipgloss.Width(lead)-rw-2, 1)
	m.input.Width = max(room-1, 1)
	in := ansi.Truncate(m.input.View(), room, "")
	return " " + lead + in + strings.Repeat(" ", max(w-lipgloss.Width(lead)-lipgloss.Width(in)-rw, 1)) + right + " "
}

// pages is the page dots and the "1–12 of 22" range of the list.
func (m *model) pages() []span {
	var out []span
	n := len(m.list.Items())
	if n == 0 {
		return out
	}
	p := m.list.Paginator
	if p.TotalPages > 1 && p.TotalPages <= 8 {
		for i := 0; i < p.TotalPages; i++ {
			if i == p.Page {
				out = append(out, sp("● ", sAccent))
			} else {
				out = append(out, sp("○ ", sDim))
			}
		}
	}
	a := p.Page*p.PerPage + 1
	return append(out, sp(fmt.Sprintf("%d–%d of %d", a, min(a+p.PerPage-1, n), n), sDim))
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

// hints are the key caps shown at the right of the top row when there is room.
func (m *model) hints(caps, few bool) []span {
	bs := []key.Binding{keys.Nav, keys.Open, keys.Tab}
	if m.w >= wideAt {
		bs = append(bs, keys.Preview)
	}
	bs = append(bs, keys.Esc)
	if few {
		bs = []key.Binding{keys.Open, keys.Esc}
	}
	var out []span
	for i, b := range bs {
		if i > 0 {
			out = append(out, sp("  ", sFg))
		}
		if !caps {
			h := b.Help()
			out = append(out, sp(h.Key, sAccent.Bold(true)), sp(" "+h.Desc, sDim))
			continue
		}
		st := sKey
		if i == 1 {
			st = sKeyHot
		}
		out = append(out, keycap(b, st)...)
	}
	return out
}

// topRow is the tabs with the key hints right-aligned: key caps when they fit, then plain hints, then just enter and esc, else none.
func (m *model) topRow() string {
	tabs := m.header()
	for _, v := range [][2]bool{{true, false}, {false, false}, {false, true}} {
		hints, hw := fit(m.hints(v[0], v[1]), m.w, nil)
		if gap := m.w - 1 - lipgloss.Width(tabs) - hw - 1; gap >= 2 {
			return tabs + strings.Repeat(" ", gap) + hints
		}
	}
	return tabs
}

func (m *model) View() string {
	if m.w == 0 || m.h == 0 {
		return ""
	}
	out := []string{m.topRow(), m.searchLine()}
	left := m.listColumn()
	if m.wide() {
		right := strings.Split(card(m.cardLines(), m.cardW(), m.bodyH()), "\n")
		for i := range left {
			out = append(out, " "+left[i]+strings.Repeat(" ", gapW)+right[i]+" ")
		}
	} else {
		for i := range left {
			out = append(out, " "+left[i]+" ")
		}
	}
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
