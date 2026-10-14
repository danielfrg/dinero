#!/usr/bin/env bash
# Build a versioned, standalone Go CLI release archive.
set -euo pipefail

if [ "$#" -lt 3 ] || [ "$#" -gt 4 ]; then
    printf 'Usage: %s VERSION GOOS GOARCH [OUTPUT_DIR]\n' "$0" >&2
    exit 1
fi
version=$1
platform_os=$2
platform_arch=$3
repo_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
version_pattern='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$'
if [[ ! $version =~ $version_pattern ]]; then
    printf 'Version must be a v-prefixed release version, such as v1.2.3 or v1.2.3-rc.1\n' >&2
    exit 1
fi
prerelease=${version%%+*}
case "$prerelease" in
    *-*)
        prerelease=${prerelease#*-}
        while [ -n "$prerelease" ]; do
            identifier=${prerelease%%.*}
            if [[ $identifier =~ ^[0-9]+$ ]] && [ "${#identifier}" -gt 1 ] && [[ $identifier == 0* ]]; then
                printf 'Numeric prerelease identifiers cannot start with zero\n' >&2
                exit 1
            fi
            [ "$prerelease" != "$identifier" ] || break
            prerelease=${prerelease#*.}
        done
        ;;
esac
case "$platform_os" in darwin|linux) ;; *) printf 'Unsupported OS: %s\n' "$platform_os" >&2; exit 1 ;; esac
case "$platform_arch" in amd64|arm64) ;; *) printf 'Unsupported architecture: %s\n' "$platform_arch" >&2; exit 1 ;; esac

output_dir=${4:-"$repo_dir/dist"}
mkdir -p "$output_dir"
output_dir=$(cd "$output_dir" && pwd)
stage_dir=$(mktemp -d)
archive_temp=''
checksum_temp=''
cleanup() {
    rm -rf "$stage_dir"
    [ -z "$archive_temp" ] || rm -f "$archive_temp"
    [ -z "$checksum_temp" ] || rm -f "$checksum_temp"
}
trap cleanup EXIT

(cd "$repo_dir" && CGO_ENABLED=0 GOOS="$platform_os" GOARCH="$platform_arch" \
    go build -trimpath -ldflags="-s -w -X github.com/danielfrg/dinero/internal/cli.Version=$version" \
    -o "$stage_dir/dinero" ./cmd/dinero)
cp "$repo_dir/LICENSE.txt" "$repo_dir/README.md" "$stage_dir/"
archive="dinero_${version#v}_${platform_os}_${platform_arch}.tar.gz"
archive_temp=$(mktemp "$output_dir/.dinero-package.XXXXXX")
COPYFILE_DISABLE=1 tar -czf "$archive_temp" -C "$stage_dir" dinero LICENSE.txt README.md
checksum_temp=$(mktemp "$output_dir/.dinero-checksum.XXXXXX")
if command -v sha256sum >/dev/null 2>&1; then
    digest=$(sha256sum "$archive_temp")
elif command -v shasum >/dev/null 2>&1; then
    digest=$(shasum -a 256 "$archive_temp")
else
    printf 'A SHA-256 tool (sha256sum or shasum) is required\n' >&2
    exit 1
fi
printf '%s  %s\n' "${digest%% *}" "$archive" > "$checksum_temp"
mv -f "$archive_temp" "$output_dir/$archive"
mv -f "$checksum_temp" "$output_dir/$archive.sha256"
printf 'Built %s\n' "$output_dir/$archive"
