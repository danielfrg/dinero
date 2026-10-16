#!/bin/bash
# Exercise the real installer without network access or a live installation.
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
INSTALLER="$SCRIPT_DIR/install.sh"
BASH_BIN=$BASH
REAL_CP=$(command -v cp)
TEST_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/dinero-install-test.XXXXXX")
trap 'rm -rf "$TEST_ROOT"' EXIT
mkdir -p "$TEST_ROOT/bin"

# Only the network and platform boundaries are fake; tar and hashes are real.
cat > "$TEST_ROOT/bin/curl" <<'EOF'
#!/bin/bash
set -eu
output=''
url=''
while [ "$#" -gt 0 ]; do
    case "$1" in
        -o|--output) output="$2"; shift 2 ;;
        -w|--write-out) shift 2 ;;
        https://*) url="$1"; shift ;;
        *) shift ;;
    esac
done
base='https://github.com/danielfrg/dinero/releases'
case "$url" in
    "$base/latest")
        [ "${DINERO_CHANNEL:-stable}" != main ] || { printf 'Main must not resolve latest\n' >&2; exit 22; }
        printf '%s' "$FAKE_LATEST_URL"
        ;;
    "$base/download/main-latest/VERSION")
        [ "${FAKE_VERSION_FAIL:-0}" = 0 ] || exit 22
        printf '%s\n' "$FAKE_VERSION_TEXT"
        ;;
    "$base/download/$FAKE_TAG/$FAKE_ASSET")
        [ "${FAKE_ARCHIVE_FAIL:-0}" = 0 ] || exit 22
        "$REAL_CP" "$FIXTURE_ARCHIVE" "$output"
        ;;
    "$base/download/$FAKE_TAG/SHA256SUMS")
        [ "${FAKE_SUMS_FAIL:-0}" = 0 ] || exit 22
        "$REAL_CP" "$FIXTURE_SUMS" "$output"
        ;;
    *) printf 'Unexpected download URL: %s\n' "$url" >&2; exit 22 ;;
esac
EOF
cat > "$TEST_ROOT/bin/uname" <<'EOF'
#!/bin/bash
case "$1" in
    -s) printf '%s\n' "$FAKE_UNAME_S" ;;
    -m) printf '%s\n' "$FAKE_UNAME_M" ;;
    *) exit 1 ;;
esac
EOF
cat > "$TEST_ROOT/bin/cp" <<'EOF'
#!/bin/bash
if [ "${FAKE_COPY_FAIL:-0}" = 1 ]; then
    printf 'incomplete copy' > "$2"
    exit 1
fi
exec "$REAL_CP" "$@"
EOF
chmod +x "$TEST_ROOT/bin/"*
export REAL_CP
ORIGINAL_PATH=$PATH
export PATH="$TEST_ROOT/bin:$ORIGINAL_PATH"
PASSED=0

fail() {
    printf 'FAIL: %s: %s\n' "$CASE_NAME" "$1" >&2
    [ ! -f "$CASE_DIR/output" ] || cat "$CASE_DIR/output" >&2
    exit 1
}

new_case() {
    CASE_NAME=$1
    CASE_DIR="$TEST_ROOT/$CASE_NAME"
    mkdir -p "$CASE_DIR/fixture" "$CASE_DIR/install"
    export DINERO_INSTALL_DIR="$CASE_DIR/install"
    export FAKE_UNAME_S=$2 FAKE_UNAME_M=$3 FAKE_TAG=$4 FAKE_ASSET=$5
    export FAKE_LATEST_URL="https://github.com/danielfrg/dinero/releases/tag/$FAKE_TAG"
    export FIXTURE_ARCHIVE="$CASE_DIR/release.tar.gz" FIXTURE_SUMS="$CASE_DIR/SHA256SUMS"
    unset DINERO_CHANNEL DINERO_VERSION FAKE_ARCHIVE_FAIL FAKE_SUMS_FAIL FAKE_COPY_FAIL FAKE_VERSION_FAIL FAKE_VERSION_TEXT
    printf '#!/bin/sh\nprintf "fixture dinero\\n"\n' > "$CASE_DIR/fixture/dinero"
    chmod 755 "$CASE_DIR/fixture/dinero"
    tar -czf "$FIXTURE_ARCHIVE" -C "$CASE_DIR/fixture" dinero
    write_checksum
    # Failure cases must retain both the contents and permissions of this file.
    printf 'existing dinero\n' > "$DINERO_INSTALL_DIR/dinero"
    chmod 640 "$DINERO_INSTALL_DIR/dinero"
}

write_checksum() {
    local hash
    if command -v sha256sum >/dev/null 2>&1; then
        hash=$(sha256sum "$FIXTURE_ARCHIVE")
    else
        hash=$(shasum -a 256 "$FIXTURE_ARCHIVE")
    fi
    printf '%s  %s\n' "${hash%% *}" "$FAKE_ASSET" > "$FIXTURE_SUMS"
}

expect_success() {
    if ! "$BASH_BIN" "$INSTALLER" > "$CASE_DIR/output" 2>&1; then
        fail 'installer should succeed'
    fi
    [ -f "$DINERO_INSTALL_DIR/dinero" ] && [ ! -L "$DINERO_INSTALL_DIR/dinero" ] || fail 'missing regular binary'
    [ -x "$DINERO_INSTALL_DIR/dinero" ] || fail 'installed binary is not executable'
    [ "$("$DINERO_INSTALL_DIR/dinero")" = 'fixture dinero' ] || fail 'installed wrong contents'
    pass
}

expect_failure() {
    if "$BASH_BIN" "$INSTALLER" > "$CASE_DIR/output" 2>&1; then
        fail 'installer should reject this input'
    fi
    case "$(cat "$CASE_DIR/output")" in
        *"$1"*) ;;
        *) fail "missing useful error mentioning $1" ;;
    esac
    [ "$(cat "$DINERO_INSTALL_DIR/dinero")" = 'existing dinero' ] || fail 'existing binary changed'
    # BSD ls appends @ or + for extended attributes/ACLs after the mode bits.
    [ "$(ls -l "$DINERO_INSTALL_DIR/dinero" | awk '{print substr($1, 1, 10)}')" = '-rw-r-----' ] || fail 'existing mode changed'
    [ "$(find "$DINERO_INSTALL_DIR" -type f | wc -l | tr -d ' ')" = 1 ] || fail 'installation temporary file leaked'
    pass
}

pass() {
    PASSED=$((PASSED + 1))
    printf 'PASS: %s\n' "$CASE_NAME"
}

# Wrong asset/version/platform selection fails at the network boundary.
new_case pinned_linux_amd64 Linux x86_64 v1.2.3 dinero_1.2.3_linux_amd64.tar.gz
export DINERO_VERSION=v1.2.3
expect_success
case "$(cat "$CASE_DIR/output")" in *PATH*) ;; *) fail 'missing PATH guidance' ;; esac

new_case latest_linux_arm64 Linux aarch64 v1.2.3 dinero_1.2.3_linux_arm64.tar.gz
expect_success

new_case explicit_stable Linux x86_64 v1.2.3 dinero_1.2.3_linux_amd64.tar.gz
export DINERO_CHANNEL=stable
expect_success
new_case explicit_stable_pinned Linux x86_64 v1.2.3 dinero_1.2.3_linux_amd64.tar.gz
export DINERO_CHANNEL=stable DINERO_VERSION=1.2.3
expect_success

new_case latest_main Linux x86_64 main-latest dinero_0.0.0-main.g012345abcdef_linux_amd64.tar.gz
export DINERO_CHANNEL=main FAKE_VERSION_TEXT=v0.0.0-main.g012345abcdef
expect_success

new_case missing_main_version Linux x86_64 main-latest dinero_0.0.0-main.g012345abcdef_linux_amd64.tar.gz
export DINERO_CHANNEL=main FAKE_VERSION_FAIL=1
expect_failure 'VERSION'
for bad_main_version in '../unsafe' '' $'v0.0.0-main.g012345abcdef\nv1.2.3'; do
    new_case "malformed_main_version_$PASSED" Linux x86_64 main-latest dinero_0.0.0-main.g012345abcdef_linux_amd64.tar.gz
    export DINERO_CHANNEL=main FAKE_VERSION_TEXT="$bad_main_version"
    expect_failure 'version'
done
for bad_channel in nightly MAIN ''; do
    new_case "invalid_channel_$PASSED" Linux x86_64 v1.2.3 dinero_1.2.3_linux_amd64.tar.gz
    export DINERO_CHANNEL="$bad_channel"
    expect_failure 'DINERO_CHANNEL'
done
for pinned_version in v1.2.3 ''; do
    new_case "main_version_conflict_$PASSED" Linux x86_64 main-latest dinero_0.0.0-main.g012345abcdef_linux_amd64.tar.gz
    export DINERO_CHANNEL=main DINERO_VERSION="$pinned_version"
    expect_failure 'DINERO_VERSION'
done
new_case main_bad_checksum Linux x86_64 main-latest dinero_0.0.0-main.g012345abcdef_linux_amd64.tar.gz
export DINERO_CHANNEL=main FAKE_VERSION_TEXT=v0.0.0-main.g012345abcdef
printf '%064d  %s\n' 0 "$FAKE_ASSET" > "$FIXTURE_SUMS"
expect_failure 'checksum'

new_case pinned_macos_amd64 Darwin amd64 v1.2.3 dinero_1.2.3_darwin_amd64.tar.gz
export DINERO_VERSION=1.2.3
expect_success

new_case prerelease_macos_arm64 Darwin arm64 v1.2.3-rc.1+build.5 dinero_1.2.3-rc.1+build.5_darwin_arm64.tar.gz
export DINERO_VERSION=1.2.3-rc.1+build.5
expect_success

new_case latest_prerelease Linux arm64 v1.2.3-rc.2 dinero_1.2.3-rc.2_linux_arm64.tar.gz
expect_success

new_case pipe_input Linux amd64 v1.2.3 dinero_1.2.3_linux_amd64.tar.gz
export DINERO_VERSION=v1.2.3
if ! cat "$INSTALLER" | "$BASH_BIN" > "$CASE_DIR/output" 2>&1; then fail 'stdin execution failed'; fi
[ "$("$DINERO_INSTALL_DIR/dinero")" = 'fixture dinero' ] || fail 'stdin execution did not install'
pass

new_case sourced Windows i386 v1.2.3 dinero_1.2.3_linux_amd64.tar.gz
if ! "$BASH_BIN" -c 'source "$1"; printf sourced' -- "$INSTALLER" > "$CASE_DIR/output" 2>&1; then fail 'cannot source installer'; fi
[ "$(cat "$CASE_DIR/output")" = sourced ] || fail 'sourcing ran main'
[ "$(cat "$DINERO_INSTALL_DIR/dinero")" = 'existing dinero' ] || fail 'sourcing installed binary'
pass

new_case unsupported_os Windows x86_64 v1.2.3 dinero_1.2.3_linux_amd64.tar.gz
expect_failure 'Unsupported OS'
new_case unsupported_arch Linux i386 v1.2.3 dinero_1.2.3_linux_amd64.tar.gz
expect_failure 'Unsupported architecture'
new_case missing_release Linux x86_64 v1.2.3 dinero_1.2.3_linux_amd64.tar.gz
export FAKE_LATEST_URL=https://github.com/danielfrg/dinero/releases
expect_failure 'release'
new_case foreign_latest_redirect Linux x86_64 v1.2.3 dinero_1.2.3_linux_amd64.tar.gz
export FAKE_LATEST_URL=https://example.com/danielfrg/dinero/releases/tag/v1.2.3
expect_failure 'release'

for invalid_version in '../escape' 'v1.2' '1.2.3/extra' 'v1.2.3;echo bad' '01.2.3' '1.2.3-01' ''; do
    new_case "invalid_version_$PASSED" Linux x86_64 v1.2.3 dinero_1.2.3_linux_amd64.tar.gz
    export DINERO_VERSION="$invalid_version"
    expect_failure 'version'
done

new_case archive_download_failure Linux x86_64 v1.2.3 dinero_1.2.3_linux_amd64.tar.gz
export FAKE_ARCHIVE_FAIL=1
expect_failure 'download'
new_case checksum_download_failure Linux x86_64 v1.2.3 dinero_1.2.3_linux_amd64.tar.gz
export FAKE_SUMS_FAIL=1
expect_failure 'SHA256SUMS'
new_case bad_checksum Linux x86_64 v1.2.3 dinero_1.2.3_linux_amd64.tar.gz
printf '%064d  %s\n' 0 "$FAKE_ASSET" > "$FIXTURE_SUMS"
expect_failure 'checksum'
new_case missing_checksum Linux x86_64 v1.2.3 dinero_1.2.3_linux_amd64.tar.gz
printf '%064d  another.tar.gz\n' 0 > "$FIXTURE_SUMS"
expect_failure 'checksum'
new_case malformed_checksum Linux x86_64 v1.2.3 dinero_1.2.3_linux_amd64.tar.gz
printf 'not-a-sha256  %s\n' "$FAKE_ASSET" > "$FIXTURE_SUMS"
expect_failure 'checksum'
new_case duplicate_checksum Linux x86_64 v1.2.3 dinero_1.2.3_linux_amd64.tar.gz
cat "$FIXTURE_SUMS" "$FIXTURE_SUMS" > "$CASE_DIR/duplicate"
mv "$CASE_DIR/duplicate" "$FIXTURE_SUMS"
expect_failure 'checksum'

new_case symlink_binary Linux x86_64 v1.2.3 dinero_1.2.3_linux_amd64.tar.gz
rm "$CASE_DIR/fixture/dinero"
ln -s /bin/sh "$CASE_DIR/fixture/dinero"
tar -czf "$FIXTURE_ARCHIVE" -C "$CASE_DIR/fixture" dinero
write_checksum
expect_failure 'regular'
new_case nested_binary Linux x86_64 v1.2.3 dinero_1.2.3_linux_amd64.tar.gz
mkdir "$CASE_DIR/fixture/nested"
mv "$CASE_DIR/fixture/dinero" "$CASE_DIR/fixture/nested/dinero"
tar -czf "$FIXTURE_ARCHIVE" -C "$CASE_DIR/fixture" nested/dinero
write_checksum
expect_failure 'dinero'
new_case directory_destination Linux x86_64 v1.2.3 dinero_1.2.3_linux_amd64.tar.gz
rm "$DINERO_INSTALL_DIR/dinero"
mkdir "$DINERO_INSTALL_DIR/dinero"
if "$BASH_BIN" "$INSTALLER" > "$CASE_DIR/output" 2>&1; then fail 'destination directory should fail'; fi
case "$(cat "$CASE_DIR/output")" in *directory*) ;; *) fail 'directory error not explained' ;; esac
[ -z "$(ls -A "$DINERO_INSTALL_DIR/dinero")" ] || fail 'binary was moved inside destination directory'
pass
new_case copy_failure Linux x86_64 v1.2.3 dinero_1.2.3_linux_amd64.tar.gz
export FAKE_COPY_FAIL=1
expect_failure 'copy'

# Dependency failures should explain what is absent before doing any work.
for missing_dependency in curl sha256; do
    new_case "missing_$missing_dependency" Linux x86_64 v1.2.3 dinero_1.2.3_linux_amd64.tar.gz
    mkdir "$CASE_DIR/tools"
    for dependency in awk chmod cp mkdir mktemp mv rm tar uname; do
        ln -s "$(command -v "$dependency")" "$CASE_DIR/tools/$dependency"
    done
    if [ "$missing_dependency" = sha256 ]; then
        ln -s "$TEST_ROOT/bin/curl" "$CASE_DIR/tools/curl"
    fi
    if PATH="$CASE_DIR/tools" "$BASH_BIN" "$INSTALLER" > "$CASE_DIR/output" 2>&1; then fail 'missing dependency should fail'; fi
    case "$(cat "$CASE_DIR/output")" in *"$missing_dependency"*) ;; *) fail 'missing dependency not explained' ;; esac
    [ "$(cat "$DINERO_INSTALL_DIR/dinero")" = 'existing dinero' ] || fail 'dependency failure changed existing binary'
    pass
done

new_case shasum_fallback Linux x86_64 v1.2.3 dinero_1.2.3_linux_amd64.tar.gz
mkdir "$CASE_DIR/tools"
for dependency in awk chmod cp curl gzip mkdir mktemp mv perl rm shasum tar uname; do
    ln -s "$(command -v "$dependency")" "$CASE_DIR/tools/$dependency"
done
if ! PATH="$CASE_DIR/tools" "$BASH_BIN" "$INSTALLER" > "$CASE_DIR/output" 2>&1; then fail 'shasum fallback should succeed'; fi
[ "$("$DINERO_INSTALL_DIR/dinero")" = 'fixture dinero' ] || fail 'shasum fallback did not install'
pass

printf '%s installer tests passed.\n' "$PASSED"
