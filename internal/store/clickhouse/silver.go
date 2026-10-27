package clickhouse

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// RebuildSilver rebuilds every stored silver table now, and returns once each rebuild is done.
//
// ClickHouse rebuilds the stored tables every 10 minutes (sql/clickhouse/017_stored_silver.sql,
// ADR-0052), so rows just written reach gold only at the next rebuild. Whatever writes rows and
// then reads gold straight away, such as a test or the made-up organization's check, calls this in
// between. The refreshers are read from ClickHouse rather than listed here, so one a later
// migration adds is rebuilt too.
//
// A rebuild that was already running may have started before the caller's rows were written. So
// each refresher's last good rebuild has to have started after this call began, and the rebuilds
// are asked for again until it has.
func RebuildSilver(ctx context.Context, c *Client) error {
	out, err := c.Query(ctx, "SELECT view, toString(now()) FROM system.view_refreshes WHERE database = '"+Database+"' ORDER BY view FORMAT TabSeparated")
	if err != nil {
		return err
	}
	var views []string
	var began string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if f := strings.Split(line, "\t"); len(f) == 2 {
			views = append(views, f[0])
			began = f[1]
		}
	}
	if len(views) == 0 {
		return fmt.Errorf("%s has no refreshable views: has migration 017 been applied?", Database)
	}
	for attempt := 0; attempt < 5; attempt++ {
		// A rebuild's start time is kept to the second, so one that starts in the second this call
		// began cannot be told from one that started just before it.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
		for _, v := range views {
			if err := c.Exec(ctx, "SYSTEM REFRESH VIEW "+Database+"."+v); err != nil {
				return fmt.Errorf("rebuild %s: %w", v, err)
			}
		}
		for _, v := range views {
			if err := c.Exec(ctx, "SYSTEM WAIT VIEW "+Database+"."+v); err != nil {
				return fmt.Errorf("rebuild %s: %w", v, err)
			}
		}
		out, err := c.Query(ctx, "SELECT count() FROM system.view_refreshes WHERE database = '"+Database+"' "+
			"AND (last_success_time IS NULL OR last_success_time <= toDateTime('"+began+"')) FORMAT TabSeparated")
		if err != nil {
			return err
		}
		if strings.TrimSpace(string(out)) == "0" {
			return nil
		}
	}
	return fmt.Errorf("the stored silver tables were asked to rebuild five times, and some never started a rebuild after %s", began)
}
