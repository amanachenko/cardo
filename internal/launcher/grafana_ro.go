package launcher

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
)

// leadershipDB holds SQL SECURITY DEFINER views over the gold layer. The read-only user is granted
// SELECT on this database only, so it can answer leadership questions without ever touching a row
// that names a person or a session (ADR-0029, roadmap item 7).
const leadershipDB = "leadership"

// stepGrafanaRO creates the gold-only ClickHouse user and proves, live, that it cannot read bronze or
// silver. It is the check the roadmap requires before anyone except the operator gets a Grafana login.
func stepGrafanaRO(ctx context.Context, opts Options) (Result, error) {
	path := filepath.Join(opts.Dir, ".env")
	env, err := ReadDotEnv(path)
	if err != nil {
		return Result{
			ID: "gold", Name: "GOLD ACCESS", Value: "cannot read .env",
			Cause: "Reading " + path + " failed: " + err.Error(),
			Fix:   "Run the secrets step first, then this one.",
		}, nil
	}
	password := strings.TrimSpace(env["GRAFANA_RO_PASSWORD"])
	generated := false
	if password == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return Result{ID: "gold", Name: "GOLD ACCESS", Value: "cannot generate a password",
				Cause: err.Error(), Fix: "Retry; the system random source did not answer."}, nil
		}
		password = hex.EncodeToString(b)
		env["GRAFANA_RO_PASSWORD"] = password
		generated = true
	}

	admin := func(sql string) error {
		_, _, err := chQuery(ctx, opts.url("8124", ""), env["CLICKHOUSE_USER"], env["CLICKHOUSE_PASSWORD"], sql)
		return err
	}

	if generated {
		if err := WriteDotEnv(path, env, Secrets{}); err != nil {
			return Result{ID: "gold", Name: "GOLD ACCESS", Value: "cannot write .env",
				Cause: err.Error(), Fix: "Check that the directory is writable."}, nil
		}
	}

	// A read-only user may only see the gold layer. Views are DEFINER-rights so the grant does not
	// have to reach bronze or silver underneath.
	steps := []string{"CREATE DATABASE IF NOT EXISTS " + leadershipDB}
	names, _, err := chQuery(ctx, opts.url("8124", ""), env["CLICKHOUSE_USER"], env["CLICKHOUSE_PASSWORD"],
		"SELECT name FROM system.tables WHERE database = 'cardo' AND name LIKE 'gold_%' AND engine LIKE '%View%' ORDER BY name")
	if err != nil {
		return Result{ID: "gold", Name: "GOLD ACCESS", Value: "cannot list the gold views",
			Cause: err.Error(), Fix: "Check that ClickHouse answers on 8124 and the schema has been migrated."}, nil
	}
	var views []string
	for _, line := range strings.Split(strings.TrimSpace(names), "\n") {
		if v := strings.TrimSpace(line); v != "" {
			views = append(views, v)
		}
	}
	if len(views) == 0 {
		return Result{ID: "gold", Name: "GOLD ACCESS", Value: "no gold views",
			Cause: "The cardo database has no gold views to expose.",
			Fix:   "Run cardo migrate, then this step again."}, nil
	}
	for _, v := range views {
		steps = append(steps, fmt.Sprintf(
			"CREATE OR REPLACE VIEW %s.%s SQL SECURITY DEFINER AS SELECT * FROM cardo.%s", leadershipDB, v, v))
	}
	steps = append(steps,
		// No SETTINGS readonly here: Grafana sets max_execution_time per query, and a readonly user
		// cannot accept that. The boundary is the grant, not a session flag.
		"CREATE USER IF NOT EXISTS cardo_ro IDENTIFIED WITH sha256_password BY '"+password+"'",
		"ALTER USER cardo_ro IDENTIFIED WITH sha256_password BY '"+password+"'",
		"ALTER USER cardo_ro SETTINGS readonly = 0",
		"GRANT SELECT ON "+leadershipDB+".* TO cardo_ro",
	)
	for _, sql := range steps {
		if err := admin(sql); err != nil {
			return Result{
				ID: "gold", Name: "GOLD ACCESS", Value: "cannot prepare the read-only user",
				Cause: "ClickHouse rejected: " + firstLine(sql) + " — " + err.Error(),
				Fix:   "Check that the cardo user may manage access (CLICKHOUSE_DEFAULT_ACCESS_MANAGEMENT=1), then run this step again.",
			}, nil
		}
	}

	// The proof: the same credentials that Grafana will use are refused the raw layers and allowed
	// the gold layer. A refusal is the successful outcome, as the board says.
	denied := []string{"cardo.bronze_actor_day", "cardo.silver_session"}
	for _, table := range denied {
		if _, _, err := chQuery(ctx, opts.url("8124", ""), "cardo_ro", password, "SELECT count() FROM "+table); err == nil {
			return Result{
				ID: "gold", Name: "GOLD ACCESS", Value: "the read-only user can read " + table,
				Cause: "SELECT on " + table + " succeeded with the gold-only credentials.",
				Fix:   "Revoke the grant that allows it and run this step again; leadership logins must not see this table.",
			}, nil
		}
	}
	if _, _, err := chQuery(ctx, opts.url("8124", ""), "cardo_ro", password, "SELECT count() FROM "+leadershipDB+".gold_fleet_adoption"); err != nil {
		return Result{
			ID: "gold", Name: "GOLD ACCESS", Value: "the read-only user cannot read gold",
			Cause: "SELECT on " + leadershipDB + ".gold_fleet_adoption failed: " + err.Error(),
			Fix:   "Confirm the leadership views were created and the grant was applied, then run this step again.",
		}, nil
	}

	return Result{
		ID: "gold", Name: "GOLD ACCESS",
		Value: fmt.Sprintf("cardo_ro · %d gold views · bronze and silver refused", len(views)),
		OK:    true,
	}, nil
}

// checkGoldAccess is the read-only twin: it proves the boundary still holds without creating
// anything. It is what `cardo check` runs, and it waits for the user to exist.
func checkGoldAccess(ctx context.Context, opts Options) (Result, error) {
	env, err := ReadDotEnv(filepath.Join(opts.Dir, ".env"))
	if err != nil {
		return Result{ID: "gold", Name: "GOLD", Value: "cannot read .env", Cause: err.Error()}, nil
	}
	password := strings.TrimSpace(env["GRAFANA_RO_PASSWORD"])
	if password == "" {
		return Result{ID: "gold", Name: "GOLD", Value: "no read-only user yet", Pending: true,
			Fix: "Run cardo gold once to create the gold-only user and prove the refusal."}, nil
	}
	for _, table := range []string{"cardo.bronze_actor_day", "cardo.silver_session"} {
		if _, _, err := chQuery(ctx, opts.url("8124", ""), "cardo_ro", password, "SELECT count() FROM "+table); err == nil {
			return Result{ID: "gold", Name: "GOLD", Value: "read-only user can read " + table,
				Cause: "The gold-only user is allowed to read " + table + ".",
				Fix:   "Revoke that grant before anyone receives a leadership login."}, nil
		}
	}
	// Each view whole, not a count of one. A view made before a migration changed its gold view
	// still counts rows, and fails once a column it lists is read: after 019 removed two columns,
	// leadership.gold_cost_daily answered count() and refused SELECT *.
	names, _, err := chQuery(ctx, opts.url("8124", ""), env["CLICKHOUSE_USER"], env["CLICKHOUSE_PASSWORD"],
		"SELECT name FROM system.tables WHERE database = '"+leadershipDB+"' ORDER BY name")
	if err != nil {
		return Result{ID: "gold", Name: "GOLD", Value: "cannot list the leadership views",
			Cause: err.Error(), Fix: "Check that ClickHouse answers on 8124."}, nil
	}
	views := strings.Fields(names)
	if len(views) == 0 {
		return Result{ID: "gold", Name: "GOLD", Value: "no leadership views",
			Fix: "Run cardo gold to recreate the leadership views and the grant."}, nil
	}
	for _, v := range views {
		if _, _, err := chQuery(ctx, opts.url("8124", ""), "cardo_ro", password, "SELECT * FROM "+leadershipDB+"."+v+" LIMIT 0"); err != nil {
			return Result{ID: "gold", Name: "GOLD", Value: "read-only user cannot read " + leadershipDB + "." + v,
				Cause: err.Error(), Fix: "Run cardo gold to recreate the leadership views and the grant. " +
					"A view made before a migration changed it fails until then."}, nil
		}
	}
	return Result{ID: "gold", Name: "GOLD", Value: "cardo_ro · gold readable · bronze and silver refused", OK: true}, nil
}
