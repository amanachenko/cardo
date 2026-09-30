# Invariants

Hard rules. **Violating any of these requires an explicit superseding ADR** — not a code comment, not a
temporary exception, not "just this once for debugging."

Each is load-bearing for trust, and each is easy to breach innocently while adding something that looks
useful. That is exactly why they are written down separately from the ADRs that produced them.

---

### INV-1 — Session transcripts are never read

No code path may open anything under `~/.claude/projects/` or its platform equivalents. Not for
debugging, not once, not behind a flag.

*Why:* it is the cheapest source of rich signal and the only thing that would let a sceptical engineer
say "it reads my code." Refusing it permanently is what makes the claim real.
*Source:* [ADR-0005](../adr/0005-collection-mechanism.md). *Tested by:* CI invariant test.

### INV-2 — `user.email` is never persisted

Pseudonymization happens before storage, always. Email may exist in flight inside the organization's network; it
must never reach a table, a log file, a metric label or an export.

*Why:* Claude Code emits `user.email` unconditionally and it cannot be suppressed at source, so the only
place the promise can be kept is in transit.
*Source:* [ADR-0006](../adr/0006-pseudonymization.md); upheld for the pilots by
[ADR-0034](../adr/0034-identity-in-pilots.md), which considered storing emails "just for the pilot"
and refused. *Tested by:* CI schema sweep for any column named or containing `email`.

### INV-3 — No per-person view is visible to anyone except that person

Tier 1 is cohort-only. If a view would let a manager rank their reports, it is the wrong view regardless
of how useful it looks. Cohort aggregates require a minimum cohort size: five, which an operator may
raise and never lower. It applies to cohort labels and to the names of artifacts engineers built
themselves ([ADR-0029](../adr/0029-minimum-group-size.md)).

**A pseudonym does not make a per-person view acceptable.** Its daily cost lines up with the cost
Anthropic's console shows by name, so a pseudonymous page about one person is that person's page
([ADR-0032](../adr/0032-engineer-coach-by-shared-link.md)). Nor does a dashboard filter: any
Grafana login can send its own SQL to the data source, so what a login can read is whatever the
data source's database user can read.

**One exception, decided and not built:** a budget owner may see the cost of people over a
published budget. Only the fields Anthropic's console already shows per person are included, and
the engineer sees their own position first
([ADR-0033](../adr/0033-per-person-cost-for-budget-owners.md)).

*Why:* this is the difference between an enablement tool and a surveillance tool, and it is the line
that will be pushed on hardest by stakeholders.
*Source:* [ADR-0001](../adr/0001-unit-of-analysis.md), [ADR-0002](../adr/0002-three-tier-data-model.md),
[ADR-0033](../adr/0033-per-person-cost-for-budget-owners.md).

### INV-4 — The mandatory event set is published and short

What is collected without consent is enumerated in the README. Adding to it requires an ADR. The list
must stay short enough to read in one sitting.

*Why:* the legibility of the boundary is the whole basis for asking people to accept a mandatory tier.
*Source:* [ADR-0007](../adr/0007-enrollment-posture.md).

### INV-5 — No content in tiers 0 or 1

No prompt text, no assistant responses, no code, no file paths, no tool parameter values. All
`OTEL_LOG_*` flags stay at `0` in the shipped managed-settings bundle.

*Why:* content is what makes telemetry feel like reading over someone's shoulder, and none of the
product's questions need it.
*Source:* [ADR-0005](../adr/0005-collection-mechanism.md), [ADR-0002](../adr/0002-three-tier-data-model.md).
*Tested by:* CI assertion over the shipped bundle and the schema.

### INV-6 — Never gate an organization's ability to upgrade Claude Code

Declare a minimum supported version. Never a maximum. Never
`requiredMaximumVersion`.

*Why:* holding a fleet back on our release cadence is how the tool gets removed, and it would block
security fixes.
*Source:* [ADR-0014](../adr/0014-version-drift.md).

### INV-7 — No data leaves the organization's network

No phone-home, no usage analytics about Cardo itself, no benchmark upload, no hosted option.

*Why:* it is an unqualified statement with no asterisk, and it stops being true the moment there is one
exception.
*Source:* [ADR-0013](../adr/0013-deployment-topology.md).

---

## If you need to break one

Write a superseding ADR. State which invariant, why the original reasoning no longer holds, and what
replaces the protection it provided. Then update this file to point at the new ADR.

Do not route around an invariant in code and leave this file saying something untrue.
