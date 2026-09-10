#!/usr/bin/env bash
# Checks that every {env:NAME} referenced by the OpenCode config is set in the
# current environment. Prints names only — never values — because this runs in
# the container creation log, which gets shared. Exits 1 if anything is missing.
#
#   bash .devcontainer/check-env.sh                # default: .devcontainer/opencode.jsonc
#   bash .devcontainer/check-env.sh path/to.jsonc
set -euo pipefail

config="${1:-$(dirname "$0")/opencode.jsonc}"
[ -f "$config" ] || { echo "check-env: no config at $config" >&2; exit 2; }

missing=0
total=0
while IFS= read -r name; do
  total=$((total + 1))
  # Indirect expansion: an empty value counts as missing, unlike printenv.
  if [ -n "${!name:-}" ]; then
    printf 'SET     %s\n' "$name"
  else
    printf 'MISSING %s\n' "$name"
    missing=$((missing + 1))
  fi
done < <(grep -oE '\{env:[A-Za-z0-9_]+\}' "$config" | sed -E 's/\{env:|\}//g' | sort -u)

if [ "$missing" -gt 0 ]; then
  echo "check-env: $missing of $total missing — fill them in .devcontainer/.env and open a new terminal" >&2
  exit 1
fi
echo "check-env: all $total set"
