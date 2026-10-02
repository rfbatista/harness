"""Interactive arrow-key picker for the agent launcher.

TTY-only. Renders the project context panel + agent list, returns the selected
agent key (or None if the user quits). UI is drawn to stderr so stdout stays clean.
"""

import os
import sys
import tty
import termios

import agent_launch as al

RESET = "\033[0m"
BOLD = "\033[1m"
DIM = "\033[2m"
CYAN = "\033[36m"
GREEN = "\033[32m"
WHITE = "\033[97m"
BG_SEL = "\033[48;5;235m"
HIDE_CURSOR = "\033[?25l"
SHOW_CURSOR = "\033[?25h"
ALT_SCREEN_ON = "\033[?1049h"
ALT_SCREEN_OFF = "\033[?1049l"
HOME_CLEAR = "\033[H\033[2J"


def _read_key(fd):
    ch = os.read(fd, 1)
    if ch == b"\x1b":
        rest = os.read(fd, 2)
        if rest == b"[A":
            return "up"
        if rest == b"[B":
            return "down"
        return "esc"
    if ch in (b"\r", b"\n"):
        return "enter"
    if ch in (b"q", b"Q"):
        return "quit"
    if ch in (b"p", b"P"):
        return "plan"
    if ch in (b"s", b"S"):
        return "skills"
    return "other"


def _rows(manifest):
    """One (key, label) per agent, in manifest order."""
    rows = []
    for key, cfg in manifest.get("agents", {}).items():
        name, detail = al.agent_summary_line(key, cfg, manifest)
        rows.append((key, f"{name}   {DIM}{detail}{RESET}"))
    return rows


def _activate_rows(manifest):
    """Targets for the global-activation sub-screen."""
    return ([("none", "none   clear ~/.claude/skills"),
             ("all", "all   every skill")]
            + [(k, k) for k in manifest.get("agents", {})])


def _rows_block(items, cursor):
    lines = []
    for i, label in enumerate(items):
        if i == cursor:
            lines.append(f"{BG_SEL}{CYAN} ❯ {WHITE}{BOLD}{label}{RESET}")
        else:
            lines.append(f"   {label}")
    return lines


def _frame(panel, rows, cursor, note=""):
    """Agent-list frame."""
    lines = [f"{CYAN}Project{RESET}"]
    for p in panel:
        lines.append("  " + p)
    lines.append("")
    lines.append(f"  {DIM}↑↓ navigate   Enter run   p with plan   q quit{RESET}")
    if note:
        lines.append(f"  {DIM}{note}{RESET}")
    lines.append("")
    lines += _rows_block([label for _key, label in rows], cursor)
    return lines


def _plan_frame(agent_name, plans, cursor):
    """Plan sub-screen frame for the chosen agent."""
    lines = [f"{CYAN}Run {WHITE}{BOLD}{agent_name}{RESET}{CYAN} with a plan:{RESET}"]
    lines.append("")
    lines.append(f"  {DIM}↑↓ navigate   Enter run with plan   Esc back{RESET}")
    lines.append("")
    lines += _rows_block([label for label, _path in plans], cursor)
    return lines


def _render(out, lines):
    # \r after each line so wrapped lines still start at column 0 in raw mode
    out.write(HOME_CLEAR + "\r\n".join(lines) + "\r\n")
    out.flush()


def _pick_plan(out, fd, agent_name, plans):
    """Plan sub-screen loop. Returns the selected plan path, or None on Esc/back."""
    cursor = 0
    while True:
        _render(out, _plan_frame(agent_name, plans, cursor))
        key = _read_key(fd)
        if key == "up":
            cursor = (cursor - 1) % len(plans)
        elif key == "down":
            cursor = (cursor + 1) % len(plans)
        elif key == "enter":
            return plans[cursor][1]
        elif key in ("esc", "quit"):
            return None


def _activate_frame(rows, cursor):
    lines = [f"  {BOLD}activate globally{RESET}  {DIM}(~/.claude/skills){RESET}", ""]
    lines += _rows_block([label for _key, label in rows], cursor)
    lines += ["", f"  {DIM}enter select · esc back{RESET}"]
    return lines


def _pick_activate(out, fd, manifest):
    rows = _activate_rows(manifest)
    cursor = 0
    while True:
        _render(out, _activate_frame(rows, cursor))
        key = _read_key(fd)
        if key == "up":
            cursor = (cursor - 1) % len(rows)
        elif key == "down":
            cursor = (cursor + 1) % len(rows)
        elif key == "enter":
            return rows[cursor][0]
        elif key in ("quit", "esc"):
            return None


def pick(manifest, agents_root, cwd):
    out = sys.stderr
    projects_root = al._projects_root(manifest)
    levels = al.scan_levels(cwd, projects_root)
    panel = al.render_context_panel(al.project_label(cwd, projects_root), levels)
    settings = manifest.get("settings", {})
    rows = _rows(manifest)
    if not rows:
        return None

    fd = sys.stdin.fileno()
    old = termios.tcgetattr(fd)
    cursor = 0
    note = ""
    out.write(ALT_SCREEN_ON + HIDE_CURSOR)
    out.flush()
    try:
        tty.setraw(fd)
        while True:
            _render(out, _frame(panel, rows, cursor, note))
            note = ""
            key = _read_key(fd)
            if key == "up":
                cursor = (cursor - 1) % len(rows)
            elif key == "down":
                cursor = (cursor + 1) % len(rows)
            elif key == "enter":
                return (rows[cursor][0], None)
            elif key == "plan":
                plans = al.find_plans(cwd, projects_root, settings)
                if not plans:
                    note = f"no plans in {al._effective_plans_dir(levels, projects_root, settings)}"
                else:
                    chosen = _pick_plan(out, fd, rows[cursor][0], plans)
                    if chosen:
                        return (rows[cursor][0], chosen)
            elif key == "skills":
                choice = _pick_activate(out, fd, manifest)
                if choice:
                    names = al.activation_names(choice, manifest, agents_root)
                    n = al.activate_global(
                        names,
                        os.path.join(agents_root, al.SKILLS_DIRNAME),
                        os.path.join(os.path.expanduser("~"), ".claude",
                                     al.SKILLS_DIRNAME),
                    )
                    note = f"activated {choice} — {n} skills"
            elif key in ("quit", "esc"):
                return None
    finally:
        termios.tcsetattr(fd, termios.TCSADRAIN, old)
        out.write(SHOW_CURSOR + ALT_SCREEN_OFF)
        out.flush()
