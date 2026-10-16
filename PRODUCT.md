# Product

## Register

product

## Users

People maintaining a self-hosted personal transaction ledger, and trusted agents
that need to read that ledger through a local CLI, REST service, or MCP connection.

## Product Purpose

Collect bank transactions through Plaid or CSV, keep them in SQLite or PostgreSQL,
and make account history, balances, and summaries easy to query. Success means
consistent answers across interfaces and deliberate, recoverable data changes.
External database tools can provide editing and dashboards; Dinero has no product UI.

## Brand Personality

Local-first, precise, and practical. Clear commands and trustworthy data matter
more than presentation.

## Anti-references

Avoid opaque financial conclusions, invented records, implicit bank or database
writes during reads, and agent interfaces that expose credentials or arbitrary SQL.

## Design Principles

- Preserve transaction provenance and the existing ledger's amount conventions.
- Share query and validation rules across CLI, REST, and MCP.
- Keep reads separate from ingestion, maintenance, and configuration changes.
- Make previews, confirmations, and failure recovery explicit.
- Keep bank credentials and personal data under the operator's control.

## Accessibility & Inclusion

Use clear text output, useful errors, stable JSON contracts, and documented
commands. Preserve meaningful distinctions between missing and zero amounts.
Date and timezone handling must be explicit and consistent across interfaces.
