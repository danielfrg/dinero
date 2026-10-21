# Dinero

A personal finance ledger. Ready for self-hosting.

Collect bank transactions through Plaid or CSV, store them in SQLite or
PostgreSQL, and read them through a CLI, REST API, or MCP. 

For dashboards or editing, connect external tools like Mathesar and Metabase.

## Installation

### Docker

Prepare `container-config/config.toml` with your database connection, then run:

```zsh
docker run -it \
  -p 127.0.0.1:8080:8080 \
  -e DINERO_CONFIG_FILE=/run/dinero/config.toml \
  --mount "type=bind,src=${PWD}/container-config,dst=/run/dinero,readonly" \
  ghcr.io/danielfrg/dinero:latest
```

- The image serves read-only REST and MCP on port 8080
- The container runs as UID/GID 65532
- For SQLite, mount an existing initialized database and use its container
  path, such as `sqlite:////run/dinero/dinero.db`. The server does not create
  or migrate the ledger.

Published images support Linux AMD64 and ARM64. `latest` tracks `main`.

### Standalone

Install the latest release for macOS or Linux on AMD64 or ARM64:

```zsh
curl -fsSL https://github.com/danielfrg/dinero/releases/latest/download/install.sh | bash
dinero --version
```

To install the newest published build from `main`:

```zsh
curl -fsSL https://github.com/danielfrg/dinero/releases/download/main-latest/install.sh \
  | DINERO_CHANNEL=main bash
```

You can also [build from source](#build).

## Usage

### Read the ledger

```zsh
dinero search --account "Checking" --after 2026-01-01 --limit 50
dinero search --description "groceries" --json
dinero balance --account "Checking" --through 2026-01-31 --json
```

Date boundaries include the named day. Expenses use positive amounts; income,
refunds, and other inflows use negative amounts. Keep that sign convention when
querying the database directly. A [sample Plaid transaction](docs/samples/sample-plaid-transaction.json)
illustrates the source format.

### Connect and import

Set your Plaid credentials in the configuration, then connect or repair an
Item with the Hosted Link flow:

```zsh
dinero link bank
dinero link --update bank
```

Preview transactions before committing them to the database:

```zsh
dinero transactions
dinero transactions --commit
```

CSV imports also preview by default:

```zsh
dinero import-csv transactions.csv "Checking"
dinero import-csv transactions.csv "Checking" --commit
```

For a new ledger, `dinero init-db` creates the transactions table after
confirmation. `dinero --help` and each command's `--help` describe maintenance,
cache, export, and filtering options.

### Read-only REST and MCP

```zsh
dinero serve
```

The default address is [http://127.0.0.1:8080](http://127.0.0.1:8080).
REST reads are under `/api`, interactive documentation is at `/api/docs`, and
OpenAPI is at `/api/openapi.json`. MCP uses Streamable HTTP at `/mcp` with six
read-only tools. [SKILL.md](SKILL.md) describes how agents should use these
reads. Neither interface can ingest bank transactions or change the ledger.

Liveness is `/healthz`; readiness is `/readyz`. Readiness checks access to the
ledger without making Plaid requests.

### Security

REST and MCP do not require an API token or Authorization header: run them on
a trusted network or behind an authenticated proxy.

## Configuration

Copy [config.sample.toml](config.sample.toml) to `~/.config/dinero/config.toml`,
or select another file with `--config` or `DINERO_CONFIG_FILE`. Read-only
commands need database configuration; Plaid credentials are required only for
Plaid operations.

```toml
timezone = "US/Central"

[database]
connection_string = "sqlite:///data/dinero.db"

[server]
host = "127.0.0.1"
port = 8080
# allowed_hosts = ["ledger.example.com"]
# allowed_origins = ["https://app.example.com"]
```

| Variable                   | Default                                         |
| -------------------------- | ----------------------------------------------- |
| `DINERO_CONFIG_FILE`       | `~/.config/dinero/config.toml`                  |
| `DINERO_DATABASE_URL`      | unset (uses the configured connection string)   |
| `DINERO_DATABASE_URL_FILE` | unset (reads the connection string from a file) |

Set only one database URL override. Both SQLite and PostgreSQL URLs, including
`postgresql+psycopg://`, are supported; relative SQLite paths resolve from the
working directory. `dinero config` prints configuration with secrets redacted,
and `serve --host` and `--port` override the configured listener. See the
sample file for Plaid credentials, Item aliases, and account-name mappings.
Keep local configuration, tokens, recovery files, and ledgers out of Git.

## Development

Requires Go matching the version pinned in the Dockerfile and CI, plus just.

```shell
just install
just check
```

`just check` runs Go race tests, vet, and the installer/package/publication-script
checks. PostgreSQL integration tests need `DINERO_TEST_POSTGRES_DSN` pointing to
a disposable test database; otherwise those tests skip. Use synthetic fixtures
and temporary configuration rather than real bank accounts or personal ledgers.

`just api` runs the read-only server using your selected local configuration.
Run `just check` before sending changes. See `AGENTS.md` for project structure
and conventions.

## Build

```zsh
just build
./bin/dinero --version
```

The standalone executable needs no language runtime.

```zsh
just image-check
```
