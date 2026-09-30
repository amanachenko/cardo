# Grafana access control and shared links — snapshot 2026-09-25

Dated snapshot. **Never edit** — write a new dated note if this goes stale.

**Measured** on the reference stack: `grafana/grafana:12.1.0` (open-source edition) with the
`grafana-clickhouse-datasource` plugin, reading ClickHouse 26.6. Both tests ran against the local
stack over loopback. Each cleaned up after itself: the temporary user and the share were deleted,
and the count of shares afterwards was 0.

The question behind them: can Grafana show each engineer a page about themselves and nobody else?

---

## 1. A Viewer can send its own SQL to the data source

**Test.** An admin created a temporary user with the `Viewer` role. As that user, a hand-written
query went to `POST /api/ds/query` against the ClickHouse data source:

```sql
SELECT count() AS rows, uniqExact(LogAttributes['user.pseudonym']) AS people
FROM cardo.bronze_otel_logs
```

**Result.** Status 200: 343 rows, 1 person. Only counts were requested and printed.

**What it means.** A dashboard's panels do not bound what a login can read. The data source's
database user does. `/api/ds/query` is how every dashboard runs its panels, so a Viewer needs it,
and it accepts any SQL. Restricting which data sources a user may query is documented as a
Grafana Enterprise feature; that was not tested here.

The reference stack's data source connects as `CLICKHOUSE_USER`, the main ClickHouse user, which
reads bronze and silver. At the time of the test only the admin had a login, so nothing was
exposed. **The first login given to anyone else can read every per-person row.**

## 2. Shared dashboards run fixed queries behind an unguessable link

**Test.**

1. `POST /api/dashboards/uid/cardo-enablement/public-dashboards` with
   `{"isEnabled": true, "share": "public"}`. The response carried an access token of
   **32 characters**.
2. `POST /api/public/dashboards/<token>/panels/<id>/query` with **no credentials**, for the first
   panel ("The organization's artifacts in use"). Status 200, 4 rows: the ClickHouse plugin's query
   ran server-side from the saved dashboard.
3. The same call with a wrong token of the same length: **404**, `publicdashboards.notFound`.
4. The share was deleted.

The query endpoint takes a panel id, not a query, so the holder of a link cannot change what runs.

**Not tested:**
- template variables on a shared dashboard (documented as unsupported);
- whether the time range can be changed from the link;
- what revoking does, beyond deleting the share.

## 3. Consequences

| Approach | Holds? |
|---|---|
| A per-person dashboard in a Grafana that other people log in to | **No.** Any login can query the rows directly (section 1) |
| A pseudonym passed as a URL parameter | **No.** A pseudonym is not a secret; every per-person query returns them |
| A shared link to a dashboard with the pseudonym fixed inside, in a Grafana only the operator logs in to | **Yes.** It is a capability link: the page runs only its saved queries, and nobody else has a login from which to send their own |

Used by [ADR-0032](../adr/0032-engineer-coach-by-shared-link.md).
