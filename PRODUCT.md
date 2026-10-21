# Product

## Users

People maintaining a self-hosted personal transaction ledger, and trusted agents
that need to read that ledger through a local CLI, REST service, or MCP connection.

Assumes deployment on a trusted network; there is no auth.

## Product Purpose

Collect bank transactions through Plaid or CSV, keep them in SQLite or
PostgreSQL, and make account history, balances, and summaries easy to query.

Success means consistent answers across interfaces and deliberate, recoverable
data changes. External database tools provide editing and dashboards, there is no UI on this project.

## Brand Personality

Local-first, precise, and practical. Clear commands and trustworthy data matter
more than presentation.

### Tone

Keep command output terse and factual. Machine-readable JSON goes to stdout and
diagnostics to stderr; add explanatory messages only when a decision, preview,
or failure needs them. Errors are useful and sanitized.

## Anti-references

Avoid opaque financial conclusions, invented records, implicit bank or database
writes during reads, and agent interfaces that expose credentials or arbitrary SQL.

## Design Principles

- Preserve transaction provenance and the existing ledger's amount conventions.
- Share query and validation rules across CLI, REST, and MCP.
- Keep reads separate from ingestion, maintenance, and configuration changes.
- Make previews, confirmations, and failure recovery explicit.
- Keep bank credentials and personal data under the operator's control.
- Keep JSON contracts stable, commands documented, and date and timezone
  handling explicit and consistent across interfaces.
- Preserve meaningful distinctions between missing and zero amounts.

## Agent Access

Agents read the same ledger through six read-only MCP tools and the REST API,
with the same query and validation rules as the CLI. Neither interface can
ingest bank transactions or change the ledger. `SKILL.md` describes how agents
should use these reads.
