# Admin and Analytics API surfaces — snapshot 2026-09-23

Dated snapshot. **Never edit** — write a new dated note if this goes stale.

Supersedes section 5 of [`2026-09-20-claude-code-telemetry-surfaces.md`](2026-09-20-claude-code-telemetry-surfaces.md),
which recorded a flattened response shape and missed the Enterprise split entirely.

Fetched from `platform.claude.com` documentation on 2026-09-23. **Documentation, not observed
responses** — the same caveat as the earlier note. A live call remains the authority.

---

## 1. There are two analytics APIs, not one

This is the headline finding and it was missed on 2026-09-20.

| | Claude Code Analytics API | Claude Enterprise Analytics API |
|---|---|---|
| Applies to | Claude Console / Claude Platform orgs | Claude Enterprise (claude.ai) orgs |
| Key type | Admin API key `sk-ant-admin01-...` | Analytics API key, scope `read:analytics` |
| Created in | Console > Settings > Admin keys | claude.ai > Organization settings > API |
| Who can create | Organization **admin** | **Primary owner** only |
| Endpoint | `/v1/organizations/usage_report/claude_code` | `/v1/organizations/analytics/...` |
| Covers | Daily Claude Code metrics per user | Org-wide engagement across chat, projects, Claude Code, connectors |

> "The key types are not interchangeable: an Admin API key cannot call the Claude Enterprise
> Analytics API, and an Analytics API key cannot call the Admin API."

An org running both products can provision both keys. Enterprise orgs calling the Admin API proper
get **only the members and invites endpoints** — the `usage_report` family is not available to them.

**Consequence for Cardo:** an organization whose engineers hold claude.ai Enterprise seats returns
nothing from the Phase 1 endpoint. Claude Code metrics for those users live on the Enterprise
Analytics user-activity endpoint instead. See [ADR-0018](../adr/0018-provider-scope.md).

---

## 2. Access preconditions

> "The Admin API is unavailable for individual accounts."

An organization is required. A personal Claude subscription (Pro/Max) is not an organization and has
no path to an admin key. An individual **can** create an org via Console > Settings > Organization
and become its owner — this is a viable self-serve test bed.

Three credentials are accepted by the Admin API:

1. **Admin API key** (`sk-ant-admin...`, `x-api-key` header) — only members with the **admin** role
   can provision one.
2. **OAuth bearer token** with `org:admin` scope — only admin, owner, or primary owner.
3. **Personal or service account key not scoped to a workspace** — "has the same permissions as the
   linked account". Workspace-scoped keys do **not** work.

### Organization roles

| Role | Permissions |
|---|---|
| `user` | playground |
| `claude_code_user` | playground + Claude Code |
| `developer` | playground + manage API keys |
| `billing` | playground + manage billing |
| `admin` | all of the above, plus manage users |

Owners and primary owners hold all admin permissions.

**No non-admin role can reach the usage report**, under any of the three credential types — option 3
inherits the linked account's permissions, and no sub-admin role carries org-wide read.

**Security note:** an Admin API key is broad — members, workspaces, invites, API key management. The
Enterprise Analytics key is narrow (`read:analytics`). The narrower credential belongs to the
stricter product, which inverts the expected difficulty of asking an organization for one.

---

## 3. Claude Code Analytics API — corrected detail

`GET https://api.anthropic.com/v1/organizations/usage_report/claude_code`
Headers: `x-api-key`, `anthropic-version: 2023-06-01`. Free for all orgs with Admin API access.

### Request parameters

| Parameter | Required | Notes |
|---|---|---|
| `starting_at` | yes | `YYYY-MM-DD`, UTC. **Single day only** — no range query |
| `limit` | no | default **20**, max **1000** |
| `page` | no | opaque cursor from the previous response's `next_page` |

The cursor parameter is named `page`, not `cursor`. Response carries `has_more` and `next_page`.

### Response shape — corrections to the 2026-09-20 note

The earlier note recorded these fields as flat. They are **nested**:

```json
{
  "data": [{
    "date": "2025-09-08T00:00:00Z",
    "actor": { "type": "user_actor", "email_address": "developer@company.com" },
    "organization_id": "dc9f6c26-...",
    "customer_type": "api",
    "terminal_type": "vscode",
    "core_metrics": {
      "num_sessions": 5,
      "lines_of_code": { "added": 1543, "removed": 892 },
      "commits_by_claude_code": 12,
      "pull_requests_by_claude_code": 2
    },
    "tool_actions": {
      "edit_tool":          { "accepted": 45, "rejected": 5 },
      "multi_edit_tool":    { "accepted": 12, "rejected": 2 },
      "write_tool":         { "accepted": 8,  "rejected": 1 },
      "notebook_edit_tool": { "accepted": 3,  "rejected": 0 }
    },
    "model_breakdown": [{
      "model": "claude-opus-5-5",
      "tokens": { "input": 100000, "output": 35000, "cache_read": 10000, "cache_creation": 5000 },
      "estimated_cost": { "currency": "USD", "amount": 113 }
    }]
  }],
  "has_more": false,
  "next_page": null
}
```

Specifically corrected:

- `core_metrics` and `tool_actions` are **nested objects**, previously recorded flat.
- `actor` is a **discriminated union**: `type` is `user_actor` (carries `email_address`, used for
  OAuth auth — "most common") or `api_actor` (carries `api_key_name`).
- **`estimated_cost.amount` is in CENTS**, not dollars. Previously unrecorded. A naive read makes
  every cost figure 100x too large.
- `customer_type` is `api` (pay-as-you-go) or `subscription` (Pro/Team) — subscription seats inside
  an org **are** reported.
- `date` is RFC 3339, not a bare date.

### Freshness and retention

> "To ensure consistent pagination results, only data older than 1 hour is included in responses."

That is stronger than "1 hour delay" — recent data is **excluded**, not merely incomplete. Retention:
"no specified deletion period", historical data accessible.

### Not covered

No per-turn detail, skill invocations, slash commands, permission decisions, or plan mode. Confirms
the wedge framing in [ADR-0017](../adr/0017-v01-scope.md).

---

## 4. Claude Enterprise Analytics API — operational detail

Endpoints under `https://api.anthropic.com/v1/organizations/analytics/`. Engagement and adoption
data is on all Enterprise plans; cost and usage endpoints apply to usage-based plans (seat-based
plans see usage credits only).

Relevant payload for Cardo — the **user activity** endpoint gives per-user daily metrics including
"Claude Code (sessions, commits, pull requests, lines of code, tool actions)". That is close to the
Claude Code Analytics core metrics, so the wedge is achievable on this path too.

Also available: activity summaries (DAU/WAU/MAU, seats, pending invites); project, **skill**, and
connector usage breakdowns; cost and usage reports.

> Connector names are normalized — `Atlassian MCP server`, `mcp-atlassian` and `atlassian_MCP` all
> appear as `atlassian`.

### Differences that change poller design

| Concern | Behaviour |
|---|---|
| **History floor** | Data only exists **on or after 2026-01-01**. Backfill cannot go deeper |
| **Freshness** | ~1 day lag; a day is typically available ~17:00 UTC the following day |
| **Unavailable date** | Returns **400 naming the most recent available day** — use it, do not guess |
| **Cost revision** | Cost values revised for up to **30 days**; invoicing-grade needs T-30 |
| **Cost format** | **Decimal strings in cents** (`"41280.000000"` = $412.80). Parse as decimal, never binary float |
| **Rate limit** | **60 rpm at organization level**, not per key |
| **Cursors** | Bound to the issuing query. Changing any parameter mid-sequence returns 400 — restart without a cursor |
| **List params** | Bracket notation, repeated: `products[]=chat&products[]=claude_code` |
| **Tail data** | Cost responses carry `data_refreshed_at`; omitting `ending_at` returns an incomplete tail |

The 400-names-the-available-day behaviour is genuinely useful — it removes the need to model the lag
client-side.

### Known limitation

Claude Code used through Amazon Bedrock is **not** returned by the Enterprise Analytics API either,
consistent with the Claude Code Analytics API exclusion.

---

## 5. Provider exclusions — unchanged

The Claude Code Analytics API tracks Claude Code on the Claude API only. Excluded: Bedrock,
Microsoft Foundry, Google Vertex, and Claude Platform on AWS. For Claude Platform on AWS the API is
"not currently available" — use the Console Usage page.

This confirms the [ADR-0018](../adr/0018-provider-scope.md) exclusion table. What ADR-0018 got wrong
was not the provider axis but the **product axis** — see section 1.

---

## 6. Open uncertainties

1. **Everything here is documentation, not observed responses.** Field names, nesting and cent-vs-
   dollar units are as documented. A single live call converts this to verified fact and should be
   the first thing done in Phase 1.
2. **The Enterprise Analytics user-activity response shape was not retrieved** — only its prose
   description. The Claude Code metric field names on that endpoint are unknown and are very likely
   *not* identical to `core_metrics` / `tool_actions`.
3. **Whether a given pilot organization is a Console org, an Enterprise org, or both** is not
   knowable from here and determines which adapter is needed. Ask before building.
4. **Admin API key rate limits** are not stated in the Claude Code Analytics docs; only the
   Enterprise API's 60 rpm is documented. Backfill pacing is therefore unvalidated.

## Source URLs

- https://platform.claude.com/docs/en/manage-claude/claude-code-analytics-api
- https://platform.claude.com/docs/en/manage-claude/admin-api
- https://platform.claude.com/docs/en/manage-claude/analytics-api
