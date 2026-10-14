#!/usr/bin/env bash
# Publish a mutable main prerelease without changing the stable release channel.
set -euo pipefail

die() { printf '%s\n' "$*" >&2; exit 1; }
if [ "$#" -ne 3 ]; then
    die "Usage: $0 VERSION FULL_COMMIT_SHA ASSET_DIR"
fi
version=$1
commit=$2
asset_dir=$3
commit_pattern='^[0-9a-f]{40}$'
[[ $commit =~ $commit_pattern ]] || die 'Commit must be a full, lowercase 40-character SHA'
[ "$version" = "v0.0.0-main.g${commit:0:12}" ] || die 'Version must match v0.0.0-main.g<first 12 commit characters>'
[ -d "$asset_dir" ] || die "Missing asset directory: $asset_dir"
asset_dir=$(cd "$asset_dir" && pwd)
[ -n "${GH_REPO:-}" ] || die 'GH_REPO is required'
[ -n "${GH_TOKEN:-}" ] || die 'GH_TOKEN is required'
command -v gh >/dev/null 2>&1 || die 'The GitHub CLI (gh) is required'
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
archive_paths=()
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
    archive_paths+=("$asset_dir/$archive")
done

head_sha=$(gh api "repos/$GH_REPO/git/ref/heads/main" --jq .object.sha)
[[ $head_sha =~ $commit_pattern ]] || die 'GitHub returned an invalid main head SHA'
if [ "$head_sha" != "$commit" ]; then
    printf 'Skipping stale main build %s; current main is %s\n' "$commit" "$head_sha"
    exit 0
fi

tag=main-latest
# A successful, authenticated list also finds leftover drafts. A failed read is
# never interpreted as an absent release (unlike a blanket release-view fallback).
release_info=$(gh api "repos/$GH_REPO/releases" --paginate --jq \
    '.[] | select(.tag_name == "main-latest") | [.id, .prerelease, .draft, (.immutable // false)] | @tsv')
title="Main build $version"
notes="Latest successful main build. Source commit: $commit"
if [ -n "$release_info" ]; then
    case "$release_info" in *$'\n'*) die 'Multiple main-latest releases returned by GitHub' ;; esac
    IFS=$'\t' read -r release_id prerelease draft immutable <<< "$release_info"
    [[ $release_id =~ ^[0-9]+$ ]] || die 'GitHub returned an invalid rolling release'
    [ "$prerelease" = true ] || die 'Refusing to overwrite a main-latest release that is not a prerelease'
    [ "$immutable" = false ] || die 'Refusing to overwrite an immutable main-latest release'
    case "$draft" in true|false) ;; *) die 'GitHub returned an invalid rolling release draft state' ;; esac
else
    gh release create "$tag" --target "$commit" --draft --prerelease --latest=false \
        --title "$title" --notes "$notes"
fi

# Commit-named archives are ready before any shared metadata points at them.
# Leaving VERSION until the last upload makes partial upload failures recoverable.
gh release upload "$tag" "${archive_paths[@]}" --clobber
gh release upload "$tag" "$asset_dir/SHA256SUMS" "$asset_dir/install.sh" --clobber
gh release upload "$tag" "$asset_dir/VERSION" --clobber

# Draft releases may not yet have created their Git tag. Query matching refs
# successfully and filter the exact tag, then create or force-update only it.
tag_sha=$(gh api "repos/$GH_REPO/git/matching-refs/tags/$tag" --jq \
    '.[] | select(.ref == "refs/tags/main-latest") | .object.sha')
if [ -n "$tag_sha" ]; then
    [[ $tag_sha =~ $commit_pattern ]] || die 'GitHub returned an invalid rolling tag SHA'
    gh api "repos/$GH_REPO/git/refs/tags/$tag" --method PATCH -f sha="$commit" -F force=true >/dev/null
else
    gh api "repos/$GH_REPO/git/refs" --method POST -f ref="refs/tags/$tag" -f sha="$commit" >/dev/null
fi
gh release edit "$tag" --target "$commit" --draft=false --prerelease --latest=false \
    --title "$title" --notes "$notes"

# Cleanup follows successful publication and touches only recognized main builds.
asset_names=$(gh release view "$tag" --json assets --jq '.assets[].name')
rolling_archive_pattern='^dinero_0\.0\.0-main\.g[0-9a-f]{12}_(darwin|linux)_(amd64|arm64)\.tar\.gz$'
while IFS= read -r asset; do
    [[ $asset =~ $rolling_archive_pattern ]] || continue
    keep=false
    for archive in "${archives[@]}"; do
        [ "$asset" != "$archive" ] || keep=true
    done
    [ "$keep" = false ] || continue
    gh release delete-asset "$tag" "$asset" --yes
done <<< "$asset_names"
printf 'Published %s from %s\n' "$version" "$commit"
