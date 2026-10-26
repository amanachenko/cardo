package launcher

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/amanachenko/cardo/internal/directory"
	"github.com/amanachenko/cardo/internal/pseudonym"
)

type teamRow struct {
	Team     string `json:"team"`
	People   int    `json:"people,omitempty"` // omitted below the floor
	Sessions int    `json:"sessions"`
}

type teamsView struct {
	Loaded      bool      `json:"loaded"`
	Batch       string    `json:"batch,omitempty"`
	LoadedAt    string    `json:"loadedAt,omitempty"`
	SourceHash  string    `json:"sourceHash,omitempty"`
	Rows        int       `json:"rows"`
	Teams       int       `json:"teams"`
	Sessions7d  int       `json:"sessions7d"`
	Mapped      int       `json:"mapped"`
	CoveragePct float64   `json:"coveragePct"`
	Unassigned  int       `json:"unassigned"`
	HiddenTeams int       `json:"hiddenTeams"`
	Floor       int       `json:"floor"`
	TeamRows    []teamRow `json:"teamRows"`
	Error       string    `json:"error,omitempty"`
}

func (s *Server) handleTeamsGet(w http.ResponseWriter, r *http.Request) {
	v, err := s.teamsView(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, v)
}

// teamsView answers "who is mapped" in numbers only. A team below the floor is not rendered at all.
func (s *Server) teamsView(ctx context.Context) (teamsView, error) {
	v := teamsView{Floor: 5, TeamRows: []teamRow{}}
	if body, err := s.ch(ctx, "SELECT min_group_size FROM cardo.settings_effective LIMIT 1"); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(body)); err == nil && n > 0 {
			v.Floor = n
		}
	}
	body, err := s.ch(ctx, "SELECT batch, toString(loaded_at) AS at, rows, teams, source_hash FROM cardo.settings_teams_batches ORDER BY loaded_at DESC LIMIT 1 FORMAT JSONEachRow")
	if err != nil {
		return v, err
	}
	if strings.TrimSpace(body) != "" {
		var b struct {
			Batch string `json:"batch"`
			At    string `json:"at"`
			Rows  int    `json:"rows"`
			Teams int    `json:"teams"`
			Hash  string `json:"source_hash"`
		}
		if err := json.Unmarshal([]byte(firstLine(body)), &b); err != nil {
			return v, err
		}
		v.Loaded, v.Batch, v.LoadedAt, v.Rows, v.Teams, v.SourceHash = true, b.Batch, shortTime(b.At), b.Rows, b.Teams, b.Hash
	}

	sessionFilter := "last_event_at > now() - INTERVAL 7 DAY AND session_id NOT LIKE '" + probePrefix + "%'"
	saltVersion := "v1"
	if env, err := ReadDotEnv(s.opts.Dir + "/.env"); err == nil {
		if v := strings.TrimSpace(env["CARDO_SALT_VERSION"]); v != "" {
			saltVersion = v
		}
	}
	if n, err := s.chInt(ctx, "SELECT count() FROM cardo.silver_session WHERE "+sessionFilter); err == nil {
		v.Sessions7d = n
	}
	if v.Loaded {
		in := "pseudonym IN (SELECT pseudonym FROM cardo.settings_teams WHERE batch = '" + escapeSQL(v.Batch) + "' AND salt_version = '" + escapeSQL(saltVersion) + "')"
		if n, err := s.chInt(ctx, "SELECT count() FROM cardo.silver_session WHERE "+sessionFilter+" AND "+in); err == nil {
			v.Mapped = n
		}
		if n, err := s.chInt(ctx, "SELECT uniqExact(pseudonym) FROM cardo.silver_session WHERE "+sessionFilter+" AND pseudonym IS NOT NULL AND pseudonym NOT IN (SELECT pseudonym FROM cardo.settings_teams WHERE batch = '"+escapeSQL(v.Batch)+"' AND salt_version = '"+escapeSQL(saltVersion)+"')"); err == nil {
			v.Unassigned = n
		}
		if v.Sessions7d > 0 {
			v.CoveragePct = float64(v.Mapped) / float64(v.Sessions7d) * 100
		}
		rows, err := s.ch(ctx, fmt.Sprintf(`SELECT t.team AS team, uniqExact(s.pseudonym) AS people, count() AS sessions
			FROM cardo.silver_session s INNER JOIN cardo.settings_teams t
			ON s.pseudonym = t.pseudonym AND t.batch = '%s' AND t.salt_version = s.salt_version
			WHERE s.%s GROUP BY t.team ORDER BY sessions DESC FORMAT JSONEachRow`, escapeSQL(v.Batch), sessionFilter))
		if err == nil {
			for _, line := range strings.Split(strings.TrimSpace(rows), "\n") {
				if strings.TrimSpace(line) == "" {
					continue
				}
				var tr struct {
					Team     string `json:"team"`
					People   int    `json:"people"`
					Sessions int    `json:"sessions"`
				}
				if json.Unmarshal([]byte(line), &tr) == nil {
					if tr.People < v.Floor {
						v.HiddenTeams++
						continue // a team of two people is a person
					}
					v.TeamRows = append(v.TeamRows, teamRow{Team: tr.Team, People: tr.People, Sessions: tr.Sessions})
				}
			}
		}
	}
	return v, nil
}

func (s *Server) chInt(ctx context.Context, sql string) (int, error) {
	body, err := s.ch(ctx, sql)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(body))
}

// handleTeamsPost previews a CSV, or applies it when the body says so. The preview hashes nothing.
func (s *Server) handleTeamsPost(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CSV   string `json:"csv"`
		Apply bool   `json:"apply"`
	}
	const maxCSV = 2 << 20
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `want {"csv":"email,team\n...","apply":false}`, http.StatusBadRequest)
		return
	}
	if len(body.CSV) > maxCSV {
		http.Error(w, "the file is larger than 2 MB; a directory of that size belongs in the identity provider, not in a paste field", http.StatusRequestEntityTooLarge)
		return
	}
	entries, preview := directory.Parse(body.CSV)
	ctx := r.Context()
	floor := 5
	if n, err := s.chInt(ctx, "SELECT min_group_size FROM cardo.settings_effective LIMIT 1"); err == nil && n > 0 {
		floor = n
	}
	if !body.Apply {
		writeJSON(w, map[string]any{"preview": preview, "belowFloor": directory.BelowFloor(preview, floor), "floor": floor})
		return
	}
	if len(preview.Malformed) > 0 {
		writeJSONStatus(w, http.StatusUnprocessableEntity, map[string]any{
			"preview": preview, "belowFloor": directory.BelowFloor(preview, floor),
			"problem": "fix the malformed lines before loading; nothing was written",
		})
		return
	}
	if len(entries) == 0 {
		http.Error(w, "the file has no rows", http.StatusBadRequest)
		return
	}

	env, err := ReadDotEnv(s.opts.Dir + "/.env")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	salt := strings.TrimSpace(env["CARDO_SALT"])
	if salt == "" {
		http.Error(w, "CARDO_SALT is not set in .env; the directory cannot be hashed", http.StatusInternalServerError)
		return
	}
	version := strings.TrimSpace(env["CARDO_SALT_VERSION"])
	if version == "" {
		version = "v1"
	}
	hasher, err := pseudonym.New(salt, version)
	if err != nil {
		http.Error(w, "the salt is not usable: "+err.Error(), http.StatusInternalServerError)
		return
	}
	rows, err := directory.Hash(entries, hasher)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	rnd := make([]byte, 4)
	_, _ = rand.Read(rnd)
	batch := time.Now().UTC().Format("20060102T150405") + "-" + hex.EncodeToString(rnd)
	sum := sha256.Sum256([]byte(body.CSV))

	// Rows first, the batch marker last: a crash between the two must not publish a partial mapping
	// as the current one. The marker is what makes a batch visible (ADR-0046).
	for start := 0; start < len(rows); start += 500 {
		end := start + 500
		if end > len(rows) {
			end = len(rows)
		}
		var b strings.Builder
		b.WriteString("INSERT INTO cardo.settings_teams (team, pseudonym, salt_version, batch) VALUES ")
		for i, row := range rows[start:end] {
			if i > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, "('%s','%s','%s','%s')", escapeSQL(row.Team), escapeSQL(row.Pseudonym), escapeSQL(version), batch)
		}
		if _, err := s.ch(ctx, b.String()); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
	}
	if _, err := s.ch(ctx, fmt.Sprintf("INSERT INTO cardo.settings_teams_batches (batch, source_hash, rows, teams) VALUES ('%s','%s',%d,%d)",
		batch, hex.EncodeToString(sum[:]), len(rows), preview.Teams)); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	// Keep the current and the previous generation; anything older is pruned so a weekly reload does
	// not grow a directory forever.
	if keep, err := s.ch(ctx, "SELECT batch FROM cardo.settings_teams_batches ORDER BY loaded_at DESC LIMIT 2 FORMAT TSV"); err == nil {
		var ids []string
		for _, line := range strings.Split(strings.TrimSpace(keep), "\n") {
			if id := strings.TrimSpace(line); id != "" {
				ids = append(ids, "'"+escapeSQL(id)+"'")
			}
		}
		if len(ids) == 2 {
			list := strings.Join(ids, ",")
			_, _ = s.ch(ctx, "ALTER TABLE cardo.settings_teams DELETE WHERE batch NOT IN ("+list+") SETTINGS mutations_sync = 1")
			_, _ = s.ch(ctx, "ALTER TABLE cardo.settings_teams_batches DELETE WHERE batch NOT IN ("+list+") SETTINGS mutations_sync = 1")
		}
	}
	if err := s.audit(ctx, "teams", batch, len(rows), "", hex.EncodeToString(sum[:]), fmt.Sprintf("%d people, %d teams", len(rows), preview.Teams)); err != nil {
		http.Error(w, "the directory was loaded but the audit line failed: "+err.Error(), http.StatusBadGateway)
		return
	}

	v, err := s.teamsView(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, v)
}
