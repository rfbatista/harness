"""Tests for the agent launcher: the harness-backed resolution, the project walk,
the argv it builds. No claude exec, no running harness."""

import os
import io
import json
import unittest
import shutil
import tempfile
import pathlib
from unittest import mock

import agent_launch as al


def _touch(path):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        f.write("x")

class ProjectWalkTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        # realpath so expectations match scan_levels (which resolves macOS /var symlink)
        self.root = pathlib.Path(os.path.realpath(self.tmp.name))
        self.projetos = self.root / "projetos"
        self.subq = self.projetos / "subq"
        self.backend = self.subq / "backend"
        # subq level: mcp + plugins (one plain plugin, one inside a marketplace)
        _touch(str(self.subq / ".mcp.json"))
        _touch(str(self.subq / "plugins" / "p1" / ".claude-plugin" / "plugin.json"))
        _touch(str(self.subq / "plugins" / "mkt" / ".claude-plugin" / "marketplace.json"))
        _touch(str(self.subq / "plugins" / "mkt" / "nested" / ".claude-plugin" / "plugin.json"))
        # backend (cwd) level: mcp + CLAUDE.md, no plugins
        _touch(str(self.backend / ".mcp.json"))
        _touch(str(self.backend / "CLAUDE.md"))

    def tearDown(self):
        self.tmp.cleanup()

    def test_scan_levels_includes_cwd_then_ancestors_stopping_before_root(self):
        levels = al.scan_levels(str(self.backend), str(self.projetos))
        self.assertEqual(
            [lv.path for lv in levels],
            [str(self.backend), str(self.subq)],
        )
        self.assertTrue(levels[0].is_cwd)
        self.assertFalse(levels[1].is_cwd)

    def test_scan_levels_marks_mcp_and_claude_md(self):
        levels = al.scan_levels(str(self.backend), str(self.projetos))
        cwd, subq = levels
        self.assertTrue(cwd.has_mcp)
        self.assertTrue(cwd.has_claude_md)
        self.assertTrue(subq.has_mcp)
        self.assertFalse(subq.has_claude_md)

    def test_scan_levels_finds_plugins_including_nested_marketplace(self):
        levels = al.scan_levels(str(self.backend), str(self.projetos))
        _, subq = levels
        self.assertEqual(
            sorted(subq.plugin_dirs),
            sorted([
                str(self.subq / "plugins" / "p1"),
                str(self.subq / "plugins" / "mkt" / "nested"),
            ]),
        )
        self.assertEqual(levels[0].plugin_dirs, [])

    def test_scan_levels_empty_when_cwd_not_under_root(self):
        outside = self.root / "elsewhere"
        outside.mkdir()
        self.assertEqual(al.scan_levels(str(outside), str(self.projetos)), [])

    def test_scan_levels_empty_when_cwd_is_root(self):
        self.assertEqual(al.scan_levels(str(self.projetos), str(self.projetos)), [])

    def test_project_repo_names_from_walked_levels(self):
        levels = al.scan_levels(str(self.backend), str(self.projetos))
        self.assertEqual(al.project_repo_names(levels), ("subq", "backend"))

    def test_project_repo_names_none_when_no_levels(self):
        self.assertIsNone(al.project_repo_names([]))

    def test_project_repo_names_repo_falls_back_to_project_when_cwd_is_project_dir(self):
        levels = al.scan_levels(str(self.subq), str(self.projetos))
        self.assertEqual(al.project_repo_names(levels), ("subq", "subq"))

class BuildArgvTest(unittest.TestCase):
    def _levels(self):
        return [
            al.Level("/p/backend", is_cwd=True, has_mcp=True, has_claude_md=True,
                     plugin_dirs=["/p/backend/plugins/x"]),
            al.Level("/p/subq", is_cwd=False, has_mcp=True, has_claude_md=False,
                     plugin_dirs=["/p/subq/plugins/y", "/p/backend/plugins/x"]),
        ]

    def test_project_args_add_dir_and_mcp_for_ancestors_only(self):
        args = al.build_project_args(self._levels())
        # cwd is neither --add-dir'd nor --mcp-config'd (native); ancestor is both
        self.assertIn("--add-dir", args)
        self.assertEqual(args.count("--add-dir"), 1)
        self.assertIn("/p/subq", args)
        self.assertIn("/p/subq/.mcp.json", args)
        self.assertNotIn("/p/backend/.mcp.json", args)

    def test_project_args_plugin_dirs_from_all_levels_deduped(self):
        args = al.build_project_args(self._levels())
        plugins = [args[i + 1] for i, a in enumerate(args) if a == "--plugin-dir"]
        self.assertEqual(plugins, ["/p/backend/plugins/x", "/p/subq/plugins/y"])

    def test_build_argv_composes_persona_then_project_then_passthrough(self):
        spec = al.AgentSpec(
            name="backend-developer",
            add_dir_paths=["/A/.generated/backend-developer"],
            mcp_path="/A/backend-developer/.mcp.json",
            load_claude_md=True,
        )
        argv = al.build_argv(spec, self._levels(), passthrough=["--resume"])
        self.assertEqual(argv[0], "claude")
        self.assertEqual(argv[-1], "--resume")
        i = argv.index("--mcp-config")
        self.assertEqual(argv[i + 1], "/A/backend-developer/.mcp.json")
        # persona add-dir appears before project add-dir
        self.assertLess(argv.index("/A/.generated/backend-developer"), argv.index("/p/subq"))

    def test_build_argv_omits_mcp_when_none(self):
        spec = al.AgentSpec(name="db", add_dir_paths=["/A/db"], mcp_path=None)
        argv = al.build_argv(spec, [], passthrough=[])
        self.assertNotIn("--mcp-config", argv)

    def test_build_env_sets_claude_md_flag_when_enabled(self):
        spec = al.AgentSpec(name="x", load_claude_md=True)
        self.assertEqual(
            al.build_env(spec).get("CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD"), "1"
        )

    def test_build_env_omits_claude_md_flag_when_disabled(self):
        spec = al.AgentSpec(name="x", load_claude_md=False)
        self.assertNotIn(
            "CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD", al.build_env(spec)
        )

    def test_build_env_sets_claude_config_dir_from_top_level_project(self):
        spec = al.AgentSpec(name="x", load_claude_md=False)
        env = al.build_env(spec, self._levels())
        self.assertEqual(env.get("CLAUDE_CONFIG_DIR"), "/p/subq/.claude")

    def test_build_env_omits_claude_config_dir_when_no_levels(self):
        spec = al.AgentSpec(name="x", load_claude_md=False)
        self.assertNotIn("CLAUDE_CONFIG_DIR", al.build_env(spec, []))
        self.assertNotIn("CLAUDE_CONFIG_DIR", al.build_env(spec))

    def test_project_config_dir_uses_last_walked_level(self):
        self.assertEqual(
            al.project_config_dir(self._levels()), "/p/subq/.claude"
        )

    def test_project_config_dir_none_when_no_levels(self):
        self.assertIsNone(al.project_config_dir([]))

    def test_project_config_dir_uses_cwd_when_cwd_is_top_level(self):
        levels = [al.Level("/p/subq", is_cwd=True, has_mcp=False,
                            has_claude_md=False)]
        self.assertEqual(al.project_config_dir(levels), "/p/subq/.claude")

class ContextPanelTest(unittest.TestCase):
    def test_project_label_relative_to_root(self):
        self.assertEqual(
            al.project_label("/home/u/projetos/subq/backend", "/home/u/projetos"),
            "subq/backend",
        )

    def test_project_label_none_when_not_under_root(self):
        self.assertIsNone(al.project_label("/tmp/x", "/home/u/projetos"))

    def test_panel_no_project(self):
        lines = al.render_context_panel(None, [])
        self.assertEqual(len(lines), 1)
        self.assertIn("no project", lines[0].lower())

    def test_panel_shows_label_and_per_level_marks(self):
        levels = [
            al.Level("/p/subq/backend", is_cwd=True, has_mcp=True, has_claude_md=True,
                     plugin_dirs=[]),
            al.Level("/p/subq", is_cwd=False, has_mcp=False, has_claude_md=False,
                     plugin_dirs=["/p/subq/plugins/y"]),
        ]
        lines = al.render_context_panel("subq/backend", levels)
        text = "\n".join(lines)
        self.assertIn("subq/backend", text)
        self.assertIn("backend", text)
        self.assertIn("mcp ✓", text)   # cwd has mcp
        self.assertIn("mcp ✗", text)   # subq has none
        self.assertIn("plugins ✓ (1)", text)  # subq has one plugin

class PlanFrameTest(unittest.TestCase):
    def test_plan_frame_lists_plan_labels_and_agent(self):
        import agent_picker
        plans = [("a.md", "/p/a.md"), ("b.md", "/p/b.md")]
        lines = agent_picker._plan_frame("backend", plans, cursor=1)
        text = "\n".join(lines)
        self.assertIn("backend", text)
        self.assertIn("a.md", text)
        self.assertIn("b.md", text)

class FindPlansTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = pathlib.Path(os.path.realpath(self.tmp.name))
        self.projetos = self.root / "projetos"
        self.subq = self.projetos / "subq"
        self.backend = self.subq / "backend"
        self.backend.mkdir(parents=True)

    def tearDown(self):
        self.tmp.cleanup()

    def _settings(self, **over):
        base = {"projects_root": str(self.projetos), "plans_dir": ".claude/plans"}
        base.update(over)
        return base

    def test_none_when_cwd_not_under_root(self):
        self.assertEqual(al.find_plans("/tmp/x", str(self.projetos), self._settings()), [])

    def test_inherits_plans_from_ancestor_first_nonempty_level(self):
        _touch(str(self.subq / ".claude" / "plans" / "b.md"))
        _touch(str(self.subq / ".claude" / "plans" / "a.md"))
        plans = al.find_plans(str(self.backend), str(self.projetos), self._settings())
        self.assertEqual([label for label, _ in plans], ["a.md", "b.md"])  # sorted
        self.assertTrue(plans[0][1].endswith("subq/.claude/plans/a.md"))

    def test_cwd_level_wins_over_ancestor(self):
        _touch(str(self.subq / ".claude" / "plans" / "ancestor.md"))
        _touch(str(self.backend / ".claude" / "plans" / "own.md"))
        plans = al.find_plans(str(self.backend), str(self.projetos), self._settings())
        self.assertEqual([label for label, _ in plans], ["own.md"])

    def test_override_by_relative_project_path(self):
        _touch(str(self.subq / "docs" / "plans" / "p.md"))
        settings = self._settings(plans_dir_overrides={"subq": "docs/plans"})
        plans = al.find_plans(str(self.backend), str(self.projetos), settings)
        self.assertEqual([label for label, _ in plans], ["p.md"])

    def test_empty_when_no_plans_dir(self):
        self.assertEqual(
            al.find_plans(str(self.backend), str(self.projetos), self._settings()), []
        )

class PlanPromptTest(unittest.TestCase):
    def test_uses_template_with_path(self):
        s = {"plan_prompt": "Do {path} now"}
        self.assertEqual(al.plan_prompt_text("/p/x.md", s), "Do /p/x.md now")

    def test_default_template_mentions_spec_and_path(self):
        text = al.plan_prompt_text("/p/x.md", {})
        self.assertIn("/p/x.md", text)
        self.assertIn("spec", text.lower())


class FakeHarness:
    """What a running harness answers: /api/get_agent_catalog, /list_agents and
    /get_agent, for the agents a test gives it."""

    def __init__(self, agents, catalog=True):
        self.agents = agents  # name -> {aliases, skills, mcp_servers, description}
        self.catalog = catalog
        self.seen = []

    def __call__(self, base_url, path):
        self.seen.append((base_url, path))
        if path == "/get_agent_catalog":
            if not self.catalog:
                raise al.HarnessUnreachable("GET /get_agent_catalog -> HTTP 404: not found")
            return {"agents": [{
                "name": n, "aliases": a.get("aliases", []), "description": a.get("description", ""),
                "resolved_skills": [s["name"] for s in a.get("skills", [])],
                "mcp_servers": [s["name"] for s in a.get("mcp_servers", [])],
            } for n, a in self.agents.items()]}
        if path == "/list_agents":
            return {"agents": [{"id": n, "name": n,
                                "skill_ids": [s["name"] for s in a.get("skills", [])],
                                "mcp_server_ids": [s["name"] for s in a.get("mcp_servers", [])]}
                               for n, a in self.agents.items()]}
        name = path.split("agent_id=")[1]
        a = self.agents[name]
        return {"agent": {"id": name, "name": name, "skills": a.get("skills", []),
                          "mcp_servers": a.get("mcp_servers", [])}}


def _skill(name, content="x"):
    return {"name": name, "files": [{"path": "SKILL.md", "content": content}]}


AGENTS = {
    "backend-developer": {"aliases": ["backend"], "skills": [_skill("tdd"), _skill("git-commit")],
                          "mcp_servers": [{"name": "postgres", "transport": "stdio", "command": "uv",
                                           "args": ["run", "postgres-mcp"]}]},
    "designer": {"skills": [_skill("design-md")]},
    "go-developer": {"aliases": ["go"], "skills": []},
}


class SettingsTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp()

    def tearDown(self):
        shutil.rmtree(self.tmp, ignore_errors=True)

    def test_reads_the_settings_key_of_the_catalogs_agents_json(self):
        with open(os.path.join(self.tmp, "agents.json"), "w") as f:
            json.dump({"settings": {"projects_root": "/p", "auto_sync": True}, "agents": {}}, f)
        self.assertEqual(al.load_settings(self.tmp), {"projects_root": "/p", "auto_sync": True})

    def test_defaults_when_the_file_is_not_on_this_machine(self):
        self.assertEqual(al.load_settings(os.path.join(self.tmp, "nope")), {})

    def test_defaults_when_the_file_does_not_parse(self):
        with open(os.path.join(self.tmp, "agents.json"), "w") as f:
            f.write("{ broken")
        self.assertEqual(al.load_settings(self.tmp), {})

    def test_catalog_dir_comes_from_the_environment(self):
        with mock.patch.dict(os.environ, {al.AGENTS_DIR_ENV: "/somewhere"}):
            self.assertEqual(al.default_agents_dir(), "/somewhere")
        with mock.patch.dict(os.environ, {}, clear=True):
            self.assertTrue(al.default_agents_dir().endswith(os.path.join(".harness", "agents")))


class FetchAgentsTest(unittest.TestCase):
    def test_names_aliases_counts_and_servers_come_from_the_catalog_read(self):
        with mock.patch.object(al, "_harness_request", FakeHarness(AGENTS)):
            agents = al.fetch_agents("http://x")
        by = {a["name"]: a for a in agents}
        self.assertEqual(by["backend-developer"]["aliases"], ["backend"])
        self.assertEqual(by["backend-developer"]["skill_count"], 2)
        self.assertEqual(by["backend-developer"]["mcp_servers"], ["postgres"])

    def test_a_harness_without_the_catalog_read_answers_from_list_agents(self):
        with mock.patch.object(al, "_harness_request", FakeHarness(AGENTS, catalog=False)):
            agents = al.fetch_agents("http://x")
        self.assertEqual({a["name"] for a in agents}, set(AGENTS))
        self.assertEqual(agents[0]["aliases"], [])

    def test_an_unreachable_harness_raises(self):
        def down(base_url, path):
            raise al.HarnessUnreachable("GET /get_agent_catalog -> connection refused")
        with mock.patch.object(al, "_harness_request", down):
            with self.assertRaises(al.HarnessUnreachable):
                al.fetch_agents("http://x")

    def test_agent_key_takes_a_name_or_an_alias(self):
        agents = [{"name": "backend-developer", "aliases": ["backend"]}, {"name": "designer", "aliases": []}]
        self.assertEqual(al.agent_key("designer", agents), "designer")
        self.assertEqual(al.agent_key("backend", agents), "backend-developer")
        with self.assertRaises(al.UnknownAgent):
            al.agent_key("nope", agents)


class ResolveAgentFromHarnessTest(unittest.TestCase):
    """resolve_agent_from_harness writes a harness agent's skills and MCP servers
    to disk, from the harness alone."""

    def setUp(self):
        self.tmp = tempfile.mkdtemp()

    def tearDown(self):
        shutil.rmtree(self.tmp, ignore_errors=True)

    def resolve(self, agents, name, **kw):
        with mock.patch.object(al, "_harness_request", FakeHarness(agents)):
            return al.resolve_agent_from_harness(name, "http://x", self.tmp, **kw)

    def test_writes_text_and_base64_skill_files(self):
        skill = {"name": "tdd", "files": [
            {"path": "SKILL.md", "content": "---\nname: tdd\n---\nbody"},
            {"path": "assets", "dir": True},
            {"path": "assets/logo.png", "content": "aGVsbG8=", "encoding": "base64"},
        ]}
        spec = self.resolve({"go-developer": {"skills": [skill]}}, "go-developer")
        skill_dir = os.path.join(self.tmp, ".generated-harness", "go-developer", ".claude", "skills", "tdd")
        with open(os.path.join(skill_dir, "SKILL.md")) as f:
            self.assertIn("name: tdd", f.read())
        with open(os.path.join(skill_dir, "assets", "logo.png"), "rb") as f:
            self.assertEqual(f.read(), b"hello")
        self.assertEqual(spec.skills, ["tdd"])
        self.assertEqual(spec.add_dir_paths, [os.path.join(self.tmp, ".generated-harness", "go-developer")])

    def test_an_alias_resolves_to_the_agent(self):
        spec = self.resolve(AGENTS, "backend")
        self.assertEqual(spec.name, "backend-developer")
        self.assertEqual(spec.skills, ["tdd", "git-commit"])

    def test_writes_stdio_mcp_server_preserving_secret_placeholder(self):
        agent = {"skills": [], "mcp_servers": [{"name": "postgres", "transport": "stdio", "command": "uv",
                  "args": ["run", "postgres-mcp"], "env": {"TOKEN": "${GITHUB_PERSONAL_ACCESS_TOKEN}"}}]}
        spec = self.resolve({"backend-developer": agent}, "backend-developer")
        with open(spec.mcp_path) as f:
            entry = json.load(f)["mcpServers"]["postgres"]
        self.assertEqual(entry["command"], "uv")
        self.assertEqual(entry["env"]["TOKEN"], "${GITHUB_PERSONAL_ACCESS_TOKEN}")

    def test_writes_http_mcp_server_with_headers(self):
        agent = {"skills": [], "mcp_servers": [{"name": "github", "transport": "streamable-http",
                  "url": "https://api.githubcopilot.com/mcp/",
                  "headers": {"Authorization": "Bearer ${GITHUB_PERSONAL_ACCESS_TOKEN}"}}]}
        spec = self.resolve({"backend-developer": agent}, "backend-developer")
        with open(spec.mcp_path) as f:
            entry = json.load(f)["mcpServers"]["github"]
        self.assertEqual(entry["type"], "http")
        self.assertEqual(entry["headers"]["Authorization"], "Bearer ${GITHUB_PERSONAL_ACCESS_TOKEN}")

    def test_no_mcp_path_when_agent_has_no_servers_and_an_old_file_goes(self):
        out_dir = os.path.join(self.tmp, ".generated-harness", "designer")
        os.makedirs(out_dir)
        stale = os.path.join(out_dir, ".mcp.json")
        with open(stale, "w") as f:
            f.write("{}")
        spec = self.resolve(AGENTS, "designer")
        self.assertIsNone(spec.mcp_path)
        self.assertFalse(os.path.exists(stale))

    def test_load_claude_md_follows_the_setting(self):
        self.assertFalse(self.resolve(AGENTS, "designer", load_claude_md=False).load_claude_md)
        self.assertTrue(self.resolve(AGENTS, "designer").load_claude_md)

    def test_unknown_agent_raises(self):
        with self.assertRaises(al.UnknownAgent):
            self.resolve({}, "nope")


class DispatchTest(unittest.TestCase):
    def setUp(self):
        self.calls = []
        self.out = io.StringIO()
        self.tmp = tempfile.mkdtemp()
        self.harness = FakeHarness(AGENTS)
        patcher = mock.patch.object(al, "_harness_request", self.harness)
        patcher.start()
        self.addCleanup(patcher.stop)
        self.addCleanup(shutil.rmtree, self.tmp, ignore_errors=True)

    def _exec(self, argv, env):
        self.calls.append((argv, env))

    def dispatch(self, args, settings=None, cwd="/tmp/outside", pick_fn=None):
        return al.dispatch(args, settings or {}, agents_root=self.tmp, cwd=cwd,
                           exec_fn=self._exec, out=self.out, pick_fn=pick_fn)

    def test_direct_name_execs_claude_with_the_materialised_add_dir(self):
        rc = self.dispatch(["backend"])
        self.assertEqual(rc, 0)
        argv, env = self.calls[0]
        self.assertEqual(argv[0], "claude")
        self.assertIn(os.path.join(self.tmp, ".generated-harness", "backend-developer"), argv)
        self.assertIn("--mcp-config", argv)
        self.assertEqual(env.get("CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD"), "1")

    def test_load_claude_md_setting_off_leaves_the_env_out(self):
        self.dispatch(["designer"], settings={"load_claude_md": False})
        self.assertNotIn("CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD", self.calls[0][1])

    def test_passthrough_forwarded(self):
        self.dispatch(["backend", "--resume", "-p", "hi"])
        self.assertEqual(self.calls[0][0][-3:], ["--resume", "-p", "hi"])

    def test_dry_run_prints_and_does_not_exec(self):
        rc = self.dispatch(["--dry-run", "backend"])
        self.assertEqual(rc, 0)
        self.assertEqual(self.calls, [])
        self.assertIn("claude", self.out.getvalue())

    def test_harness_flag_is_accepted_and_changes_nothing(self):
        self.assertEqual(self.dispatch(["designer", "--harness"]), 0)
        self.assertEqual(len(self.calls), 1)

    def test_harness_url_overrides_the_base_url(self):
        self.dispatch(["designer", "--harness-url", "http://elsewhere:9000"])
        self.assertTrue(all(u == "http://elsewhere:9000" for u, _ in self.harness.seen))

    def test_unknown_agent_lists_the_harness_agents_without_exec(self):
        rc = self.dispatch(["nope"])
        self.assertEqual(rc, 2)
        self.assertEqual(self.calls, [])
        self.assertIn("Unknown agent in harness: nope", self.out.getvalue())
        self.assertIn("designer", self.out.getvalue())

    def test_unreachable_harness_errors_clearly_without_exec(self):
        def down(base_url, path):
            raise al.HarnessUnreachable("GET /get_agent_catalog -> connection refused")
        with mock.patch.object(al, "_harness_request", down):
            rc = self.dispatch(["go-developer"])
        self.assertEqual(rc, 1)
        self.assertIn("connection refused", self.out.getvalue())
        self.assertEqual(self.calls, [])

    def test_print_aliases_emits_a_line_per_name_and_alias(self):
        rc = self.dispatch(["--print-aliases"])
        self.assertEqual(rc, 0)
        text = self.out.getvalue()
        self.assertIn("alias backend='agent backend'", text)
        self.assertIn("alias designer='agent designer'", text)
        self.assertEqual(self.calls, [])

    def test_list_summarises_every_agent(self):
        self.assertEqual(self.dispatch(["--list"]), 0)
        text = self.out.getvalue()
        self.assertIn("backend-developer (backend)", text)
        self.assertIn("2 skills", text)
        self.assertIn("mcp: postgres", text)
        self.assertIn("0 skills", text)

    def test_no_args_uses_the_picker_when_provided(self):
        picked = []
        def pick(agents, settings, cwd):
            picked.append([a["name"] for a in agents])
            return "designer"
        self.dispatch([], pick_fn=pick)
        self.assertEqual(picked, [list(AGENTS)])
        self.assertIn(os.path.join(self.tmp, ".generated-harness", "designer"), self.calls[0][0])

    def test_no_args_without_picker_lists_and_no_exec(self):
        rc = self.dispatch([], pick_fn=None)
        self.assertEqual(self.calls, [])
        self.assertIn("backend-developer", self.out.getvalue())
        self.assertEqual(rc, 0)

    def test_the_flags_of_the_local_tree_say_where_the_agents_went(self):
        for flag in al.REMOVED_FLAGS:
            self.out.truncate(0)
            self.out.seek(0)
            self.assertEqual(self.dispatch([flag]), 2)
            self.assertIn("harness", self.out.getvalue())
        self.assertEqual(self.calls, [])

    def test_plan_flag_appends_prompt_as_last_arg(self):
        self.dispatch(["backend", "--plan", "/p/x.md"])
        self.assertIn("/p/x.md", self.calls[0][0][-1])

    def test_dry_run_with_plan_prints_prompt_without_exec(self):
        rc = self.dispatch(["--dry-run", "backend", "--plan", "/p/x.md"])
        self.assertEqual(rc, 0)
        self.assertEqual(self.calls, [])
        self.assertIn("/p/x.md", self.out.getvalue())


class DispatchNameFlagTest(unittest.TestCase):
    """agent launches under <projects_root>/<project>/<repo>/... get a
    predictable --name <agent>-<project>-<repo> so other sessions can
    address them via ListAgents/SendMessage."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = pathlib.Path(os.path.realpath(self.tmp.name))
        self.projetos = self.root / "projetos"
        self.backend = self.projetos / "subq" / "backend"
        self.backend.mkdir(parents=True)
        self.settings = {"projects_root": str(self.projetos)}
        self.calls = []
        self.out = io.StringIO()
        patcher = mock.patch.object(al, "_harness_request", FakeHarness(AGENTS))
        patcher.start()
        self.addCleanup(patcher.stop)

    def tearDown(self):
        self.tmp.cleanup()

    def _exec(self, argv, env):
        self.calls.append((argv, env))

    def dispatch(self, args):
        return al.dispatch(args, self.settings, agents_root=str(self.root), cwd=str(self.backend),
                           exec_fn=self._exec, out=self.out)

    def test_name_flag_set_from_project_repo_layout(self):
        self.dispatch(["backend"])
        argv, _ = self.calls[0]
        self.assertEqual(argv[argv.index("--name") + 1], "backend-developer-subq-backend")

    def test_no_name_flag_when_outside_projects_root(self):
        al.dispatch(["backend"], self.settings, agents_root=str(self.root), cwd="/tmp/outside",
                    exec_fn=self._exec, out=self.out)
        self.assertNotIn("--name", self.calls[0][0])

    def test_explicit_long_name_flag_is_respected(self):
        self.dispatch(["backend", "--name", "custom"])
        argv, _ = self.calls[0]
        self.assertEqual(argv.count("--name"), 1)
        self.assertEqual(argv[argv.index("--name") + 1], "custom")

    def test_explicit_short_name_flag_is_respected(self):
        self.dispatch(["backend", "-n", "custom"])
        argv, _ = self.calls[0]
        self.assertNotIn("--name", argv)
        self.assertEqual(argv[argv.index("-n") + 1], "custom")

    def test_name_flag_comes_before_trailing_plan_prompt(self):
        self.dispatch(["backend", "--plan", "/A/plans/p.md"])
        argv, _ = self.calls[0]
        self.assertIn("--name", argv)
        self.assertNotEqual(argv[-1], "backend-developer-subq-backend")
        self.assertIn("Read the plan at /A/plans/p.md", argv[-1])


class PickerRowsTest(unittest.TestCase):
    def test_every_row_is_an_agent_of_the_harness(self):
        import agent_picker
        with mock.patch.object(al, "_harness_request", FakeHarness(AGENTS)):
            agents = al.fetch_agents("http://x")
        rows = agent_picker._rows(agents)
        self.assertEqual([key for key, _label in rows], list(AGENTS))
        for key, _label in rows:
            al.agent_key(key, agents)  # must not raise



if __name__ == "__main__":
    unittest.main()
