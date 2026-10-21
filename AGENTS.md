# Agent Instructions

Dinero is a self-hosted personal finance ledger.
It builds one Go binary that ingests bank transactions through Plaid or CSV,
stores them in SQLite or PostgreSQL, and serves read-only access through a CLI,
a Huma REST API, and an MCP endpoint. There is no UI or frontend.

Read `PRODUCT.md` for user intent and `DESIGN.md` for CLI, API, and data contracts.

The `README.md` and `justfile` own setup and usage.
They are written for humans: avoid big restructures, but update them when changes are user facing.

## Architecture

- `cmd/dinero` is the executable entry point; `internal/cli` owns Cobra commands.
- `internal/config` resolves TOML, database overrides, and protected config writes.
- `internal/ledger` defines shared reads, validation, dates, and amount handling.
- `internal/store` implements SQLite and PostgreSQL persistence.
- `internal/httpapi`, `internal/mcp`, and `internal/server` expose read-only access.
- `internal/plaid`, `internal/ingest`, `internal/importcsv`, and
  `internal/maintenance` own bank ingestion and explicit local writes.
- `scripts` packages releases and tests installation and containers;
  `testdata/compat` and `internal/testutil` provide synthetic fixtures.
- Builds into a single static binary at `bin/dinero`; release archives and
  installer contracts belong to `scripts`.

## Behavior Invariants

- Keep business rules shared across CLI, REST, and MCP; transports adapt
  `internal/ledger` rather than reimplement it.
- Preserve SQLite and PostgreSQL compatibility, decimal amount behavior,
  transaction sign conventions, inclusive dates, stable ordering, and existing
  output formats.
- Keep REST and MCP read-only; never expose arbitrary SQL, ingestion, Link, or
  credentials through them.
- Read requests must not initialize databases, refresh bank connections, or
  commit transactions.
- Preserve confirmation, preview, dry-run, and atomic-write behavior.
- Do not log credentials, access tokens, connection strings, or financial
  payloads. Keep request-policy checks and sanitized public errors intact.

## Development

Use `just install`, `just api`, `just check`, `just build`, and `just image-check`.
Follow the Go version pinned by CI and the Dockerfile.

Use temporary configuration, synthetic data, and isolated test databases. Never
run development checks against real bank connections, personal configuration,
or deployed ledgers. PostgreSQL integration tests use `DINERO_TEST_POSTGRES_DSN`;
point it only at a disposable test database and report when those tests skip.

In docs, use synthetic examples without personal paths, private hostnames, bank
records, or infrastructure deployment details. Keep `PRODUCT.md` and `DESIGN.md`
short and current, and `SKILL.md` aligned with the actual read-only service and
CLI contracts. Do not commit generated plans, specs, agent reports, or local
agent/design-tool state.

The container smoke test uses synthetic data to verify nonroot/read-only
operation, REST, MCP, probes, and graceful shutdown. Release archives and
version injection use the existing scripts under `scripts/`.

## Deployment

Daniel (danielfrg/danrodriguez) deploys this service in his homelab; the deployment source is in `~/code/danielfrg/homelab`.
