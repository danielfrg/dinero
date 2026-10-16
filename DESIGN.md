# Interface Design

## Foundation

Dinero is a Go CLI with read-only REST and MCP interfaces. Its design vocabulary
is commands, data contracts, and recoverable operations. Keep shared behavior in
`internal/ledger`; transports should adapt it rather than reimplement its rules.

## CLI Rules

- Use the existing Cobra command and flag patterns.
- Keep machine-readable JSON on stdout and diagnostics on stderr.
- Preserve stable output formats, exit behavior, filtering, and ordering.
- Keep mutation previews and explicit commit/confirmation controls.
- Do not mix human progress text into JSON or expose credential values.

## REST and MCP Rules

REST reads live under `/api`; Huma publishes `/api/openapi.json` and `/api/docs`.
Streamable HTTP MCP lives at `/mcp` and exposes the same read-only ledger concepts.
Keep account, category, transaction, balance, summary, and status reads consistent.

REST and MCP have no application authentication. Keep host/origin checks,
read-only database access, request limits, and sanitized errors. Health and
readiness remain distinct: `/healthz` checks the process; `/readyz` checks a
bounded ledger read. Neither depends on calling Plaid.

## Data and Persistence

Plaid's amount convention is positive for expenses and negative for income,
refunds, or other inflows. Preserve decimal semantics and inclusive date filters.
REST amounts and balance outputs use decimal strings or null; CLI transaction
search retains its existing numeric JSON amounts. Do not infer a currency from
the legacy ledger.

Keep SQLite and PostgreSQL behavior aligned. Read-only operations must not create
or migrate a database. Ingestion, config updates, imports, and exports preserve
validation, transaction boundaries, atomic replacement, and recovery files.

## Changes and Verification

Use synthetic fixtures for behavior and compatibility checks. Exercise REST,
the official MCP client, read-only container operation, and graceful shutdown
through the existing smoke test. Preserve installer, package layout, checksums,
and version injection when changing builds or releases.

Use `PRODUCT.md` for user intent and `README.md` for setup and commands.
