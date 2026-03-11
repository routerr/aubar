#!/usr/bin/env bash

set -euo pipefail

mode="${1:---staged}"
repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

pattern='sk-[A-Za-z0-9_-]{20,}|AIza[0-9A-Za-z_-]{20,}|GOCSPX-[0-9A-Za-z_-]{20,}|gh[pousr]_[A-Za-z0-9]{20,}|Bearer [A-Za-z0-9._-]{16,}|"((access|refresh)_token|client_secret|api[_-]?key|token)"[[:space:]]*:[[:space:]]*"[^"]{12,}"'
allow_pattern='fixture-|example-|your-user|set-in-external-env|test-client-|dummy-|placeholder|not-a-real-secret'

files=()
case "$mode" in
  --staged)
    while IFS= read -r -d '' path; do
      files+=("$path")
    done < <(git diff --cached --name-only --diff-filter=ACMR -z)
    ;;
  --all)
    while IFS= read -r path; do
      files+=("$path")
    done < <(git ls-files --cached --others --exclude-standard)
    ;;
  *)
    echo "usage: scripts/check-secrets.sh [--staged|--all]" >&2
    exit 2
    ;;
esac

if [ "${#files[@]}" -eq 0 ]; then
  echo "secret scan: no files to inspect"
  exit 0
fi

hits=()
for file in "${files[@]}"; do
  [ -f "$file" ] || continue
  matches="$(rg -I -nH --color never -e "$pattern" -- "$file" || true)"
  [ -n "$matches" ] || continue
  filtered="$(printf '%s\n' "$matches" | rg -v "$allow_pattern" || true)"
  [ -n "$filtered" ] || continue
  hits+=("$filtered")
done

if [ "${#hits[@]}" -gt 0 ]; then
  echo "secret scan: possible credential-like content found" >&2
  printf '%s\n' "${hits[@]}" >&2
  exit 1
fi

echo "secret scan: ok"
