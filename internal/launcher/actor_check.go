package launcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// actorTypes are the only values the Claude Code Analytics API documents: a person authenticating
// with OAuth (user_actor) or a person authenticating with an API key (api_actor). Both are people.
// A service run is labelled by the collector (cardo.actor), not by the Admin API, so any third
// value here means the upstream schema moved and the adapter must be looked at before its rows are
// trusted (risk #12).
var actorTypes = map[string]bool{"user_actor": true, "api_actor": true}

func checkActorTypes(ctx context.Context, opts Options) (Result, error) {
	r := Result{ID: "actors", Name: "ACTOR TYPES"}
	env, err := ReadDotEnv(filepath.Join(opts.Dir, ".env"))
	if err != nil {
		if os.IsNotExist(err) {
			r.Pending = true
			r.Fix = "Run cardo up once; it writes .env and starts the stack."
			return r, nil
		}
		return r, err
	}
	body, _, err := chQuery(ctx, opts.url("8124", ""), env["CLICKHOUSE_USER"], env["CLICKHOUSE_PASSWORD"],
		"SELECT actor_type, count() FROM cardo.bronze_actor_day GROUP BY actor_type FORMAT TSV")
	if err != nil {
		return r, err
	}
	lines := strings.Split(strings.TrimSpace(body), "\n")
	if strings.TrimSpace(body) == "" {
		r.Pending = true
		r.Fix = "Poll the Admin API once; the check will name every actor type the API returns."
		return r, nil
	}
	var known, unknown []string
	for _, line := range lines {
		parts := strings.Split(strings.TrimSpace(line), "\t")
		if len(parts) != 2 {
			continue
		}
		kind, n := parts[0], parts[1]
		known = append(known, fmt.Sprintf("%s %s", kind, n))
		if !actorTypes[kind] {
			unknown = append(unknown, kind)
		}
	}
	r.Value = strings.Join(known, " · ")
	if len(unknown) > 0 {
		r.Cause = "the Admin API returned actor types the adapter does not know: " + strings.Join(unknown, ", ")
		r.Fix = "Update internal/source/console's actor mapping and the fleet view before counting these rows as people."
		return r, nil
	}
	r.OK = true
	return r, nil
}
