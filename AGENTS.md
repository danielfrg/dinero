# Dinero

Read `PRODUCT.md` for user intent and `DESIGN.md` for CLI, API, and data
contracts. `README.md` owns setup and usage; `justfile` owns development commands.

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

## Conventions

Keep business rules shared across CLI, REST, and MCP. Preserve SQLite and
PostgreSQL compatibility, decimal amount behavior, transaction sign conventions,
inclusive dates, stable ordering, and existing output formats. Keep REST and MCP
read-only; never expose arbitrary SQL, ingestion, Link, or credentials through them.

Preserve confirmation, preview, dry-run, and atomic-write behavior. Read requests
must not initialize databases, refresh bank connections, or commit transactions.
Do not log credentials, access tokens, connection strings, or financial payloads.
Keep request-policy checks and sanitized public errors intact.

## Documentation

README sections are overview, installation, usage, configuration, development,
and build. Keep `PRODUCT.md` and `DESIGN.md` short and current. Keep `SKILL.md`
aligned with the actual read-only service and CLI contracts.

Use synthetic examples without personal paths, private hostnames, bank records,
or infrastructure deployment details. Keep docs limited to useful references
and sample data. Do not commit generated plans, specs, agent reports, or local
agent/design-tool state. `LICENSE.txt` contains the standard Apache 2.0 license;
release packaging and the Dockerfile use that filename.

## Development

Use `just install`, `just api`, `just check`, `just build`, and `just image-check`.
Follow the Go version pinned by CI and the Dockerfile. Keep the native binary at
`bin/dinero`; release archives and installer contracts belong to `scripts`.

Use temporary configuration, synthetic data, and isolated test databases. Never
run development checks against real bank connections, personal configuration,
or deployed ledgers. PostgreSQL integration tests use `DINERO_TEST_POSTGRES_DSN`;
point it only at a disposable test database and report when those tests skip.
