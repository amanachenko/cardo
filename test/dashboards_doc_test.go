package test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

// docs/design/dashboards.md says, for every chart, what it asks, who it is for, what it reads,
// what it cannot show and how far it has been checked, so that any chart can be challenged later.
// It is only worth that while it lists the charts the dashboards have: a panel added with no entry,
// or an entry left behind by a panel that went, fails here.
//
// The document mirrors each dashboard. A "## <dashboard title>" section holds a "### <row title>"
// for each row and a "#### <panel title>" for each panel, under its row's heading. A text panel is
// a panel, and every panel's entry says its status. Other sections may use "###" as they like, but
// only a dashboard's section may hold "####" entries, so the section of a dashboard that was
// renamed or removed fails too.
func TestDashboardsDocumentListsEveryPanel(t *testing.T) {
	root := repoRoot(t)
	b, err := os.ReadFile(filepath.Join(root, "docs", "design", "dashboards.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := readDashboardsDoc(string(b))

	documented := map[string]bool{}
	for _, path := range jsonFiles(t, filepath.Join(root, "dashboards")) {
		rel, _ := filepath.Rel(root, path)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var d struct {
			Title  string      `json:"title"`
			Panels []gridPanel `json:"panels"`
		}
		if err := json.Unmarshal(raw, &d); err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		documented[d.Title] = true
		s, ok := doc[d.Title]
		if !ok {
			t.Errorf("docs/design/dashboards.md has no %q section for %s", "## "+d.Title, rel)
			continue
		}
		want := panelLayout(d.Panels)
		for _, w := range want {
			if !slices.Contains(s.entries, w) {
				t.Errorf("docs/design/dashboards.md, %q: %s is on %s and has no entry", d.Title, w, rel)
			}
		}
		for _, e := range s.entries {
			if !slices.Contains(want, e) {
				t.Errorf("docs/design/dashboards.md, %q: %s has an entry and is not on %s", d.Title, e, rel)
			}
		}
		for _, e := range s.noStatus {
			t.Errorf("docs/design/dashboards.md, %q: %s says no **Status:**", d.Title, e)
		}
	}
	for title, s := range doc {
		if !documented[title] && s.panels > 0 {
			t.Errorf("docs/design/dashboards.md lists panels under %q, which is no dashboard's title", "## "+title)
		}
	}
}

type gridPanel struct {
	Type    string `json:"type"`
	Title   string `json:"title"`
	GridPos struct {
		X int `json:"x"`
		Y int `json:"y"`
	} `json:"gridPos"`
	Panels []gridPanel `json:"panels"`
}

// docEntry is a row's heading, or a panel's under its row. A panel above the first row has no row.
type docEntry struct {
	row, panel string
}

func (e docEntry) String() string {
	if e.panel == "" {
		return fmt.Sprintf("the row %q", "### "+e.row)
	}
	if e.row == "" {
		return fmt.Sprintf("the panel %q, above every row", "#### "+e.panel)
	}
	return fmt.Sprintf("the panel %q under %q", "#### "+e.panel, "### "+e.row)
}

// panelLayout lists a dashboard's rows and panels as Grafana draws them: top to bottom, and left to
// right. A panel belongs to the row above it, and a collapsed row keeps its panels inside it.
func panelLayout(panels []gridPanel) []docEntry {
	sorted := slices.Clone(panels)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i].GridPos, sorted[j].GridPos
		return a.Y < b.Y || a.Y == b.Y && a.X < b.X
	})
	var out []docEntry
	row := ""
	for _, p := range sorted {
		if p.Type != "row" {
			out = append(out, docEntry{row, p.Title})
			continue
		}
		row = p.Title
		out = append(out, docEntry{row: row})
		for _, c := range p.Panels {
			out = append(out, docEntry{row, c.Title})
		}
	}
	return out
}

type docSection struct {
	entries  []docEntry
	panels   int
	noStatus []docEntry
}

// readDashboardsDoc reads each "##" section's "###" and "####" headings, outside code blocks, and
// which "####" entries have no status line before the next heading.
func readDashboardsDoc(md string) map[string]*docSection {
	out := map[string]*docSection{}
	var s *docSection
	var row string
	var entry *docEntry
	status, fenced := false, false
	closeEntry := func() {
		if entry != nil && !status {
			s.noStatus = append(s.noStatus, *entry)
		}
		entry = nil
	}
	for _, line := range strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "```") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		switch {
		case strings.HasPrefix(line, "## "):
			if s != nil {
				closeEntry()
			}
			s = &docSection{}
			out[strings.TrimSpace(line[3:])] = s
			row = ""
		case s == nil:
		case strings.HasPrefix(line, "### "):
			closeEntry()
			row = strings.TrimSpace(line[4:])
			s.entries = append(s.entries, docEntry{row: row})
		case strings.HasPrefix(line, "#### "):
			closeEntry()
			e := docEntry{row, strings.TrimSpace(line[5:])}
			s.entries = append(s.entries, e)
			s.panels++
			entry, status = &e, false
		case strings.Contains(line, "**Status:**"):
			status = true
		}
	}
	if s != nil {
		closeEntry()
	}
	return out
}
