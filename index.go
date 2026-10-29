package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	headBytes   = 256 << 10
	tailBytes   = 1 << 20
	maxLineJSON = 400 << 10
	maxBackScan = 16 << 20
)

// readWindow returns the whole lines inside [start, end) of the file.
func readWindow(path string, start, end int64) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	b := make([]byte, end-start)
	if _, err := f.ReadAt(b, start); err != nil && err != io.EOF {
		return nil
	}
	b = b[bytes.IndexByte(b, '\n')+1:]
	if i := bytes.LastIndexByte(b, '\n'); i >= 0 {
		b = b[:i+1]
	}
	return b
}

type Session struct {
	ID         string
	Path       string
	Cwd        string
	Branch     string
	Title      string
	FirstText  string
	LastPrompt string
	Head       []Msg
	Tail       []Msg
	Started    time.Time
	Updated    time.Time
	Msgs       int
	InTx       bool `json:"-"`
	gitInfo    `json:"-"`
}

// gitInfo is the per-cwd git view, cached in the index so launch stays instant.
type gitInfo struct {
	Repo    string // main checkout's folder name; the cwd's own name outside git
	Linked  string // linked worktree name; "" for the main checkout
	Primary bool   // main checkout of a repo that has linked worktrees
	NoGit   bool   // the folder exists and is not a git repo
	Checked int64
}

// Msg is one chat turn kept for the preview: your prompt, or Claude's consecutive text blocks merged.
type Msg struct {
	User bool
	Text string
	At   time.Time
}

type cacheEntry struct {
	Mtime int64
	Size  int64
	S     Session
}

type record struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Cwd       string `json:"cwd"`
	GitBranch string `json:"gitBranch"`
	Sidechain bool   `json:"isSidechain"`
	Origin    *struct {
		Kind string `json:"kind"`
	} `json:"origin"`
	Message struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	CustomTitle string `json:"customTitle"`
	AITitle     string `json:"aiTitle"`
	Summary     string `json:"summary"`
}

type block struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func blocks(raw json.RawMessage) []block {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return []block{{Type: "text", Text: s}}
	}
	var bs []block
	json.Unmarshal(raw, &bs)
	return bs
}

func isHumanText(t string) bool {
	t = strings.TrimSpace(t)
	for _, p := range []string{"<", "[Request interrupted", "Caveat:", "Base directory for this skill:"} {
		if strings.HasPrefix(t, p) {
			return false
		}
	}
	// Slash-command and skill echoes ("`/design` was invoked ...") make poor titles and previews.
	return t != "" && !(strings.HasPrefix(t, "`/") && strings.Contains(firstLine(t), "invoked"))
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return s
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func readChunks(path string, size int64) (head, tail []byte, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	if size <= headBytes+tailBytes {
		head, err = io.ReadAll(f)
		return head, nil, err
	}
	head = make([]byte, headBytes)
	if _, err = f.ReadAt(head, 0); err != nil {
		return
	}
	head = head[:bytes.LastIndexByte(head, '\n')+1]
	tail = make([]byte, tailBytes)
	if _, err = f.ReadAt(tail, size-tailBytes); err != nil {
		return
	}
	tail = tail[bytes.IndexByte(tail, '\n')+1:]
	return head, tail, nil
}

func parseSession(path string, info os.FileInfo) *Session {
	head, tail, err := readChunks(path, info.Size())
	if err != nil {
		return nil
	}
	s := &Session{ID: strings.TrimSuffix(filepath.Base(path), ".jsonl"), Path: path, Updated: info.ModTime()}
	var customTitle, aiTitle, summary string
	collect := func(chunk []byte, into *[]Msg) {
		for _, line := range bytes.Split(chunk, []byte{'\n'}) {
			switch {
			case bytes.HasPrefix(line, []byte(`{"type":"custom-title"`)):
				var r record
				if json.Unmarshal(line, &r) == nil && r.CustomTitle != "" {
					customTitle = r.CustomTitle
				}
			case bytes.HasPrefix(line, []byte(`{"type":"ai-title"`)):
				var r record
				if json.Unmarshal(line, &r) == nil && r.AITitle != "" {
					aiTitle = r.AITitle
				}
			case bytes.HasPrefix(line, []byte(`{"type":"summary"`)):
				var r record
				if json.Unmarshal(line, &r) == nil && r.Summary != "" {
					summary = r.Summary
				}
			case bytes.HasPrefix(line, []byte(`{"parentUuid"`)) && len(line) < maxLineJSON:
				var r record
				if json.Unmarshal(line, &r) != nil || r.Sidechain {
					continue
				}
				if r.Cwd != "" && s.Cwd == "" {
					s.Cwd = r.Cwd
				}
				if r.GitBranch != "" {
					s.Branch = r.GitBranch
				}
				at, err := time.Parse(time.RFC3339, r.Timestamp)
				if err == nil && s.Started.IsZero() {
					s.Started = at
				}
				switch r.Type {
				case "user":
					if r.Origin != nil && r.Origin.Kind != "human" {
						continue
					}
					for _, b := range blocks(r.Message.Content) {
						if b.Type == "text" && isHumanText(b.Text) {
							if s.FirstText == "" {
								s.FirstText = b.Text
							}
							s.LastPrompt = b.Text
							addUser(into, b.Text, at)
						}
					}
				case "assistant":
					for _, b := range blocks(r.Message.Content) {
						if b.Type == "text" {
							addClaude(into, b.Text, at)
						}
					}
				}
			}
		}
	}
	var headMsgs, tailMsgs []Msg
	collect(head, &headMsgs)
	headPrompt := s.LastPrompt
	s.LastPrompt = ""
	collect(tail, &tailMsgs)
	// Big tool results (images, logs) can fill the tail window; walk back until the latest human prompt shows up.
	if tail != nil && s.LastPrompt == "" {
		branch := s.Branch
		for end, read := info.Size()-tailBytes, int64(0); end > headBytes && read < maxBackScan && s.LastPrompt == ""; end, read = end-tailBytes, read+tailBytes {
			if chunk := readWindow(path, max(end-tailBytes, headBytes), end); chunk != nil {
				var older []Msg
				collect(chunk, &older)
				tailMsgs = append(older, tailMsgs...)
			}
		}
		s.Branch = branch
	}
	if s.LastPrompt == "" {
		s.LastPrompt = headPrompt
	}
	s.Head, s.Tail = splitChat(headMsgs, tailMsgs, tail == nil)
	switch {
	case customTitle != "":
		s.Title = customTitle
	case aiTitle != "":
		s.Title = aiTitle
	case summary != "":
		s.Title = summary
	default:
		s.Title = oneLine(s.FirstText, 120)
	}
	s.Msgs = countHuman(path)
	return s
}

func countHuman(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	pat := []byte(`"origin":{"kind":"human"}`)
	buf := make([]byte, 1<<20)
	n, carry := 0, 0
	for {
		m, err := f.Read(buf[carry:])
		end := carry + m
		n += bytes.Count(buf[:end], pat)
		carry = 0
		if end >= len(pat) {
			carry = len(pat) - 1
			copy(buf, buf[end-carry:end])
		}
		if err != nil {
			return n
		}
	}
}

const gitTTL = 6 * time.Hour

func gitOut(dir string, args ...string) (string, bool) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimSpace(string(out)), err == nil
}

func realpath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// probeGit marks the cwd as primary (main checkout) or a linked worktree, but only when the repo has linked worktrees.
func probeGit(cwd string) gitInfo {
	g := gitInfo{Repo: filepath.Base(cwd), Checked: now().Unix()}
	if _, err := os.Stat(cwd); err != nil {
		return g // deleted folder: keep the transcript's branch, no worktree tag
	}
	top, ok := gitOut(cwd, "rev-parse", "--git-common-dir", "--show-toplevel")
	if !ok {
		g.NoGit = true
		return g
	}
	lines := strings.Split(top, "\n")
	top = lines[len(lines)-1]
	wl, ok := gitOut(cwd, "worktree", "list", "--porcelain")
	if !ok {
		return g
	}
	var trees []string
	for _, l := range strings.Split(wl, "\n") {
		if p, found := strings.CutPrefix(l, "worktree "); found {
			trees = append(trees, realpath(p))
		}
	}
	if len(trees) == 0 {
		return g
	}
	g.Repo = filepath.Base(trees[0])
	if len(trees) > 1 {
		if realpath(top) == trees[0] {
			g.Primary = true
		} else {
			g.Linked = filepath.Base(top)
		}
	}
	return g
}

type index struct {
	Files map[string]cacheEntry
	Git   map[string]gitInfo
}

func cachePath() string {
	d, _ := os.UserCacheDir()
	return filepath.Join(d, "claudes", "index-v6.json")
}

func keep(s *Session) bool {
	return s.Msgs > 0 && s.Cwd != "" && !strings.HasPrefix(s.Cwd, "/tmp") && !strings.HasPrefix(s.Cwd, "/private/tmp")
}

func loadSessions() []Session {
	root := filepath.Join(os.Getenv("HOME"), ".claude", "projects")
	paths, _ := filepath.Glob(filepath.Join(root, "*", "*.jsonl"))

	idx := index{}
	if b, err := os.ReadFile(cachePath()); err == nil {
		json.Unmarshal(b, &idx)
	}
	var mu sync.Mutex
	next := index{Files: map[string]cacheEntry{}, Git: map[string]gitInfo{}}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 16)
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			ce, ok := idx.Files[p]
			if !ok || ce.Mtime != info.ModTime().UnixNano() || ce.Size != info.Size() {
				s := parseSession(p, info)
				if s == nil {
					return
				}
				ce = cacheEntry{info.ModTime().UnixNano(), info.Size(), *s}
			}
			mu.Lock()
			next.Files[p] = ce
			mu.Unlock()
		}()
	}
	wg.Wait()

	var stale []string
	for _, ce := range next.Files {
		cwd := ce.S.Cwd
		if _, done := next.Git[cwd]; done || !keep(&ce.S) {
			continue
		}
		g, ok := idx.Git[cwd]
		next.Git[cwd] = g
		if !ok || now().Unix()-g.Checked >= int64(gitTTL/time.Second) {
			stale = append(stale, cwd)
		}
	}
	for _, cwd := range stale {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			g := probeGit(cwd)
			mu.Lock()
			next.Git[cwd] = g
			mu.Unlock()
		}()
	}
	wg.Wait()

	if b, err := json.Marshal(next); err == nil {
		os.MkdirAll(filepath.Dir(cachePath()), 0o755)
		os.WriteFile(cachePath(), b, 0o644)
	}
	var out []Session
	for _, ce := range next.Files {
		s := ce.S
		if !keep(&s) {
			continue
		}
		s.gitInfo = next.Git[s.Cwd]
		if s.Branch == "HEAD" || s.NoGit {
			s.Branch = ""
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out
}

// transcriptHits returns the indexes of sessions whose user/assistant lines
// contain every term (case-insensitive); cancelled() aborts early.
func transcriptHits(sessions []Session, terms []string, cancelled func() bool) map[int]bool {
	lower := make([][]byte, len(terms))
	for i, t := range terms {
		lower[i] = []byte(strings.ToLower(t))
	}
	want := 1<<len(terms) - 1
	hits := map[int]bool{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i := range sessions {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			f, err := os.Open(sessions[i].Path)
			if err != nil {
				return
			}
			defer f.Close()
			r := bufio.NewReaderSize(f, 1<<20)
			found := 0
			for found != want && !cancelled() {
				line, err := readLine(r)
				if bytes.HasPrefix(line, []byte(`{"parentUuid"`)) && !bytes.Contains(line[:min(200, len(line))], []byte(`"attachment"`)) {
					lo := bytes.ToLower(line)
					for k, t := range lower {
						if bytes.Contains(lo, t) {
							found |= 1 << k
						}
					}
				}
				if err != nil {
					break
				}
			}
			if found == want {
				mu.Lock()
				hits[i] = true
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return hits
}

func readLine(r *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		part, isPrefix, err := r.ReadLine()
		if len(line) < maxLineJSON {
			line = append(line, part...)
		}
		if !isPrefix || err != nil {
			return line, err
		}
	}
}

const (
	msgRunes = 360
	keepMsgs = 14
	// shortChat is the most messages a fully-read session shows in one pane instead of split head and tail panes.
	shortChat = 8
)

func capText(t string) string {
	t = strings.TrimSpace(t)
	if r := []rune(t); len(r) > msgRunes {
		return string(r[:msgRunes]) + "…"
	}
	return t
}

// splitChat picks the head and tail messages for the preview; a fully read session (whole) is one chat that is
// split in half so the two panes never repeat a message.
func splitChat(head, tail []Msg, whole bool) (h, t []Msg) {
	if whole {
		if len(head) <= shortChat {
			return head, nil
		}
		half := len(head) / 2
		head, tail = head[:half], head[half:]
	}
	if len(head) > keepMsgs {
		head = head[:keepMsgs]
	}
	if len(tail) > keepMsgs {
		tail = tail[len(tail)-keepMsgs:]
	}
	return head, tail
}

func addUser(into *[]Msg, text string, at time.Time) {
	*into = append(*into, Msg{User: true, Text: capText(text), At: at})
}

// addClaude merges consecutive text blocks of one turn into a single message.
func addClaude(into *[]Msg, text string, at time.Time) {
	if strings.TrimSpace(text) == "" {
		return
	}
	if n := len(*into); n > 0 && !(*into)[n-1].User {
		(*into)[n-1].Text = capText((*into)[n-1].Text + "\n\n" + text)
		return
	}
	*into = append(*into, Msg{Text: capText(text), At: at})
}

// loadChat reads the whole transcript for every message, so the preview can scroll the full history.
func loadChat(path string) []Msg {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var msgs []Msg
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := readLine(r)
		if bytes.HasPrefix(line, []byte(`{"parentUuid"`)) && len(line) < maxLineJSON {
			var rec record
			if json.Unmarshal(line, &rec) == nil && !rec.Sidechain {
				at, _ := time.Parse(time.RFC3339, rec.Timestamp)
				switch {
				case rec.Type == "user" && (rec.Origin == nil || rec.Origin.Kind == "human"):
					for _, b := range blocks(rec.Message.Content) {
						if b.Type == "text" && isHumanText(b.Text) {
							addUser(&msgs, b.Text, at)
						}
					}
				case rec.Type == "assistant":
					for _, b := range blocks(rec.Message.Content) {
						if b.Type == "text" {
							addClaude(&msgs, b.Text, at)
						}
					}
				}
			}
		}
		if err != nil {
			return msgs
		}
	}
}
