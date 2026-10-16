# Dinero

Personal finance tools for collecting transactions through Plaid or CSV, storing
them in SQLite or PostgreSQL, and reading them through a CLI, REST API, or MCP.
Use database tools such as Mathesar or Metabase for editing and dashboards.

## Installation

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

### Docker

Prepare `container-config/config.toml` with your database connection, then run:

```zsh
docker run -d --name dinero \
  -p 127.0.0.1:8080:8080 \
  -e DINERO_CONFIG_FILE=/run/dinero/config.toml \
  --mount "type=bind,src=${PWD}/container-config,dst=/run/dinero,readonly" \
  ghcr.io/danielfrg/dinero:latest
```

The image serves REST and MCP on port 8080 as UID/GID 65532. Mounted configuration
and data must be readable by that user. For SQLite, mount an existing initialized
database and use its container path, such as `sqlite:////run/dinero/dinero.db`.
The server does not create or migrate the ledger.

Images support Linux AMD64 and ARM64. `latest` follows published `main` builds;
`sha-<commit>` tags and digests pin exact images. Versioned releases also publish
version tags.

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

Set your Plaid credentials in the configuration, then connect or repair an Item:

```zsh
dinero link bank
dinero link --update bank
```

Follow the Hosted Link flow with your institution. Preview transactions before
committing them to the database:

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
read-only tools. [SKILL.md](SKILL.md) describes how agents should use these reads.

REST and MCP do not require an API token or Authorization header. Run them on a
trusted network or behind an authenticated proxy. Configure allowed hosts and
browser origins when using a remote hostname. Neither interface can ingest bank
transactions or change the ledger.

Liveness is `/healthz`; readiness is `/readyz`. Readiness checks access to the
ledger without making Plaid requests.

## Configuration

Copy [config.sample.toml](config.sample.toml) to `~/.config/dinero/config.toml`.
Use `--config` or `DINERO_CONFIG_FILE` to select another file. Read-only commands
need database configuration; Plaid credentials are required only for Plaid operations.

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

Both SQLite URLs and PostgreSQL URLs, including `postgresql+psycopg://`, are
supported. Relative SQLite paths resolve from the working directory. Keep local
configuration, tokens, recovery files, and ledgers out of Git.

| Variable | Purpose |
| --- | --- |
| `DINERO_CONFIG_FILE` | Override the configuration file path |
| `DINERO_DATABASE_URL` | Override the configured database connection |
| `DINERO_DATABASE_URL_FILE` | Read the database connection from a file |

Set only one database URL override. `dinero config` prints configuration with
secrets redacted. See the sample file for Plaid credentials, Item aliases, and
account-name mappings. `serve --host` and `--port` override the configured listener.

## Development

Use Go matching the version pinned in the Dockerfile and CI, plus just.

```zsh
just install
just check
```

`just check` runs Go race tests, vet, and the installer/package/publication-script
checks. PostgreSQL integration tests need `DINERO_TEST_POSTGRES_DSN` pointing to
a disposable test database; otherwise those tests skip. Use synthetic fixtures
and temporary configuration rather than real bank accounts or personal ledgers.

`just api` runs the read-only server using your selected local configuration.
There is no frontend development server. The project structure is:

```text
cmd/dinero/       Executable entry point
internal/cli/     CLI commands and output
internal/ledger/  Shared query and validation rules
internal/store/   SQLite and PostgreSQL persistence
internal/httpapi/ REST and OpenAPI
internal/mcp/     Read-only MCP tools
internal/server/  Transport wiring and lifecycle
scripts/          Installer, release packaging, and container smoke tests
testdata/compat/  Synthetic compatibility fixtures
```

`PRODUCT.md` describes users and purpose; `DESIGN.md` records interface contracts;
`AGENTS.md` gives contributors the working rules. Keep plans, agent reports, and
local tooling state uncommitted.

## Build

```zsh
just build
./bin/dinero --version
```

The standalone executable needs no frontend or language runtime. Build and
smoke-test the container with:

```zsh
just image-check
```

The smoke test uses synthetic data to verify nonroot/read-only operation, REST,
MCP, probes, and graceful shutdown. Release archives and version injection use
the existing scripts under `scripts/`.
