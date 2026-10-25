package launcher

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/amanachenko/cardo/internal/coach"
	"github.com/amanachenko/cardo/internal/pseudonym"
)

// coachView is what the page needs: the report, the eight-week trend, and the window it covers.
type coachView struct {
	Pseudonym    string       `json:"pseudonym"`
	From         string       `json:"from"`
	To           string       `json:"to"`
	Report       coach.Report `json:"report"`
	Weekly       []coach.Week `json:"weekly"`
	Unidentified int          `json:"unidentified"`
}

// coachToken signs one pseudonym with the board token. It is deterministic, so a link can be issued
// again without storage; rotating the board token revokes every link at once.
func coachToken(boardToken, pseudonym string) string {
	m := hmac.New(sha256.New, []byte(boardToken))
	m.Write([]byte("coach:" + pseudonym))
	return hex.EncodeToString(m.Sum(nil))[:32]
}

// validPseudonym reports whether p is a pseudonym this server could have issued: 64 lowercase hex
// characters, which is what a salted SHA-256 looks like.
//
// The value is interpolated into SQL, so it is checked before it reaches a query. A pseudonym that
// is not one of ours is a mistake or an attempt, and either way it is refused rather than
// escaped.
func validPseudonym(p string) bool {
	if len(p) != 64 {
		return false
	}
	for _, r := range p {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func (s *Server) buildCoachView(ctx context.Context, pseudonymID string, days int) (coachView, error) {
	env, err := s.chEnv()
	if err != nil {
		return coachView{}, err
	}
	user, pass := env["CLICKHOUSE_USER"], env["CLICKHOUSE_PASSWORD"]
	now := time.Now().UTC()
	to := now
	from := to.AddDate(0, 0, -(days - 1))
	prevTo := from.AddDate(0, 0, -1)
	prevFrom := prevTo.AddDate(0, 0, -(days - 1))

	nowW, arts, err := coach.Query(ctx, s.opts.url("8124", ""), user, pass, pseudonymID, from, to)
	if err != nil {
		return coachView{}, err
	}
	prevW, _, err := coach.Query(ctx, s.opts.url("8124", ""), user, pass, pseudonymID, prevFrom, prevTo)
	if err != nil {
		return coachView{}, err
	}
	untried, err := coach.Untried(ctx, s.opts.url("8124", ""), user, pass, pseudonymID, from, to)
	if err != nil {
		return coachView{}, err
	}
	weekly, err := coach.Weekly(ctx, s.opts.url("8124", ""), user, pass, pseudonymID, 8, now)
	if err != nil {
		return coachView{}, err
	}
	rep := coach.Report{
		Pseudonym: pseudonymID, From: from, To: to, PrevFrom: prevFrom,
		Now: nowW, Prev: prevW, Artifacts: arts, Untried: coach.Names(untried),
	}
	rep.Suggestions = coach.Suggest(untried)
	unidentified, _ := coach.Unidentified(ctx, s.opts.url("8124", ""), user, pass, from, to)
	return coachView{Pseudonym: pseudonymID, From: from.Format("2006-01-02"), To: to.Format("2006-01-02"), Report: rep, Weekly: weekly, Unidentified: unidentified}, nil
}

func (s *Server) writeCoach(w http.ResponseWriter, r *http.Request, pseudonymID string) {
	days := 28
	view, err := s.buildCoachView(r.Context(), pseudonymID, days)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, view)
}

// handleCoach answers the operator preview; cookie auth is already applied.
func (s *Server) handleCoach(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimSpace(r.URL.Query().Get("pseudonym"))
	if p == "" {
		http.Error(w, "pseudonym is required", http.StatusBadRequest)
		return
	}
	if !validPseudonym(p) {
		http.Error(w, "that is not a pseudonym", http.StatusBadRequest)
		return
	}
	s.writeCoach(w, r, p)
}

// handleCoachOpen answers a per-person link: the token must match this pseudonym exactly.
//
// The key is checked first, so a wrong key always answers 401 and never says whether the
// pseudonym exists. The shape is checked next, before the value reaches a query.
func (s *Server) handleCoachOpen(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimSpace(r.URL.Query().Get("pseudonym"))
	k := strings.TrimSpace(r.URL.Query().Get("k"))
	if p == "" || k == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	want := coachToken(s.token, p)
	if !hmac.Equal([]byte(k), []byte(want)) {
		http.Error(w, "This link is no longer valid.", http.StatusUnauthorized)
		return
	}
	if !validPseudonym(p) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	s.writeCoach(w, r, p)
}

// handleCoachResolve turns an email into the value the page needs, using the collector's salt.
func (s *Server) handleCoachResolve(w http.ResponseWriter, r *http.Request) {
	env, err := s.chEnv()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	version := env["CARDO_SALT_VERSION"]
	if version == "" {
		version = "v1"
	}
	id, err := pseudonym.ForEmail(env["CARDO_SALT"], version, r.URL.Query().Get("email"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]string{"pseudonym": id.Value, "version": id.Version})
}

// handleCoachLink issues the page URL for one pseudonym.
func (s *Server) handleCoachLink(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Pseudonym string `json:"pseudonym"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Pseudonym) == "" {
		http.Error(w, "pseudonym is required", http.StatusBadRequest)
		return
	}
	if !validPseudonym(strings.TrimSpace(body.Pseudonym)) {
		http.Error(w, "that is not a pseudonym", http.StatusBadRequest)
		return
	}
	u := "/coach/" + body.Pseudonym + "?k=" + coachToken(s.token, body.Pseudonym)
	writeJSON(w, map[string]string{"url": u})
}

// handleCoachPage serves the same shell for a per-person link; the app hides everything but the
// coach when the path starts with /coach/.
func (s *Server) handleCoachPage(w http.ResponseWriter, r *http.Request) {
	body, err := webFS.ReadFile("web/index.html")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(body)
}
