#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo 'Usage: validate-release-tag.sh vMAJOR.MINOR.PATCH[-PRERELEASE][+BUILD]' >&2
  exit 2
fi

tag=$1
semver_identifier='(0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)'
semver_tag_regex="^v(0|[1-9][0-9]*)[.](0|[1-9][0-9]*)[.](0|[1-9][0-9]*)(-${semver_identifier}([.]${semver_identifier})*)?([+][0-9A-Za-z-]+([.][0-9A-Za-z-]+)*)?$"

if [[ ! $tag =~ $semver_tag_regex ]] || ! git check-ref-format "refs/tags/$tag"; then
  printf 'Invalid release tag: %s (expected a v-prefixed SemVer tag)\n' "$tag" >&2
  exit 1
fi
