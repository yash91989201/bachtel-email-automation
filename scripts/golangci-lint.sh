#!/usr/bin/env bash
set -euo pipefail

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
version=$(cat "$root/.golangci-version")
binary="$root/bin/golangci-lint/$version/golangci-lint"

# Use upstream release binaries without adding linter dependencies to go.mod.
if [[ ! -x "$binary" ]]; then
  installer=$(mktemp)
  trap 'rm -f "$installer"' EXIT
  curl --fail --silent --show-error --location \
    https://golangci-lint.run/install.sh --output "$installer"
  sh "$installer" -b "$(dirname -- "$binary")" "$version"
  rm -f "$installer"
  trap - EXIT
fi

cd -- "$root"
exec "$binary" "$@"
