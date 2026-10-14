# dinero

Dinero tracks personal transactions in the existing eight-column SQL ledger. The Go CLI is the deployable executable for read-only REST/MCP service, queries, imports, and scheduled ingestion. The Python package, dashboard, and analysis helpers remain available during the transition.

## Build and configure

Go 1.25.0 or newer is required by the module. The container and CI use pinned Go 1.27.2. Build locally with `go build -o bin/dinero-go ./cmd/dinero` or `just build`. Copy [config.sample.toml](config.sample.toml) to `~/.config/dinero/config.toml`; `DINERO_CONFIG_FILE` or `--config` selects a different file. PostgreSQL and SQLite URLs use the existing `[database].connection_string` format, including `postgresql+psycopg://` and `sqlite:///`. `DINERO_DATABASE_URL` or `DINERO_DATABASE_URL_FILE` can override the URL for a deployment. The default timezone is `US/Central`.

Plaid credentials and account mappings remain under `[plaid]`. `dinero config` displays a redacted view. See [bank linking](docs/link.md) for local Link and credential handling. The default service listener is `127.0.0.1:8080`; the image binds `0.0.0.0:8080`. The service needs a separate file named by `DINERO_API_TOKEN_FILE` holding a read token. See [deployment and cutover](docs/deployment.md) for mounts, nonroot permissions, backups, rollout, and rollback.

## Commands

`bin/dinero-go --help` lists `config`, `search`, `balance`, `import-csv`, `transactions`, `init-db`, `mkrules`, `build-cache`, `mkdataset`, `link`, and `serve`. Use `--json` where available for one JSON value on stdout. Queries preserve the legacy CLI search number format and decimal-string balance; REST/MCP return decimal strings or null for amounts.

```sh
bin/dinero-go init-db --yes
bin/dinero-go search --account "Checking" --description coffee --json
bin/dinero-go balance --account "Checking" --through 2026-08-31 --json
bin/dinero-go import-csv transactions.csv "Checking" --json
bin/dinero-go import-csv transactions.csv "Checking" --commit --yes --json
bin/dinero-go transactions --dry-run --json
bin/dinero-go transactions --yes
bin/dinero-go build-cache
bin/dinero-go mkrules --commit --yes
bin/dinero-go mkdataset
```

`import-csv` previews by default and writes only with `--commit`; `transactions` previews with `--dry-run` and is run as an explicit scheduled job with `--yes`. Keep only one ingestion writer active. `build-cache` writes account and category JSON under the Dinero state directory; `DINERO_STATE_DIR` selects a disposable location for tests. `mkdataset` exports `data/all.csv` and `data/transactions.db` from the ledger.

## Read-only API

Start `bin/dinero-go serve` with `DINERO_API_TOKEN_FILE` set. `/healthz` and `/readyz` are probes. Protected REST reads are under `/api`, with OpenAPI at `/api/openapi.json`; MCP uses `/mcp` and six read-only tools. All protected requests need the static bearer read token. See [MCP client setup](docs/mcp.md). The image contains no dashboard; the legacy Python Streamlit dashboard remains in `dashboards/` for existing users.

## Development

`just check` runs Go tests, vet and race detection. `just compat` compares independent synthetic Python and Go SQLite fixtures across query, import, cache, rules, and export behavior. Set a synthetic config before collecting Python tests because legacy Python modules load config at import time. The CI job supplies disposable PostgreSQL for store contracts. The existing PyPI workflow remains separate from Go artifact builds.
