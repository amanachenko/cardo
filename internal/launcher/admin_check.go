package launcher

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// checkAdminData answers the other half of "what the views rely on": the Admin API path. It is
// pending until the first poll, and fails when the newest day is older than two days, which is how
// a dead poller looks from the outside.
func checkAdminData(ctx context.Context, opts Options) (Result, error) {
	r := Result{ID: "admin", Name: "ADMIN DATA"}
	env, err := ReadDotEnv(filepath.Join(opts.Dir, ".env"))
	if err != nil {
		r.Pending = true
		r.Fix = "Run cardo up once; it writes .env and starts the stack."
		return r, nil
	}
	body, _, err := chQuery(ctx, opts.url("8124", ""), env["CLICKHOUSE_USER"], env["CLICKHOUSE_PASSWORD"],
		"SELECT toString(max(day)), count(), uniqExact(pseudonym) FROM cardo.bronze_actor_day FORMAT TSV")
	if err != nil {
		return r, err
	}
	parts := strings.Split(strings.TrimSpace(body), "\t")
	if len(parts) != 3 || parts[1] == "0" {
		r.Pending = true
		r.Fix = "No Admin API rows yet: set CARDO_ADMIN_KEY in Settings, then the poller fills this within a day."
		return r, nil
	}
	rows, _ := strconv.Atoi(parts[1])
	people, _ := strconv.Atoi(parts[2])
	r.Value = fmt.Sprintf("last %s · %d rows · %d people", parts[0], rows, people)
	last, err := time.Parse("2006-01-02", parts[0])
	if err != nil {
		return r, nil
	}
	if age := time.Since(last.AddDate(0, 0, 1)); age > 48*time.Hour {
		r.Cause = fmt.Sprintf("the newest Admin API day is %s, %d days old", parts[0], int(age.Hours()/24))
		r.Fix = "Check the poller service: docker compose --profile admin ps, then cardo poll once by hand."
		return r, nil
	}
	r.OK = true
	return r, nil
}
