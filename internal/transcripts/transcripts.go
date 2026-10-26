package transcripts

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Usage struct {
	In          int64 `json:"input_tokens"`
	Out         int64 `json:"output_tokens"`
	CacheRead   int64 `json:"cache_read_input_tokens"`
	CacheCreate int64 `json:"cache_creation_input_tokens"`
}

type Prompt struct {
	UUID string
	TS   time.Time
	Len  int
}

type Request struct {
	PromptID string
	TS       time.Time
	Model    string
	Usage    Usage
}

type Session struct {
	ID        string
	Start     time.Time
	End       time.Time
	Models    map[string]bool
	Prompts   int
	Usage     Usage
	ModelsTok map[string]Usage
	PromptLog []Prompt
	Requests  []Request
}

type Stats struct {
	Files     int
	Sessions  []*Session
	BadLines  int
	PromptSum int
}

type line struct {
	Type      string          `json:"type"`
	SessionID string          `json:"sessionId"`
	Timestamp string          `json:"timestamp"`
	UUID      string          `json:"uuid"`
	Parent    string          `json:"parentUuid"`
	IsSide    bool            `json:"isSidechain"`
	RequestID string          `json:"requestId"`
	Message   json.RawMessage `json:"message"`
}

type message struct {
	ID      string          `json:"id"`
	Role    string          `json:"role"`
	Model   string          `json:"model"`
	Usage   Usage           `json:"usage"`
	Content json.RawMessage `json:"content"`
}

// Scan walks every transcript under root and returns one row per session.
func Scan(root string) (Stats, error) {
	st := Stats{}
	sessions := map[string]*Session{}
	promptOf := map[string]map[string]string{}
	parentOf := map[string]string{}
	lastPrompt := map[string]string{}
	// Claude Code writes a reply as one line per part (its thinking, its text, each tool call), and
	// every line carries the whole reply's usage. A reply is counted once, by its message id, in
	// whichever file it appears first.
	type reply struct {
		s *Session
		i int
	}
	replies := map[string]reply{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		st.Files++
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1024*1024), 8*1024*1024)
		for sc.Scan() {
			var l line
			if json.Unmarshal(sc.Bytes(), &l) != nil {
				st.BadLines++
				continue
			}
			if l.SessionID == "" {
				continue
			}
			if l.UUID != "" && l.Parent != "" {
				parentOf[l.SessionID+"\x00"+l.UUID] = l.Parent
			}
			s := sessions[l.SessionID]
			if s == nil {
				s = &Session{ID: l.SessionID, Models: map[string]bool{}}
				sessions[l.SessionID] = s
			}
			ts, _ := time.Parse(time.RFC3339Nano, l.Timestamp)
			if !ts.IsZero() {
				if s.Start.IsZero() || ts.Before(s.Start) {
					s.Start = ts
				}
				if ts.After(s.End) {
					s.End = ts
				}
			}
			var m message
			if len(l.Message) > 0 && json.Unmarshal(l.Message, &m) == nil {
				if m.Model != "" {
					s.Models[m.Model] = true
				}
				if l.Type == "user" && !l.IsSide && m.Role == "user" && isPrompt(m.Content) {
					s.Prompts++
					st.PromptSum++
					n := contentLen(m.Content)
					s.PromptLog = append(s.PromptLog, Prompt{UUID: l.UUID, TS: ts, Len: n})
					if promptOf[l.SessionID] == nil {
						promptOf[l.SessionID] = map[string]string{}
					}
					promptOf[l.SessionID][l.UUID] = l.UUID
					lastPrompt[l.SessionID] = l.UUID
				}
				if l.Type == "assistant" && m.Usage.In+m.Usage.Out+m.Usage.CacheRead+m.Usage.CacheCreate > 0 {
					id := m.ID
					if id == "" {
						id = l.RequestID
					}
					if r, seen := replies[id]; seen && id != "" {
						// If the parts disagree, the largest output count is the reply's.
						if m.Usage.Out > r.s.Requests[r.i].Usage.Out {
							r.s.Requests[r.i].Usage = m.Usage
						}
					} else {
						pid := resolvePrompt(l.SessionID, l.UUID, l.Parent, parentOf, promptOf, lastPrompt)
						s.Requests = append(s.Requests, Request{PromptID: pid, TS: ts, Model: m.Model, Usage: m.Usage})
						if id != "" {
							replies[id] = reply{s, len(s.Requests) - 1}
						}
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return st, err
	}
	for _, s := range sessions {
		// A session's totals are the sum of its replies, each counted once.
		for _, r := range s.Requests {
			s.Usage.add(r.Usage)
			if s.ModelsTok == nil {
				s.ModelsTok = map[string]Usage{}
			}
			acc := s.ModelsTok[r.Model]
			acc.add(r.Usage)
			s.ModelsTok[r.Model] = acc
		}
		st.Sessions = append(st.Sessions, s)
	}
	return st, nil
}

func (u *Usage) add(v Usage) {
	u.In += v.In
	u.Out += v.Out
	u.CacheRead += v.CacheRead
	u.CacheCreate += v.CacheCreate
}

// isPrompt is true for a typed prompt, false for tool results and system reminders.
func isPrompt(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var str string
	if json.Unmarshal(raw, &str) == nil {
		return strings.TrimSpace(str) != "" && !strings.HasPrefix(str, "<")
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		for _, p := range parts {
			if p.Type == "text" && strings.TrimSpace(p.Text) != "" && !strings.HasPrefix(p.Text, "<") {
				return true
			}
		}
	}
	return false
}

func contentLen(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	var str string
	if json.Unmarshal(raw, &str) == nil {
		return len([]rune(str))
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	n := 0
	if json.Unmarshal(raw, &parts) == nil {
		for _, p := range parts {
			if p.Type == "text" {
				n += len([]rune(p.Text))
			}
		}
	}
	return n
}

// resolvePrompt walks the parent chain until it finds a prompt uuid; transcripts are chronological
// within a session, so the last seen prompt is a safe fallback.
func resolvePrompt(session, self, parent string, parentOf map[string]string, promptOf map[string]map[string]string, last map[string]string) string {
	byUUID := promptOf[session]
	cur := parent
	for i := 0; i < 40 && cur != ""; i++ {
		if byUUID[cur] != "" {
			return cur
		}
		next := parentOf[session+"\x00"+cur]
		if next == cur {
			break
		}
		cur = next
	}
	return last[session]
}
