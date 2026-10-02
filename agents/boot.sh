#!/usr/bin/env bash
# Rebuild agents/.generated/ from agents.json. Mirrors nvim/boot.sh.
# The launcher also syncs lazily on its own, so this is for explicit/CI runs.
exec python3 "$(cd "$(dirname "$0")" && pwd)/agent_launch.py" --sync "$@"
