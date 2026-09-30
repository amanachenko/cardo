package test

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Every XML file under deploy/ must parse.
//
// This exists because of a real failure, not a hypothetical one: the ClickHouse quieting overlay
// shipped with "--" inside an XML comment, which the spec forbids. ClickHouse refused to start and
// crash-looped, and the only symptom was a container restarting. A config file that the server
// will reject is worth catching in CI rather than in a demo.
//
// Go's XML decoder rejects "--" in a comment for the same reason Poco does, so this test would
// have caught it.
func TestDeployXMLIsWellFormed(t *testing.T) {
	root := filepath.Join(repoRoot(t), "deploy")
	if _, err := os.Stat(root); os.IsNotExist(err) {
		t.Skip("no deploy/ directory yet")
	}

	var checked int
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".xml") {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		rel, _ := filepath.Rel(repoRoot(t), path)
		dec := xml.NewDecoder(f)
		for {
			_, err := dec.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Errorf("%s is not well-formed XML: %v\n"+
					"  ClickHouse refuses to start on a config it cannot parse, and the only\n"+
					"  symptom is a restarting container.", rel, err)
				return nil
			}
		}
		checked++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked == 0 {
		t.Fatal("no XML files were checked; this test would pass vacuously")
	}
}

// INV-3, at the layer where it would actually be breached.
//
// The SQL test guards the views. This guards the dashboards, which is where someone adds "just one
// per-person panel" without touching any SQL at all. A dashboard may read only gold views, and may
// not name a pseudonym.
func TestINV3_DashboardsReadOnlyCohortViews(t *testing.T) {
	dir := filepath.Join(repoRoot(t), "dashboards")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Skip("no dashboards/ directory yet")
	}

	var panels int
	for _, path := range jsonFiles(t, dir) {
		rel, _ := filepath.Rel(repoRoot(t), path)
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var dash struct {
			Panels []struct {
				Title   string `json:"title"`
				Type    string `json:"type"`
				Targets []struct {
					RawSQL string `json:"rawSql"`
				} `json:"targets"`
			} `json:"panels"`
		}
		if err := json.Unmarshal(b, &dash); err != nil {
			t.Errorf("%s is not valid JSON: %v", rel, err)
			continue
		}

		for _, p := range dash.Panels {
			for _, tgt := range p.Targets {
				sql := strings.ToLower(tgt.RawSQL)
				if sql == "" {
					continue
				}
				panels++

				if strings.Contains(sql, "pseudonym") {
					t.Errorf("INV-3 violated: %s panel %q names a pseudonym:\n  %s\n"+
						"  Dashboards are cohort-only. A per-person row on a dashboard is a\n"+
						"  ranking a manager can sort, which is the thing this project promises\n"+
						"  not to build.", rel, p.Title, tgt.RawSQL)
				}
				for _, layer := range []string{"bronze_", "silver_"} {
					if strings.Contains(sql, layer) {
						t.Errorf("INV-3 violated: %s panel %q reads %s directly.\n"+
							"  Dashboards read gold views. Bronze and silver carry a pseudonym\n"+
							"  column, so reading them is one SELECT away from a leaderboard.",
							rel, p.Title, strings.TrimSuffix(layer, "_"))
					}
				}
				if !strings.Contains(sql, "gold_") {
					t.Errorf("INV-3: %s panel %q reads no gold view:\n  %s",
						rel, p.Title, tgt.RawSQL)
				}
			}
		}
	}
	if panels == 0 {
		t.Fatal("no dashboard queries were checked; this test would pass vacuously")
	}
}

// ADR-0008 makes the volume denominator a product constraint rather than styling: an acceptance
// rate without one rewards timidity, because a session that proposes nothing is never rejected and
// scores 100%. A panel that charts a rate must chart the volume it is measured over beside it.
//
// Each rate the gold views publish is paired with the column that is its volume. The friction
// index's three components are all here (ADR-0008): edit rejection, permission waits and
// compactions.
//
// The volume must be an output column, selected bare or named with AS. Appearing in the SQL is
// not enough: a panel computes its rate from the volume, as in
// `sum(edit_rejections) / nullIf(sum(edit_proposals), 0)`, so the name is there whether or not
// the volume is charted.
var rateVolumes = []struct {
	rate   string
	volume *regexp.Regexp
	name   string
}{
	{"acceptance_rate", outputColumn(`[a-z_]*proposals`), "proposals"},
	{"rejection_rate", outputColumn(`[a-z_]*proposals`), "edit_proposals"},
	{"compactions_per_session", outputColumn(`sessions`), "sessions"},
	{"permission_wait_s", outputColumn(`permission_answered`), "permission_answered"},
	{"permission_prompts_per_prompt", outputColumn(`prompts`), "prompts"},
	{"people_share", outputColumn(`active_people`), "active_people"},
}

// outputColumn matches a column in a lower-cased SELECT list: after AS, or bare between commas,
// and followed by a comma or FROM.
func outputColumn(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?:\bas\s+|[\s,]|^)` + name + `\s*(?:,|\bfrom\b)`)
}

func TestADR0008_RatesAreChartedWithTheirDenominator(t *testing.T) {
	dir := filepath.Join(repoRoot(t), "dashboards")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Skip("no dashboards/ directory yet")
	}

	checked := map[string]int{}
	for _, path := range jsonFiles(t, dir) {
		rel, _ := filepath.Rel(repoRoot(t), path)
		b, _ := os.ReadFile(path)
		var dash struct {
			Panels []struct {
				Title   string `json:"title"`
				Targets []struct {
					RawSQL string `json:"rawSql"`
				} `json:"targets"`
			} `json:"panels"`
		}
		if err := json.Unmarshal(b, &dash); err != nil {
			continue
		}
		for _, p := range dash.Panels {
			for _, tgt := range p.Targets {
				sql := strings.ToLower(tgt.RawSQL)
				for _, r := range rateVolumes {
					if !strings.Contains(sql, r.rate) {
						continue
					}
					checked[r.rate]++
					if !r.volume.MatchString(sql) {
						t.Errorf("ADR-0008 violated: %s panel %q charts %s with no volume beside it:\n  %s\n"+
							"  Select %s too. A rate without the volume it is measured over rewards\n"+
							"  timidity: proposing nothing is never rejected.", rel, p.Title, r.rate, tgt.RawSQL, r.name)
					}
				}
			}
		}
	}
	// The acceptance rate is on the Admin API dashboard and the rejection rate on the collector's.
	// If either disappears from every panel, this test has stopped checking what it was written for.
	for _, rate := range []string{"acceptance_rate", "rejection_rate"} {
		if checked[rate] == 0 {
			t.Errorf("no panel charting %s was found to check; this test would pass vacuously", rate)
		}
	}
}

func jsonFiles(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".json") {
			return err
		}
		out = append(out, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// Every ClickHouse overlay under deploy/compose/clickhouse/ must be mounted by docker-compose.yml.
//
// This is another test that exists because of a real failure. config.d is mounted one file at a
// time rather than as a directory, because a directory bind would shadow the image's own
// docker_related_config.xml and leave the server unreachable. The consequence is that adding an
// overlay file is a two-step change, and doing only the first step fails in the worst possible way:
// the server starts cleanly, logs nothing unusual, reports itself healthy, and runs on stock
// defaults. The file is right there in the repo, so every later reader assumes it is in force.
//
// That is precisely how the idle-wakeup overlay was first "applied" -- written, container
// restarted, and silently ignored, discovered only because system.server_settings still showed the
// defaults it was supposed to have changed. A config that is present but not loaded is worse than
// one that is absent, because absence is visible.
func TestDeployOverlaysAreAllMounted(t *testing.T) {
	root := repoRoot(t)
	composePath := filepath.Join(root, "deploy", "compose", "docker-compose.yml")
	compose, err := os.ReadFile(composePath)
	if err != nil {
		t.Skip("no compose stack yet:", err)
	}
	text := string(compose)

	var checked int
	for _, dir := range []string{"config.d", "users.d"} {
		hostDir := filepath.Join(root, "deploy", "compose", "clickhouse", dir)
		entries, err := os.ReadDir(hostDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".xml") {
				continue
			}
			checked++
			// The exact string the compose file must contain: the source side of the bind.
			want := "./clickhouse/" + dir + "/" + e.Name() + ":"
			if !strings.Contains(text, want) {
				t.Errorf("deploy/compose/clickhouse/%s/%s is not mounted by docker-compose.yml.\n"+
					"  ClickHouse will start without it and run on defaults, reporting no error.\n"+
					"  Add to the clickhouse service's volumes:\n"+
					"    - ./clickhouse/%s/%s:/etc/clickhouse-server/%s/%s:ro",
					dir, e.Name(), dir, e.Name(), dir, e.Name())
			}
		}
	}
	if checked == 0 {
		t.Fatal("found no ClickHouse overlay files to check; this test is not testing anything")
	}
	t.Logf("checked %d ClickHouse overlay files against the compose mounts", checked)
}
