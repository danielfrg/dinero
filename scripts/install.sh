#!/bin/bash
# Install a verified GitHub release of Dinero on macOS or Linux.
# curl -fsSL https://github.com/danielfrg/dinero/releases/latest/download/install.sh | bash
# Main builds: curl -fsSL https://github.com/danielfrg/dinero/releases/download/main-latest/install.sh | DINERO_CHANNEL=main bash
# Optional: DINERO_VERSION=1.2.3 DINERO_INSTALL_DIR="$HOME/.local/bin"

dinero_error() {
    printf 'dinero: %s\n' "$*" >&2
    return 1
}

dinero_version() {
    local version="${1#v}" identifier prerelease
    local pattern='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$'
    [[ "$version" =~ $pattern ]] || { dinero_error "Invalid version: $1. Expected X.Y.Z with optional prerelease/build metadata."; return 1; }
    prerelease="${version%%+*}"
    case "$prerelease" in
        *-*)
            prerelease="${prerelease#*-}"
            while [ -n "$prerelease" ]; do
                identifier="${prerelease%%.*}"
                if [[ "$identifier" =~ ^[0-9]+$ ]] && [ "${#identifier}" -gt 1 ] && [[ "$identifier" == 0* ]]; then
                    dinero_error "Invalid version: $1. Numeric prerelease identifiers cannot start with zero."
                    return 1
                fi
                [ "$prerelease" != "$identifier" ] || break
                prerelease="${prerelease#*.}"
            done
            ;;
    esac
    printf 'v%s\n' "$version"
}

dinero_latest_version() {
    local repo_url='https://github.com/danielfrg/dinero' final_url
    final_url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "$repo_url/releases/latest") || {
        dinero_error "Cannot find the latest release. Check $repo_url/releases or set DINERO_VERSION."
        return 1
    }
    case "$final_url" in
        "$repo_url/releases/tag/"*) dinero_version "${final_url#"$repo_url/releases/tag/"}" ;;
        *) dinero_error "No latest release found. Check $repo_url/releases or set DINERO_VERSION." ;;
    esac
}

dinero_verify_checksum() {
    local archive="$1" sums="$2" asset="$3" expected actual
    expected=$(awk -v filename="$asset" '
        $2 == filename || $2 == "*" filename { count++; hash=$1 }
        END { if (count == 1) print tolower(hash); else exit 1 }
    ' "$sums") || {
        dinero_error "Expected exactly one checksum for $asset in SHA256SUMS."
        return 1
    }
    if [ "${#expected}" != 64 ] || [[ ! "$expected" =~ ^[0-9a-f]+$ ]]; then
        dinero_error "Invalid SHA256 checksum for $asset."
        return 1
    fi
    if command -v sha256sum >/dev/null 2>&1; then
        actual=$(sha256sum "$archive") || { dinero_error 'Cannot calculate SHA256 checksum.'; return 1; }
    else
        actual=$(shasum -a 256 "$archive") || { dinero_error 'Cannot calculate SHA256 checksum.'; return 1; }
    fi
    actual="${actual%% *}"
    [ "$actual" = "$expected" ] || { dinero_error "SHA256 checksum mismatch for $asset."; return 1; }
}

main() (
    # A subshell keeps options and cleanup traps local when the file is sourced.
    set -euo pipefail
    local dependency os arch version install_dir work_dir install_temp='' asset base_url listing details release_version
    local channel="${DINERO_CHANNEL-stable}"
    for dependency in curl uname tar mktemp mkdir cp chmod mv rm awk; do
        command -v "$dependency" >/dev/null 2>&1 || { dinero_error "Missing dependency: $dependency."; return 1; }
    done
    if ! command -v sha256sum >/dev/null 2>&1 && ! command -v shasum >/dev/null 2>&1; then
        dinero_error 'Missing sha256 dependency: install sha256sum or shasum.'
        return 1
    fi
    case "$channel" in
        stable|main) ;;
        *) dinero_error "Invalid DINERO_CHANNEL: $channel. Expected stable or main."; return 1 ;;
    esac
    if [ "$channel" = main ] && [ "${DINERO_VERSION+x}" = x ]; then
        dinero_error 'DINERO_CHANNEL=main cannot be combined with DINERO_VERSION.'
        return 1
    fi
    os=$(uname -s)
    case "$os" in
        Darwin) os=darwin ;;
        Linux) os=linux ;;
        *) dinero_error "Unsupported OS: $os. Dinero supports macOS and Linux."; return 1 ;;
    esac
    arch=$(uname -m)
    case "$arch" in
        x86_64|amd64) arch=amd64 ;;
        aarch64|arm64) arch=arm64 ;;
        *) dinero_error "Unsupported architecture: $arch. Dinero supports amd64 and arm64."; return 1 ;;
    esac
    if [ "$channel" = main ]; then
        base_url='https://github.com/danielfrg/dinero/releases/download/main-latest'
        printf 'Finding the newest Dinero main build...\n'
        release_version=$(curl -fsSL "$base_url/VERSION") || {
            dinero_error 'Failed to download VERSION for the main build. Check https://github.com/danielfrg/dinero/releases/tag/main-latest.'
            return 1
        }
        version=$(dinero_version "$release_version") || return 1
    else
        if [ "${DINERO_VERSION+x}" = x ]; then
            version=$(dinero_version "$DINERO_VERSION") || return 1
        else
            printf 'Finding the latest Dinero release...\n'
            version=$(dinero_latest_version) || return 1
        fi
        base_url="https://github.com/danielfrg/dinero/releases/download/$version"
    fi
    if [ "${DINERO_INSTALL_DIR+x}" = x ]; then
        [ -n "$DINERO_INSTALL_DIR" ] || { dinero_error 'DINERO_INSTALL_DIR cannot be empty.'; return 1; }
        install_dir=$DINERO_INSTALL_DIR
    elif [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
        install_dir=/usr/local/bin
    else
        install_dir="$HOME/.local/bin"
    fi
    mkdir -p "$install_dir" || { dinero_error "Cannot create install directory: $install_dir."; return 1; }
    install_dir=$(cd "$install_dir" && pwd) || return 1
    [ -w "$install_dir" ] || { dinero_error "Install directory is not writable: $install_dir. Set DINERO_INSTALL_DIR to a writable directory."; return 1; }
    [ ! -d "$install_dir/dinero" ] || { dinero_error "Cannot replace directory: $install_dir/dinero."; return 1; }

    work_dir=$(mktemp -d "${TMPDIR:-/tmp}/dinero-install.XXXXXX") || { dinero_error 'Cannot create download directory.'; return 1; }
    trap 'rm -rf "$work_dir"; if [ -n "$install_temp" ]; then rm -f "$install_temp"; fi' EXIT
    trap 'exit 1' HUP INT TERM
    asset="dinero_${version#v}_${os}_${arch}.tar.gz"
    printf 'Downloading Dinero %s for %s/%s...\n' "$version" "$os" "$arch"
    curl -fsSL "$base_url/$asset" -o "$work_dir/$asset" || {
        dinero_error "Failed to download $asset. This release or platform may be unavailable; check https://github.com/danielfrg/dinero/releases."
        return 1
    }
    curl -fsSL "$base_url/SHA256SUMS" -o "$work_dir/SHA256SUMS" || {
        dinero_error 'Failed to download SHA256SUMS; cannot verify the release.'
        return 1
    }
    dinero_verify_checksum "$work_dir/$asset" "$work_dir/SHA256SUMS" "$asset" || return 1

    # Extract only the expected root member, after validating its archive type.
    listing=$(tar -tzf "$work_dir/$asset") || { dinero_error 'Cannot read release archive.'; return 1; }
    [ "$(printf '%s\n' "$listing" | awk '$0 == "dinero" { count++ } END { print count+0 }')" = 1 ] || {
        dinero_error 'Release archive must contain one root-level dinero executable.'
        return 1
    }
    details=$(tar -tvzf "$work_dir/$asset" dinero) || { dinero_error 'Cannot inspect dinero in the release archive.'; return 1; }
    case "$details" in
        -*) ;;
        *) dinero_error 'Release archive dinero must be a regular executable file, without links.'; return 1 ;;
    esac
    mkdir "$work_dir/extracted"
    tar -xzf "$work_dir/$asset" -C "$work_dir/extracted" dinero || { dinero_error 'Cannot extract dinero.'; return 1; }
    if [ ! -f "$work_dir/extracted/dinero" ] || [ -L "$work_dir/extracted/dinero" ] || [ ! -x "$work_dir/extracted/dinero" ]; then
        dinero_error 'Release archive dinero must be a regular executable file, without links.'
        return 1
    fi

    # Stage in the destination directory so the final rename is atomic.
    install_temp=$(mktemp "$install_dir/.dinero.XXXXXX") || { dinero_error "Cannot create temporary file in $install_dir."; return 1; }
    cp "$work_dir/extracted/dinero" "$install_temp" || { dinero_error 'Cannot copy dinero into the install directory.'; return 1; }
    chmod 755 "$install_temp" || { dinero_error 'Cannot make the installed binary executable.'; return 1; }
    mv -f "$install_temp" "$install_dir/dinero" || { dinero_error 'Cannot replace the installed dinero binary.'; return 1; }
    install_temp=''
    printf 'Installed Dinero %s to %s/dinero\n' "$version" "$install_dir"
    case ":$PATH:" in
        *":$install_dir:"*) ;;
        *) printf 'Add the install directory to PATH for this shell:\n  export PATH=%q:"$PATH"\n' "$install_dir" ;;
    esac
    printf 'Get started with: dinero --help\n'
)

# stdin execution (curl | bash) has no BASH_SOURCE; sourcing only defines functions.
if [[ "${BASH_SOURCE[0]-}" == "$0" || -z "${BASH_SOURCE[0]-}" ]]; then
    main "$@"
fi
