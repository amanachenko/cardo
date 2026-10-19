package launcher

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

//go:embed web/index.html web/app.js web/cardo-ui.css web/favicon.svg
var webFS embed.FS

// Server is the board: the same steps as the CLI, driven from a browser on loopback.
//
// It is a view over RunStep, not a second implementation. The token in the URL exists because any
// page in the browser can reach 127.0.0.1: without it, a random tab could start a compose stack.
type Server struct {
	// catalogMu serializes version allocation, the file write and the row insert. The board is one
	// process by design; this makes two browser tabs unable to claim the same version number and
	// overwrite each other's file.
	catalogMu sync.Mutex
	// overview runs live checks; a 3 s cache keeps tab switching from re-running docker and every
	// query, while Refresh asks for a fresh pass explicitly.
	ovMu   sync.Mutex
	ovAt   time.Time
	ovBody []byte
	opts   Options
	token  string

	mu      sync.Mutex
	results map[string]Result
	running map[string]bool
	subs    map[chan Event]struct{}
}

// Event is one server-sent update: a step changed state, or a line of progress.
type Event struct {
	ID     string            `json:"id,omitempty"`
	State  string            `json:"state,omitempty"`
	Value  string            `json:"value,omitempty"`
	Cause  string            `json:"cause,omitempty"`
	Fix    string            `json:"fix,omitempty"`
	Note   string            `json:"note,omitempty"`
	Reveal map[string]string `json:"reveal,omitempty"` // shown once; never persisted
}

// NewServer prepares a board. The token is generated here and printed by the CLI.
func NewServer(opts Options) (*Server, error) {
	opts.defaults()
	if opts.Version == "" {
		opts.Version = "dev"
	}
	token := opts.Token
	if token == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		token = hex.EncodeToString(b)
	}
	return &Server{
		opts:    opts,
		token:   token,
		results: map[string]Result{},
		running: map[string]bool{},
		subs:    map[chan Event]struct{}{},
	}, nil
}

// Token is the one-time secret the URL carries.
func (s *Server) Token() string { return s.token }

// URL is what the CLI prints. The token rides in the query string so the page can use it.
func (s *Server) URL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host, port = "127.0.0.1", "8484"
	}
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	// Composed rather than formatted: INV-7 forbids a host literal in the source, and the URL is
	// the only place the board's address appears.
	u := url.URL{Scheme: "http", Host: net.JoinHostPort(host, port), RawQuery: "t=" + s.token}
	return u.String()
}

// Handler is the HTTP surface. Static files need no token; everything that acts does.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", s.auth(s.handleState))
	mux.HandleFunc("POST /api/run", s.auth(s.handleRun))
	mux.HandleFunc("GET /api/events", s.auth(s.handleEvents))
	mux.HandleFunc("GET /api/teams", s.auth(s.handleTeamsGet))
	mux.HandleFunc("POST /api/teams", s.auth(s.handleTeamsPost))
	mux.HandleFunc("GET /api/catalog", s.auth(s.handleCatalogList))
	mux.HandleFunc("GET /api/catalog/{slug}", s.auth(s.handleCatalogDetail))
	mux.HandleFunc("POST /api/catalog", s.auth(s.handleCatalogUpload))
	mux.HandleFunc("POST /api/catalog/validate", s.auth(s.handleCatalogValidate))
	mux.HandleFunc("POST /api/catalog/{slug}/status", s.auth(s.handleCatalogStatus))
	mux.HandleFunc("GET /api/overview", s.auth(s.handleOverview))
	mux.HandleFunc("GET /api/coach", s.auth(s.handleCoach))
	mux.HandleFunc("GET /api/coach/open", s.handleCoachOpen)
	mux.HandleFunc("GET /api/coach/resolve", s.auth(s.handleCoachResolve))
	mux.HandleFunc("POST /api/coach/link", s.auth(s.handleCoachLink))
	mux.HandleFunc("GET /api/session/latest", s.auth(s.handleLatestSession))
	mux.HandleFunc("GET /api/settings", s.auth(s.handleSettingsGet))
	mux.HandleFunc("POST /api/settings", s.auth(s.handleSettingsPost))
	mux.HandleFunc("GET /api/admin", s.auth(s.handleAdminState))
	mux.HandleFunc("POST /api/admin", s.auth(s.handleAdminSave))
	mux.HandleFunc("POST /api/admin/run", s.auth(s.handleAdminRun))

	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		panic(err) // the embed directive above makes this impossible
	}
	files := http.FileServer(http.FS(sub))
	mux.HandleFunc("GET /coach/", s.handleCoachPage)
	mux.HandleFunc("GET /{$}", s.handleRoot(files))
	mux.Handle("GET /", files)
	return mux
}

// Serve runs the board until ctx is cancelled.
func (s *Server) Serve(ctx context.Context, addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return s.ServeListener(ctx, ln)
}

// ServeListener serves on a listener the caller already owns, so the URL is printed only after the
// port is actually held.
func (s *Server) ServeListener(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// cookieName scopes the session cookie to this port. Two installers on one host (8484 and a second
// stack for a pilot) must not overwrite each other's session.
func cookieName(r *http.Request) string {
	_, port, err := net.SplitHostPort(r.Host)
	if err != nil || port == "" {
		return "cardo_session"
	}
	return "cardo_session_" + port
}

// handleRoot trades the token in the query string for an HttpOnly cookie and redirects, so the
// secret leaves the address bar and history and a reload keeps working. A link whose token no
// longer matches is refused here, with a sentence that says how to get a new one.
func (s *Server) handleRoot(files http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if t := r.URL.Query().Get("t"); t != "" {
			if t != s.token {
				http.Error(w, "This installation link is no longer valid. Restart the installer with `cardo ui` and open the URL it prints.", http.StatusUnauthorized)
				return
			}
			http.SetCookie(w, &http.Cookie{
				Name:     cookieName(r),
				Value:    s.token,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
			})
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		files.ServeHTTP(w, r)
	}
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authed(r) {
			http.Error(w, "This installation link is no longer valid. Restart the installer with `cardo ui` and open the URL it prints.", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// authed accepts the header, the query token, or the cookie the root handler sets.
func (s *Server) authed(r *http.Request) bool {
	if r.Header.Get("X-Cardo-Token") == s.token {
		return true
	}
	if r.URL.Query().Get("t") == s.token {
		return true
	}
	if c, err := r.Cookie(cookieName(r)); err == nil && c.Value == s.token {
		return true
	}
	return false
}

type stepView struct {
	Meta
	State string `json:"state"`
	Value string `json:"value,omitempty"`
	Cause string `json:"cause,omitempty"`
	Fix   string `json:"fix,omitempty"`
}

type stateView struct {
	Groups []groupView `json:"groups"`
	Done   int         `json:"done"`
	Total  int         `json:"total"`
}

type groupView struct {
	Name  string     `json:"name"`
	Steps []stepView `json:"steps"`
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out stateView
	index := map[string]int{}
	for _, m := range Steps {
		meta := m
		meta.Cmd = commandFor(m, runtime.GOOS)
		v := stepView{Meta: meta, State: "wait"}
		if m.Planned {
			v.State = "planned"
			v.Value = "not implemented"
		}
		if res, ok := s.results[m.ID]; ok {
			v.Value, v.Cause, v.Fix = res.Value, res.Cause, res.Fix
			if res.OK {
				v.State = "ok"
			} else if res.Pending {
				v.State = "wait"
			} else {
				v.State = "fail"
			}
		}
		if s.running[m.ID] {
			v.State = "run"
		}
		if _, ok := index[m.Group]; !ok {
			index[m.Group] = len(out.Groups)
			out.Groups = append(out.Groups, groupView{Name: m.Group})
		}
		g := &out.Groups[index[m.Group]]
		g.Steps = append(g.Steps, v)
		out.Total++
		if v.State == "ok" {
			out.Done++
		}
	}
	writeJSON(w, out)
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Step    string `json:"step"`
		Project string `json:"project"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "want {\"step\": \"docker\"} or {\"step\": \"all\"}", http.StatusBadRequest)
		return
	}
	ids := []string{body.Step}
	if body.Step == "check" {
		ids = []string{"docker", "clickhouse", "grafana", "collector"}
	}
	if body.Step == "all" || body.Step == "" {
		ids = nil
		for _, m := range Steps {
			if m.Planned {
				continue // not built yet; the board says so instead of pretending
			}
			ids = append(ids, m.ID)
		}
	}
	for _, id := range ids {
		m, ok := MetaByID(id)
		if !ok {
			http.Error(w, "unknown step "+id, http.StatusBadRequest)
			return
		}
		if m.Planned {
			http.Error(w, id+" is not built yet; the board says so instead of pretending", http.StatusConflict)
			return
		}
	}
	for _, id := range ids {
		if s.isRunning(id) {
			http.Error(w, "that step is already running", http.StatusConflict)
			return
		}
	}
	opts := s.opts
	if body.Project != "" {
		opts.Project = body.Project
	}
	go s.runAll(ids, opts)
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, map[string]any{"started": ids})
}

func (s *Server) runAll(ids []string, opts Options) {
	for _, id := range ids {
		s.setRunning(id, true)
		s.broadcast(Event{ID: id, State: "run"})
		res, err := RunStep(context.Background(), opts, id)
		s.setRunning(id, false)
		if err != nil {
			s.record(id, Result{ID: id, Name: strings.ToUpper(id), Value: "failed", Fix: err.Error()})
			s.broadcast(Event{ID: id, State: "fail", Value: "failed", Cause: err.Error(), Fix: "This is an internal error; report it with the message above."})
			return
		}
		// Credentials leave the process exactly once, in this event, and are dropped from the stored
		// result so a later /api/state or a second connection cannot see them.
		reveal := res.Reveal
		res.Reveal = nil
		s.record(id, res)
		state := "fail"
		if res.OK {
			state = "ok"
		}
		s.broadcast(Event{ID: id, State: state, Value: res.Value, Cause: res.Cause, Fix: res.Fix, Reveal: reveal})
		if !res.OK {
			s.broadcast(Event{Note: "done"})
			return // a board that keeps going after a failure hides the first cause
		}
	}
	s.broadcast(Event{Note: "done"})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan Event, 16)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
	}()

	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-ch:
			b, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()
		}
	}
}

func (s *Server) broadcast(ev Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- ev:
		default: // a slow tab must never block a check
		}
	}
}

func (s *Server) record(id string, res Result) {
	s.mu.Lock()
	s.results[id] = res
	s.mu.Unlock()
}

func (s *Server) setRunning(id string, running bool) {
	s.mu.Lock()
	s.running[id] = running
	s.mu.Unlock()
}

func (s *Server) isRunning(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running[id]
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write json: %v", err)
	}
}

// commandFor picks the platform-specific diagnostic when a step has one.
func commandFor(m Meta, goos string) string {
	if c, ok := m.Commands[goos]; ok {
		return c
	}
	return m.Cmd
}

// adminView is what the board may know about the Admin API path. It never carries the key.
type adminView struct {
	Configured bool   `json:"configured"`
	Interval   string `json:"interval"`
	LastDay    string `json:"lastDay"`
	Rows       int    `json:"rows"`
	Days       int    `json:"days"`
	Error      string `json:"error,omitempty"`
}

func (s *Server) handleAdminState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.adminState(r.Context()))
}

// adminState reads .env for the key's presence and ClickHouse for what the poller has landed.
func (s *Server) adminState(ctx context.Context) adminView {
	v := adminView{Interval: "24h"}
	env, err := ReadDotEnv(filepath.Join(s.opts.Dir, ".env"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		v.Error = err.Error()
		return v
	}
	v.Configured = strings.TrimSpace(env["CARDO_ADMIN_KEY"]) != ""
	if i := strings.TrimSpace(env["CARDO_POLL_INTERVAL"]); i != "" {
		v.Interval = i
	}
	day, rows, days, err := s.adminCounts(ctx)
	if err != nil {
		v.Error = err.Error()
		return v
	}
	v.LastDay, v.Rows, v.Days = day, rows, days
	return v
}

// adminCounts answers "has anything arrived, and how fresh is it" from bronze_actor_day.
func (s *Server) adminCounts(ctx context.Context) (string, int, int, error) {
	env, err := ReadDotEnv(filepath.Join(s.opts.Dir, ".env"))
	if err != nil {
		return "", 0, 0, err
	}
	body, _, err := chQuery(ctx, s.opts.url("8124", ""), env["CLICKHOUSE_USER"], env["CLICKHOUSE_PASSWORD"],
		"SELECT toString(max(day)) AS last, count() AS rows, uniqExact(day) AS days FROM cardo.bronze_actor_day FORMAT JSONEachRow")
	if err != nil {
		return "", 0, 0, err
	}
	var row struct {
		Last string `json:"last"`
		Rows int    `json:"rows"`
		Days int    `json:"days"`
	}
	for _, line := range strings.Split(strings.TrimSpace(body), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return "", 0, 0, err
		}
	}
	if row.Rows == 0 {
		row.Last = "" // max(day) on an empty table is 1970-01-01, which reads as real data
	}
	return row.Last, row.Rows, row.Days, nil
}

// handleAdminSave writes the key and the interval into .env (mode 0600) and starts the poller.
// An empty key keeps the existing one: the field is write-only, so a reload cannot erase it.
func (s *Server) handleAdminSave(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Key      string `json:"key"`
		Interval string `json:"interval"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "want {\"key\": \"...\", \"interval\": \"24h\"}", http.StatusBadRequest)
		return
	}
	if body.Interval != "" {
		d, err := time.ParseDuration(body.Interval)
		if err != nil || d < time.Hour {
			http.Error(w, "interval must be a duration of at least 1h, for example 24h", http.StatusBadRequest)
			return
		}
	}
	path := filepath.Join(s.opts.Dir, ".env")
	env, err := ReadDotEnv(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	key := strings.TrimSpace(body.Key)
	if key == "" {
		state := s.adminState(r.Context())
		if !state.Configured {
			http.Error(w, "a key is required the first time; paste the Admin API key", http.StatusBadRequest)
			return
		}
		writeJSON(w, state) // nothing to save: do not restart a working poller
		return
	}
	env["CARDO_ADMIN_KEY"] = key
	if body.Interval != "" {
		env["CARDO_POLL_INTERVAL"] = body.Interval
	}
	if err := WriteDotEnv(path, env, Secrets{}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.audit(r.Context(), "admin-key", "CARDO_ADMIN_KEY", 0, "", "", "interval "+body.Interval); err != nil {
		http.Error(w, "the key was saved but its audit line failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	// Start (or recreate) the poller so the key takes effect without another command.
	if _, err := s.opts.Deps.Exec(r.Context(), s.opts.Dir, "docker", "compose", "--profile", "admin", "up", "-d", "poller"); err != nil {
		http.Error(w, "the key is saved, but the poller did not start: "+err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]any{"configured": true, "started": true})
}

// handleAdminRun starts the poller again; it polls immediately on start, then sleeps.
func (s *Server) handleAdminRun(w http.ResponseWriter, r *http.Request) {
	if _, err := s.opts.Deps.Exec(r.Context(), s.opts.Dir, "docker", "compose", "--profile", "admin", "up", "-d", "poller"); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]any{"started": true})
}
