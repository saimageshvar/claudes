package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
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
	maxFiles    = 5
)

type Session struct {
	ID         string
	Path       string
	Cwd        string
	Branch     string
	Title      string
	FirstText  string
	FirstReply string
	LastPrompt string
	LastReply  string
	Files      []string
	Started    time.Time
	Updated    time.Time
	Msgs       int
	Worktree   string
	InTx       bool
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
}

type block struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Name  string `json:"name"`
	Input struct {
		FilePath string `json:"file_path"`
	} `json:"input"`
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
	return t != "" && !strings.HasPrefix(t, "<") && !strings.HasPrefix(t, "[Request interrupted") && !strings.HasPrefix(t, "Caveat:")
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
	var customTitle, aiTitle string
	var files []string
	seenFile := map[string]bool{}
	lastHuman := func(chunk []byte, isHead bool) {
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
				if t, err := time.Parse(time.RFC3339, r.Timestamp); err == nil && s.Started.IsZero() {
					s.Started = t
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
						}
					}
				case "assistant":
					for _, b := range blocks(r.Message.Content) {
						switch {
						case b.Type == "text" && strings.TrimSpace(b.Text) != "":
							if s.FirstReply == "" && s.FirstText != "" {
								s.FirstReply = b.Text
							}
							s.LastReply = b.Text
						case b.Type == "tool_use" && (b.Name == "Edit" || b.Name == "Write") && b.Input.FilePath != "":
							if !seenFile[b.Input.FilePath] {
								seenFile[b.Input.FilePath] = true
								files = append(files, b.Input.FilePath)
							}
						}
					}
				}
			}
		}
	}
	lastHuman(head, true)
	lastHuman(tail, false)
	if len(files) > maxFiles {
		files = files[len(files)-maxFiles:]
	}
	s.Files = files
	switch {
	case customTitle != "":
		s.Title = customTitle
	case aiTitle != "":
		s.Title = aiTitle
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

func worktreeOf(cwd string) string {
	if fi, err := os.Stat(filepath.Join(cwd, ".git")); err == nil && !fi.IsDir() {
		return filepath.Base(cwd)
	}
	return ""
}

func cachePath() string {
	d, _ := os.UserCacheDir()
	return filepath.Join(d, "claudes", "index-v2.json")
}

func loadSessions() []Session {
	root := filepath.Join(os.Getenv("HOME"), ".claude", "projects")
	paths, _ := filepath.Glob(filepath.Join(root, "*", "*.jsonl"))

	cache := map[string]cacheEntry{}
	if b, err := os.ReadFile(cachePath()); err == nil {
		json.Unmarshal(b, &cache)
	}
	var mu sync.Mutex
	next := map[string]cacheEntry{}
	var out []Session
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
			ce, ok := cache[p]
			if !ok || ce.Mtime != info.ModTime().UnixNano() || ce.Size != info.Size() {
				s := parseSession(p, info)
				if s == nil {
					return
				}
				ce = cacheEntry{info.ModTime().UnixNano(), info.Size(), *s}
			}
			mu.Lock()
			next[p] = ce
			mu.Unlock()
		}()
	}
	wg.Wait()
	if b, err := json.Marshal(next); err == nil {
		os.MkdirAll(filepath.Dir(cachePath()), 0o755)
		os.WriteFile(cachePath(), b, 0o644)
	}
	for _, ce := range next {
		s := ce.S
		if s.Msgs == 0 || s.Cwd == "" || strings.HasPrefix(s.Cwd, "/tmp") || strings.HasPrefix(s.Cwd, "/private/tmp") {
			continue
		}
		s.Worktree = worktreeOf(s.Cwd)
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
