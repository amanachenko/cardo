# ADR-0055 — Trend rows outlive the rows behind them, and only when they describe five people

**Status:** Accepted
**Date:** 2026-10-25
**Evidence:** Judgment. [risks.md](../../risks.md) #10 and #14 record the deliberate deferral this
reverses; nothing has changed about the marts' shape since, so the shape question is answered here
rather than waited out.

## Context

[ADR-0016](0016-retention.md) promised that derived aggregates carrying no pseudonym are retained
indefinitely, "so longitudinal trends survive the expiry of the rows behind them."
[risks.md](../../risks.md) #10 records that this is promised and not built: every gold mart is a
view over bronze, and bronze expires at the window in force
([ADR-0053](0053-retention-default-and-organization-choice.md)). An organization that has run
Cardo for a year can answer "how did adoption move last quarter" and cannot answer "how did it
move last year", which is the question the product exists to make answerable.

The gap was left open on purpose, and the reason is still true: materializing marts whose shape had
not survived contact with real data is the wrong order (#10), and a materialized table is a
migration each time the marts change (#14). So this record does not materialize the marts. It adds
one surface whose shape is decided once.

## The constraint that decides the shape

[ADR-0029](0029-minimum-group-size.md)'s five-person floor is applied by the views at read time: a
label below the threshold is shown as `other`, "and its rows are counted there". Materializing
anything means freezing whatever is written, so the floor has to survive that.

Three facts settle it:

1. **The fold cannot be recomputed from per-group rows.** `other` merges the small groups of a
   period. Its distinct-people count and its wait-time quantiles are computed over the union, and
   neither is derivable from per-group values — a person in two small groups is one person, and a
   median of medians is not a median. A table of per-group rows therefore cannot reproduce `other`,
   and a table holding `other` cannot be re-folded when the floor is raised.
2. **A label frozen in a permanent table freezes the floor decision.** ADR-0029 rejected labelling
   at ingest for exactly this reason: "a label written at ingest is frozen: a pattern corrected
   later would not relabel the past." A row written under a floor of five that still names a
   six-person team after the floor is raised to ten is that same fault, one layer down.
3. **An aggregate over fewer than five people is a person's record.** ADR-0029 says a name one
   person uses identifies them, and that a two-person team's friction chart is two people's
   friction. Keeping such a row past the window would break the tier rule
   ([ADR-0016](0016-retention.md)), which is the whole control.

## Decision

**1. Three trend tables hold the trends, and nothing else.** ClickHouse only; the collector path
has one store ([ADR-0028](0028-file-store-is-admin-api-only.md)).

| Table | Grain | Answers |
|---|---|---|
| `cardo.trend_day` | day | how much the fleet did, and what it cost it |
| `cardo.trend_day_cohort` | day, cohort | how friction moved for a team |
| `cardo.trend_week_artifact` | week, kind, artifact, file_kind | how adoption moved |

They carry **no pseudonym and no content** ([INV-5](../design/invariants.md)): counts, sums and
quantiles only. There is no column that can name a person, and the tests assert that.

**2. Every row describes at least five people.** The five-person floor is applied twice:

- at **write** time, to what is stored — a row whose group had fewer than five people in its
  period is not written at all;
- at **read** time, to what is published — the row carries its group's people count, so a raised
  floor hides it retroactively.

This is the fix ADR-0029 deferred: "suppressing any view whose whole population is below the
minimum would blank every pilot of fewer than five people. It is left for the first deployment that
needs it." A permanent record is that deployment. The live marts keep ADR-0029 exactly as written;
only the trend store is stricter.

**3. There is no `other` bucket.** By fact 1 above, a fold cannot be recomputed from per-group rows.
The trend surface publishes named groups and omits `other`; a bucket that is wrong is worse than a
bucket that is absent. The live marts keep theirs.

**4. Refresh is a `REPLACE PARTITION` per period.** `cardo trend` recomputes a day and swaps that
day's partition in one statement. An empty day on a dashboard looks exactly like a quiet one, so a
refresh that fails partway must leave the previous numbers in place — a `DELETE` then `INSERT`
would leave a gap where a quiet day and a broken refresh look identical.

**5. The deletion path is written down** in [privacy.md](../design/privacy.md). Because every trend
row describes at least five people and carries no pseudonym, a deletion request removes the
person's rows from the stores and does not touch the trend rows: they are not that person's record.
An organization that wants them gone anyway deletes the partition, and the procedure names exactly
what that removes.

**6. A salt rotation does not rewrite the trend rows.** They carry no pseudonym, so there is
nothing to re-hash. What they do carry is counts of distinct people, computed with the salt in
force when the row was written. A rotation splits one person into two pseudonyms, so a period that
spans a rotation counts each person once per salt. [ADR-0006](0006-pseudonymization.md) keeps one
stable salt in v0.1, so this is a rule for when rotation arrives, not a defect today.

## Consequences

- **A pilot of fewer than five people gets no longitudinal trend.** That is the point. Their live
  marts still show the window, exactly as today.
- **An artifact used by fewer than five people in a week has no row that week.** The live
  `gold_artifact_usage` still shows it inside the window. Early rollout weeks are visible live and
  absent from history, which is the cost of not keeping a permanent record of a handful of people.
- The trend surface and the live marts are two surfaces with one difference (`other`), and
  `data-model.md` says so where both are described.
- The trend rows grow without bound — one row per day, per team-day, per artifact-week. On the
  reference stack that is hundreds of rows a year.
- The rollup has to run while the rows it reads are alive. A `cardo trend` that is down for longer
  than the window leaves a hole, and the hole is silent, like the expiry it exists to prevent.

## Rejected alternatives and why

- **Materialize the gold marts as they publish today.** The fold would be frozen at write time. A
  floor raised later would not reach history, which is the fault ADR-0029 already rejected once.
- **Store per-group rows and fold at read time.** `other`'s distinct-people count and its quantiles
  are not derivable from per-group values, so the published `other` would disagree with the live
  marts. Two surfaces that disagree about one number is the trap this project keeps out of.
- **ClickHouse materialized views over bronze.** They fire on INSERT, and the store writes with
  `REPLACE PARTITION`, which does not trigger them. risks.md #10 already refused this for the same
  reason, and it would make the fold question harder, not easier.
- **Materialize silver.** Silver carries pseudonyms. It is tier 1 and expires with everything else,
  so it buys speed ([risks.md](../../risks.md) #14) and no retention at all.
- **Keep the session rows in the aggregate.** A session row plus a cohort label is one person's
  activity trace, whatever its column names say.
- **Store every row and let the read-time floor hide it.** The rows would still be in the store, and
  a row describing two people is personal data whether or not a view shows it. The gate belongs at
  write time.
- **Extend the Admin API path's rollup to cover this.** That path's marts are the leadership ones
  and its store is the file store with a DuckDB mirror ([ADR-0028](0028-file-store-is-admin-api-only.md));
  a change to one target belongs in both. The collector path is the one with the enablement
  question and one store, so it goes first.