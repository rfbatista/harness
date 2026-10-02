#!/usr/bin/env python3
"""Import the file-based agents/skills in this directory (agents.json + skills/)
into a running `harness` instance over its HTTP API.

Safe to re-run: matches existing skills/agents by name and replaces them, so
the files stay the live edit source and the harness DB is resynced from them,
rather than a one-shot export that drifts.

harness has no in-place "refresh skill content from path" operation, so a
same-name skill is deleted and reimported on each run — this drops any manual
publish state on that skill (flagged to stderr, not silently lost) and gives
it a new id, so agents are always (re)created/updated *after* all skills, once
every referenced skill has a fresh id.

MCP server configs (.mcp.json, referencing secrets as unexpanded ${VAR}
placeholders — see CLAUDE.md's Caution section) are imported via harness's own
import_mcp_servers endpoint, which already dedupes/updates by name; the
placeholder strings are never read or expanded here.

Usage:
    python3 agents/import_to_harness.py [--base-url http://127.0.0.1:8080] [--agents-dir DIR]

Defaults: --base-url http://127.0.0.1:8080, --agents-dir = this script's directory.
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import urllib.error
import urllib.request

import agent_launch as al


def _request(base_url: str, method: str, path: str, body: dict | None = None) -> dict:
    url = base_url.rstrip("/") + "/api" + path
    data = json.dumps(body).encode() if body is not None else None
    headers = {"Content-Type": "application/json"} if data else {}
    req = urllib.request.Request(url, data=data, method=method, headers=headers)
    try:
        with urllib.request.urlopen(req) as resp:
            return json.loads(resp.read())
    except urllib.error.HTTPError as e:
        raise RuntimeError(f"{method} {path} -> HTTP {e.code}: {e.read().decode()}") from None
    except urllib.error.URLError as e:
        raise RuntimeError(f"{method} {path} -> {e.reason} (is harness running at {base_url}?)") from None


def import_skills(base_url: str, skills_dir: str, out=sys.stderr) -> dict:
    """skill directory name -> harness skill id, after (re-)importing every skill
    directory under skills_dir."""
    existing = {s["name"]: s for s in _request(base_url, "GET", "/list_skills")["skills"]}
    ids = {}
    for name in sorted(os.listdir(skills_dir)):
        path = os.path.join(skills_dir, name)
        if not os.path.isfile(os.path.join(path, "SKILL.md")):
            continue
        if name in existing:
            old = existing[name]
            if old.get("published_path"):
                print(f"warning: {name} was published at {old['published_path']!r}; "
                      f"republish it after this run", file=out)
            _request(base_url, "POST", "/delete_skill", {"skill_id": old["id"]})
        skill = _request(base_url, "POST", "/import_skill_from_path", {"path": path})["skill"]
        ids[skill["name"]] = skill["id"]
    return ids


def import_mcp_servers(base_url: str, mcp_path: str) -> list:
    with open(mcp_path) as f:
        content = f.read()
    out = _request(base_url, "POST", "/import_mcp_servers",
                    {"content": content, "on_duplicate": "update"})
    return [s["id"] for s in out["mcp_servers"]]


def import_agents(base_url: str, plan: dict, skill_ids: dict, out=sys.stderr) -> None:
    existing = {a["name"]: a for a in _request(base_url, "GET", "/list_agents")["agents"]}
    for name, info in plan.items():
        missing = [s for s in info["skills"] if s not in skill_ids]
        if missing:
            print(f"warning: {name} references unresolved skills: {missing}", file=out)
        agent_skill_ids = [skill_ids[s] for s in info["skills"] if s in skill_ids]
        mcp_server_ids = import_mcp_servers(base_url, info["mcp_path"]) if info["mcp_path"] else []
        body = {
            "name": name,
            "description": info.get("description", ""),
            "skill_ids": agent_skill_ids,
            "mcp_server_ids": mcp_server_ids,
        }
        if name in existing:
            _request(base_url, "POST", "/update_agent", {**body, "agent_id": existing[name]["id"]})
        else:
            _request(base_url, "POST", "/create_agent", body)


def run(base_url: str, agents_dir: str, out=sys.stderr) -> None:
    manifest = al.load_manifest(os.path.join(agents_dir, "agents.json"))
    plan = al.dump_skill_plan(manifest, agents_dir)
    skill_ids = import_skills(base_url, os.path.join(agents_dir, "skills"), out=out)
    import_agents(base_url, plan, skill_ids, out=out)
    print(f"imported {len(skill_ids)} skills, {len(plan)} agents", file=out)


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__,
                                      formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--base-url", default="http://127.0.0.1:8080",
                         help="harness HTTP API base (default: %(default)s)")
    parser.add_argument("--agents-dir",
                         default=os.path.dirname(os.path.abspath(__file__)),
                         help="agents/ dir holding agents.json + skills/ (default: this script's dir)")
    args = parser.parse_args(argv)

    try:
        run(args.base_url, args.agents_dir)
    except RuntimeError as e:
        print(f"error: {e}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
