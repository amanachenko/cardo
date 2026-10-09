---
paths:
  - "deploy/compose/**"
  - "internal/store/**"
  - "test/deploy_test.go"
---

# ClickHouse: the store and the reference stack

After touching the store, run `make stack-up && make clickhouse-test` (live server, so `-count=1`).

## Things that look like good ideas and are not

- Replacing a day with DELETE-then-INSERT instead of `REPLACE PARTITION`. A process that dies in
  between leaves the day empty, and an empty day on a dashboard looks exactly like a quiet one.
- Trusting a ClickHouse server-side default that affects correctness. `async_insert` flipped to **on**
  in 26.x, which put cardo's staging INSERT behind a queue that `REPLACE PARTITION` does not wait for.
  `Insert` sends `async_insert=0` and `input_format_skip_unknown_fields=0` on every call; the
  destination is the operator's ClickHouse (INV-7) and they may never have seen our overlay.
- Hashing a migration's raw bytes. On Windows, git rewrites line endings when it touches a file,
  and one checkout held `003_gold.sql` as CRLF beside LF siblings. The runner hashes with line
  endings normalized, and still accepts checksums recorded the old way.
- Leaving ClickHouse's system logs at their defaults. A stock single-node server spends a continuous
  fraction of a core merging its own diagnostics, forever, with no TTL — measured at 894 MiB of
  `system.*` against 1.65 MiB of real data. See `deploy/compose/clickhouse/config.d/`.
- Adding a file to `deploy/compose/clickhouse/config.d/` and stopping there. It is mounted a file at a
  time, so an overlay without a `volumes:` line is silently ignored: clean start, no warning, stock
  defaults. `TestDeployOverlaysAreAllMounted` is the only thing that notices.
- Tuning ClickHouse from a list of plausible settings. The timer and pool defaults all *looked* like
  the idle-CPU culprit and together changed it by nothing; one stack trace found the real cause in a
  minute. `system.stack_trace` with `allow_introspection_functions=1`, then read the frames.
- A healthcheck on localhost. On a fresh volume the image's entrypoint runs a temporary server on
  127.0.0.1, then restarts into the real one; the probe passes against the first, and migrate is
  refused. The compose healthcheck connects by the service name.
  `TestDeployClickHouseHealthyMeansReachable` holds it there.
- A `--` inside an XML comment. The spec forbids it, ClickHouse crash-loops, and the only symptom is
  a restarting container. `TestDeployXMLIsWellFormed` catches it.
