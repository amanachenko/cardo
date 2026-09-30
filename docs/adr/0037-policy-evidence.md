# ADR-0037 — Policy evidence: conformance, coverage and other organizations

**Status:** Accepted — **not built**
**Date:** 2026-09-25
**Evidence:** `2026-09-25-stakeholder-field-notes.md` (organizations that ban adoption;
`organization.id`); `2026-09-23-admin-analytics-apis.md` (per-person activity from Anthropic).
Otherwise judgment.

Refines [ADR-0003](0003-efficiency-before-security.md), whose security v1 is enforcement evidence. It
says what the evidence covers and what form it takes.

## Context

Some organizations ban Claude Code, and in them the obstacle is policy, not willingness. For them,
evidence that controls actually hold is the way in, before there is anything to measure. The same
evidence serves every organization's security and compliance team: Cardo's third user
([ADR-0030](0030-stakeholders-and-questions.md)).

The facts that shape what can honestly be claimed:

- **The bundle carries no policy** ([ADR-0024](0024-bundle-configures-telemetry-only.md)). The
  policies are the organization's own managed settings: deny rules, `disableBypassPermissionsMode`,
  approved MCP servers. Cardo can only observe that nothing violating them happened.
- **"No violations seen" is only as strong as the proof that every machine reports.**
- Both data paths hash to the same pseudonym. So everyone Anthropic's usage data says used Claude
  Code can be checked against everyone who reports telemetry.
- **Every OTel record carries `organization.id`, and it is kept.** When managed settings are
  delivered as a file, they apply whoever is signed in. A session on a personal or another
  company's account then reports with a different id. Settings delivered by Anthropic's server
  apply only after signing in to the organization, so that case is invisible.

## Decision

The evidence covers three things.

**1. Conformance of the machines that report.** Over the period:

- no bypass-mode sessions;
- only MCP servers on the organization's approved list;
- Claude Code at or above the organization's minimum version;
- the managed hooks present.

Every exception is listed.

**2. Coverage.** Every pseudonym active in Anthropic's usage data also reports telemetry. A gap
means a machine outside managed settings. This uses the Console adapter now and the Enterprise
adapter once it exists.

**3. Sessions from other organizations,** meaning any `organization.id` that is not the
organization's.

It is delivered in two forms:

- **First, a periodic evidence report** for a risk committee or auditor. It gives coverage,
  conformance and every exception, and **states what the evidence cannot see**.
- **Second, events to the organization's SIEM** as they happen: a bypass-mode session, an unapproved
  MCP server, a session from another organization, a machine that stopped reporting.

**Named exceptions for security** come later, under their own identity decision: the identified
stream of ADR-0002.

**Behavioural detection** (anomalies, exfiltration-shaped sequences) is out of scope. Each form of
it needs either content, which INV-5 forbids, or a baseline of normal that does not exist yet.

## Consequences

- The report is the deliverable of the third pilot, at an organization that currently bans Claude
  Code ([`docs/roadmap.md`](../roadmap.md)).
- **The organization supplies two things:** its approved MCP list and its minimum version. Both
  are configuration, not something Cardo infers.
- **What the evidence cannot see, stated in every report:**
  - personal accounts on machines whose settings come from Anthropic's server;
  - personal accounts entirely outside the organization;
  - what was run, touched or contacted (INV-5).

  `forceLoginOrgUUID` prevents the first two; Cardo does not detect them.
- A SIEM feed is identified data leaving Cardo's store, but it stays inside the organization's
  network (INV-7). What it carries follows the identity decision for security.

## Rejected alternatives and why

- **A Grafana dashboard only.** A risk committee signs off a document, not a dashboard.
- **The SIEM feed first.** It matters only once Claude Code is in use. For an organization that bans
  it, the report is what gets it allowed.
- **Conformance without coverage.** Without coverage, "no violations seen" may only mean "not
  seen".
- **Named exceptions now.** They need the identified stream, which is a larger decision than the
  first pilot needs.
- **Detection.** Out of reach without content or a baseline, as above, and better done by
  preventing it in the first place (ADR-0003).
