# Go service deployment and cutover

The deployable image runs `dinero serve --host 0.0.0.0 --port 8080` as UID/GID 65532. It contains the statically built Go executable, CA certificates, and timezone data. The image does not contain Python, Node, the Streamlit dashboard, a config file, or bank credentials. It serves read-only REST at `/api` and Streamable HTTP MCP at `/mcp`; see [MCP access](mcp.md). `/healthz` checks the process and `/readyz` checks the database. Put TLS at a trusted proxy for remote access and restrict the listener to trusted networks.

Build a local image with `docker build --build-arg VERSION="$(git describe --tags --always)" -t dinero:local .`. No registry upload is implied. The config can keep its existing `[database]` and `[plaid]` sections. A deployment can provide the database connection through `DINERO_DATABASE_URL_FILE` so credentials stay outside the image. Use a separate read token of at least 16 non-whitespace bytes in `DINERO_API_TOKEN_FILE`; it is unrelated to Plaid tokens. The mounted files must be readable by UID 65532. Use a private host directory owned by UID 65532, mode 0700, with each secret file mode 0400. Keep the config and read token out of Git and Docker build context.

Example Compose wiring, after preparing `/srv/dinero-secrets/config.toml`, `/srv/dinero-secrets/database-url`, and `/srv/dinero-secrets/read-token` with those permissions:

```yaml
services:
  dinero:
    image: dinero:local
    user: "65532:65532"
    ports: ["127.0.0.1:8080:8080"]
    environment:
      DINERO_CONFIG_FILE: /run/dinero/config.toml
      DINERO_DATABASE_URL_FILE: /run/dinero/database-url
      DINERO_API_TOKEN_FILE: /run/dinero/read-token
    volumes:
      - /srv/dinero-secrets:/run/dinero:ro
    read_only: true
    security_opt: ["no-new-privileges:true"]
```

Set `[server].allowed_hosts` to the actual Host header your proxy forwards and `allowed_origins` for browser clients. The container binds every interface internally; the example publishes only localhost on the host. For an external probe, `curl -fsS http://127.0.0.1:8080/readyz` should return `ready`. A protected request needs `Authorization: Bearer <read-token>`; `/api/accounts` and `/api/openapi.json` can be used to check API access. The read token and config are loaded at startup. After rotation or a config change, restart the service; changing mounted file bytes alone does not refresh the running process. Linking on a laptop does not update a deployed secret file; explicitly refresh that copy and restart.

Run bank ingestion as a separate, scheduled `dinero transactions --yes` job with the intended writable config, database, and state directory. It must have Plaid credentials and write access to the database. The read-only server never performs ingestion. Preview with `dinero transactions --dry-run --json` using the same job configuration before scheduling. Keep a single writer: the Python and Go ingestion jobs must never run concurrently.

## Cutover and rollback

1. Pause the Python writer and verify it has exited. Record its schedule and image/version.
2. Back up the PostgreSQL database and the complete config/secret files using the existing encrypted backup procedure. Confirm a disposable restore can be read by both Python and Go, including Budget's direct SQL queries against `public.transactions`. The eight legacy columns stay unchanged.
3. Point a separate, sanitized config at the disposable restore with read-only database credentials. Run the Python and Go `search` and `balance` commands below against that same copy, then compare parsed JSON. Select representative accounts and date windows, including boundary dates and nullable rows. Run `testdata/compat/check.py` and the PostgreSQL contract suite separately as synthetic preflight checks; they create their own fixtures and never compare the restored ledger.
4. Start the Go image with the read token and intended DB config. Verify `/readyz`, an authorized REST read, and MCP initialize/list/call through the official SDK. Preview Go ingestion, then explicitly switch the scheduled writer to `transactions --yes` after review.
5. If rollback is needed, pause the Go writer first, restore the backed-up DB/config if a write must be undone, then start the Python writer. Never have both writers active. Keep the previous image and schedule available until the cutover is accepted.

Run this comparison from the repository checkout after building `bin/dinero-go`. Prepare a private config file containing the restored copy's read-only database URL, the original timezone, and empty Plaid credentials/tokens. Python still requires the legacy Plaid section structure, even for read commands. For example, use the following shape with the actual restored-copy URL in the private file:

```toml
timezone = "US/Central"
[plaid]
client_id = ""
secret = ""
env = "development"
products = "transactions"
[plaid.tokens]
[plaid.account_id_to_name]
[database]
connection_string = "postgresql+psycopg://restore_reader:password@restore-host:5432/restore_db"
```

Keep the restored database isolated from the live writer. Set the account and dates to values present in that copy; repeat for several windows. The temporary directory holds transaction output, so remove it through the normal secure cleanup procedure when the review is complete.

```zsh
umask 077
cutover_config=/path/to/disposable-restore-readonly-config.toml
cutover_work=$(mktemp -d)
mkdir -p "$cutover_work/xdg"
cutover_account='Test Account'
cutover_after=2026-09-01
cutover_before=2026-09-03
cutover_through=2026-09-03

env -u DINERO_DATABASE_URL -u DINERO_DATABASE_URL_FILE -u DINERO_API_TOKEN_FILE \
  DINERO_CONFIG_FILE="$cutover_config" XDG_CONFIG_HOME="$cutover_work/xdg" \
  uv run --offline python -m dinero.cli.main search --account "$cutover_account" \
  --after "$cutover_after" --before "$cutover_before" --sort date --asc \
  --limit 500 --json > "$cutover_work/python-search.json"
env -u DINERO_DATABASE_URL -u DINERO_DATABASE_URL_FILE -u DINERO_API_TOKEN_FILE \
  DINERO_CONFIG_FILE="$cutover_config" XDG_CONFIG_HOME="$cutover_work/xdg" \
  DINERO_STATE_DIR="$cutover_work/xdg/dinero" \
  ./bin/dinero-go search --account "$cutover_account" \
  --after "$cutover_after" --before "$cutover_before" --sort date --asc \
  --limit 500 --json > "$cutover_work/go-search.json"

env -u DINERO_DATABASE_URL -u DINERO_DATABASE_URL_FILE -u DINERO_API_TOKEN_FILE \
  DINERO_CONFIG_FILE="$cutover_config" XDG_CONFIG_HOME="$cutover_work/xdg" \
  uv run --offline python -m dinero.cli.main balance --account "$cutover_account" \
  --through "$cutover_through" --json > "$cutover_work/python-balance.json"
env -u DINERO_DATABASE_URL -u DINERO_DATABASE_URL_FILE -u DINERO_API_TOKEN_FILE \
  DINERO_CONFIG_FILE="$cutover_config" XDG_CONFIG_HOME="$cutover_work/xdg" \
  DINERO_STATE_DIR="$cutover_work/xdg/dinero" \
  ./bin/dinero-go balance --account "$cutover_account" \
  --through "$cutover_through" --json > "$cutover_work/go-balance.json"

python3 - "$cutover_work" <<'PY'
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
def read(name):
    return json.loads((root / name).read_text())

# Python has no deterministic tie-break for equal date values; compare rows by ID.
assert sorted(read('python-search.json'), key=lambda row: row['id']) == sorted(
    read('go-search.json'), key=lambda row: row['id']
), 'search results differ'
assert read('python-balance.json') == read('go-balance.json'), 'balances differ'
print('Restored-copy search and balance JSON match')
PY
```

The CLI search limit is 500 rows. Narrow each window until neither result is truncated; repeat across accounts and dates as needed. A mismatch requires review before switching the writer. These commands only read the restored database and write comparison files under `cutover_work`.

`mkdataset` generates both outputs completely before replacing the destination files, but the two filenames cannot be committed in one filesystem rename. If replacement fails between renames, rerun `mkdataset` to regenerate the pair. The source database is unchanged. PyPI package publication remains a separate legacy workflow; the Go release workflow only uploads build artifacts and does not deploy or publish an image.
