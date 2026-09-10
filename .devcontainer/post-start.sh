#!/usr/bin/env bash
# Runs on every container start. Fails loudly: a silent failure here is how a
# broken tool setup goes unnoticed until it is needed.
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

mkdir -p  ~/.config/opencode/
cp "$script_dir/opencode.jsonc" ~/.config/opencode/opencode.jsonc

# Secrets come from .devcontainer/.env, loaded by every interactive shell via
# ~/.bashrc so an edit takes effect in the next terminal, no rebuild needed.
# Loaded here too, so the check below reports the current state (names only).
# A missing one is worth knowing, not worth failing the container start over.
. "$script_dir/load-env.sh"
bash "$script_dir/check-env.sh" || true

# postStartCommand runs on every start: keep each ~/.bashrc line to one copy.
add_bashrc_line() {
    grep -qxF "$1" ~/.bashrc 2>/dev/null || echo "$1" >> ~/.bashrc
}
add_bashrc_line 'export PATH="$HOME/.local/bin:$PATH"'
add_bashrc_line ". \"$script_dir/load-env.sh\""

# Install once; a start with rtk already present needs no network for this.
command -v rtk > /dev/null 2>&1 || [ -x "$HOME/.local/bin/rtk" ] ||
    curl -fsSL https://raw.githubusercontent.com/rtk-ai/rtk/refs/heads/master/install.sh | sh

# `rtk init -g` writes ~/.claude/RTK.md and does not create the directory;
# a fresh container has none, and the missing dir failed the whole step.
mkdir -p ~/.claude
export RTK_TELEMETRY_DISABLED=1
"$HOME/.local/bin/rtk" init -g --opencode --auto-patch

# To avoid Git warnings about the workspace being an unsafe directory, mark it as safe.
git config --global --add safe.directory /workspaces