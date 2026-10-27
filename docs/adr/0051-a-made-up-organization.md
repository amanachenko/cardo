# ADR-0051 — A made-up organization is where views are seen and checked, never where findings come from

**Status:** Accepted
**Date:** 2026-10-22
**Evidence:** Judgment, no evidence, from a design discussion between the project's owner and a
coding agent on 2026-10-22. Two facts about the collector were observed while building it, on
`otel/opentelemetry-collector-contrib` 0.161.0: the hook receiver can turn a request header into
an attribute, named `header.` plus the header's canonical name, and it stores the header's values
as a list.

Extends [the ClickHouse and Grafana reference stack (ADR-0009)](0009-storage-agnostic-clickhouse-reference.md) and
[the minimum group size (ADR-0029)](0029-minimum-group-size.md) without replacing either.

## Context

Nobody has yet seen Cardo's views work. The two Grafana dashboards are drafts, and the only data
they have shown is one person's sessions. With one person, almost every view falls under the
five-person minimum and shows "other". A view that names a command once five people use it cannot
be judged on one person's data. Neither can a cost chart meant for a whole organization.

The repository already generates data, but only for the Admin API path. The sample store
(`test/sample_store_test.go`) does it, and so does any stand-in for Anthropic's Admin API. Nothing
generates the hook events and OpenTelemetry records that the collector path's views read: which
commands, skills, subagents and connectors people use, which instructions file they load, and
where sessions go wrong.

So a made-up organization is needed: invented engineers, teams and sessions, enough to see each
view as an organization would. Two things make that a decision rather than a test fixture.

- **Getting twelve weeks of history in needs a change to the collector.** A hook payload carries
  no time of its own, so the collector stamps each hook event with the moment it arrives. Loaded
  in ten minutes, twelve weeks of invented hook events would all land on one day. The rule that
  fixes that, "take the event's time from the sender", would let anyone who can reach a real
  collector backdate events ([risks.md](../../risks.md) #19).
- **Invented data is easy to mistake for evidence.** For example, a chart on the made-up
  organization shows `/deploy-check` cutting cost per accepted edit by 30%. It shows that only
  because the generator was told to plant it. Once a screenshot or a sentence leaves the room,
  nothing says so.

## Decision

**1. The made-up organization lives in a stack of its own.**
- Compose project `cardo-madeup`, with its own ports, all on loopback: Grafana 3003, ClickHouse
  8126, OTLP 4320 and hooks 8090. See `deploy/compose/docker-compose.made-up.yml`.
- `scripts/made-up.sh` starts it, loads it and removes it, and sends to no other stack.
- It is never loaded into a stack that holds real rows, and real rows are never copied into it.

**2. Its sessions go through the real collector, as Claude Code's would.** The generator sends
hook events and OpenTelemetry records to the collector's endpoints, and the collector does to them
what it does to real ones:
- it drops anything that looks like content;
- it turns each email address into a code name;
- it classifies repositories.

**3. The time rule belongs to the made-up stack alone.**
- **Where it lives:** `deploy/compose/made-up/collector-made-up.yml`. The made-up collector reads
  it after the shipped `deploy/collector/config.yaml`.
- **What it does:** it reads one header the generator sends, `X-Cardo-Made-Up-Time`, sets the
  hook event's time from it, and removes it. The JSON body stays exactly what Claude Code sends,
  so the list of field names that arrived (`cardo.received_keys`) is the same as on a real stack.
- **What the tests hold** (`test/madeup_test.go`):
  - the shipped collector never mentions the header;
  - the made-up file adds that rule and nothing else;
  - the made-up stack shares no port with another stack.

**4. It says it is not real, wherever it shows.**
- Every invented email address is at `example.com`, a domain reserved for examples.
- Its Grafana files the dashboards under a folder named "MADE-UP ORGANIZATION - not real data".
  The folder is in the path at the top of every dashboard. Grafana also opens on a page that says
  the same.
- It gets a fresh salt each time it is set up, so it holds nothing worth keeping.

**5. It plants stories, and writes down the answers.**
- **A story** is a pattern put in on purpose, aimed at a view. For example: "`/deploy-check` starts
  in one team in week 2, and crosses five people in week 5". The view must hide it as "other" until
  week 5, and name it after.
- **The answer key:** the generator records what it planted, and the checks compare each view with
  it, so they hold at any size.
- **A story no view can show** is written down as a gap in Cardo, never left out.
- **When a view is done:** once it has been seen showing its story.

**6. Nothing seen in it is a finding.** It shows that a view shows what was put in. It cannot show
that a signal means anything with real people: that waits for an organization's data. So:
- nothing from it goes into `docs/research/`, or into a document, commit or pull request, as
  evidence about Claude Code, engineers or organizations;
- its figures, such as how spend and session counts spread across people, are plausible defaults,
  not measurements, and the generator says so where they are set.

## Consequences

- **A third Cardo on the machine,** beside the one that collects and the preview, on ports of its
  own.
- **The made-up collector's hook pipeline is a copy of the shipped list plus one entry.** In a
  second config file a list replaces the first file's list, so it cannot be appended to. When the
  shipped pipeline changes, the test fails until the copy follows.
- **The generator copies the event shapes seen from real Claude Code sessions**
  (`test/fixtures/hooks/`). When Claude Code changes what it sends, the generator has to follow, as
  the collector does.
- **A view that looks right on made-up data can still be wrong on real data.** When an
  organization's data first arrives, it is set beside the made-up assumptions to see which ones
  held.
- **It ages like real data.** Raw rows are kept for 90 days, so its twelve weeks all fit, and a
  made-up stack left running loses its oldest days the way a real one does.

## Rejected alternatives and why

- **Writing rows straight into the raw tables, skipping the collector.** It is faster, but it is a
  guess at what the collector would have stored. When the collector changes, the made-up data
  quietly stops matching, and the views are checked against something no real stack holds.
- **Loading it into the preview stack, beside a copy of real rows.** Real and invented rows would be
  indistinguishable in every view, and decision 6 could not hold.
- **A setting in the shipped collector that takes the time from the sender, off by default.** One
  wrong value in an organization's `.env` and anyone on its network can backdate events. Only the
  made-up stack needs the rule, so only it has it.
- **The time in the hook's JSON body instead of a header.** The body is what Claude Code sends. A
  field of our own in it would show up in `cardo.received_keys`, so the made-up stack's record of
  arriving field names would differ from a real one's.
- **Many copies of one person's real sessions under invented names.** One person's way of working
  copied 60 times is not an organization, and that person's data under other names is still that
  person's data.
- **One average engineer with random spread.** People's spend and session counts differ widely
  and do not move together. Someone with dozens of long sessions can spend more than someone with
  thousands of short ones. A spread around one average hides exactly the unevenness that decides
  whether a chart misleads.
- **Small fixtures with exact counts only, for CI.** They check queries, but nobody ever sees the
  pages, and seeing them is what was missing.
