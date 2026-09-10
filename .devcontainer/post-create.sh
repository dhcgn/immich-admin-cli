#!/usr/bin/env bash
# Runs after the container is created: project dependencies and the browser
set -euo pipefail

go generate -v ./...
