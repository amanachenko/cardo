package clickhouse

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// These tests run against a real ClickHouse and are skipped unless CARDO_CLICKHOUSE_URL is set:
//
//	CARDO_CLICKHOUSE_URL=... CARDO_CLICKHOUSE_USER=... CARDO_CLICKHOUSE_PASSWORD=... \
//	  go test ./internal/store/clickhouse/ -run TestRepo
//
// The view under test reads the collector path: a session with a pseudonym and a repository
// classification, an InstructionsLoaded hook for the same session, and the deployment settings
// the views read.

// seedFile is one instruction file a session loaded.
type seedFile struct {
	// name is the file's own name, as the collector would keep it.
	name string
	// org says the organization shipped that file, so its name is kept under
	// CARDO_ORG_ARTIFACTS whatever the instructions-names setting is (ADR-0026).
	org bool
}

// repoIdentitySession is one session, in one repository class, loading some instruction files.
type repoIdentitySession struct {
	// repoClass is what the collector classified the repository as: org, external or none.
	repoClass string
	// repo is what the collector kept: owner and name for an organization repository, the host
	// alone for an external one, empty for a session outside any repository.
	repo string
	// scope is Claude Code's own scope for the files: Project, User or Managed.
	scope string
	// files are the instruction files the session loaded.
	files []seedFile
	// people is how many engineers ran this session, each with their own pseudonym.
	people int
}

// seedRepoIdentity writes the sessions the way the collector would have stored them, and returns
// the names it used, so the test can clean up.
//
// The collector keeps two names, and this writes both: instructions_name for the organization's
// own artifacts (ADR-0026) and instructions_project_name for a repository's own files, which it
// keeps only when the organization asked for it (ADR-0035). namesMode says which mode the
// collector ran in.
func seedRepoIdentity(t *testing.T, st *Store, namesMode string, sessions []repoIdentitySession) []string {
	t.Helper()
	ctx := context.Background()
	stamp := fmt.Sprintf("repoid-%d", time.Now().UnixNano())

	var pseudos []string
	for si, s := range sessions {
		for p := 0; p < s.people; p++ {
			pseudo := fmt.Sprintf("%064x", time.Now().UnixNano()+int64(si*1000+p))
			pseudos = append(pseudos, pseudo)
			session := fmt.Sprintf("%s-sess-%d-%d", stamp, si, p)

			// The session's OTel rows: the pseudonym, and the repository the collector
			// classified. A session outside any repository has neither key.
			res := fmt.Sprintf("{'cardo.salt_version':'v1','user.pseudonym':'%s'", pseudo)
			if s.repo != "" || s.repoClass != "none" {
				res += fmt.Sprintf(",'cardo.repo':'%s','cardo.repo.class':'%s'", s.repo, s.repoClass)
			}
			res += "}"
			otel := fmt.Sprintf(
				"INSERT INTO cardo.bronze_otel_logs (Timestamp, ServiceName, Body, ResourceSchemaUrl, ResourceAttributes, ScopeSchemaUrl, ScopeName, ScopeVersion, ScopeAttributes, LogAttributes, EventName) " +
					"VALUES (toDateTime64('2026-09-15 10:00:00',9),'claude-code','','x'," +
					res + "," +
					"'','','',{}," +
					"{'session.id':'" + session + "','user.pseudonym':'" + pseudo + "'}," +
					"'claude_code.user_prompt')")
			if err := st.c.Exec(ctx, otel); err != nil {
				t.Fatalf("cannot seed the session: %v", err)
			}
		}

		// The instruction loads for that session, one row per file.
		for p := 0; p < s.people; p++ {
			session := fmt.Sprintf("%s-sess-%d-%d", stamp, si, p)
			for fi, f := range s.files {
				orgName := ""
				if f.org {
					orgName = f.name
				}
				projectName := ""
				if namesMode == "project" && s.scope == "Project" {
					projectName = f.name
				}
				hook := fmt.Sprintf(
					"INSERT INTO cardo.bronze_hook_events (Timestamp, ServiceName, Body, ResourceSchemaUrl, ResourceAttributes, ScopeSchemaUrl, ScopeName, ScopeVersion, ScopeAttributes, LogAttributes, EventName) " +
						"VALUES (toDateTime64('2026-09-15 10:00:00',9),'claude-code','','x',{},'','','',{}," +
						"{'session_id':'" + session + "','prompt_id':'p" + fmt.Sprint(p) + "-" + fmt.Sprint(fi) + "'," +
						"'instructions_name':'" + orgName + "','instructions_project_name':'" + projectName + "'," +
						"'instructions_file':'CLAUDE.md','load_reason':'session_start','memory_type':'" + s.scope + "'}," +
						"'InstructionsLoaded')")
				if err := st.c.Exec(ctx, hook); err != nil {
					t.Fatalf("cannot seed the instruction load: %v", err)
				}
			}
		}
	}
	t.Cleanup(func() {
		_ = st.c.Exec(ctx, fmt.Sprintf(
			"ALTER TABLE cardo.bronze_hook_events DELETE WHERE LogAttributes['session_id'] LIKE '%s-sess-%%' SETTINGS mutations_sync = 1", stamp))
		for _, p := range pseudos {
			_ = st.c.Exec(ctx, fmt.Sprintf(
				"ALTER TABLE cardo.bronze_otel_logs DELETE WHERE LogAttributes['user.pseudonym'] = '%s' SETTINGS mutations_sync = 1", p))
		}
	})
	return pseudos
}

// setInstructionsNames puts the deployment choice in force for the duration of the test, and puts
// back what was there before.
func setInstructionsNames(t *testing.T, st *Store, mode string) {
	t.Helper()
	ctx := context.Background()
	prev := query(t, st, "SELECT argMax(value, version) FROM cardo.settings WHERE key = 'instructions_names' FORMAT TabSeparated")
	t.Cleanup(func() {
		if err := st.ApplyViewSettings(context.Background(), ViewSettings{InstructionsNames: &prev}); err != nil {
			t.Errorf("could not restore the instructions-names setting: %v", err)
		}
	})
	if err := st.ApplyViewSettings(ctx, ViewSettings{InstructionsNames: &mode}); err != nil {
		t.Fatal(err)
	}
}

// The repository is a property of the session, and the view says which class it is in: the
// organization's own repositories by owner and name, everything else external plus its host, and a
// session outside any repository as none.
func TestRepoIdentityClassifiesEveryRepository(t *testing.T) {
	st := teamStore(t)
	setInstructionsNames(t, st, "project")
	seedRepoIdentity(t, st, "project", []repoIdentitySession{
		{repoClass: "org", repo: "github.com/acme/widgets", scope: "Project", people: 6,
			files: []seedFile{{name: "testing.md"}}},
		{repoClass: "external", repo: "gitlab.com", scope: "Project", people: 6,
			files: []seedFile{{name: "testing.md"}}},
		{repoClass: "none", scope: "Project", people: 6,
			files: []seedFile{{name: "testing.md"}}},
	})

	rows := rowsOf(t, st, "SELECT repo_class, repo, people FROM cardo.gold_repo_instructions "+
		"WHERE week = toDate('2026-09-14') ORDER BY repo_class")
	if len(rows) != 3 {
		t.Fatalf("the view holds %d rows, want one per repository class: %v", len(rows), rows)
	}
	want := []string{
		"external\tgitlab.com\t6",
		"none\tnone\t6",
		"org\tgithub.com/acme/widgets\t6",
	}
	for i, w := range want {
		got := rows[i]["repo_class"] + "\t" + rows[i]["repo"] + "\t" + rows[i]["people"]
		if got != w {
			t.Errorf("row %d = %q, want %q", i, got, w)
		}
	}
}

// The full URL is never stored, so no view can show one. The organization keeps its owner and
// name, and everything else keeps its host alone.
func TestRepoIdentityShowsNoFullURL(t *testing.T) {
	st := teamStore(t)
	setInstructionsNames(t, st, "project")
	seedRepoIdentity(t, st, "project", []repoIdentitySession{
		{repoClass: "org", repo: "github.com/acme/widgets", scope: "Project", people: 6,
			files: []seedFile{{name: "testing.md"}}},
		{repoClass: "external", repo: "gitlab.com", scope: "Project", people: 6,
			files: []seedFile{{name: "testing.md"}}},
	})

	got := query(t, st, "SELECT repo FROM cardo.gold_repo_instructions "+
		"WHERE week = toDate('2026-09-14') ORDER BY repo FORMAT TabSeparated")
	if strings.TrimSpace(got) == "" {
		t.Fatal("the view holds no rows, so the check below proves nothing")
	}
	for _, bad := range []string{"https://", "http://", "git@", ".git"} {
		if strings.Contains(got, bad) {
			t.Errorf("the view shows %q: %q", bad, got)
		}
	}
	// An external repository is its host alone: no owner, no path.
	for _, line := range strings.Split(strings.TrimSpace(got), "\n") {
		if strings.HasPrefix(line, "gitlab.com") && strings.Contains(line, "/") {
			t.Errorf("an external repository shows more than its host: %q", line)
		}
	}
}

// By default no file name is kept, so the number of distinct files is absent rather than zero:
// a zero would say "no files" where the truth is "not looked at".
func TestRepoInstructionsCountsNoFilesWithoutTheSetting(t *testing.T) {
	st := teamStore(t)
	setInstructionsNames(t, st, "")
	seedRepoIdentity(t, st, "", []repoIdentitySession{
		{repoClass: "org", repo: "github.com/acme/widgets", scope: "Project", people: 6,
			files: []seedFile{{name: "testing.md"}}},
	})

	got := query(t, st, "SELECT isNull(instruction_files), instruction_loads FROM cardo.gold_repo_instructions "+
		"WHERE week = toDate('2026-09-14') AND repo_class = 'org' FORMAT TabSeparated")
	if want := "1\t6"; got != want {
		t.Errorf("the file count is %q, want %q (absent, with the loads beside it)", got, want)
	}
}

// With the setting on, the number of distinct files is there, beside the number of loads.
func TestRepoInstructionsCountsTheFilesWithTheSetting(t *testing.T) {
	st := teamStore(t)
	setInstructionsNames(t, st, "project")
	seedRepoIdentity(t, st, "project", []repoIdentitySession{
		{repoClass: "org", repo: "github.com/acme/widgets", scope: "Project", people: 6,
			files: []seedFile{{name: "testing.md"}}},
	})

	got := query(t, st, "SELECT instruction_files, instruction_loads FROM cardo.gold_repo_instructions "+
		"WHERE week = toDate('2026-09-14') AND repo_class = 'org' FORMAT TabSeparated")
	if want := "1\t6"; got != want {
		t.Errorf("the file count is %q, want %q", got, want)
	}
}

// The names are shown for the organization's own repositories, and only there (ADR-0035).
func TestRepoInstructionFilesAreShownForTheOrganizationsRepositoriesOnly(t *testing.T) {
	st := teamStore(t)
	setInstructionsNames(t, st, "project")
	seedRepoIdentity(t, st, "project", []repoIdentitySession{
		{repoClass: "org", repo: "github.com/acme/widgets", scope: "Project", people: 6,
			files: []seedFile{{name: "testing.md"}}},
		{repoClass: "external", repo: "gitlab.com", scope: "Project", people: 6,
			files: []seedFile{{name: "testing.md"}}},
	})

	got := query(t, st, "SELECT repo, file_name FROM cardo.gold_repo_instruction_files "+
		"WHERE week = toDate('2026-09-14') ORDER BY repo FORMAT TabSeparated")
	if want := "github.com/acme/widgets\ttesting.md"; got != want {
		t.Errorf("the named files are %q, want %q", got, want)
	}
}

// A name one person uses identifies them, so it is counted as "other" until enough people use it
// (ADR-0029). The organization's own artifacts are exempt, the way they are everywhere else.
func TestRepoInstructionFilesApplyTheFivePersonRule(t *testing.T) {
	st := teamStore(t)
	setInstructionsNames(t, st, "project")
	seedRepoIdentity(t, st, "project", []repoIdentitySession{
		{repoClass: "org", repo: "github.com/acme/widgets", scope: "Project", people: 2,
			files: []seedFile{{name: "testing.md"}, {name: "acme-standards-v7.md", org: true}}},
	})

	got := query(t, st, "SELECT file_name FROM cardo.gold_repo_instruction_files "+
		"WHERE week = toDate('2026-09-14') ORDER BY file_name FORMAT TabSeparated")
	want := "acme-standards-v7.md\nother"
	if got != want {
		t.Errorf("the named files are %q, want %q", got, want)
	}
}

// A person's own files are never named, whatever the setting says (ADR-0035). The collector drops
// the name, and a view that took a name from anywhere else would name a person.
func TestRepoInstructionFilesNeverNameAPersonsOwnFiles(t *testing.T) {
	st := teamStore(t)
	setInstructionsNames(t, st, "project")
	seedRepoIdentity(t, st, "project", []repoIdentitySession{
		{repoClass: "org", repo: "github.com/acme/widgets", scope: "User", people: 6,
			files: []seedFile{{name: "personal-notes.md"}}},
	})

	got := query(t, st, "SELECT count() FROM cardo.gold_repo_instruction_files "+
		"WHERE week = toDate('2026-09-14') FORMAT TabSeparated")
	if want := "0"; got != want {
		t.Errorf("a person's own file is named %s time(s), want 0", got)
	}
}
