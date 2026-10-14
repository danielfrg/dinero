---
name: dinero
description: Read the user's personal finance ledger through the configured Dinero REST or MCP service, or local CLI fallback.
version: 1.1.0
---

Dinero exposes read-only transaction, account, category, balance, summary, and status data. Prefer the configured REST service or MCP connection when available. Use only the user's existing connection; never print bank credentials. REST and MCP do not require authentication.

For REST, discover the schema at `/api/openapi.json` and use `GET /api/accounts`, `/api/categories`, `/api/transactions`, `/api/balance`, `/api/summary`, or `/api/status` as needed. The server also has `/healthz` and `/readyz`. No API token or Authorization header is needed. Search defaults to 50 rows, supports up to 500 per page, and uses stable ordering. Amounts in REST responses are decimal strings or null; currency is unknown in the legacy ledger.

For MCP, connect to `/mcp` using Streamable HTTP without authentication. Initialize the session, list tools, then call `list_accounts`, `list_categories`, `search_transactions`, `get_balance`, `summarize_transactions`, or `get_status`. Plain `GET /mcp` is not initialization. MCP tools are read-only and expose no SQL, bank ingestion, Link, or tokens.

If the service is unavailable, use the local `dinero` CLI. Read `~/.config/dinero/cache/accounts.json` and `categories.json` for valid names; run `dinero build-cache` when those files are absent and local access is authorized. Use `dinero search --json` with `--account`, `--category`, `--subcategory`, `--description`, `--after`, `--before`, `--year`, `--month`, `--limit`, and `--sort` as needed. Description matches are case-insensitive. `--before` includes the named day. Use `dinero balance --account "Checking" --through YYYY-MM-DD --json` for a cumulative amount. CLI search JSON amounts remain numeric; balance amounts are decimal strings. Do not run `transactions`, `import-csv --commit`, or `link` for a read request.
