#!/usr/bin/env bash
# Runs once when the container is created, before post-create.sh.
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

npm install -g npm@11

# Agent tooling for the container only; not a project dependency.
npm install -g --allow-scripts=opencode-ai opencode-ai

# `opencode web` spawns xdg-open to show its URL and dies without it. This
# stand-in forwards to VS Code's $BROWSER helper (opens on the host) or prints
# the URL.
sudo install -m 0755 "$script_dir/xdg-open" /usr/local/bin/xdg-open
