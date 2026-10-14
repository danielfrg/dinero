# dinero

Tools to manage my finances.

- Transaction tracker
  - Plaid for collecting transactions from financial institutions
  - SQL Database for storing transactions
- MCP server for agents to read transactions

I use Mathesar as a viewer and editor and Metabase to create dashboards.
Your imagination is the limit here!

## Install

Install the latest Go release for macOS or Linux, on Intel/AMD or ARM64:

```shell
curl -fsSL https://github.com/danielfrg/dinero/releases/latest/download/install.sh | bash
dinero --version
```

To install the newest published build from `main`:

```shell
curl -fsSL https://github.com/danielfrg/dinero/releases/download/main-latest/install.sh \
  | DINERO_CHANNEL=main bash
```

Use a docker image:

```shell
docker run -p 127.0.0.1:8080:8080 \
  -e DINERO_CONFIG_FILE=/run/dinero/config.toml \
  --mount type=bind,src=/path/to/container-config,dst=/run/dinero \
  ghcr.io/danielfrg/dinero:latest
```

## Build

```shell
just build
```

Copy [config.sample.toml](config.sample.toml) to `~/.config/dinero/config.toml`.
Use `DINERO_CONFIG_FILE` or `--config` to select a different file.

PostgreSQL and SQLite URLs use the existing `[database].connection_string` format, including `postgresql+psycopg://` and `sqlite:///`.

## Requirements

### Plaid

- Create a [Plaid account](https://dashboard.plaid.com/)
- You need development access to handle multiple accounts
  - You can use it on development mode for free up to 100 accounts
- I asked for production access and it was simple, just fill the form and say
  you are not a company and that you will use it for personal use
- Get your `client_id` and `secret` from the [Plaid dashboard](https://dashboard.plaid.com/developers/keys)

Make sure your config file contains your Plaid credentials alongside your existing database configuration:

```
[plaid]
client_id = "YOUR_CLIENT_ID"
secret = "YOUR_PRODUCTION_SECRET"
env = "production"
products = "transactions"
```

Start a new connection with:

```
dinero link <name: e.g. chase>

# To update/refresh a connection
dinero link --update <name: e.g. chase>
```

And follow the auth flow with your institution.

## Usage

```
# See transactions from Plaid
dinero transactions

# commit them to the database
dinero transactions --commit
```

## Read-only API

```
dinero serve
```

- REST reads are under `/api`, with OpenAPI at `/api/openapi.json`
- MCP uses `/mcp` and six read-only tools

REST and MCP connections do not require an API token or an `Authorization` header.

This will expose all the data, run in a secure network only.
