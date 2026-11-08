#!/bin/sh
set -eu

repo='nexssp/flow'
api_url="https://api.github.com/repos/$repo/releases/latest"

fail() {
  printf 'nflow installer: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf 'Usage: %s [version|latest] [install-directory]\n' "$0" >&2
  printf 'Set NFLOW_INSTALL_DIR to change the default directory ($HOME/.local/bin).\n' >&2
  exit 2
}

fetch() {
  url=$1
  destination=$2
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL --retry 3 --retry-delay 1 "$url" -o "$destination"
  elif command -v wget >/dev/null 2>&1; then
    wget -q -O "$destination" "$url"
  else
    fail 'curl or wget is required to download the release.'
  fi
}

validate_version() {
  case "$1" in
    ''|*[!0-9A-Za-z._+-]*) fail "invalid release version: $1" ;;
  esac
  case "$1" in
    [0-9A-Za-z]*) ;;
    *) fail "invalid release version: $1" ;;
  esac
}

checksum_file() {
  file=$1
  if command -v sha256sum >/dev/null 2>&1; then
    result=$(sha256sum "$file")
    printf '%s\n' "${result%% *}"
  elif command -v shasum >/dev/null 2>&1; then
    result=$(shasum -a 256 "$file")
    printf '%s\n' "${result%% *}"
  elif command -v openssl >/dev/null 2>&1; then
    openssl dgst -sha256 "$file" | awk '{print $NF}'
  else
    fail 'sha256sum, shasum, or openssl is required to verify the release.'
  fi
}

[ "$#" -le 2 ] || usage
requested_version=${1:-latest}
if [ "$#" -ge 2 ]; then
  install_dir=$2
else
  home=${HOME:-}
  [ -n "$home" ] || fail 'HOME is unset; pass an install directory as the second argument.'
  install_dir=${NFLOW_INSTALL_DIR:-"$home/.local/bin"}
fi
[ -n "$install_dir" ] || fail 'install directory must not be empty.'

case "$requested_version" in
  latest|'') version=latest ;;
  *)
    version=$requested_version
    case "$version" in v*|V*) version=${version#?} ;; esac
    validate_version "$version"
    ;;
esac

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) fail "unsupported operating system: $(uname -s). Use install.ps1 on Windows." ;;
esac
case "$(uname -m)" in
  x86_64|amd64|AMD64) arch=amd64 ;;
  aarch64|arm64|ARM64) arch=arm64 ;;
  *) fail "unsupported architecture: $(uname -m)" ;;
esac

work_dir=$(mktemp -d "${TMPDIR:-/tmp}/nflow-install.XXXXXX") || fail 'could not create a temporary directory.'
install_tmp=''
cleanup() {
  if [ -n "$install_tmp" ]; then
    rm -f "$install_tmp"
  fi
  if [ -n "$work_dir" ]; then
    rm -rf "$work_dir"
  fi
}
trap cleanup 0
trap 'exit 1' HUP INT TERM

if [ "$version" = latest ]; then
  release_json="$work_dir/release.json"
  fetch "$api_url" "$release_json" || fail 'could not resolve the latest stable GitHub release.'
  version=$(sed -n 's/^[[:space:]]*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' "$release_json" | sed -n '1p')
  case "$version" in v*|V*) version=${version#?} ;; esac
  validate_version "$version"
fi

tag="v$version"
archive="nflow_${version}_${os}_${arch}.tar.gz"
checksums="nflow_${version}_checksums.txt"
release_url="https://github.com/$repo/releases/download/$tag"
archive_path="$work_dir/$archive"
checksums_path="$work_dir/$checksums"

fetch "$release_url/$checksums" "$checksums_path" || fail "could not download checksums for $tag."
fetch "$release_url/$archive" "$archive_path" || fail "could not download $archive."

expected=$(awk -v name="$archive" '
  {
    candidate = $2
    sub(/^\*/, "", candidate)
    if (candidate == name) {
      count++
      hash = $1
    }
  }
  END {
    if (count != 1) exit 1
    print hash
  }
' "$checksums_path") || fail "no unique checksum entry found for $archive."
case "$expected" in
  ''|*[!0-9a-fA-F]*) fail "invalid checksum entry for $archive." ;;
esac
[ "${#expected}" -eq 64 ] || fail "invalid checksum length for $archive."
expected=$(printf '%s' "$expected" | tr '[:upper:]' '[:lower:]')
actual=$(checksum_file "$archive_path")
actual=$(printf '%s' "$actual" | tr '[:upper:]' '[:lower:]')
[ "$actual" = "$expected" ] || fail "SHA-256 verification failed for $archive."

extract_dir="$work_dir/extracted"
mkdir -p "$extract_dir"
tar -xzf "$archive_path" -C "$extract_dir" nflow || fail 'could not extract nflow from the release archive.'
[ -f "$extract_dir/nflow" ] || fail 'release archive does not contain the nflow executable.'

mkdir -p "$install_dir" || fail "could not create install directory: $install_dir"
install_tmp=$(mktemp "$install_dir/.nflow.XXXXXX") || fail "could not create a temporary file in $install_dir."
cp "$extract_dir/nflow" "$install_tmp" || fail 'could not copy the nflow executable.'
chmod 755 "$install_tmp" || fail 'could not mark nflow executable.'
mv -f "$install_tmp" "$install_dir/nflow" || fail "could not install nflow into $install_dir."
install_tmp=''

printf 'Installed nflow %s at %s/nflow\n' "$version" "$install_dir"
case ":${PATH:-}:" in
  *":$install_dir:"*) ;;
  *) printf 'Add %s to your PATH to run nflow from any directory.\n' "$install_dir" ;;
esac
