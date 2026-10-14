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

```zsh
curl -fsSL https://github.com/danielfrg/dinero/releases/latest/download/install.sh | bash
dinero --version
```

The installer verifies the package's SHA-256 checksum and installs `dinero` in
`/usr/local/bin` when writable, otherwise `~/.local/bin`. It prints instructions
if the destination is missing from your `PATH`. Run it again to upgrade.

To choose a release or installation directory:

```zsh
curl -fsSL https://github.com/danielfrg/dinero/releases/latest/download/install.sh \
  | DINERO_VERSION=v1.2.3 DINERO_INSTALL_DIR="$HOME/.local/bin" bash
```

The install URL becomes available once the first Go release is published.
Packages are also available on the [GitHub Releases page](https://github.com/danielfrg/dinero/releases).

To install the newest published build from `main`:

```zsh
curl -fsSL https://github.com/danielfrg/dinero/releases/download/main-latest/install.sh \
  | DINERO_CHANNEL=main bash
```

Successful **Checks** runs for pushes to `main` trigger packages from that
exact commit and update the `main-latest` prerelease. The binary reports a
version such as `v0.0.0-main.gabcdef012345`, identifying the source commit.
Run the same command again to upgrade. The main install URL becomes available
after the first successful main publish. `DINERO_INSTALL_DIR` works for either
channel; `DINERO_CHANNEL=main` cannot be combined with `DINERO_VERSION`.

Pushing a `v*` version tag publishes a tagged release. The default installer
selects the latest stable tagged release. Main builds and version tags with a
prerelease suffix use a separate release channel. Manual **Release** runs
build the `dinero-release` artifact without publishing it.

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

## Docker image

GitHub Actions builds and tests Linux AMD64 and ARM64 images. Successful checks
on `main` publish `ghcr.io/danielfrg/dinero:latest` and `sha-<full-commit>`;
version tags publish matching versioned images. Pull requests only build and
test images. Images become available after the first successful publication.
The workflow records the published digest for deployments to pin.

Build and test an image locally:

```zsh
docker build -t dinero:local .
bash scripts/container-smoke.sh dinero:local
```

The image starts `dinero serve` on port 8080 as UID/GID 65532. Mount a readable
configuration directory and expose the port:

```zsh
docker run --rm --read-only \
  -p 127.0.0.1:8080:8080 \
  -e DINERO_CONFIG_FILE=/run/dinero/config.toml \
  --mount type=bind,src=/path/to/container-config,dst=/run/dinero,readonly \
  ghcr.io/danielfrg/dinero:latest
```

The mounted `config.toml` must be readable by the container user. PostgreSQL
connections use the same database configuration as the CLI. For ingress or
internal Service names, append `--allowed-host=dinero.example.com,dinero-api`
and, for browser clients, `--allowed-origin=https://dinero.example.com`.
These extend the allowlists in `[server]` without changing the shared config.
The container serves reads; run ingestion and bank linking as separate CLI jobs.

On first publication, set the GHCR package visibility to public before using
anonymous pulls. See [GitHub's container registry documentation](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry).
