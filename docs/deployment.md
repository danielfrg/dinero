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
3. Against that restored copy, compare `search`, `balance`, `build-cache`, `mkrules`, `mkdataset`, and CSV import previews with `testdata/compat/check.py` and the PostgreSQL contract suite. Use only synthetic data for automated tests; production comparisons should be read-only and reviewable.
4. Start the Go image with the read token and intended DB config. Verify `/readyz`, an authorized REST read, and MCP initialize/list/call through the official SDK. Preview Go ingestion, then explicitly switch the scheduled writer to `transactions --yes` after review.
5. If rollback is needed, pause the Go writer first, restore the backed-up DB/config if a write must be undone, then start the Python writer. Never have both writers active. Keep the previous image and schedule available until the cutover is accepted.

`mkdataset` generates both outputs completely before replacing the destination files, but the two filenames cannot be committed in one filesystem rename. If replacement fails between renames, rerun `mkdataset` to regenerate the pair. The source database is unchanged. PyPI package publication remains a separate legacy workflow; the Go release workflow only uploads build artifacts and does not deploy or publish an image.
