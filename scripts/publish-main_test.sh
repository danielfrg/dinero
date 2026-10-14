#!/usr/bin/env bash
# Exercise the real publisher with real archives/hashes and an offline gh boundary.
set -euo pipefail
repo_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
publisher="$repo_dir/scripts/publish-main.sh"
bash_bin=$BASH
test_root=$(mktemp -d "${TMPDIR:-/tmp}/dinero-publish-test.XXXXXX")
trap 'rm -rf "$test_root"' EXIT
mkdir -p "$test_root/bin"

cat > "$test_root/bin/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
fail() { printf 'Unexpected gh call: %s\n' "$*" >&2; exit 90; }
log() { printf '%s\n' "$*" >> "$FAKE_STATE/actions"; }
read_failure() {
    if [ "${FAKE_READ_FAILURE:-}" = "$1" ]; then
        printf 'simulated network failure\n' >&2
        exit 41
    fi
}
write_failure() {
    if [ "${FAKE_WRITE_FAILURE:-}" = "$1" ]; then
        log "failed:$1"
        printf 'simulated publishing failure\n' >&2
        exit 43
    fi
}
case "${1:-}" in
    api)
        shift
        endpoint=$1
        shift
        method=GET
        sha=''
        ref=''
        force=''
        query=''
        while [ "$#" -gt 0 ]; do
            case "$1" in
                --method|-X) method=$2; shift 2 ;;
                --jq) query=$2; shift 2 ;;
                --paginate) shift ;;
                -f|-F)
                    case "$2" in
                        sha=*) sha=${2#sha=} ;;
                        ref=*) ref=${2#ref=} ;;
                        force=*) force=${2#force=} ;;
                        *) fail "$2" ;;
                    esac
                    shift 2
                    ;;
                *) fail "$*" ;;
            esac
        done
        case "$method:$endpoint" in
            GET:repos/example/dinero/git/ref/heads/main)
                [ "$query" = .object.sha ] || fail 'main head query'
                log read:head
                read_failure head
                cat "$FAKE_STATE/head"
                ;;
            GET:repos/example/dinero/releases)
                case "$query" in
                    *'select(.tag_name == "main-latest")'*'.immutable'*'@tsv'*) ;;
                    *) fail 'rolling release selection or immutability check' ;;
                esac
                log read:releases
                read_failure releases
                if [ -f "$FAKE_STATE/exists" ]; then
                    printf '42\t%s\t%s\t%s\n' "$(cat "$FAKE_STATE/prerelease")" "$(cat "$FAKE_STATE/draft")" "$(cat "$FAKE_STATE/immutable")"
                fi
                ;;
            GET:repos/example/dinero/git/matching-refs/tags/main-latest)
                case "$query" in
                    *'select(.ref == "refs/tags/main-latest")'*'.object.sha'*) ;;
                    *) fail 'matching refs must select the exact rolling tag' ;;
                esac
                log read:tag
                read_failure tag
                [ ! -f "$FAKE_STATE/tag_sha" ] || cat "$FAKE_STATE/tag_sha"
                ;;
            PATCH:repos/example/dinero/git/refs/tags/main-latest)
                [ -f "$FAKE_STATE/tag_sha" ] || fail 'PATCH missing tag'
                [ "$sha" = "$FAKE_COMMIT" ] && [ "$force" = true ] || fail 'tag target or force'
                write_failure tag
                log write:tag
                printf '%s\n' "$sha" > "$FAKE_STATE/tag_sha"
                ;;
            POST:repos/example/dinero/git/refs)
                [ "$ref" = refs/tags/main-latest ] && [ "$sha" = "$FAKE_COMMIT" ] || fail 'new tag target'
                [ ! -f "$FAKE_STATE/tag_sha" ] || fail 'POST existing tag'
                write_failure tag
                log write:tag
                printf '%s\n' "$sha" > "$FAKE_STATE/tag_sha"
                ;;
            *) fail "$method $endpoint" ;;
        esac
        ;;
    release)
        operation=$2
        tag=$3
        [ "$tag" = main-latest ] || fail 'changed a stable tag'
        shift 3
        case "$operation" in
            create|edit)
                draft=''
                prerelease=''
                latest=''
                target=''
                title=''
                notes=''
                while [ "$#" -gt 0 ]; do
                    case "$1" in
                        --draft) draft=true; shift ;;
                        --draft=false) draft=false; shift ;;
                        --prerelease) prerelease=true; shift ;;
                        --latest=false) latest=false; shift ;;
                        --target) target=$2; shift 2 ;;
                        --title) title=$2; shift 2 ;;
                        --notes) notes=$2; shift 2 ;;
                        *) fail "$*" ;;
                    esac
                done
                [ "$prerelease" = true ] && [ "$latest" = false ] || fail 'stable latest affected'
                [ "$target" = "$FAKE_COMMIT" ] || fail 'release target'
                case "$title" in *[Mm]ain*build*) ;; *) fail 'missing main build title' ;; esac
                case "$notes" in *"$FAKE_COMMIT"*) ;; *) fail 'missing full source commit' ;; esac
                if [ "$operation" = create ]; then
                    [ ! -f "$FAKE_STATE/exists" ] && [ "$draft" = true ] || fail 'unsafe create'
                    write_failure create
                    log write:create
                    touch "$FAKE_STATE/exists"
                    printf true > "$FAKE_STATE/prerelease"
                    printf false > "$FAKE_STATE/immutable"
                else
                    [ -f "$FAKE_STATE/exists" ] && [ "$draft" = false ] || fail 'unsafe edit'
                    # Publication must happen after all four archives and shared assets.
                    for os in darwin linux; do
                        for arch in amd64 arm64; do
                            [ -f "$FAKE_STATE/assets/dinero_${FAKE_VERSION#v}_${os}_${arch}.tar.gz" ] || fail 'published before archives'
                        done
                    done
                    for name in SHA256SUMS install.sh VERSION; do
                        [ -f "$FAKE_STATE/assets/$name" ] || fail 'published before shared assets'
                    done
                    [ "$(cat "$FAKE_STATE/assets/VERSION")" = "$FAKE_VERSION" ] || fail 'published old version'
                    write_failure edit
                    log write:edit
                fi
                printf '%s' "$draft" > "$FAKE_STATE/draft"
                printf '%s\n' "$notes" > "$FAKE_STATE/notes"
                ;;
            upload)
                [ -f "$FAKE_STATE/exists" ] || fail 'upload without release'
                [ "${!#}" = --clobber ] || fail 'missing clobber for reruns'
                while [ "$#" -gt 1 ]; do
                    asset=$1
                    name=${asset##*/}
                    if [ "${FAKE_UPLOAD_FAILURE:-}" = "$name" ]; then
                        log "failed:upload:$name"
                        printf 'simulated asset upload failure\n' >&2
                        exit 42
                    fi
                    log "write:upload:$name"
                    cp "$asset" "$FAKE_STATE/assets/$name"
                    shift
                done
                ;;
            view)
                [ "$#" = 4 ] && [ "$1" = --json ] && [ "$2" = assets ] && [ "$3" = --jq ] || fail 'unexpected view'
                log read:assets
                read_failure assets
                for asset in "$FAKE_STATE/assets/"*; do
                    [ ! -f "$asset" ] || printf '%s\n' "${asset##*/}"
                done
                ;;
            delete-asset)
                [ "$#" = 2 ] && [ "$2" = --yes ] || fail 'unsafe asset delete'
                write_failure delete
                log "write:delete:$1"
                rm "$FAKE_STATE/assets/$1"
                ;;
            *) fail "$operation" ;;
        esac
        ;;
    *) fail "$*" ;;
esac
EOF
chmod +x "$test_root/bin/gh"
export PATH="$test_root/bin:$PATH"
export GH_REPO=example/dinero GH_TOKEN=offline-test-token
export FAKE_COMMIT=012345abcdef012345abcdef012345abcdef0123
export FAKE_VERSION=v0.0.0-main.g012345abcdef
old_commit=abcdef012345abcdef012345abcdef012345abcd
old_version=v0.0.0-main.gabcdef012345
passed=0

fail() {
    printf 'FAIL: %s: %s\n' "$case_name" "$1" >&2
    [ ! -f "$case_dir/output" ] || cat "$case_dir/output" >&2
    exit 1
}
pass() { passed=$((passed + 1)); printf 'PASS: %s\n' "$case_name"; }
new_case() {
    case_name=$1
    case_dir="$test_root/$case_name"
    export FAKE_STATE="$case_dir/state"
    asset_dir="$case_dir/local assets"
    mkdir -p "$FAKE_STATE/assets" "$asset_dir" "$case_dir/stage"
    : > "$FAKE_STATE/actions"
    printf '%s\n' "$FAKE_COMMIT" > "$FAKE_STATE/head"
    printf '%s\n' "$old_commit" > "$FAKE_STATE/stable_tag"
    printf 'stable archive\n' > "$FAKE_STATE/stable_asset"
    printf 'unrelated asset\n' > "$FAKE_STATE/assets/manual.pdf"
    printf 'stable archive\n' > "$FAKE_STATE/assets/dinero_1.2.3_linux_amd64.tar.gz"
    # A similar-looking name outside the supported platforms must be retained.
    printf 'unrelated archive\n' > "$FAKE_STATE/assets/dinero_0.0.0-main.gabcdef012345_windows_amd64.tar.gz"
    unset FAKE_READ_FAILURE FAKE_UPLOAD_FAILURE FAKE_WRITE_FAILURE
    printf 'fixture executable\n' > "$case_dir/stage/dinero"
    : > "$asset_dir/SHA256SUMS"
    for os in darwin linux; do
        for arch in amd64 arm64; do
            name="dinero_${FAKE_VERSION#v}_${os}_${arch}.tar.gz"
            tar -czf "$asset_dir/$name" -C "$case_dir/stage" dinero
            if command -v sha256sum >/dev/null 2>&1; then
                hash=$(sha256sum "$asset_dir/$name")
            else
                hash=$(shasum -a 256 "$asset_dir/$name")
            fi
            printf '%s  %s\n' "${hash%% *}" "$name" >> "$asset_dir/SHA256SUMS"
        done
    done
    printf '#!/bin/sh\nprintf installer\n' > "$asset_dir/install.sh"
    printf '%s\n' "$FAKE_VERSION" > "$asset_dir/VERSION"
}
existing_release() {
    touch "$FAKE_STATE/exists"
    printf true > "$FAKE_STATE/prerelease"
    printf false > "$FAKE_STATE/draft"
    printf false > "$FAKE_STATE/immutable"
    printf '%s\n' "$old_commit" > "$FAKE_STATE/tag_sha"
    printf '%s\n' "$old_version" > "$FAKE_STATE/assets/VERSION"
    printf 'old checksum file\n' > "$FAKE_STATE/assets/SHA256SUMS"
    for os in darwin linux; do
        for arch in amd64 arm64; do
            printf 'old archive\n' > "$FAKE_STATE/assets/dinero_${old_version#v}_${os}_${arch}.tar.gz"
        done
    done
}
run_publisher() {
    "$bash_bin" "$publisher" "${1:-$FAKE_VERSION}" "${2:-$FAKE_COMMIT}" "$asset_dir" > "$case_dir/output" 2>&1
}
expect_success() { run_publisher || fail 'publisher should succeed'; }
expect_failure() {
    if run_publisher "${1:-$FAKE_VERSION}" "${2:-$FAKE_COMMIT}"; then fail 'publisher should fail'; fi
}
assert_no_mutations() {
    if awk '/^write:/ {found=1} END {exit !found}' "$FAKE_STATE/actions"; then fail 'mutated remote state'; fi
}
assert_published() {
    [ "$(cat "$FAKE_STATE/tag_sha")" = "$FAKE_COMMIT" ] || fail 'rolling tag points at wrong commit'
    [ "$(cat "$FAKE_STATE/draft")" = false ] || fail 'release still draft'
    [ "$(cat "$FAKE_STATE/prerelease")" = true ] || fail 'release is not prerelease'
    [ "$(cat "$FAKE_STATE/assets/VERSION")" = "$FAKE_VERSION" ] || fail 'wrong version pointer'
    for os in darwin linux; do
        for arch in amd64 arm64; do
            name="dinero_${FAKE_VERSION#v}_${os}_${arch}.tar.gz"
            cmp "$asset_dir/$name" "$FAKE_STATE/assets/$name" || fail 'archive contents differ'
            [ ! -f "$FAKE_STATE/assets/dinero_${old_version#v}_${os}_${arch}.tar.gz" ] || fail 'obsolete rolling archive retained'
        done
    done
    cmp "$asset_dir/SHA256SUMS" "$FAKE_STATE/assets/SHA256SUMS" || fail 'wrong checksums'
    cmp "$asset_dir/install.sh" "$FAKE_STATE/assets/install.sh" || fail 'wrong installer'
    [ "$(cat "$FAKE_STATE/stable_tag")" = "$old_commit" ] || fail 'stable tag moved'
    [ "$(cat "$FAKE_STATE/stable_asset")" = 'stable archive' ] || fail 'stable asset changed'
    [ -f "$FAKE_STATE/assets/manual.pdf" ] || fail 'unrelated asset deleted'
    [ -f "$FAKE_STATE/assets/dinero_1.2.3_linux_amd64.tar.gz" ] || fail 'stable archive deleted'
    [ -f "$FAKE_STATE/assets/dinero_0.0.0-main.gabcdef012345_windows_amd64.tar.gz" ] || fail 'unrelated platform archive deleted'
    # VERSION follows every archive and other shared upload; cleanup follows publish.
    awk '
        /^write:upload:dinero_/ { if(shared) exit 1; archives++ }
        /^write:upload:SHA256SUMS$/ || /^write:upload:install.sh$/ { if(archives!=4 || version) exit 1; shared++ }
        /^write:upload:VERSION$/ { if(archives!=4 || shared!=2) exit 1; version=1 }
        /^write:edit$/ { if(!version) exit 1; published=1 }
        /^write:delete:/ { if(!published) exit 1 }
        END { if(!published) exit 1 }
    ' "$FAKE_STATE/actions" || fail 'unsafe upload/publication/cleanup order'
}

# Missing create/publish, wrong ref, unsafe ordering, or broad cleanup all fail here.
new_case first_publish
expect_success
assert_published
pass

new_case update
existing_release
expect_success
assert_published
if awk '/^write:create$/ {found=1} END {exit !found}' "$FAKE_STATE/actions"; then fail 'recreated existing release'; fi
pass

new_case rerun
existing_release
expect_success
: > "$FAKE_STATE/actions"
expect_success
assert_published
pass

new_case recover_draft_without_tag
existing_release
printf true > "$FAKE_STATE/draft"
rm "$FAKE_STATE/tag_sha"
expect_success
assert_published
pass

new_case stale_build
existing_release
printf '%s\n' "$old_commit" > "$FAKE_STATE/head"
expect_success
assert_no_mutations
[ "$(cat "$FAKE_STATE/assets/VERSION")" = "$old_version" ] || fail 'stale build advanced version'
pass

# Local validation must reject bad inputs before touching the remote boundary.
for invalid in wrong_version wrong_commit uppercase_commit missing_archive missing_sums missing_installer missing_version mismatched_version multiline_version corrupt_archive duplicate_checksum missing_checksum malformed_checksum; do
    new_case "$invalid"
    args_version=$FAKE_VERSION
    args_commit=$FAKE_COMMIT
    case "$invalid" in
        wrong_version) args_version=v0.0.0-main.gffffffffffff ;;
        wrong_commit) args_commit=012345abcdef ;;
        uppercase_commit) args_commit=012345ABCDEF012345ABCDEF012345ABCDEF0123 ;;
        missing_archive) rm "$asset_dir/dinero_${FAKE_VERSION#v}_linux_arm64.tar.gz" ;;
        missing_sums) rm "$asset_dir/SHA256SUMS" ;;
        missing_installer) rm "$asset_dir/install.sh" ;;
        missing_version) rm "$asset_dir/VERSION" ;;
        mismatched_version) printf '%s\n' "$old_version" > "$asset_dir/VERSION" ;;
        multiline_version) printf '%s\n%s\n' "$FAKE_VERSION" "$old_version" > "$asset_dir/VERSION" ;;
        corrupt_archive) printf corrupt >> "$asset_dir/dinero_${FAKE_VERSION#v}_linux_arm64.tar.gz" ;;
        duplicate_checksum) cat "$asset_dir/SHA256SUMS" >> "$asset_dir/duplicate"; cat "$asset_dir/duplicate" >> "$asset_dir/SHA256SUMS" ;;
        missing_checksum) awk 'NR!=1' "$asset_dir/SHA256SUMS" > "$asset_dir/replacement"; mv "$asset_dir/replacement" "$asset_dir/SHA256SUMS" ;;
        malformed_checksum) printf 'not-a-hash  dinero_%s_darwin_amd64.tar.gz\n' "${FAKE_VERSION#v}" > "$asset_dir/SHA256SUMS" ;;
    esac
    expect_failure "$args_version" "$args_commit"
    [ ! -s "$FAKE_STATE/actions" ] || fail 'invalid local assets reached GitHub'
    pass
done

for invalid_release in not_prerelease immutable; do
    new_case "$invalid_release"
    existing_release
    case "$invalid_release" in
        not_prerelease) printf false > "$FAKE_STATE/prerelease" ;;
        immutable) printf true > "$FAKE_STATE/immutable" ;;
    esac
    expect_failure
    assert_no_mutations
    pass
done

for failed_read in head releases; do
    new_case "failed_read_$failed_read"
    export FAKE_READ_FAILURE=$failed_read
    expect_failure
    assert_no_mutations
    [ ! -f "$FAKE_STATE/exists" ] || fail 'read failure mistaken for absent release'
    pass
done

for failed_upload in "dinero_${FAKE_VERSION#v}_linux_arm64.tar.gz" SHA256SUMS install.sh VERSION; do
    new_case "failed_upload_$passed"
    existing_release
    export FAKE_UPLOAD_FAILURE=$failed_upload
    expect_failure
    [ "$(cat "$FAKE_STATE/assets/VERSION")" = "$old_version" ] || fail 'failed upload advanced version pointer'
    [ "$(cat "$FAKE_STATE/tag_sha")" = "$old_commit" ] || fail 'failed upload advanced tag'
    if awk '/^write:delete:/ || /^write:edit$/ {found=1} END {exit !found}' "$FAKE_STATE/actions"; then fail 'failure published or cleaned old archives'; fi
    [ -f "$FAKE_STATE/assets/dinero_${old_version#v}_linux_arm64.tar.gz" ] || fail 'failure removed prior archives'
    pass
done

for failed_write in create tag edit delete; do
    new_case "failed_write_$failed_write"
    if [ "$failed_write" != create ]; then existing_release; fi
    export FAKE_WRITE_FAILURE=$failed_write
    expect_failure
    # A publication failure must stop before destructive archive cleanup.
    if [ "$failed_write" != delete ]; then
        if awk '/^write:delete:/ {found=1} END {exit !found}' "$FAKE_STATE/actions"; then fail 'publication failure still cleaned archives'; fi
    fi
    pass
done

for failed_read in tag assets; do
    new_case "failed_read_$failed_read"
    existing_release
    export FAKE_READ_FAILURE=$failed_read
    expect_failure
    if awk '/^write:delete:/ {found=1} END {exit !found}' "$FAKE_STATE/actions"; then fail 'read failure still cleaned archives'; fi
    pass
done

new_case failed_initial_upload_recovery
export FAKE_UPLOAD_FAILURE="dinero_${FAKE_VERSION#v}_linux_arm64.tar.gz"
expect_failure
[ "$(cat "$FAKE_STATE/draft")" = true ] || fail 'failed initial upload exposed release'
[ ! -f "$FAKE_STATE/assets/VERSION" ] || fail 'failed initial upload advanced version'
unset FAKE_UPLOAD_FAILURE
: > "$FAKE_STATE/actions"
expect_success
assert_published
pass

printf '%s publisher tests passed.\n' "$passed"
