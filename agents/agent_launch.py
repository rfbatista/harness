#!/usr/bin/env python3
"""Agent launcher: compose `claude` flags for an agent persona + the current project.

Manifest-driven (agents/agents.json). Skills live in one place, agents/skills/,
and each agent names the ones it wants; `--sync` materialises that list as a
symlink tree under agents/.generated/<agent>/.claude/skills/, which is the single
directory the launcher hands to `claude --add-dir`.

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

SKILLS_DIRNAME = "skills"
GENERATED_DIRNAME = ".generated"
HARNESS_GENERATED_DIRNAME = ".generated-harness"
DEFAULT_HARNESS_BASE_URL = "http://127.0.0.1:8080"
# .generated/<agent>/.claude/skills/<skill> -> ../../../../skills/<skill>
LINK_PREFIX = "../../../../" + SKILLS_DIRNAME + "/"


class UnknownAgent(Exception):
    """Raised when a name matches no agent key or alias."""


class UnknownBundle(Exception):
    """Raised when a "@name" skill entry matches no bundle."""


class BundleCycle(Exception):
    """Raised when bundles reference each other in a loop."""


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


def resolve_skills(entries, bundles: dict, _seen=frozenset()) -> list[str]:
    """Expand a skills list: "@name" pulls in bundles[name] (which may itself
    reference bundles). Deduped first-seen, order preserved."""
    out = []
    for e in entries or []:
        if not e.startswith("@"):
            out.append(e)
            continue
        b = e[1:]
        if b in _seen:
            raise BundleCycle(b)
        if b not in bundles:
            raise UnknownBundle(b)
        out.extend(resolve_skills(bundles[b], bundles, _seen | {b}))
    return _dedupe(out)


def generated_dir(agents_root: str, key: str) -> str:
    return os.path.join(agents_root, GENERATED_DIRNAME, key)


def resolve_agent(name: str, manifest: dict, agents_root: str) -> AgentSpec:
    """Resolve an agent name (key or alias) to concrete paths/flags."""
    agents = manifest.get("agents", {})
    settings = manifest.get("settings", {})

    key = None
    if name in agents:
        key = name
    else:
        for k, cfg in agents.items():
            if name in (cfg.get("aliases") or []):
                key = k
                break
    if key is None:
        raise UnknownAgent(name)

    cfg = agents[key]
    own_dir = cfg.get("dir", key)  # only where .mcp.json / .claude/agents live now
    add_dir_paths = [generated_dir(agents_root, key)]
    skills = resolve_skills(cfg.get("skills"), manifest.get("bundles", {}))

    mcp = cfg.get("mcp")
    mcp_path = None
    if mcp:
        mcp_dir = own_dir if mcp == "self" else mcp
        mcp_path = os.path.join(agents_root, mcp_dir, ".mcp.json")

    load_claude_md = cfg.get("load_claude_md", settings.get("load_claude_md", True))

    return AgentSpec(
        name=key,
        add_dir_paths=add_dir_paths,
        mcp_path=mcp_path,
        load_claude_md=load_claude_md,
        skills=skills,
    )


# ── harness-backed resolution ────────────────────────────────────────────────
# `agent <name> --harness` resolves the persona's skills/MCP servers from a
# running harness instance (https://github.com/rfbatista/harness) instead of
# this file's agents.json/skills/ tree — harness becomes the source of truth
# for *what* an agent has, materialised locally so the rest of the launch
# pipeline (build_argv, exec) is unchanged: the actual `claude` process still
# runs here, in this terminal, exactly like a local-manifest launch.

class HarnessUnreachable(Exception):
    """Raised when the harness API can't be reached or returns an error."""


def _harness_request(base_url: str, path: str) -> dict:
    url = base_url.rstrip("/") + "/api" + path
    try:
        with urllib.request.urlopen(url, timeout=10) as resp:
            return json.loads(resp.read())
    except urllib.error.HTTPError as e:
        raise HarnessUnreachable(f"GET {path} -> HTTP {e.code}: {e.read().decode()}") from None
    except urllib.error.URLError as e:
        raise HarnessUnreachable(f"GET {path} -> {e.reason} (is harness running at {base_url}?)") from None


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


def resolve_agent_from_harness(name: str, base_url: str, agents_root: str) -> AgentSpec:
    """Resolve an agent by harness Agent.Name, materialising its skills and MCP
    servers under <agents_root>/.generated-harness/<name>/ so the caller can
    build/exec exactly like a local-manifest launch."""
    agents = _harness_request(base_url, "/list_agents").get("agents") or []
    match = next((a for a in agents if a.get("name") == name), None)
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

    mcp_path = None
    servers = agent.get("mcp_servers") or []
    if servers:
        mcp_servers = {s["name"]: _mcp_entry_from_harness(s) for s in servers}
        mcp_path = os.path.join(out_dir, ".mcp.json")
        with open(mcp_path, "w") as fh:
            json.dump({"mcpServers": mcp_servers}, fh, indent=2)

    return AgentSpec(
        name=name,
        add_dir_paths=[out_dir],
        mcp_path=mcp_path,
        load_claude_md=True,
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


# ── manifest validation ──────────────────────────────────────────────────────

def validate_manifest(manifest: dict, known_skills: set) -> list[str]:
    """Every problem with the manifest, as human-readable lines. Empty == valid.

    Pure: takes the set of skill names that exist, not a path.
    """
    errors = []
    bundles = manifest.get("bundles", {})
    agents = manifest.get("agents", {})

    def check(owner, entries):
        try:
            names = resolve_skills(entries, bundles)
        except UnknownBundle as e:
            errors.append(f"{owner}: unknown bundle @{e.args[0]}")
            return
        except BundleCycle as e:
            errors.append(f"{owner}: bundle cycle through @{e.args[0]}")
            return
        for n in names:
            if n not in known_skills:
                errors.append(f"{owner}: unknown skill {n!r}")

    for b, entries in bundles.items():
        check(f"bundle @{b}", entries)
    for key, cfg in agents.items():
        check(f"agent {key}", cfg.get("skills"))

    seen = {}
    for key, cfg in agents.items():
        for n in [key] + list(cfg.get("aliases") or []):
            if n in seen and seen[n] != key:
                errors.append(f"agent {key}: name/alias {n!r} collides with {seen[n]}")
            seen.setdefault(n, key)

    return errors


def known_skills(agents_root: str) -> set:
    """Skill names on disk: a dir under agents/skills/ holding a SKILL.md."""
    root = os.path.join(agents_root, SKILLS_DIRNAME)
    if not os.path.isdir(root):
        return set()
    return {d for d in os.listdir(root)
            if os.path.isfile(os.path.join(root, d, "SKILL.md"))}


# ── generated tree ───────────────────────────────────────────────────────────

def lock_warnings(agents_root: str, known: set) -> list[str]:
    """Warn-only drift between skills-lock.json and what is actually on disk.
    The lock is keyed by skill name, which the central dir now matches 1:1."""
    path = os.path.join(agents_root, "skills-lock.json")
    if not os.path.isfile(path):
        return []
    try:
        locked = set(json.load(open(path)).get("skills", {}))
    except (OSError, ValueError):
        return [f"warning: could not read {os.path.basename(path)}"]
    return [f"warning: locked skill missing from {SKILLS_DIRNAME}/: {n}"
            for n in sorted(locked - known)]


def plan_sync(manifest: dict) -> dict:
    """agent key -> ordered skill names. The one source of truth for --sync and
    the staleness check, so a launch can never disagree with a sync."""
    bundles = manifest.get("bundles", {})
    return {
        key: resolve_skills(cfg.get("skills"), bundles)
        for key, cfg in manifest.get("agents", {}).items()
    }


def dump_skill_plan(manifest: dict, agents_root: str) -> dict:
    """agent key -> {skills, mcp_path, description}, for external tooling that
    needs the fully-resolved picture (bundle/alias expansion already applied)
    without reimplementing resolve_agent()/plan_sync() itself. mcp_path is
    omitted when the agent has no mcp config or the file doesn't exist on disk."""
    out = {}
    for key, cfg in manifest.get("agents", {}).items():
        spec = resolve_agent(key, manifest, agents_root)
        mcp_path = spec.mcp_path if spec.mcp_path and os.path.isfile(spec.mcp_path) else None
        out[key] = {
            "skills": spec.skills,
            "mcp_path": mcp_path,
            "description": cfg.get("description", ""),
        }
    return out


def generated_is_stale(key: str, wanted, agents_root: str) -> bool:
    d = os.path.join(generated_dir(agents_root, key), ".claude", SKILLS_DIRNAME)
    try:
        return set(os.listdir(d)) != set(wanted)
    except OSError:
        return True


def _relink(path: str, target: str) -> bool:
    """Ensure `path` is a symlink to `target`. True if anything changed."""
    if os.path.islink(path):
        if os.readlink(path) == target:
            return False
        os.unlink(path)
    elif os.path.exists(path):
        os.remove(path)
    os.symlink(target, path)
    return True


def _mirror_subagents(key: str, own_dir: str, agents_root: str) -> list[str]:
    """Link <own_dir>/.claude/agents/*.md into the generated tree, so an agent
    that ships subagent definitions keeps them under its single --add-dir."""
    src = os.path.join(agents_root, own_dir, ".claude", "agents")
    dst = os.path.join(generated_dir(agents_root, key), ".claude", "agents")
    if not os.path.isdir(src):
        if os.path.isdir(dst):
            shutil.rmtree(dst)
            return [f"{key}: removed .claude/agents"]
        return []
    os.makedirs(dst, exist_ok=True)
    changed = []
    wanted = sorted(f for f in os.listdir(src) if f.endswith(".md"))
    for f in wanted:
        target = os.path.join("../../../..", own_dir, ".claude", "agents", f)
        if _relink(os.path.join(dst, f), target):
            changed.append(f"{key}: linked agent {f}")
    for f in os.listdir(dst):
        if f not in wanted:
            os.unlink(os.path.join(dst, f))
            changed.append(f"{key}: unlinked agent {f}")
    return changed


def sync_agent(key: str, wanted, agents_root: str, own_dir=None) -> list[str]:
    """Materialise one agent's skill list. Idempotent; returns an action log."""
    d = os.path.join(generated_dir(agents_root, key), ".claude", SKILLS_DIRNAME)
    os.makedirs(d, exist_ok=True)
    changed = []
    for s in wanted:
        if _relink(os.path.join(d, s), LINK_PREFIX + s):
            changed.append(f"{key}: linked {s}")
    for f in os.listdir(d):
        if f not in set(wanted):
            os.unlink(os.path.join(d, f))
            changed.append(f"{key}: unlinked {f}")
    changed += _mirror_subagents(key, own_dir or key, agents_root)
    return changed


def sync_generated(plan: dict, agents_root: str, manifest=None, prune=True) -> list[str]:
    """Materialise every agent's skill list; prune trees for agents that are gone."""
    agents = (manifest or {}).get("agents", {})
    changed = []
    for key, wanted in plan.items():
        own_dir = agents.get(key, {}).get("dir", key)
        changed += sync_agent(key, wanted, agents_root, own_dir=own_dir)
    root = os.path.join(agents_root, GENERATED_DIRNAME)
    if prune and os.path.isdir(root):
        for name in os.listdir(root):
            if name in plan:
                continue
            stale = os.path.join(root, name)
            assert os.path.dirname(os.path.abspath(stale)) == os.path.abspath(root)
            shutil.rmtree(stale) if os.path.isdir(stale) else os.remove(stale)
            changed.append(f"pruned {name}")
    return changed


# ── global activation (~/.claude/skills) ─────────────────────────────────────

def activate_global(names, skills_root: str, home_skills_dir: str) -> int:
    """Point ~/.claude/skills at the given skills. Only ever removes symlinks —
    a real directory there is somebody else's and is left alone."""
    os.makedirs(home_skills_dir, exist_ok=True)
    for f in os.listdir(home_skills_dir):
        p = os.path.join(home_skills_dir, f)
        if os.path.islink(p):
            os.unlink(p)
    linked = 0
    for n in names:
        target = os.path.join(skills_root, n)
        dest = os.path.join(home_skills_dir, n)
        if os.path.exists(dest) and not os.path.islink(dest):
            continue  # real dir — not ours to touch
        os.symlink(target, dest)
        linked += 1
    return linked


def activation_names(choice: str, manifest: dict, agents_root: str) -> list[str]:
    if choice == "none":
        return []
    if choice == "all":
        return sorted(known_skills(agents_root))
    return resolve_skills(
        manifest.get("agents", {}).get(choice, {}).get("skills"),
        manifest.get("bundles", {}),
    )


# ── manifest + CLI ───────────────────────────────────────────────────────────

def load_manifest(path: str) -> dict:
    with open(path) as f:
        return json.load(f)


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


def _projects_root(manifest: dict) -> str:
    return os.path.expanduser(
        manifest.get("settings", {}).get("projects_root", "~/projetos")
    )


def _alias_lines(manifest: dict) -> list[str]:
    """`alias <name>='agent <name>'` for every agent key and declared alias."""
    names = []
    for key, cfg in manifest.get("agents", {}).items():
        names.append(key)
        names.extend(cfg.get("aliases") or [])
    return [f"alias {n}='agent {n}'" for n in _dedupe(names)]


def agent_summary_line(key: str, cfg: dict, manifest: dict) -> tuple:
    """(name, detail) for one agent. Shared by --list and the TTY picker so the
    two renderings can't drift."""
    aliases = cfg.get("aliases") or []
    alias_str = f" ({', '.join(aliases)})" if aliases else ""
    entries = cfg.get("skills") or []
    try:
        n = len(resolve_skills(entries, manifest.get("bundles", {})))
    except (UnknownBundle, BundleCycle):
        n = -1
    count = "invalid" if n < 0 else f"{n} skill{'' if n == 1 else 's'}"
    refs = " ".join(e for e in entries if e.startswith("@"))
    own = sum(1 for e in entries if not e.startswith("@"))
    comp = " ".join(x for x in (refs, f"+{own}" if own and refs else "") if x)
    mcp = cfg.get("mcp")
    detail = "   ".join(
        x for x in (count, comp, f"mcp: {mcp}" if mcp else "") if x
    )
    return f"{key}{alias_str}", detail


def _agent_summaries(manifest: dict) -> list[str]:
    rows = [agent_summary_line(k, c, manifest)
            for k, c in manifest.get("agents", {}).items()]
    w = max((len(n) for n, _ in rows), default=0)
    return [f"{n.ljust(w)}   {d}" for n, d in rows]


def _report_invalid(manifest, agents_root, out) -> int:
    errors = validate_manifest(manifest, known_skills(agents_root))
    for e in errors:
        print(e, file=out)
    return 2 if errors else 0


def _do_sync(manifest, agents_root, out, check=False) -> int:
    """Validate first and bail before touching the filesystem, so a bad manifest
    can never leave a half-synced tree."""
    rc = _report_invalid(manifest, agents_root, out)
    if rc:
        return rc
    plan = plan_sync(manifest)
    if check:
        stale = [k for k, w in plan.items() if generated_is_stale(k, w, agents_root)]
        root = os.path.join(agents_root, GENERATED_DIRNAME)
        extra = [d for d in (os.listdir(root) if os.path.isdir(root) else [])
                 if d not in plan]
        if stale or extra:
            for k in stale:
                print(f"stale: {k}", file=out)
            for d in extra:
                print(f"orphan: {d}", file=out)
            return 1
        print("in sync", file=out)
        return 0
    changed = sync_generated(plan, agents_root, manifest)
    for w in lock_warnings(agents_root, known_skills(agents_root)):
        print(w, file=out)
    links = sum(len(v) for v in plan.values())
    print(f"{len(plan)} agents · {links} links · {len(changed)} changes", file=out)
    return 0


def _do_activate(choice, manifest, agents_root, out) -> int:
    home = os.path.join(os.path.expanduser("~"), ".claude", SKILLS_DIRNAME)
    tag = os.path.join(home, ".active-agent")
    if choice is None:
        if os.path.isfile(tag):
            print(f"active agent: {open(tag).read().strip()}", file=out)
        else:
            print("active agent: (none)", file=out)
        return 0
    agents = manifest.get("agents", {})
    if choice not in ("all", "none") and choice not in agents:
        print(f"Unknown agent: {choice}\n", file=out)
        print("\n".join(_agent_summaries(manifest)), file=out)
        return 2
    names = activation_names(choice, manifest, agents_root)
    n = activate_global(names, os.path.join(agents_root, SKILLS_DIRNAME), home)
    if choice == "none":
        if os.path.isfile(tag):
            os.remove(tag)
        print("cleared all managed skills", file=out)
    else:
        with open(tag, "w") as f:
            f.write(choice)
        print(f"active agent: {choice} — {n} skills linked", file=out)
    return 0


def dispatch(args, manifest, agents_root, cwd, exec_fn, out, pick_fn=None,
             sync_fn=None) -> int:
    """Parse args and either exec claude (via exec_fn) or print. Returns exit code."""
    dry_run = False
    do_sync = False
    check = False
    activate = _UNSET = object()
    plan_path = None
    use_harness = False
    harness_base_url = os.environ.get("HARNESS_BASE_URL", DEFAULT_HARNESS_BASE_URL)
    positional = []
    it = iter(args)
    for a in it:
        if a == "--dry-run":
            dry_run = True
        elif a == "--sync":
            do_sync = True
        elif a == "--check":
            check = True
        elif a == "--activate":
            activate = next(it, None)
        elif a == "--validate":
            return _report_invalid(manifest, agents_root, out)
        elif a == "--plan":
            plan_path = next(it, None)
        elif a == "--harness":
            use_harness = True
        elif a == "--harness-url":
            use_harness = True
            harness_base_url = next(it, harness_base_url)
        elif a == "--dump-skill-plan":
            print(json.dumps(dump_skill_plan(manifest, agents_root)), file=out)
            return 0
        elif a == "--print-aliases":
            print("\n".join(_alias_lines(manifest)), file=out)
            return 0
        elif a in ("--list", "--help", "-h"):
            print("\n".join(_agent_summaries(manifest)), file=out)
            return 0
        else:
            positional.append(a)

    if do_sync:
        return _do_sync(manifest, agents_root, out, check=check)
    if activate is not _UNSET:
        return _do_activate(activate, manifest, agents_root, out)

    if not positional:
        picked = pick_fn(manifest, agents_root, cwd) if pick_fn else None
        if not picked:
            print("Select an agent to run:\n", file=out)
            print("\n".join(_agent_summaries(manifest)), file=out)
            return 0
        name, picked_plan = picked if isinstance(picked, tuple) else (picked, None)
        plan_path = plan_path or picked_plan
        passthrough = []
    else:
        name = positional[0]
        passthrough = positional[1:]

    if use_harness:
        try:
            spec = resolve_agent_from_harness(name, harness_base_url, agents_root)
        except UnknownAgent:
            print(f"Unknown agent in harness: {name}", file=out)
            return 2
        except HarnessUnreachable as e:
            print(f"error: {e}", file=out)
            return 1
        # Skills/MCP config just came from harness, not agents.json — the
        # local-manifest auto-sync step below doesn't apply to this launch.
    else:
        try:
            spec = resolve_agent(name, manifest, agents_root)
        except UnknownAgent:
            print(f"Unknown agent: {name}\n", file=out)
            print("\n".join(_agent_summaries(manifest)), file=out)
            return 2

        settings = manifest.get("settings", {})
        if settings.get("auto_sync", True):
            wanted = plan_sync(manifest).get(spec.name, [])
            if generated_is_stale(spec.name, wanted, agents_root):
                try:
                    own_dir = manifest["agents"][spec.name].get("dir", spec.name)
                    (sync_fn or sync_agent)(spec.name, wanted, agents_root, own_dir)
                except OSError as e:  # read-only FS etc — never block a launch
                    print(f"warning: could not sync {spec.name}: {e}", file=sys.stderr)

    projects_root = _projects_root(manifest)
    levels = scan_levels(cwd, projects_root)
    names = project_repo_names(levels)
    has_name_flag = any(p == "-n" or p == "--name" or p.startswith("--name=")
                         for p in passthrough)
    if names and not has_name_flag:
        project, repo = names
        passthrough = list(passthrough) + ["--name", f"{spec.name}-{project}-{repo}"]
    if plan_path:
        passthrough = list(passthrough) + [
            plan_prompt_text(plan_path, manifest.get("settings", {}))
        ]
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


def _pick(manifest, agents_root, cwd):  # pragma: no cover - interactive TTY
    from agent_picker import pick
    return pick(manifest, agents_root, cwd)


def main(argv=None) -> int:
    argv = list(sys.argv[1:] if argv is None else argv)
    manifest_path = os.path.join(os.path.dirname(os.path.abspath(__file__)), "agents.json")
    manifest = load_manifest(manifest_path)
    agents_root = os.path.dirname(os.path.abspath(__file__))
    pick_fn = _pick if (not argv and sys.stdin.isatty() and sys.stdout.isatty()) else None
    return dispatch(argv, manifest, agents_root, os.getcwd(),
                    exec_fn=_real_exec, out=sys.stdout, pick_fn=pick_fn)


if __name__ == "__main__":
    sys.exit(main())
