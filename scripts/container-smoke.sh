#!/usr/bin/env bash
# Exercise the shipped image using synthetic data, never a local Dinero config.
set -euo pipefail

image_ref=${1:?usage: container-smoke.sh IMAGE}
smoke_dir=$(mktemp -d "${TMPDIR:-/tmp}/dinero-container.XXXXXX")
smoke_suffix=$(basename "$smoke_dir" | tr -cd '[:alnum:]')
container_name="dinero-smoke-$smoke_suffix"
container_created=false

cleanup() {
    local result=$?
    if [ "$container_created" = true ]; then
        if [ "$result" -ne 0 ]; then docker logs "$container_name" >&2 || true; fi
        docker rm -f "$container_name" >/dev/null 2>&1 || true
    fi
    # The read-only bind fixture must become writable before its owner removes it.
    chmod -R u+w "$smoke_dir" 2>/dev/null || true
    rm -rf "$smoke_dir" || { [ "$result" -ne 0 ] || result=1; }
    return "$result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

[[ $(docker image inspect "$image_ref" --format '{{.Config.User}}') == 65532:65532 ]]
docker run --rm --read-only --entrypoint /dinero "$image_ref" --version

go run ./testdata/compat/server_smoke.go fixture "$smoke_dir/fixture"
chmod 0555 "$smoke_dir/fixture"
chmod 0444 "$smoke_dir/fixture"/*
docker run -d --name "$container_name" --read-only \
    --tmpfs /tmp:rw,noexec,nosuid,size=16m --cap-drop ALL \
    --security-opt no-new-privileges --pids-limit 128 \
    -p 127.0.0.1::8080 \
    -e DINERO_CONFIG_FILE=/run/dinero/config.toml \
    -v "$smoke_dir/fixture:/run/dinero:ro" "$image_ref" >/dev/null
container_created=true
base_url="http://$(docker port "$container_name" 8080/tcp)"

ready=false
for _ in {1..60}; do
    if curl --fail --silent --max-time 2 "$base_url/readyz" >/dev/null; then
        ready=true
        break
    fi
    sleep .2
done
[ "$ready" = true ] || { printf 'Container did not become ready: %s\n' "$base_url" >&2; exit 1; }
curl --fail --silent --show-error "$base_url/healthz" >/dev/null
content_type=$(curl --fail --silent --show-error -o "$smoke_dir/index.html" \
    -w '%{content_type}' "$base_url/")
[[ "$content_type" == text/html* ]]
root_html=$(cat "$smoke_dir/index.html")
[[ "$root_html" == *'<title>Dinero</title>'* ]]
[[ "$root_html" == *'Dinero API'* ]]
curl --fail --silent --show-error "$base_url/api/docs" >/dev/null
curl --fail --silent --show-error "$base_url/api/openapi.json" >/dev/null
go run ./testdata/compat/server_smoke.go rest "$base_url"
DINERO_MCP_URL="$base_url/mcp" go run ./testdata/compat/mcp_smoke.go

# SIGTERM must reach the Go executable and finish without Docker killing it.
docker stop --time 10 "$container_name" >/dev/null
[[ $(docker inspect "$container_name" --format '{{.State.ExitCode}}') == 0 ]]
printf 'Container verified: nonroot, read-only filesystem/data, HTML, probes, anonymous REST/MCP and graceful shutdown.\n'
