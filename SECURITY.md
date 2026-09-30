# Security

Cardo handles data about how engineers work, and its main promises are about what it will not keep.
A way to break one of those promises is a security problem, even when nothing crashes.

## Reporting a problem

Report it privately, with the **Report a vulnerability** button on this repository's Security tab.
Please do not open a public issue for it.

Leave out anything from a real organization: no salt, no admin key, no email address, no telemetry.
A report built on invented data is enough; the files in `test/fixtures/` show the shapes.

Cardo is pre-alpha. Expect an acknowledgement within a week; there is no guaranteed date for a fix.
Only `main` is supported, and there are no releases yet.

## What counts

Anything that breaks an invariant ([docs/design/invariants.md](docs/design/invariants.md)), for
example:

- an email address, or anything else that identifies a person, reaching storage unhashed (INV-2);
- a pseudonym that can be reversed without the salt;
- prompt text, code, file paths or tool parameters reaching tier 0 or 1 (INV-5);
- a hook field getting past the collector's allowlist;
- a per-person view that anyone but that person can open (INV-3), apart from the Grafana gap below;
- data leaving the network Cardo runs in (INV-7), or a session transcript being read (INV-1);
- the poller being aimed at a host other than Anthropic's.

The ordinary kinds count too: injection into the SQL the binary sends, a credential written to a
log, and so on.

## Known, and documented

These are recorded in [risks.md](risks.md) with what is being done about them. A report that adds to
one is still welcome.

- **Any Grafana login can send its own SQL** to the data source, and read whatever the data source's
  database user can, bronze included (#15). Until the fix is built, only the operator has a login.
- **The reference stack listens on loopback only, without TLS.** Putting the collector on a network
  needs TLS in front of it and a firewall, which the reference stack does not provide yet.
- **A stable salt keeps pseudonymous data personal** for its 90-day retention (#5).

## Not Cardo

Problems in Claude Code itself go to Anthropic. Problems in the OpenTelemetry Collector, ClickHouse or
Grafana go to those projects.
