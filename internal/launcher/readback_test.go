package launcher

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// probeEnv stands in for the collector and ClickHouse, and records what the installer sent.
type probeEnv struct {
	mu       sync.Mutex
	bodies   []string
	chAnswer func(sql string) string
}

func (p *probeEnv) urls() func(port, path string) string {
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		p.mu.Lock()
		p.bodies = append(p.bodies, r.URL.Path+" "+string(b))
		p.mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"partialSuccess":{}}`))
	}))
	ch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		p.mu.Lock()
		p.bodies = append(p.bodies, "ch "+string(b))
		answer := p.chAnswer(string(b))
		p.mu.Unlock()
		_, _ = w.Write([]byte(answer))
	}))
	return func(port, path string) string {
		switch port {
		case "8124":
			return ch.URL + path
		case "4319", "8089":
			return collector.URL + path
		}
		return "http://127.0.0.1:" + port + path
	}
}

func TestDataStepSendsAUniqueProbeAndReadsItBack(t *testing.T) {
	env := &probeEnv{chAnswer: func(sql string) string { return "1\n" }}
	opts := Options{Dir: t.TempDir(), URLFn: env.urls(), PollTimeout: time.Second}
	writeEnvFile(t, opts.Dir)

	res, err := RunStep(context.Background(), opts, "data")
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Fatalf("data step failed: %s / %s", res.Cause, res.Fix)
	}
	if !strings.Contains(res.Value, "logs 1") || !strings.Contains(res.Value, "hooks 1") {
		t.Errorf("value = %q, want both counts", res.Value)
	}

	env.mu.Lock()
	defer env.mu.Unlock()
	var sawLog, sawHook bool
	for _, b := range env.bodies {
		if strings.Contains(b, "/v1/logs") && strings.Contains(b, probePrefix) {
			sawLog = true
		}
		if strings.Contains(b, "/v1/hooks") && strings.Contains(b, probePrefix) {
			sawHook = true
		}
	}
	if !sawLog || !sawHook {
		t.Fatalf("probe was not marked as the installer's: %v", env.bodies)
	}
}

func TestProbeDeletesItsOwnRows(t *testing.T) {
	env := &probeEnv{chAnswer: func(sql string) string {
		if strings.HasPrefix(sql, "ALTER TABLE") {
			return ""
		}
		return "1\n"
	}}
	opts := Options{Dir: t.TempDir(), URLFn: env.urls(), PollTimeout: time.Second}
	writeEnvFile(t, opts.Dir)

	res, err := RunStep(context.Background(), opts, "data")
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Fatalf("data step failed: %s", res.Cause)
	}
	if !strings.Contains(res.Value, "removed") {
		t.Errorf("value = %q, want it to say the probe rows were removed", res.Value)
	}
	env.mu.Lock()
	defer env.mu.Unlock()
	deletes := 0
	for _, b := range env.bodies {
		b = strings.TrimPrefix(b, "ch ")
		if strings.HasPrefix(b, "ALTER TABLE cardo.bronze_otel_logs DELETE") || strings.HasPrefix(b, "ALTER TABLE cardo.bronze_hook_events DELETE") {
			deletes++
		}
	}
	if deletes != 2 {
		t.Fatalf("deletes = %d, want both tables cleaned", deletes)
	}
}

func TestFieldsWaitsForARealSession(t *testing.T) {
	env := &probeEnv{chAnswer: func(sql string) string { return "" }} // JSONEachRow with no rows
	opts := Options{Dir: t.TempDir(), URLFn: env.urls(), PollTimeout: time.Second}
	writeEnvFile(t, opts.Dir)

	res, err := RunStep(context.Background(), opts, "fields")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Pending || res.OK {
		t.Fatalf("fields = %+v, want pending", res)
	}
	if res.Cause != "" {
		t.Errorf("pending step must not carry a failure cause, got %q", res.Cause)
	}
	if !strings.Contains(res.Fix, "Claude Code") {
		t.Errorf("fix = %q, want it to name the action", res.Fix)
	}
}

func TestFieldsNamesWhatTheViewsAreMissing(t *testing.T) {
	env := &probeEnv{chAnswer: func(sql string) string {
		return `{"id":"3f9a1c2d4e5f","pseudonym":"","app_version":"2.1.281","has_otel":1,"has_hooks":0,"prompts":0}` + "\n"
	}}
	opts := Options{Dir: t.TempDir(), URLFn: env.urls(), PollTimeout: time.Second}
	writeEnvFile(t, opts.Dir)

	res, err := RunStep(context.Background(), opts, "fields")
	if err != nil {
		t.Fatal(err)
	}
	if res.OK {
		t.Fatal("fields passed with no pseudonym, no hooks and no prompts")
	}
	for _, want := range []string{"pseudonym", "hook rows", "prompt_id"} {
		if !strings.Contains(res.Cause, want) {
			t.Errorf("cause %q does not name %q", res.Cause, want)
		}
	}
}

func TestFieldsPassesACompleteSession(t *testing.T) {
	env := &probeEnv{chAnswer: func(sql string) string {
		return `{"id":"3f9a1c2d4e5f","pseudonym":"p_abc","app_version":"2.1.281","has_otel":1,"has_hooks":1,"prompts":3}` + "\n"
	}}
	opts := Options{Dir: t.TempDir(), URLFn: env.urls(), PollTimeout: time.Second}
	writeEnvFile(t, opts.Dir)

	res, err := RunStep(context.Background(), opts, "fields")
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || !strings.Contains(res.Value, "3f9a1c2d") {
		t.Fatalf("fields = %+v, want a pass naming the session", res)
	}
}

func TestDataStepFailsWhenNothingArrives(t *testing.T) {
	env := &probeEnv{chAnswer: func(sql string) string { return "0\n" }}
	opts := Options{Dir: t.TempDir(), URLFn: env.urls(), PollTimeout: 60 * time.Millisecond}
	writeEnvFile(t, opts.Dir)

	res, err := RunStep(context.Background(), opts, "data")
	if err != nil {
		t.Fatal(err)
	}
	if res.OK {
		t.Fatal("data step passed while both counts were zero")
	}
	if !strings.Contains(res.Cause, "accepted") {
		t.Errorf("cause = %q, want it to say the probes were accepted but nothing arrived", res.Cause)
	}
	if res.Fix == "" {
		t.Error("no recovery for an empty read-back")
	}
}

func TestSessionStepFindsRealSessionsOnly(t *testing.T) {
	env := &probeEnv{chAnswer: func(sql string) string {
		if strings.Contains(sql, "NOT LIKE") {
			return "3\n"
		}
		return "0\n"
	}}
	opts := Options{Dir: t.TempDir(), URLFn: env.urls(), PollTimeout: time.Second}
	writeEnvFile(t, opts.Dir)

	res, err := RunStep(context.Background(), opts, "session")
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || !strings.Contains(res.Value, "3") {
		t.Fatalf("session step = %+v, want 3 hook events", res)
	}
}

func TestSessionStepExplainsAnEmptyStack(t *testing.T) {
	env := &probeEnv{chAnswer: func(sql string) string { return "0\n" }}
	opts := Options{Dir: t.TempDir(), URLFn: env.urls(), PollTimeout: time.Second}
	writeEnvFile(t, opts.Dir)

	res, err := RunStep(context.Background(), opts, "session")
	if err != nil {
		t.Fatal(err)
	}
	if res.OK {
		t.Fatal("session step passed with no sessions")
	}
	if !strings.Contains(res.Fix, "claude") {
		t.Errorf("fix = %q, want it to name the action", res.Fix)
	}
}

// writeEnvFile gives the read-back the credentials it queries with.
func writeEnvFile(t *testing.T, dir string) {
	t.Helper()
	secrets := Secrets{Salt: strings.Repeat("a", 64), ClickHousePassword: "ch-pass", GrafanaPassword: "gr-pass"}
	if err := WriteDotEnv(dir+"/.env", nil, secrets); err != nil {
		t.Fatal(err)
	}
}
