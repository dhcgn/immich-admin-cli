# Source this, don't run it.
#
# Exports every KEY=value line of .devcontainer/.env (gitignored) into the
# current shell. The file sits on the bind mount, so an edit on the host is
# live in the container: open a new terminal, or `source ~/.bashrc`, and the
# new values are there — no rebuild. Absent file: nothing happens.
#
# Wired in three places: ~/.bashrc (interactive terminals, by post-start.sh),
# post-start.sh itself (so check-env.sh reports the current state), and the
# OpenCode tasks in .vscode/tasks.json (a task's `bash -c` reads no .bashrc).
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
if [ -f "$script_dir/.env" ]; then
  # Host editors on Windows save CRLF; a trailing \r would end up in the values.
  sed -i 's/\r$//' "$script_dir/.env"
  set -a
  . "$script_dir/.env"
  set +a
fi
