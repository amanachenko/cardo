package launcher

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/amanachenko/cardo/internal/catalog"
)

// maxArtifactBytes bounds an upload. A skill is a few kilobytes; anything near this ceiling is a
// mistake or an archive, not an artifact.
const maxArtifactBytes = 256 << 10

type catalogRow struct {
	Slug       string `json:"slug"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Owner      string `json:"owner"`
	Status     string `json:"status"`
	Version    int    `json:"version"`
	Hash       string `json:"hash"`
	Note       string `json:"note"`
	CreatedAt  string `json:"createdAt"`
	ApprovedAt string `json:"approvedAt,omitempty"`
	// Reach is the latest week's people count, or "" when the cohort is below the floor.
	Reach string `json:"reach"`
}

type catalogVersion struct {
	Version    int    `json:"version"`
	Status     string `json:"status"`
	Hash       string `json:"hash"`
	Note       string `json:"note"`
	CreatedAt  string `json:"createdAt"`
	ApprovedAt string `json:"approvedAt,omitempty"`
}

type catalogDetail struct {
	catalogRow
	Body     string           `json:"body"`
	Path     string           `json:"path"`
	Versions []catalogVersion `json:"versions"`
}

func (s *Server) catalogRoot() string { return filepath.Join(s.opts.Dir, "catalog") }

type catalogPage struct {
	Rows   []catalogRow `json:"rows"`
	Total  int          `json:"total"`
	Limit  int          `json:"limit"`
	Offset int          `json:"offset"`
}

func (s *Server) handleCatalogList(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	total, err := s.catalogTotal(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	rows, err := s.catalogRows(r.Context(), limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	s.addReach(r.Context(), rows)
	writeJSON(w, catalogPage{Rows: rows, Total: total, Limit: limit, Offset: offset})
}

func (s *Server) catalogTotal(ctx context.Context) (int, error) {
	env, err := s.chEnv()
	if err != nil {
		return 0, err
	}
	body, err := chCount(ctx, s.opts.url("8124", ""), env["CLICKHOUSE_USER"], env["CLICKHOUSE_PASSWORD"],
		"SELECT uniqExact(slug) FROM cardo.settings_artifact_versions")
	return int(body), err
}

// chEnv reads the stack credentials the catalog needs; it never writes.
func (s *Server) chEnv() (map[string]string, error) {
	return ReadDotEnv(filepath.Join(s.opts.Dir, ".env"))
}

// chInt runs a query that returns one unsigned integer.
func chInt(ctx context.Context, endpoint, user, pass, sql string) int {
	body, err := chCount(ctx, endpoint, user, pass, sql)
	if err != nil {
		return 0
	}
	return int(body)
}

func (s *Server) catalogRows(ctx context.Context, limit, offset int) ([]catalogRow, error) {
	env, err := s.chEnv()
	if err != nil {
		return nil, err
	}
	sql := fmt.Sprintf(`SELECT slug, argMax(version, version) AS cur, argMax(kind, version) AS kind,
		argMax(name, version) AS name, argMax(owner, version) AS owner, argMax(status, version) AS status,
		argMax(content_hash, version) AS hash, toString(argMax(created_at, version)) AS created
		FROM cardo.settings_artifact_versions GROUP BY slug ORDER BY slug LIMIT %d OFFSET %d FORMAT JSONEachRow`, limit, offset)
	body, _, err := chQuery(ctx, s.opts.url("8124", ""), env["CLICKHOUSE_USER"], env["CLICKHOUSE_PASSWORD"], sql)
	if err != nil {
		return nil, err
	}
	var rows []catalogRow
	for _, line := range strings.Split(strings.TrimSpace(body), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var r catalogRow
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			return nil, err
		}
		rows = append(rows, r)
	}
	return rows, nil
}

// addReach joins the catalog to gold_artifact_usage. A cohort below the floor is reported as a dash,
// never as a number small enough to identify a person.
func (s *Server) addReach(ctx context.Context, rows []catalogRow) {
	env, err := s.chEnv()
	if err != nil {
		return
	}
	floor := chInt(ctx, s.opts.url("8124", ""), env["CLICKHOUSE_USER"], env["CLICKHOUSE_PASSWORD"],
		"SELECT min_group_size FROM cardo.settings_effective")
	body, _, err := chQuery(ctx, s.opts.url("8124", ""), env["CLICKHOUSE_USER"], env["CLICKHOUSE_PASSWORD"],
		"SELECT kind, lower(artifact) AS artifact, argMax(people, week) AS people, argMax(people_share, week) AS share FROM cardo.gold_artifact_usage WHERE week >= today() - 21 GROUP BY kind, artifact FORMAT JSONEachRow")
	if err != nil {
		return
	}
	reach := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(body), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var u struct {
			Kind     string  `json:"kind"`
			Artifact string  `json:"artifact"`
			People   float64 `json:"people"`
			Share    float64 `json:"share"`
		}
		if json.Unmarshal([]byte(line), &u) != nil {
			continue
		}
		if int(u.People) < floor {
			reach[u.Kind+"\x00"+u.Artifact] = "—"
			continue
		}
		reach[u.Kind+"\x00"+u.Artifact] = fmt.Sprintf("%d · %.0f%%", int(u.People), u.Share*100)
	}
	for i := range rows {
		if v, ok := reach[rows[i].Kind+"\x00"+strings.ToLower(rows[i].Name)]; ok {
			rows[i].Reach = v
		}
	}
}

func (s *Server) handleCatalogDetail(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	d, err := s.catalogDetail(r.Context(), slug)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, d)
}

func (s *Server) catalogDetail(ctx context.Context, slug string) (*catalogDetail, error) {
	body, err := s.ch(ctx, "SELECT version, kind, name, owner, status, content_hash, path, note, toString(created_at) AS created, toString(approved_at) AS approved FROM cardo.settings_artifact_versions WHERE slug = '"+escapeSQL(slug)+"' ORDER BY version DESC FORMAT JSONEachRow")
	if err != nil {
		return nil, err
	}
	var d catalogDetail
	for _, line := range strings.Split(strings.TrimSpace(body), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var v struct {
			Version  int    `json:"version"`
			Kind     string `json:"kind"`
			Name     string `json:"name"`
			Owner    string `json:"owner"`
			Status   string `json:"status"`
			Hash     string `json:"content_hash"`
			Path     string `json:"path"`
			Note     string `json:"note"`
			Created  string `json:"created"`
			Approved string `json:"approved"`
		}
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			return nil, err
		}
		d.Versions = append(d.Versions, catalogVersion{Version: v.Version, Status: v.Status, Hash: v.Hash, Note: v.Note, CreatedAt: shortTime(v.Created), ApprovedAt: shortTime(v.Approved)})
		if len(d.Versions) == 1 {
			d.Slug, d.Kind, d.Name, d.Owner, d.Status = slug, v.Kind, v.Name, v.Owner, v.Status
			d.Version, d.Hash, d.Note, d.Path = v.Version, v.Hash, v.Note, v.Path
			d.CreatedAt, d.ApprovedAt = shortTime(v.Created), shortTime(v.Approved)
		}
	}
	if len(d.Versions) == 0 {
		return nil, fmt.Errorf("no such artifact: %s", slug)
	}
	if b, err := os.ReadFile(filepath.Join(s.opts.Dir, d.Path)); err == nil {
		d.Body = string(b)
	}
	return &d, nil
}

// readUpload is the one place a multipart upload becomes a catalog.Upload.
func readUpload(r *http.Request) (catalog.Upload, error) {
	if err := r.ParseMultipartForm(maxArtifactBytes + (64 << 10)); err != nil {
		return catalog.Upload{}, fmt.Errorf("expected a multipart form with file, kind, slug, name, owner")
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return catalog.Upload{}, fmt.Errorf("the form has no file field")
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, maxArtifactBytes+1))
	if err != nil {
		return catalog.Upload{}, err
	}
	up := catalog.Upload{
		Kind:  catalog.Kind(strings.TrimSpace(r.FormValue("kind"))),
		Slug:  strings.ToLower(strings.TrimSpace(r.FormValue("slug"))),
		Name:  strings.TrimSpace(r.FormValue("name")),
		Owner: strings.TrimSpace(r.FormValue("owner")),
		Body:  body,
	}
	if header != nil {
		up.FileName = header.Filename
		if up.Name == "" {
			up.Name = strings.TrimSuffix(header.Filename, filepath.Ext(header.Filename))
		}
	}
	return up, nil
}

// handleCatalogValidate answers "would this be accepted, and what changed" without writing anything.
// The console uses it before the save button, so the diff is a review point rather than a note.
func (s *Server) handleCatalogValidate(w http.ResponseWriter, r *http.Request) {
	up, err := readUpload(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	problems := catalog.Validate(up, maxArtifactBytes)
	diff := ""
	if prev, err := s.approvedBody(r.Context(), up.Slug); err == nil {
		diff = catalog.DiffSummary([]byte(prev.body), up.Body)
	}
	next, _ := s.nextVersion(r.Context(), up.Slug)
	writeJSON(w, map[string]any{"problems": problems, "diff": diff, "version": next})
}

// handleCatalogUpload validates, writes the file as a new immutable version, and records a draft.
func (s *Server) handleCatalogUpload(w http.ResponseWriter, r *http.Request) {
	up, err := readUpload(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	problems := catalog.Validate(up, maxArtifactBytes)
	if len(problems) > 0 {
		writeJSONStatus(w, http.StatusUnprocessableEntity, map[string]any{"problems": problems})
		return
	}

	// The diff is against the approved version, because that is what the views currently call the
	// organization's standard.
	var beforeHash, diff string
	if prev, err := s.approvedBody(r.Context(), up.Slug); err == nil {
		beforeHash = prev.hash
		diff = catalog.DiffSummary([]byte(prev.body), up.Body)
	}

	s.catalogMu.Lock()
	defer s.catalogMu.Unlock()
	next, err := s.nextVersion(r.Context(), up.Slug)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	rel := filepath.Join("catalog", string(up.Kind), up.Slug, strconv.Itoa(next), "content"+up.Kind.Ext())
	abs := filepath.Join(s.opts.Dir, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(abs, up.Body, 0o644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sum := sha256.Sum256(up.Body)
	hash := hex.EncodeToString(sum[:])
	note := strings.TrimSpace(r.FormValue("note"))
	insert := fmt.Sprintf(`INSERT INTO cardo.settings_artifact_versions
		(slug, version, kind, name, owner, status, content_hash, path, note)
		VALUES ('%s', %d, '%s', '%s', '%s', 'draft', '%s', '%s', '%s')`,
		escapeSQL(up.Slug), next, escapeSQL(string(up.Kind)), escapeSQL(up.Name), escapeSQL(up.Owner), hash, escapeSQL(rel), escapeSQL(strings.TrimSpace(note)))
	if _, err := s.ch(r.Context(), insert); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if err := s.audit(r.Context(), "upload", up.Slug, next, beforeHash, hash, diff); err != nil {
		http.Error(w, "the version was saved but its audit line failed: "+err.Error(), http.StatusBadGateway)
		return
	}

	d, err := s.catalogDetail(r.Context(), up.Slug)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSONStatus(w, http.StatusCreated, d)
}

func (s *Server) approvedBody(ctx context.Context, slug string) (struct{ hash, body string }, error) {
	var out struct{ hash, body string }
	body, err := s.ch(ctx, "SELECT content_hash, path FROM cardo.settings_artifact_versions WHERE slug = '"+escapeSQL(slug)+"' AND status = 'approved' ORDER BY version DESC LIMIT 1 FORMAT JSONEachRow")
	if err != nil || strings.TrimSpace(body) == "" {
		return out, fmt.Errorf("no approved version")
	}
	var row struct {
		Hash string `json:"content_hash"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(firstLine(body)), &row); err != nil {
		return out, err
	}
	b, err := os.ReadFile(filepath.Join(s.opts.Dir, row.Path))
	out.hash, out.body = row.Hash, string(b)
	return out, err
}

func (s *Server) nextVersion(ctx context.Context, slug string) (int, error) {
	body, err := s.ch(ctx, "SELECT max(version) FROM cardo.settings_artifact_versions WHERE slug = '"+escapeSQL(slug)+"'")
	if err != nil {
		return 0, err
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return 1, nil
	}
	n, err := strconv.Atoi(body)
	if err != nil {
		return 0, err
	}
	return n + 1, nil
}

// handleCatalogStatus moves a version to approved or retired. Approval is what makes an artifact the
// organization's standard; retirement keeps the history and stops the label.
func (s *Server) handleCatalogStatus(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	var body struct {
		Action  string `json:"action"` // approve | retire
		Version int    `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `want {"action":"approve","version":2}`, http.StatusBadRequest)
		return
	}
	if body.Action != "approve" && body.Action != "retire" && body.Action != "rollback" {
		http.Error(w, "action must be approve, retire or rollback", http.StatusBadRequest)
		return
	}
	if body.Version <= 0 {
		http.Error(w, "version is required", http.StatusBadRequest)
		return
	}
	status := "approved"
	if body.Action == "retire" {
		status = "retired"
	}
	beforeStatus, beforeHash, _ := s.versionState(r.Context(), slug, body.Version)
	stmt := fmt.Sprintf("ALTER TABLE cardo.settings_artifact_versions UPDATE status = '%s', approved_at = now64(3) WHERE slug = '%s' AND version = %d SETTINGS mutations_sync = 1",
		status, escapeSQL(slug), body.Version)
	if _, err := s.ch(r.Context(), stmt); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if body.Action == "rollback" {
		// A rollback is "approve this older version and retire anything newer": history stays.
		_, _ = s.ch(r.Context(), fmt.Sprintf("ALTER TABLE cardo.settings_artifact_versions UPDATE status = 'retired' WHERE slug = '%s' AND version > %d AND status = 'approved' SETTINGS mutations_sync = 1", escapeSQL(slug), body.Version))
	}
	if err := s.audit(r.Context(), body.Action, slug, body.Version, beforeHash, beforeHash, beforeStatus+" → "+status); err != nil {
		http.Error(w, "the status changed but its audit line failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	d, err := s.catalogDetail(r.Context(), slug)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, d)
}

// audit writes the line the console shows under "Recent changes".
func (s *Server) audit(ctx context.Context, action, object string, version int, before, after, note string) error {
	_, err := s.ch(ctx, fmt.Sprintf(`INSERT INTO cardo.settings_changes (action, object, version, before_hash, after_hash, note)
		VALUES ('%s', '%s', %d, '%s', '%s', '%s')`,
		escapeSQL(action), escapeSQL(object), version, escapeSQL(before), escapeSQL(after), escapeSQL(note)))
	return err
}

func writeJSONStatus(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// versionState reads the status and hash before a mutation, so the audit line can say what changed.
func (s *Server) versionState(ctx context.Context, slug string, version int) (status, hash string, err error) {
	body, err := s.ch(ctx, fmt.Sprintf("SELECT status, content_hash FROM cardo.settings_artifact_versions WHERE slug = '%s' AND version = %d LIMIT 1 FORMAT JSONEachRow", escapeSQL(slug), version))
	if err != nil || strings.TrimSpace(body) == "" {
		return "", "", err
	}
	var row struct {
		Status string `json:"status"`
		Hash   string `json:"content_hash"`
	}
	if err := json.Unmarshal([]byte(firstLine(body)), &row); err != nil {
		return "", "", err
	}
	return row.Status, row.Hash, nil
}
