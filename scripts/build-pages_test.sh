#!/usr/bin/env bash
# Tests for scripts/build-pages.sh.
set -euo pipefail

repo_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
builder="$repo_dir/scripts/build-pages.sh"
root=$(mktemp -d "${TMPDIR:-/tmp}/dinero-pages-test.XXXXXX")
trap 'rm -rf "$root"' EXIT
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }

commit=0123456789abcdef0123456789abcdef01234567
version="v0.0.0-main.g${commit:0:12}"
if command -v sha256sum >/dev/null 2>&1; then hash_cmd=(sha256sum); else hash_cmd=(shasum -a 256); fi

make_assets() {
    local dir=$1
    mkdir -p "$dir"
    : > "$dir/SHA256SUMS"
    for os in darwin linux; do
        for arch in amd64 arm64; do
            local name="dinero_${version#v}_${os}_${arch}.tar.gz"
            printf 'archive %s' "$name" > "$dir/$name"
            (cd "$dir" && "${hash_cmd[@]}" "$name") >> "$dir/SHA256SUMS"
        done
    done
    printf '#!/bin/bash\n' > "$dir/install.sh"
    printf '%s\n' "$version" > "$dir/VERSION"
}

expect_failure() {
    local message=$1; shift
    if "$@" >/dev/null 2>"$root/err"; then fail "expected failure: $message"; fi
    grep -q -- "$message" "$root/err" || fail "expected '$message', got: $(cat "$root/err")"
}

# Success: site layout and landing page.
make_assets "$root/ok"
"$builder" "$version" "$commit" "$root/ok" "$root/site" >/dev/null
for f in main/SHA256SUMS main/install.sh main/VERSION index.html .nojekyll \
    "main/dinero_${version#v}_darwin_amd64.tar.gz" "main/dinero_${version#v}_linux_arm64.tar.gz"; do
    [ -s "$root/site/$f" ] || [ "$f" = .nojekyll ] && [ -e "$root/site/$f" ] || fail "missing $f"
done
grep -q "$version" "$root/site/index.html" || fail 'landing page lacks version'
grep -q 'danielfrg.github.io/dinero/main/install.sh' "$root/site/index.html" || fail 'landing page lacks install command'

# Failures.
expect_failure 'Output directory must be empty' "$builder" "$version" "$commit" "$root/ok" "$root/site"
expect_failure 'full, lowercase' "$builder" "$version" abc "$root/ok" "$root/s1"
expect_failure 'Version must match' "$builder" v1.2.3 "$commit" "$root/ok" "$root/s2"
expect_failure 'Missing asset directory' "$builder" "$version" "$commit" "$root/none" "$root/s3"

make_assets "$root/missing"; rm "$root/missing/install.sh"
expect_failure 'Missing or empty release asset: install.sh' "$builder" "$version" "$commit" "$root/missing" "$root/s4"

make_assets "$root/badsum"; printf 'tampered' > "$root/badsum/dinero_${version#v}_linux_amd64.tar.gz"
expect_failure 'Invalid checksum' "$builder" "$version" "$commit" "$root/badsum" "$root/s5"

make_assets "$root/badver"; printf 'v9.9.9\n' > "$root/badver/VERSION"
expect_failure 'VERSION must contain' "$builder" "$version" "$commit" "$root/badver" "$root/s6"

printf 'build-pages tests passed\n'
