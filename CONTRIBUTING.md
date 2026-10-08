# Contributing

Cardo is pre-alpha, and contributions are welcome. Start with the design record: most changes that
look like obvious improvements have already been considered, and the reason they were refused is
written down.

## Where to help

The order of work is the [work items](docs/roadmap.md#work-items) at the end of the roadmap. Each
says whether it is taken, open, waiting for something, or needs a decision first. Before starting
an open one, say so in an issue or a draft pull request.

## Before changing anything

Read, in this order:

1. [docs/design/invariants.md](docs/design/invariants.md): the hard rules. CI tests guard them. If
   one fails, fix the change, not the test.
2. [docs/adr/README.md](docs/adr/README.md): every decision, and the alternatives it rejected.
3. [docs/design/architecture.md](docs/design/architecture.md): what exists today.

[CLAUDE.md](CLAUDE.md) is written for coding agents. It is also the shortest list of the project's
conventions, and of the things that look like good ideas and are not. Those that belong to one part
of the repository, the collector, the managed-settings bundle, ClickHouse, or the views and
dashboards, are in [.claude/rules/](.claude/rules/).

A change that contradicts an accepted ADR or an invariant needs a new ADR that supersedes it
([how](docs/adr/0000-adr-process.md)), not a workaround.

## Sign off your commits

Every commit carries a `Signed-off-by:` line, which `git commit -s` adds. It certifies the
[Developer Certificate of Origin](https://developercertificate.org/): that you wrote the change, or
otherwise have the right to submit it under the project's license. There is no CLA. Contributions
are licensed under Apache-2.0, as section 5 of the [license](LICENSE) says.

## Work you do for someone else

A change you make on an employer's or a customer's time may belong to them. Before sending one
here:

- they have agreed that general improvements may be contributed back;
- nothing specific to them comes with it: no configuration, no names, no data, no dashboards built
  for them.

## Nothing from a real organization

Treat everything committed here as public. Never commit:

- telemetry, pseudonyms, salts, keys or email addresses from a real organization;
- the name of an organization that runs Cardo, or anything that identifies one;
- transcripts, prompts or code from anyone's sessions.

Test fixtures use invented values. A pilot's findings become a dated research note only with
everything that identifies the organization removed, and with its approval. Otherwise they stay in
a private repository ([ADR-0038](docs/adr/0038-public-before-the-adoption-pilot.md)).

## Checks

- `go test ./...` before every commit.
- `go test -count=1 ./test/ -run TestINV` after anything touching identity, storage or SQL.
- `make stack-up && make clickhouse-test` after touching the store or `sql/clickhouse/`, and
  `make collector-test`, with the collector's own `CARDO_SALT` exported, after touching
  `deploy/collector/`. Tests that talk to a live server run with `-count=1`: Go's test cache does
  not know the server changed.
- `go.mod`'s require block stays empty ([ADR-0022](docs/adr/0022-clickhouse-access.md)).

## Documents

- ADRs are immutable. Supersede one with a new ADR; only the old one's status line changes.
- Research notes in `docs/research/` are dated snapshots and are never updated.
- `docs/design/` is edited in place and must describe what exists, not what is planned.
- The commit message says why. Anything expensive to reverse, or that someone might innocently
  undo, gets an ADR instead.
