#!/usr/bin/env bash
# Print N random 64-character alphanumeric strings (default 1), one per line.
# Used as creative anchors by the seed-string-direction skill.
set -euo pipefail
n="${1:-1}"
for _ in $(seq 1 "$n"); do
  LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c 64
  echo
done
