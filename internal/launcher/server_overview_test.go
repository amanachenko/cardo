package launcher

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/amanachenko/cardo/internal/retention"
)

// sessionStub answers the two queries the session card makes.
func sessionStub(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	if err := WriteDotEnv(dir+"/.env", map[string]string{"CLICKHOUSE_USER": "cardo", "CLICKHOUSE_PASSWORD": "pw"},
		Secrets{Salt: strings.Repeat("ab", 32), ClickHousePassword: "pw", GrafanaPassword: "gr"}); err != nil {
		t.Fatal(err)
	}
	ch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b bytes.Buffer
		_, _ = b.ReadFrom(r.Body)
		sql := b.String()
		switch {
		case strings.Contains(sql, "silver_artifact_load"):
			_, _ = w.Write([]byte("{\"name\":\"review-pr\",\"kind\":\"skill\"}\n"))
		case strings.Contains(sql, "silver_session"):
			_, _ = w.Write([]byte("{\"session_id\":\"abc123def456\",\"app_version\":\"2.1.289\",\"started\":\"2026-10-18 09:00:00\",\"last\":\"2026-10-18 09:42:00\",\"ended\":1,\"end_reason\":\"clear\",\"has_otel\":1,\"has_hooks\":1,\"prompts\":7}\n"))
		default:
			_, _ = w.Write([]byte(""))
		}
	}))
	t.Cleanup(ch.Close)
	s, err := NewServer(Options{Dir: dir, Out: &bytes.Buffer{}, Version: "test", URLFn: func(port, path string) string { return ch.URL + path }})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSessionCardCarriesTheProofWithoutIdentity(t *testing.T) {
	s := sessionStub(t)
	card, err := s.latestSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if card == nil {
		t.Fatal("no card")
	}
	if card.AppVersion != "2.1.289" || !card.HasOTel || !card.HasHooks || card.Prompts != 7 {
		t.Fatalf("card = %+v", card)
	}
	if len(card.Artifacts) != 1 || card.Artifacts[0] != "skill review-pr" {
		t.Errorf("artifacts = %v", card.Artifacts)
	}
	if strings.Contains(card.ID, "@") {
		t.Errorf("session id carries an address: %q", card.ID)
	}
}

func TestSettingsRefuseAFloorBelowFive(t *testing.T) {
	s, _ := newTestServer(t)
	body := strings.NewReader(`{"minGroupSize":3}`)
	req := httptest.NewRequest(http.MethodPost, "/api/settings", body)
	req.Header.Set("X-Cardo-Token", s.Token())
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "below five") {
		t.Errorf("body = %q", rec.Body.String())
	}
}

func TestSettingsWriteOrgArtifactsReachesEnv(t *testing.T) {
	s := sessionStub(t)
	body := strings.NewReader(`{"orgArtifacts":"review-pr, deploy-check"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/settings", body)
	req.Header.Set("X-Cardo-Token", s.Token())
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	env, err := ReadDotEnv(s.opts.Dir + "/.env")
	if err != nil {
		t.Fatal(err)
	}
	if env["CARDO_ORG_ARTIFACTS"] != "review-pr, deploy-check" {
		t.Errorf("CARDO_ORG_ARTIFACTS = %q", env["CARDO_ORG_ARTIFACTS"])
	}
	var view settingsView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
}

// The effective window is 90 days when the organization has chosen none, and the console says so
// (ADR-0053). A number with no history is a guess.
func TestRetentionShowsTheDefaultWhenNothingWasChosen(t *testing.T) {
	note := retentionNote(retention.DefaultDays, true)
	if !strings.Contains(note, "90 (default)") {
		t.Errorf("note = %q, want it to say 90 (default)", note)
	}
	if !strings.Contains(note, "before and after a tool is rolled out") {
		t.Errorf("note = %q, want it to explain why 90 days", note)
	}
}

// A window above the default keeps rows tied to a code name longer than the default says, and the
// engineers have to be told.
func TestRetentionAboveTheDefaultSaysToTellEngineers(t *testing.T) {
	note := retentionNote(180, false)
	if !strings.Contains(note, "tell engineers") {
		t.Errorf("note = %q, want it to say the engineers have to be told", note)
	}
	if !strings.Contains(note, "above the default of 90") {
		t.Errorf("note = %q, want it to name the default", note)
	}
}

// Zero keeps everything, and says that plainly: it is the choice that keeps personal data longest.
func TestRetentionZeroSaysWhatItMeans(t *testing.T) {
	note := retentionNote(0, false)
	if !strings.Contains(note, "TTL is taken off") {
		t.Errorf("note = %q, want it to say the TTL is taken off", note)
	}
}
