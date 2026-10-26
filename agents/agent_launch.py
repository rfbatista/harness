#!/usr/bin/env python3
"""Agent launcher: compose `claude` flags for an agent persona + the current project.

The agents are the harness's agent catalog (a folder in the harness data
directory, edited from its web UI). The launcher asks a running harness for
them: the names and aliases from /api/get_agent_catalog, and an agent's skills
and MCP servers from /api/get_agent. They are written under
agents/.generated-harness/<agent>/, which is the directory handed to
`claude --add-dir` (and the MCP config to `--mcp-config`). The actual `claude`
process runs here, in this terminal.

The launcher's own settings (projects_root, plan_prompt, plans_dir, ...) are
the "settings" key of the catalog's agents.json: it reads them from that file
when it is on this machine (HARNESS_AGENTS_DIR, default ~/.harness/agents) and
uses its defaults otherwise.

Pure resolution/walk functions are unit-tested; the CLI wraps them and execs `claude`.
"""

from __future__ import annotations

import os
import sys
import json
import shutil
import base64
import urllib.error
import urllib.request
from dataclasses import dataclass, field

HARNESS_GENERATED_DIRNAME = ".generated-harness"
DEFAULT_HARNESS_BASE_URL = "http://127.0.0.1:8080"
AGENTS_DIR_ENV = "HARNESS_AGENTS_DIR"
DEFAULT_PROJECTS_ROOT = "~/projetos"


class UnknownAgent(Exception):
    """Raised when a name matches no agent name or alias."""


class HarnessUnreachable(Exception):
    """Raised when the harness API can't be reached or returns an error."""


@dataclass
class AgentSpec:
    name: str
    add_dir_paths: list[str] = field(default_factory=list)
    mcp_path: str | None = None
    load_claude_md: bool = True
    skills: list[str] = field(default_factory=list)


def _dedupe(items):
    seen = set()
    out = []
    for it in items:
        if it not in seen:
            seen.add(it)
            out.append(it)
    return out


# ── the harness's agent catalog ──────────────────────────────────────────────

def _harness_request(base_url: str, path: str) -> dict:
    url = base_url.rstrip("/") + "/api" + path
    try:
        with urllib.request.urlopen(url, timeout=10) as resp:
            return json.loads(resp.read())
    except urllib.error.HTTPError as e:
        raise HarnessUnreachable(f"GET {path} -> HTTP {e.code}: {e.read().decode()}") from None
    except urllib.error.URLError as e:
        raise HarnessUnreachable(f"GET {path} -> {e.reason} (is harness running at {base_url}?)") from None


def fetch_agents(base_url: str) -> list[dict]:
    """Every agent of the harness: name, aliases, description, skill_count and
    mcp_servers. From the catalog read; a harness without it answers with what
    list_agents has (no aliases)."""
    try:
        catalog = _harness_request(base_url, "/get_agent_catalog")
    except HarnessUnreachable as e:
        if "HTTP 404" not in str(e):
            raise
        catalog = None
    if catalog is not None:
        return [{
            "name": a["name"],
            "aliases": a.get("aliases") or [],
            "description": a.get("description") or "",
            "skill_count": len(a.get("resolved_skills") or []),
            "mcp_servers": a.get("mcp_servers") or [],
        } for a in catalog.get("agents") or []]
    agents = _harness_request(base_url, "/list_agents").get("agents") or []
    return [{
        "name": a["name"],
        "aliases": [],
        "description": a.get("description") or "",
        "skill_count": len(a.get("skill_ids") or []),
        "mcp_servers": a.get("mcp_server_ids") or [],
    } for a in agents]


def agent_key(name: str, agents: list[dict]) -> str:
    """The agent a name or alias stands for. Raises UnknownAgent."""
    for a in agents:
        if a["name"] == name:
            return a["name"]
    for a in agents:
        if name in a["aliases"]:
            return a["name"]
    raise UnknownAgent(name)


def _write_skill_files(skills_dir: str, skill_name: str, files: list) -> None:
    for f in files:
        path = os.path.join(skills_dir, skill_name, f["path"])
        if f.get("dir"):
            os.makedirs(path, exist_ok=True)
            continue
        os.makedirs(os.path.dirname(path), exist_ok=True)
        content = f.get("content") or ""
        if f.get("encoding") == "base64":
            with open(path, "wb") as fh:
                fh.write(base64.b64decode(content))
        else:
            with open(path, "w") as fh:
                fh.write(content)


def _mcp_entry_from_harness(server: dict) -> dict:
    """One harness MCPServerDTO -> one entry of a Claude Code .mcp.json
    "mcpServers" map. Secret-bearing fields (env values, header values) are
    copied through exactly as harness stored them — including unexpanded
    ${VAR} placeholders — never resolved here."""
    transport = server.get("transport")
    if transport == "stdio":
        entry = {"command": server.get("command", ""), "args": server.get("args") or []}
        if server.get("env"):
            entry["env"] = server["env"]
        return entry
    entry = {
        "type": "http" if transport == "streamable-http" else transport,
        "url": server.get("url", ""),
    }
    if server.get("headers"):
        entry["headers"] = server["headers"]
    return entry


def resolve_agent_from_harness(name: str, base_url: str, agents_root: str,
                               agents: list[dict] | None = None,
                               load_claude_md: bool = True) -> AgentSpec:
    """Resolve an agent by harness name or alias, materialising its skills and
    MCP servers under <agents_root>/.generated-harness/<name>/ so the caller can
    build/exec the launch."""
    name = agent_key(name, agents if agents is not None else fetch_agents(base_url))
    listed = _harness_request(base_url, "/list_agents").get("agents") or []
    match = next((a for a in listed if a.get("name") == name), None)
    if match is None:
        raise UnknownAgent(name)
    agent = _harness_request(base_url, f"/get_agent?agent_id={match['id']}")["agent"]

    out_dir = os.path.join(agents_root, HARNESS_GENERATED_DIRNAME, name)
    os.makedirs(out_dir, exist_ok=True)
    skills_dir = os.path.join(out_dir, ".claude", "skills")
    if os.path.isdir(skills_dir):
        shutil.rmtree(skills_dir)
    skills = agent.get("skills") or []
    for skill in skills:
        _write_skill_files(skills_dir, skill["name"], skill.get("files") or [])

    mcp_path = os.path.join(out_dir, ".mcp.json")
    servers = agent.get("mcp_servers") or []
    if servers:
        mcp_servers = {s["name"]: _mcp_entry_from_harness(s) for s in servers}
        with open(mcp_path, "w") as fh:
            json.dump({"mcpServers": mcp_servers}, fh, indent=2)
    else:
        # A file from an earlier launch must not outlive the servers it named.
        mcp_path = None
        stale = os.path.join(out_dir, ".mcp.json")
        if os.path.isfile(stale):
            os.remove(stale)

    return AgentSpec(
        name=name,
        add_dir_paths=[out_dir],
        mcp_path=mcp_path,
        load_claude_md=load_claude_md,
        skills=[s["name"] for s in skills],
    )


@dataclass
class Level:
    path: str
    is_cwd: bool
    has_mcp: bool
    has_claude_md: bool
    plugin_dirs: list[str] = field(default_factory=list)


def find_plugin_dirs(level_path: str) -> list[str]:
    """Plugin dirs under <level>/plugins/: any dir with .claude-plugin/plugin.json,
    descending into marketplace folders to find their plugin subdirs."""
    plugins_root = os.path.join(level_path, "plugins")
    if not os.path.isdir(plugins_root):
        return []
    found = []
    for dirpath, dirnames, _ in os.walk(plugins_root):
        if os.path.isfile(os.path.join(dirpath, ".claude-plugin", "plugin.json")):
            found.append(dirpath)
            dirnames[:] = []  # a plugin dir is a leaf; don't descend further
    return _dedupe(found)


def scan_levels(cwd: str, projects_root: str) -> list[Level]:
    """Levels from cwd (index 0) up to but excluding projects_root.

    Empty when cwd is not strictly under projects_root.
    """
    cwd = os.path.realpath(cwd)
    root = os.path.realpath(projects_root)
    levels = []
    cur = cwd
    while cur != root and cur.startswith(root + os.sep):
        levels.append(
            Level(
                path=cur,
                is_cwd=(cur == cwd),
                has_mcp=os.path.isfile(os.path.join(cur, ".mcp.json")),
                has_claude_md=os.path.isfile(os.path.join(cur, "CLAUDE.md")),
                plugin_dirs=find_plugin_dirs(cur),
            )
        )
        parent = os.path.dirname(cur)
        if parent == cur:
            break
        cur = parent
    return levels


def build_project_args(levels: list[Level]) -> list[str]:
    """Flags from the walked levels: ancestors get --add-dir/--mcp-config;
    every level's plugins get --plugin-dir (deduped, first-seen order)."""
    args = []
    for lv in levels:
        if lv.is_cwd:
            continue  # cwd's dir/mcp load natively
        args += ["--add-dir", lv.path]
        if lv.has_mcp:
            args += ["--mcp-config", os.path.join(lv.path, ".mcp.json")]
    plugins = _dedupe([p for lv in levels for p in lv.plugin_dirs])
    for p in plugins:
        args += ["--plugin-dir", p]
    return args


def build_argv(spec: AgentSpec, levels: list[Level], passthrough: list[str]) -> list[str]:
    """Full `claude` argv: persona flags, then project flags, then passthrough."""
    argv = ["claude"]
    for d in spec.add_dir_paths:
        argv += ["--add-dir", d]
    if spec.mcp_path:
        argv += ["--mcp-config", spec.mcp_path]
    argv += build_project_args(levels)
    argv += list(passthrough)
    return argv


def project_config_dir(levels: list[Level]) -> str | None:
    """<top-level project dir>/.claude, used as CLAUDE_CONFIG_DIR.

    The top-level project dir is the level directly under projects_root —
    levels are cwd-first up to but excluding projects_root, so it's the last
    one walked. e.g. cwd=~/projetos/subqdocs/subqdocs-backend gives
    ~/projetos/subqdocs/.claude, shared by every sub-project under subqdocs.
    None when cwd isn't under projects_root (no levels).
    """
    if not levels:
        return None
    return os.path.join(levels[-1].path, ".claude")


def build_env(spec: AgentSpec, levels: list[Level] | None = None) -> dict:
    """Env overrides for the launch."""
    env = {}
    if spec.load_claude_md:
        env["CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD"] = "1"
    config_dir = project_config_dir(levels or [])
    if config_dir:
        env["CLAUDE_CONFIG_DIR"] = config_dir
    return env


def project_repo_names(levels: list[Level]) -> tuple[str, str] | None:
    """(project, repo) basenames from the <projects_root>/<project>/<repo>/...
    layout scan_levels() walks — project is the "top-level project dir"
    (levels[-1], the same one project_config_dir() uses), repo is the level
    directly under it. None when cwd isn't under projects_root; when cwd *is*
    the project dir itself (no repo level below it), repo falls back to the
    project name.
    """
    if not levels:
        return None
    project = os.path.basename(levels[-1].path)
    repo = os.path.basename(levels[-2].path) if len(levels) >= 2 else project
    return project, repo


def project_label(cwd: str, projects_root: str) -> str | None:
    """cwd relative to projects_root (forward slashes), or None if not strictly under it."""
    cwd = os.path.realpath(cwd)
    root = os.path.realpath(projects_root)
    if cwd != root and cwd.startswith(root + os.sep):
        return os.path.relpath(cwd, root)
    return None


def _mark(flag: bool) -> str:
    return "✓" if flag else "✗"


def render_context_panel(label, levels: list[Level]) -> list[str]:
    """Human-readable preview of the project context that will be wired in."""
    if not label:
        return ["📂  (no project — not under projects root)"]
    lines = [f"📂  {label}"]
    width = max((len(os.path.basename(lv.path)) + 1) for lv in levels) if levels else 0
    for lv in levels:
        name = (os.path.basename(lv.path) + "/").ljust(width + 1)
        n = len(lv.plugin_dirs)
        plugins = f"✓ ({n})" if n else "✗"
        lines.append(
            f"    {name}  mcp {_mark(lv.has_mcp)}   "
            f"CLAUDE.md {_mark(lv.has_claude_md)}   plugins {plugins}"
        )
    return lines


DEFAULT_PLAN_PROMPT = "Read the plan at {path} and create a spec from it."


def _effective_plans_dir(levels: list[Level], projects_root: str, settings: dict) -> str:
    """The most cwd-ward override matching a walked level's relative path, else default."""
    overrides = settings.get("plans_dir_overrides", {})
    for lv in levels:  # levels are cwd-first, so first match is most cwd-ward
        rel = os.path.relpath(lv.path, projects_root)
        if rel in overrides:
            return overrides[rel]
    return settings.get("plans_dir", ".claude/plans")


def find_plans(cwd: str, projects_root: str, settings: dict) -> list:
    """(label, abs_path) plan files from the first walked level with a non-empty plans dir."""
    levels = scan_levels(cwd, projects_root)
    if not levels:
        return []
    plans_dir = _effective_plans_dir(levels, projects_root, settings)
    for lv in levels:
        folder = os.path.join(lv.path, plans_dir)
        if os.path.isdir(folder):
            mds = sorted(f for f in os.listdir(folder) if f.endswith(".md"))
            if mds:
                return [(f, os.path.join(folder, f)) for f in mds]
    return []


def plan_prompt_text(path: str, settings: dict) -> str:
    return settings.get("plan_prompt", DEFAULT_PLAN_PROMPT).format(path=path)


# ── settings + CLI ───────────────────────────────────────────────────────────

def default_agents_dir() -> str:
    """Where the catalog is on this machine: HARNESS_AGENTS_DIR, else the
    harness's default data dir."""
    return os.environ.get(AGENTS_DIR_ENV) or os.path.join(
        os.path.expanduser("~"), ".harness", "agents")


def load_settings(agents_dir: str) -> dict:
    """The "settings" key of the catalog's agents.json; {} when the file is not
    on this machine or does not parse."""
    try:
        with open(os.path.join(agents_dir, "agents.json")) as f:
            settings = json.load(f).get("settings", {})
    except (OSError, ValueError, AttributeError):
        return {}
    return settings if isinstance(settings, dict) else {}


def _projects_root(settings: dict) -> str:
    return os.path.expanduser(settings.get("projects_root", DEFAULT_PROJECTS_ROOT))


def _alias_lines(agents: list[dict]) -> list[str]:
    """`alias <name>='agent <name>'` for every agent name and declared alias."""
    names = []
    for a in agents:
        names.append(a["name"])
        names.extend(a["aliases"])
    return [f"alias {n}='agent {n}'" for n in _dedupe(names)]


def agent_summary_line(agent: dict) -> tuple:
    """(name, detail) for one agent. Shared by --list and the TTY picker so the
    two renderings can't drift."""
    aliases = agent["aliases"]
    alias_str = f" ({', '.join(aliases)})" if aliases else ""
    n = agent["skill_count"]
    count = f"{n} skill{'' if n == 1 else 's'}"
    servers = ", ".join(agent["mcp_servers"])
    detail = "   ".join(x for x in (count, f"mcp: {servers}" if servers else "") if x)
    return f"{agent['name']}{alias_str}", detail


def _agent_summaries(agents: list[dict]) -> list[str]:
    rows = [agent_summary_line(a) for a in agents]
    w = max((len(n) for n, _ in rows), default=0)
    return [f"{n.ljust(w)}   {d}" for n, d in rows]


# What the launcher did when agents lived in a folder of this repository.
REMOVED_FLAGS = ("--sync", "--check", "--validate", "--dump-skill-plan", "--activate")


def dispatch(args, settings, agents_root, cwd, exec_fn, out, pick_fn=None) -> int:
    """Parse args and either exec claude (via exec_fn) or print. Returns exit code."""
    dry_run = False
    plan_path = None
    harness_base_url = os.environ.get("HARNESS_BASE_URL", DEFAULT_HARNESS_BASE_URL)
    list_only = False
    print_aliases = False
    positional = []
    it = iter(args)
    for a in it:
        if a == "--dry-run":
            dry_run = True
        elif a == "--plan":
            plan_path = next(it, None)
        elif a == "--harness":
            pass  # the only mode now; accepted for the scripts that still pass it
        elif a == "--harness-url":
            harness_base_url = next(it, harness_base_url)
        elif a in REMOVED_FLAGS:
            print(f"{a} is gone: the agents live in the harness now. "
                  "Manage them in its web UI (the Agents page).", file=out)
            return 2
        elif a == "--print-aliases":
            print_aliases = True
        elif a in ("--list", "--help", "-h"):
            list_only = True
        else:
            positional.append(a)

    try:
        agents = fetch_agents(harness_base_url)
    except HarnessUnreachable as e:
        print(f"error: {e}", file=out)
        return 1

    if print_aliases:
        print("\n".join(_alias_lines(agents)), file=out)
        return 0
    if list_only:
        print("\n".join(_agent_summaries(agents)), file=out)
        return 0

    if not positional:
        picked = pick_fn(agents, settings, cwd) if pick_fn else None
        if not picked:
            print("Select an agent to run:\n", file=out)
            print("\n".join(_agent_summaries(agents)), file=out)
            return 0
        name, picked_plan = picked if isinstance(picked, tuple) else (picked, None)
        plan_path = plan_path or picked_plan
        passthrough = []
    else:
        name = positional[0]
        passthrough = positional[1:]

    try:
        spec = resolve_agent_from_harness(
            name, harness_base_url, agents_root, agents=agents,
            load_claude_md=settings.get("load_claude_md", True))
    except UnknownAgent:
        print(f"Unknown agent in harness: {name}\n", file=out)
        print("\n".join(_agent_summaries(agents)), file=out)
        return 2
    except HarnessUnreachable as e:
        print(f"error: {e}", file=out)
        return 1

    projects_root = _projects_root(settings)
    levels = scan_levels(cwd, projects_root)
    names = project_repo_names(levels)
    has_name_flag = any(p == "-n" or p == "--name" or p.startswith("--name=")
                         for p in passthrough)
    if names and not has_name_flag:
        project, repo = names
        passthrough = list(passthrough) + ["--name", f"{spec.name}-{project}-{repo}"]
    if plan_path:
        passthrough = list(passthrough) + [plan_prompt_text(plan_path, settings)]
    argv = build_argv(spec, levels, passthrough)
    env = {**os.environ, **build_env(spec, levels)}

    if dry_run:
        label = project_label(cwd, projects_root)
        print("\n".join(render_context_panel(label, levels)), file=out)
        config_dir = project_config_dir(levels)
        if config_dir:
            print(f"\nCLAUDE_CONFIG_DIR={config_dir}", file=out)
        print("\n" + " ".join(argv), file=out)
        return 0

    exec_fn(argv, env)
    return 0


def _real_exec(argv, env):  # pragma: no cover - replaces the process
    os.execvpe(argv[0], argv, env)


def _pick(agents, settings, cwd):  # pragma: no cover - interactive TTY
    from agent_picker import pick
    return pick(agents, settings, cwd)


def main(argv=None) -> int:
    argv = list(sys.argv[1:] if argv is None else argv)
    agents_root = os.path.dirname(os.path.abspath(__file__))
    settings = load_settings(default_agents_dir())
    pick_fn = _pick if (not argv and sys.stdin.isatty() and sys.stdout.isatty()) else None
    return dispatch(argv, settings, agents_root, os.getcwd(),
                    exec_fn=_real_exec, out=sys.stdout, pick_fn=pick_fn)


if __name__ == "__main__":
    sys.exit(main())
