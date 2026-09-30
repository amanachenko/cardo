# Stakeholder field notes — snapshot 2026-09-25

Dated snapshot. **Never edit** — write a new dated note if this goes stale.

**Source.** Sections 1 to 5 are field observations from engineering organizations adopting Claude
Code, recorded during the design review of 2026-09-25. They are **a small sample, not a survey**:
the organizations that were easiest to reach, not a cross-section of the industry. Section 6
restates facts from earlier notes. Section 7 is one measurement from our own two sessions and one
field estimate.

---

## 1. How adoption is happening

- **Bottom-up, through champions.** Designated champions try approaches and workflows. The rest of
  the organization adopts what has been shown to be valuable.
- **Chartered variety.** Some organizations have given their teams a charter to find their own best
  approach and share what works. The result is a large variety of team-grown skills, tools and
  conventions.
- **The democratic phase is expected to end soon.** Most organizations are moving towards making
  sure the cost is justified; boards of directors ask this more and more. They are also asking what
  size engineering and its teams should now be, given the gain in capability.
- **Convergence is the next task.** Picking the team-grown approaches that work well and are
  cost-efficient matters, and leadership will want every team on the best option, sooner rather
  than later.

## 2. Measurement lags usage

Usage has run ahead of measurement. Most organizations have not started measuring impact, because
the task is hard. The operator has yet to see one that measures it reliably. Gains in lead time and
story points are real, but vary widely, and every organization is different.

## 3. Leadership already watches cost per person

- Leadership puts real effort into getting usage cost into a readable, ranked form. Some take it
  from Anthropic's admin dashboards, some from Datadog's pre-built dashboards, and some export it to
  Snowflake for their own analysis.
- **They look at individuals and ask targeted questions.** Of people above a set budget (for
  example $1–2k) or above the average, and the opposite: why someone has not been using Claude Code
  enough.
- Engineers know this happens, and dislike it.
- **Splitting the numbers by team is in demand.** Whether the standard tools make that easy is
  unclear.

## 4. Some organizations ban adoption

In those, the obstacle is policy, not willingness. Demonstrating that access restrictions are
actually implemented and followed might unblock them.

## 5. The users

Who Cardo is for, as observed:

1. **Engineering leadership** — the CTO, or whoever is tasked with adoption. Their concern is that
   adoption is productive and the budget is well spent.
2. **Individual engineers.** The company expects more and more adoption and efficiency from them. A
   personal view of how they are doing, with points to improve, gives them the feeling of being in
   control.
3. **Security operations and compliance** — being able to show that whatever policies the
   administrators enforce actually hold.

Engineers liking it helps adoption, but leadership decides first.

---

## 6. What Anthropic already shows (restated)

- The Claude Code Analytics API reports, per person by email and per day: sessions, lines added
  and removed, commits and PRs by Claude Code, per-tool accept and reject counts, and tokens and
  cost per model ([2026-09-23-admin-analytics-apis.md](2026-09-23-admin-analytics-apis.md)).
- The Claude Enterprise Analytics API documents per-user Claude Code activity plus **skill and
  connector usage breakdowns**. That is documentation, not observed.
- Neither has a team field.

So "who uses it, how much, and at what cost" is answered per person by the vendor, and "is skill X
used" partly so. What the vendor does not show: which instructions versions the fleet is on,
home-grown artifacts spreading, friction, how context and spend build up, and anything by team.

## 7. Auto mode

- **Field estimate:** nearly everyone now runs Claude Code in auto mode ("99%").
- **Our two real sessions** ([2026-09-24](2026-09-24-first-real-hook-payloads.md),
  [second](2026-09-24-second-real-session.md)): 15 hook events arrived with `permission_mode=auto`
  and 13 with `default`. In auto mode, an allowed call produces no `PermissionRequest`, and no edit is
  held for review. `PermissionDenied`, the event for the classifier blocking a call, has never been
  observed.

Two of the friction index's three components (permission wait and edit rejections) therefore fall
towards zero in an auto-mode fleet. Only compaction is left.

## 8. What the telemetry can and cannot see for these questions

- **Repository.** Claude Code can send `vcs.repository.url.full`, `vcs.owner.name` and
  `vcs.repository.name` when `OTEL_METRICS_INCLUDE_REPOSITORY` is on (2.1.269+, default off;
  [2026-09-20](2026-09-20-claude-code-telemetry-surfaces.md)). Documented, not observed. The shipped
  bundle sets it off and the collector deletes `vcs.*`.
- **Organization.** `organization.id` is on every OTel record, had one value across both sessions,
  and is kept. When managed settings are delivered as a file, they apply whoever is signed in, so a
  session on a personal or another company's account would report with a different id.
- **Commits and PRs.** Only counts: `claude_code.commit.count` and `claude_code.pull_request.count`
  per session, and the Analytics API's per-day counts. **No surface carries a commit or PR
  identifier.** Claude Code adds a co-author line to its commits by default, which the git host
  can see.
- **Instructions files.** `InstructionsLoaded` gives path, scope and load reason. No content hash
  and no size.
- **Out of reach without content** (INV-5): commands run, files touched, network destinations.

---

## What was decided from this

[ADR-0030](../adr/0030-stakeholders-and-questions.md) to
[ADR-0037](../adr/0037-policy-evidence.md), and the pilot plan in [`docs/roadmap.md`](../roadmap.md).
