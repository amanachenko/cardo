# What an individual account can actually reach on the Admin API

**Dated snapshot, 2026-09-23.** Measured against the live API, not read from documentation. This is
the first time anything in this project has been checked against a real Anthropic endpoint. Like
every note in this directory it is never updated; if the behaviour changes, write a new dated note.

## What was tested

A personal API key (`sk-ant-api03-…`), created in the Claude Console with **Linked account** set to
the user themselves and **no workspace scope**. That shape matters: the Admin API documentation says
it accepts "a personal key or service account key that isn't scoped to a specific workspace", and
that workspace-scoped keys do not work. This key was the accepted shape.

The account is a personal Claude **Pro** subscription. Its Console organization exists and is named
`Andrew's Individual Org`, `type: organization`.

Each endpoint was called with `anthropic-version: 2023-06-01` and the key in `x-api-key`.

## Result

```
200  /v1/organizations/me
403  /v1/organizations/users
403  /v1/organizations/api_keys
403  /v1/organizations/usage_report/messages
403  /v1/organizations/cost_report
403  /v1/organizations/usage_report/claude_code
```

The 403 body:

```json
{"type":"error","error":{"type":"permission_error",
 "message":"Missing permissions. Please check with Anthropic support if you think this is in error."}}
```

## What it means

The documentation's opening tip — *"The Admin API is unavailable for individual accounts"* — is
literal and complete. It is a rule about the **account type**, and it sits behind the credential-type
rule rather than beside it. Presenting an acceptable credential gets you past the first gate and
straight into the second.

**`GET /v1/organizations/me` is a trap for anyone verifying a key.** It returns 200 with the
organization's id, type and name on an account that can read nothing else. It is the obvious
"does my key work" probe and it answers yes when the answer is no. Any preflight check must call the
endpoint it actually intends to use.

`customer_type` cannot be read as a way in. The Claude Code Analytics API documents the field as
`api` for pay-as-you-go and `subscription` for "Pro/Team" customers, which reads as though a Pro seat
is in scope. Whatever that field describes, it is not reachable from an individual Pro account: the
endpoint 403s before any row is produced. Read it as describing seats **inside** an organization
that already has Admin API access.

## Consequences for Cardo

- **Phase 1 cannot be validated on an individual account.** Not through a personal key, not through
  a service account key, not by generating Claude Code usage first. The gate is upstream of data.
- The adapter's field names remain **documentation, not observation**. Nothing about the response
  body was learned here, because no response body was ever returned.
- What *was* learned is the failure mode, and it is now in the operator-facing 403 hint in
  `internal/source/console/errors.go`. An organization evaluating Cardo on a personal account would
  otherwise spend the same hour we did.
- ADR-0018 already scopes v1 to organizations on Claude API direct with an Enterprise Claude Code
  licence. Those organizations have the Admin API by definition. This measurement does not threaten
  that decision; it confirms that the evaluation shortcut we hoped for does not exist.

## What was not established

Whether converting the Console organization to a Team organization grants Admin API access, and what
that conversion costs or changes about an existing claude.ai Pro subscription. The Console offers the
conversion; none of the documentation read on this date states its billing consequences. Not tested,
because the cost of being wrong is the operator's money.

The OAuth route (`ant auth login --scope "org:admin"`, available to admin, owner and primary owner
roles) was not tried. Given that the block is account type rather than role, it is not expected to
help, but that is an expectation and not a measurement.
