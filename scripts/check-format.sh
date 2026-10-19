#!/usr/bin/env bash
set -euo pipefail

# Check source without rewriting it. Generation consistency is checked separately.
unformatted=$(gofmt -l cmd internal)
if [[ -n "$unformatted" ]]; then
  printf 'Run make fmt; unformatted Go files:\n%s\n' "$unformatted" >&2
  exit 1
fi

while IFS= read -r -d '' source; do
  if ! go tool templ fmt -stdout "$source" | cmp -s "$source" -; then
    printf 'Run make fmt; unformatted template: %s\n' "$source" >&2
    exit 1
  fi
done < <(git ls-files -z -- '*.templ')
