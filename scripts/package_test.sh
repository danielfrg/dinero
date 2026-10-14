#!/usr/bin/env bash
# Exercise the real release packager without accessing personal configuration.
set -euo pipefail
repo_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
work_dir=$(mktemp -d)
trap 'rm -rf "$work_dir"' EXIT

case "$(uname -s)" in
    Darwin) platform_os=darwin ;;
    Linux) platform_os=linux ;;
    *) printf 'Unsupported test platform\n' >&2; exit 1 ;;
esac
case "$(uname -m)" in
    arm64|aarch64) platform_arch=arm64 ;;
    amd64|x86_64) platform_arch=amd64 ;;
    *) printf 'Unsupported test architecture\n' >&2; exit 1 ;;
esac
version=v0.0.0-test.1+build.5
archive="dinero_${version#v}_${platform_os}_${platform_arch}.tar.gz"

bash "$repo_dir/scripts/package.sh" "$version" "$platform_os" "$platform_arch" "$work_dir/packages"
mkdir "$work_dir/extracted"
tar -xzf "$work_dir/packages/$archive" -C "$work_dir/extracted"
[ -f "$work_dir/extracted/dinero" ] && [ -x "$work_dir/extracted/dinero" ]
[ -f "$work_dir/extracted/LICENSE.txt" ]
[ -f "$work_dir/extracted/README.md" ]
[ "$("$work_dir/extracted/dinero" --version)" = "dinero version $version" ]
if command -v sha256sum >/dev/null 2>&1; then
    (cd "$work_dir/packages" && sha256sum -c "$archive.sha256")
else
    (cd "$work_dir/packages" && shasum -a 256 -c "$archive.sha256")
fi
printf 'PASS native package contents, version, executable mode and checksum\n'

cp "$work_dir/packages/$archive" "$work_dir/original.tar.gz"
cp "$work_dir/packages/$archive.sha256" "$work_dir/original.sha256"
if GOFLAGS=-invalid-dinero-test-flag bash "$repo_dir/scripts/package.sh" "$version" "$platform_os" "$platform_arch" "$work_dir/packages" >"$work_dir/build-failure.log" 2>&1; then
    printf 'FAIL broken build unexpectedly succeeded\n' >&2; exit 1
fi
cmp "$work_dir/packages/$archive" "$work_dir/original.tar.gz"
cmp "$work_dir/packages/$archive.sha256" "$work_dir/original.sha256"
printf 'PASS failed build preserves existing release package\n'

for bad_version in '../unsafe' 'v1.2' 'v1.2.3;echo' 'v01.2.3' 'v1.2.3-rc.01'; do
    if bash "$repo_dir/scripts/package.sh" "$bad_version" "$platform_os" "$platform_arch" "$work_dir/rejected" >"$work_dir/rejected.log" 2>&1; then
        printf 'FAIL accepted unsafe or malformed version: %s\n' "$bad_version" >&2; exit 1
    fi
done
if bash "$repo_dir/scripts/package.sh" "$version" windows amd64 "$work_dir/rejected" >"$work_dir/rejected.log" 2>&1; then
    printf 'FAIL accepted unsupported OS\n' >&2; exit 1
fi
if bash "$repo_dir/scripts/package.sh" "$version" linux 386 "$work_dir/rejected" >"$work_dir/rejected.log" 2>&1; then
    printf 'FAIL accepted unsupported architecture\n' >&2; exit 1
fi
[ ! -e "$work_dir/rejected" ]
printf 'PASS malformed version and unsupported platform validation\n'
