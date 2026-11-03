#!/usr/bin/env bash
# Assemble the GitHub Pages site that serves the unstable main channel.
# The site holds only the newest main build; each deploy replaces it entirely.
set -euo pipefail

die() { printf '%s\n' "$*" >&2; exit 1; }
if [ "$#" -ne 4 ]; then
    die "Usage: $0 VERSION FULL_COMMIT_SHA ASSET_DIR OUTPUT_DIR"
fi
version=$1
commit=$2
asset_dir=$3
out_dir=$4
commit_pattern='^[0-9a-f]{40}$'
[[ $commit =~ $commit_pattern ]] || die 'Commit must be a full, lowercase 40-character SHA'
[ "$version" = "v0.0.0-main.g${commit:0:12}" ] || die 'Version must match v0.0.0-main.g<first 12 commit characters>'
[ -d "$asset_dir" ] || die "Missing asset directory: $asset_dir"
asset_dir=$(cd "$asset_dir" && pwd)
if [ -e "$out_dir" ]; then
    [ -d "$out_dir" ] && [ -z "$(ls -A "$out_dir")" ] || die "Output directory must be empty: $out_dir"
fi
if command -v sha256sum >/dev/null 2>&1; then
    hash_tool=sha256sum
elif command -v shasum >/dev/null 2>&1; then
    hash_tool=shasum
else
    die 'A SHA-256 tool (sha256sum or shasum) is required'
fi

archives=()
for os in darwin linux; do
    for arch in amd64 arm64; do
        archives+=("dinero_${version#v}_${os}_${arch}.tar.gz")
    done
done
for asset in "${archives[@]}" SHA256SUMS install.sh VERSION; do
    [ -f "$asset_dir/$asset" ] && [ -s "$asset_dir/$asset" ] || die "Missing or empty release asset: $asset"
done
awk -v version="$version" 'NR != 1 || $0 != version {bad=1} END {exit (bad || NR != 1)}' \
    "$asset_dir/VERSION" || die 'VERSION must contain exactly the requested version'
for archive in "${archives[@]}"; do
    expected=$(awk -v asset="$archive" '
        $2 == asset {
            count++
            if (NF != 2 || length($1) != 64 || $1 !~ /^[0-9a-f]+$/) bad=1
            digest=$1
        }
        END {if (count != 1 || bad) exit 1; print digest}
    ' "$asset_dir/SHA256SUMS") || die "Missing, duplicate or malformed checksum for $archive"
    if [ "$hash_tool" = sha256sum ]; then
        actual=$(sha256sum "$asset_dir/$archive")
    else
        actual=$(shasum -a 256 "$asset_dir/$archive")
    fi
    [ "${actual%% *}" = "$expected" ] || die "Invalid checksum for $archive"
done

mkdir -p "$out_dir/main"
for asset in "${archives[@]}" SHA256SUMS install.sh VERSION; do
    cp "$asset_dir/$asset" "$out_dir/main/$asset"
done
: > "$out_dir/.nojekyll"
cat > "$out_dir/index.html" <<HTML
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Dinero</title>
<style>
body { font: 16px/1.5 system-ui, sans-serif; max-width: 42rem; margin: 3rem auto; padding: 0 1rem; }
pre { background: #8881; padding: .75rem 1rem; overflow-x: auto; border-radius: 6px; }
</style>
</head>
<body>
<h1>Dinero</h1>
<p>Install the latest release for macOS or Linux on AMD64 or ARM64:</p>
<pre>curl -fsSL https://github.com/danielfrg/dinero/releases/latest/download/install.sh | bash</pre>
<p>Install the newest unstable build from <code>main</code> (currently <code>${version}</code>, commit <code>${commit:0:12}</code>):</p>
<pre>curl -fsSL https://danielfrg.github.io/dinero/main/install.sh | DINERO_CHANNEL=main bash</pre>
<p><a href="https://github.com/danielfrg/dinero">Source on GitHub</a></p>
</body>
</html>
HTML
printf 'Built Pages site for %s from %s in %s\n' "$version" "$commit" "$out_dir"
