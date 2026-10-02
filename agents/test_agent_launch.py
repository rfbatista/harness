"""Tests for the agent launcher (pure resolution + project walk, no claude exec)."""

import os
import io
import json
import unittest
import shutil
import tempfile
import pathlib
from unittest import mock

import agent_launch as al


MANIFEST = {
    "settings": {"projects_root": "~/projetos", "load_claude_md": True,
                 "auto_sync": False},
    "bundles": {
        "developer": ["git-commit", "tdd"],
        "javascript-developer": ["tree-shaking", "git-commit"],
        "react-developer": ["hooks-pattern"],
    },
    "agents": {
        "backend-developer": {
            "skills": ["api-design", "@developer", "@javascript-developer"],
            "mcp": "self",
            "aliases": ["backend"],
        },
        "node-developer": {
            "skills": ["node", "@developer", "@javascript-developer"],
            "mcp": "backend-developer",
        },
        "designer": {"skills": ["design-md"], "mcp": "self"},
        "database-manager": {"skills": []},
        "frontend": {
            "dir": "frontend-developer",
            "skills": ["@developer", "@react-developer"],
        },
        "go-developer": {"skills": ["@developer"], "load_claude_md": False},
    },
}

ALL_SKILLS = {"api-design", "git-commit", "tdd", "tree-shaking",
              "hooks-pattern", "node", "design-md"}


class ResolveAgentTest(unittest.TestCase):
    def test_add_dir_is_the_single_generated_dir(self):
        spec = al.resolve_agent("backend-developer", MANIFEST, agents_root="/A")
        self.assertEqual(spec.add_dir_paths, ["/A/.generated/backend-developer"])

    def test_skills_expand_own_first_then_bundles_deduped(self):
        spec = al.resolve_agent("backend-developer", MANIFEST, agents_root="/A")
        self.assertEqual(
            spec.skills,
            ["api-design", "git-commit", "tdd", "tree-shaking"],
        )

    def test_resolves_by_alias(self):
        spec = al.resolve_agent("backend", MANIFEST, agents_root="/A")
        self.assertEqual(spec.name, "backend-developer")

    def test_mcp_self_points_at_own_dir(self):
        spec = al.resolve_agent("backend-developer", MANIFEST, agents_root="/A")
        self.assertEqual(spec.mcp_path, "/A/backend-developer/.mcp.json")

    def test_mcp_other_points_at_named_agent(self):
        spec = al.resolve_agent("node-developer", MANIFEST, agents_root="/A")
        self.assertEqual(spec.mcp_path, "/A/backend-developer/.mcp.json")

    def test_no_mcp_when_omitted(self):
        spec = al.resolve_agent("database-manager", MANIFEST, agents_root="/A")
        self.assertIsNone(spec.mcp_path)

    def test_dir_override_keys_generated_dir_by_key_not_dir(self):
        # `dir` now only says where .mcp.json / .claude/agents live; the
        # generated tree is keyed by the manifest key so it stays unique.
        spec = al.resolve_agent("frontend", MANIFEST, agents_root="/A")
        self.assertEqual(spec.add_dir_paths, ["/A/.generated/frontend"])
        self.assertEqual(spec.skills, ["git-commit", "tdd", "hooks-pattern"])

    def test_load_claude_md_defaults_true(self):
        spec = al.resolve_agent("backend-developer", MANIFEST, agents_root="/A")
        self.assertTrue(spec.load_claude_md)

    def test_load_claude_md_per_agent_override(self):
        spec = al.resolve_agent("go-developer", MANIFEST, agents_root="/A")
        self.assertFalse(spec.load_claude_md)

    def test_unknown_agent_raises(self):
        with self.assertRaises(al.UnknownAgent):
            al.resolve_agent("nope", MANIFEST, agents_root="/A")


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


class ManifestLoadTest(unittest.TestCase):
    def test_loads_json_manifest(self):
        with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False) as f:
            json.dump(MANIFEST, f)
            path = f.name
        self.addCleanup(os.unlink, path)
        m = al.load_manifest(path)
        self.assertIn("backend-developer", m["agents"])


class DispatchTest(unittest.TestCase):
    def setUp(self):
        self.calls = []
        self.out = io.StringIO()

    def _exec(self, argv, env):
        self.calls.append((argv, env))

    def dispatch(self, args, pick_fn=None):
        return al.dispatch(
            args, MANIFEST, agents_root="/A", cwd="/tmp/outside",
            exec_fn=self._exec, out=self.out, pick_fn=pick_fn,
        )

    def test_direct_name_execs_claude(self):
        rc = self.dispatch(["backend"])
        self.assertEqual(rc, 0)
        self.assertEqual(len(self.calls), 1)
        argv, env = self.calls[0]
        self.assertEqual(argv[0], "claude")
        self.assertIn("/A/.generated/backend-developer", argv)
        self.assertEqual(env.get("CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD"), "1")

    def test_passthrough_forwarded(self):
        self.dispatch(["backend", "--resume", "-p", "hi"])
        argv, _ = self.calls[0]
        self.assertEqual(argv[-3:], ["--resume", "-p", "hi"])

    def test_dry_run_prints_and_does_not_exec(self):
        rc = self.dispatch(["--dry-run", "backend"])
        self.assertEqual(rc, 0)
        self.assertEqual(self.calls, [])
        self.assertIn("claude", self.out.getvalue())

    def test_print_aliases_emits_lines(self):
        rc = self.dispatch(["--print-aliases"])
        self.assertEqual(rc, 0)
        text = self.out.getvalue()
        self.assertIn("alias backend='agent backend'", text)
        self.assertIn("alias node-developer='agent node-developer'", text)
        self.assertEqual(self.calls, [])

    def test_no_name_flag_when_outside_projects_root(self):
        self.dispatch(["backend"])
        argv, _ = self.calls[0]
        self.assertNotIn("--name", argv)

    def test_no_args_uses_picker_when_provided(self):
        self.dispatch([], pick_fn=lambda *a: "designer")
        argv, _ = self.calls[0]
        self.assertIn("/A/.generated/designer", argv)

    def test_no_args_without_picker_lists_and_no_exec(self):
        rc = self.dispatch([], pick_fn=None)
        self.assertEqual(self.calls, [])
        self.assertIn("backend", self.out.getvalue())
        self.assertEqual(rc, 0)


class ResolveAgentFromHarnessTest(unittest.TestCase):
    """resolve_agent_from_harness materialises a harness Agent's skills/MCP
    servers to disk without ever touching agents.json — the fake below plays
    the part of a running harness instance's /api/{list,get}_agents."""

    def setUp(self):
        self.tmp = tempfile.mkdtemp()

    def tearDown(self):
        shutil.rmtree(self.tmp, ignore_errors=True)

    def _fake(self, agents_by_name):
        def request(base_url, path):
            if path == "/list_agents":
                return {"agents": [{"id": a["id"], "name": name}
                                    for name, a in agents_by_name.items()]}
            agent_id = path.split("agent_id=")[1]
            agent = next(a for a in agents_by_name.values() if a["id"] == agent_id)
            return {"agent": agent}
        return request

    def test_writes_text_and_base64_skill_files(self):
        agent = {
            "id": "a1", "name": "go-developer",
            "skills": [{
                "name": "tdd",
                "files": [
                    {"path": "SKILL.md", "content": "---\nname: tdd\n---\nbody"},
                    {"path": "assets", "dir": True},
                    {"path": "assets/logo.png", "content": "aGVsbG8=", "encoding": "base64"},
                ],
            }],
        }
        with mock.patch.object(al, "_harness_request", self._fake({"go-developer": agent})):
            spec = al.resolve_agent_from_harness("go-developer", "http://x", self.tmp)

        skill_dir = os.path.join(self.tmp, ".generated-harness", "go-developer",
                                  ".claude", "skills", "tdd")
        with open(os.path.join(skill_dir, "SKILL.md")) as f:
            self.assertIn("name: tdd", f.read())
        with open(os.path.join(skill_dir, "assets", "logo.png"), "rb") as f:
            self.assertEqual(f.read(), b"hello")
        self.assertEqual(spec.skills, ["tdd"])
        self.assertEqual(spec.add_dir_paths,
                          [os.path.join(self.tmp, ".generated-harness", "go-developer")])

    def test_writes_stdio_mcp_server_preserving_secret_placeholder(self):
        agent = {
            "id": "a1", "name": "backend-developer", "skills": [],
            "mcp_servers": [{"name": "postgres", "transport": "stdio",
                              "command": "uv", "args": ["run", "postgres-mcp"],
                              "env": {"TOKEN": "${GITHUB_PERSONAL_ACCESS_TOKEN}"}}],
        }
        with mock.patch.object(al, "_harness_request", self._fake({"backend-developer": agent})):
            spec = al.resolve_agent_from_harness("backend-developer", "http://x", self.tmp)

        with open(spec.mcp_path) as f:
            written = json.load(f)
        entry = written["mcpServers"]["postgres"]
        self.assertEqual(entry["command"], "uv")
        self.assertEqual(entry["env"]["TOKEN"], "${GITHUB_PERSONAL_ACCESS_TOKEN}")

    def test_writes_http_mcp_server_with_headers(self):
        agent = {
            "id": "a1", "name": "backend-developer", "skills": [],
            "mcp_servers": [{"name": "github", "transport": "streamable-http",
                              "url": "https://api.githubcopilot.com/mcp/",
                              "headers": {"Authorization": "Bearer ${GITHUB_PERSONAL_ACCESS_TOKEN}"}}],
        }
        with mock.patch.object(al, "_harness_request", self._fake({"backend-developer": agent})):
            spec = al.resolve_agent_from_harness("backend-developer", "http://x", self.tmp)

        with open(spec.mcp_path) as f:
            written = json.load(f)
        entry = written["mcpServers"]["github"]
        self.assertEqual(entry["type"], "http")
        self.assertEqual(entry["headers"]["Authorization"], "Bearer ${GITHUB_PERSONAL_ACCESS_TOKEN}")

    def test_no_mcp_path_when_agent_has_no_servers(self):
        agent = {"id": "a1", "name": "designer", "skills": []}
        with mock.patch.object(al, "_harness_request", self._fake({"designer": agent})):
            spec = al.resolve_agent_from_harness("designer", "http://x", self.tmp)
        self.assertIsNone(spec.mcp_path)

    def test_unknown_agent_raises(self):
        with mock.patch.object(al, "_harness_request", self._fake({})):
            with self.assertRaises(al.UnknownAgent):
                al.resolve_agent_from_harness("nope", "http://x", self.tmp)


class DispatchHarnessTest(unittest.TestCase):
    def setUp(self):
        self.calls = []
        self.out = io.StringIO()
        self.tmp = tempfile.mkdtemp()

    def tearDown(self):
        shutil.rmtree(self.tmp, ignore_errors=True)

    def _exec(self, argv, env):
        self.calls.append((argv, env))

    def dispatch(self, args):
        return al.dispatch(
            args, MANIFEST, agents_root=self.tmp, cwd="/tmp/outside",
            exec_fn=self._exec, out=self.out,
        )

    def test_harness_flag_execs_with_materialised_add_dir(self):
        agent = {"id": "a1", "name": "go-developer",
                  "skills": [{"name": "tdd", "files": [{"path": "SKILL.md", "content": "x"}]}]}

        def fake(base_url, path):
            if path == "/list_agents":
                return {"agents": [{"id": "a1", "name": "go-developer"}]}
            return {"agent": agent}

        with mock.patch.object(al, "_harness_request", fake):
            rc = self.dispatch(["go-developer", "--harness"])
        self.assertEqual(rc, 0)
        argv, _ = self.calls[0]
        self.assertIn(os.path.join(self.tmp, ".generated-harness", "go-developer"), argv)

    def test_harness_flag_skips_local_agents_json_sync(self):
        sync_calls = []

        def fake(base_url, path):
            if path == "/list_agents":
                return {"agents": [{"id": "a1", "name": "go-developer"}]}
            return {"agent": {"id": "a1", "name": "go-developer", "skills": []}}

        with mock.patch.object(al, "_harness_request", fake):
            al.dispatch(
                ["go-developer", "--harness"], MANIFEST, agents_root=self.tmp,
                cwd="/tmp/outside", exec_fn=self._exec, out=self.out,
                sync_fn=lambda *a, **k: sync_calls.append((a, k)),
            )
        self.assertEqual(sync_calls, [])

    def test_unknown_agent_in_harness_errors_without_exec(self):
        with mock.patch.object(al, "_harness_request",
                                lambda base_url, path: {"agents": []}):
            rc = self.dispatch(["nope", "--harness"])
        self.assertEqual(rc, 2)
        self.assertEqual(self.calls, [])

    def test_harness_unreachable_errors_clearly_without_exec(self):
        def fake(base_url, path):
            raise al.HarnessUnreachable("GET /list_agents -> connection refused")

        with mock.patch.object(al, "_harness_request", fake):
            rc = self.dispatch(["go-developer", "--harness"])
        self.assertEqual(rc, 1)
        self.assertIn("connection refused", self.out.getvalue())
        self.assertEqual(self.calls, [])

    def test_harness_url_flag_implies_harness_and_overrides_base_url(self):
        seen_urls = []

        def fake(base_url, path):
            seen_urls.append(base_url)
            if path == "/list_agents":
                return {"agents": [{"id": "a1", "name": "go-developer"}]}
            return {"agent": {"id": "a1", "name": "go-developer", "skills": []}}

        with mock.patch.object(al, "_harness_request", fake):
            self.dispatch(["go-developer", "--harness-url", "http://elsewhere:9000"])
        self.assertTrue(all(u == "http://elsewhere:9000" for u in seen_urls))


class DispatchNameFlagTest(unittest.TestCase):
    """agent launches under <projects_root>/<project>/<repo>/... get a
    predictable --name <agent-key>-<project>-<repo> so other sessions can
    address them via ListAgents/SendMessage."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = pathlib.Path(os.path.realpath(self.tmp.name))
        self.projetos = self.root / "projetos"
        self.backend = self.projetos / "subq" / "backend"
        self.backend.mkdir(parents=True)
        self.manifest = {
            **MANIFEST,
            "settings": {**MANIFEST["settings"], "projects_root": str(self.projetos)},
        }
        self.calls = []
        self.out = io.StringIO()

    def tearDown(self):
        self.tmp.cleanup()

    def _exec(self, argv, env):
        self.calls.append((argv, env))

    def dispatch(self, args):
        return al.dispatch(
            args, self.manifest, agents_root="/A", cwd=str(self.backend),
            exec_fn=self._exec, out=self.out,
        )

    def test_name_flag_set_from_project_repo_layout(self):
        self.dispatch(["backend"])
        argv, _ = self.calls[0]
        i = argv.index("--name")
        self.assertEqual(argv[i + 1], "backend-developer-subq-backend")

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
    def test_every_row_key_resolves_to_an_agent(self):
        import agent_picker
        for key, _label in agent_picker._rows(MANIFEST):
            al.resolve_agent(key, MANIFEST, agents_root="/A")  # must not raise


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


class DispatchPlanTest(unittest.TestCase):
    def setUp(self):
        self.calls = []
        self.out = io.StringIO()

    def _exec(self, argv, env):
        self.calls.append((argv, env))

    def test_plan_flag_appends_prompt_as_last_arg(self):
        rc = al.dispatch(
            ["backend", "--plan", "/p/x.md"], MANIFEST, agents_root="/A",
            cwd="/tmp/outside", exec_fn=self._exec, out=self.out,
        )
        self.assertEqual(rc, 0)
        argv, _ = self.calls[0]
        self.assertIn("/p/x.md", argv[-1])

    def test_dry_run_with_plan_prints_prompt_without_exec(self):
        rc = al.dispatch(
            ["--dry-run", "backend", "--plan", "/p/x.md"], MANIFEST, agents_root="/A",
            cwd="/tmp/outside", exec_fn=self._exec, out=self.out,
        )
        self.assertEqual(rc, 0)
        self.assertEqual(self.calls, [])
        self.assertIn("/p/x.md", self.out.getvalue())


if __name__ == "__main__":
    unittest.main()


class ResolveSkillsTest(unittest.TestCase):
    BUNDLES = {
        "base": ["a", "b"],
        "wide": ["@base", "c"],
        "loop": ["@loop"],
        "ping": ["@pong"],
        "pong": ["@ping"],
    }

    def test_plain_list_passes_through(self):
        self.assertEqual(al.resolve_skills(["x", "y"], self.BUNDLES), ["x", "y"])

    def test_bundle_expands_in_place(self):
        self.assertEqual(al.resolve_skills(["x", "@base"], self.BUNDLES),
                         ["x", "a", "b"])

    def test_bundle_may_reference_a_bundle(self):
        self.assertEqual(al.resolve_skills(["@wide"], self.BUNDLES), ["a", "b", "c"])

    def test_overlap_dedupes_keeping_first_seen(self):
        self.assertEqual(al.resolve_skills(["b", "@base"], self.BUNDLES), ["b", "a"])

    def test_unknown_bundle_raises(self):
        with self.assertRaises(al.UnknownBundle):
            al.resolve_skills(["@nope"], self.BUNDLES)

    def test_self_reference_raises_cycle(self):
        with self.assertRaises(al.BundleCycle):
            al.resolve_skills(["@loop"], self.BUNDLES)

    def test_mutual_reference_raises_cycle(self):
        with self.assertRaises(al.BundleCycle):
            al.resolve_skills(["@ping"], self.BUNDLES)

    def test_empty_and_none(self):
        self.assertEqual(al.resolve_skills([], self.BUNDLES), [])
        self.assertEqual(al.resolve_skills(None, self.BUNDLES), [])


class ValidateManifestTest(unittest.TestCase):
    def test_clean_manifest_has_no_errors(self):
        self.assertEqual(al.validate_manifest(MANIFEST, ALL_SKILLS), [])

    def test_unknown_skill_names_skill_and_owner(self):
        m = {"bundles": {}, "agents": {"a": {"skills": ["ghost"]}}}
        errs = al.validate_manifest(m, ALL_SKILLS)
        self.assertEqual(len(errs), 1)
        self.assertIn("ghost", errs[0])
        self.assertIn("agent a", errs[0])

    def test_unknown_skill_inside_a_bundle_is_attributed_to_the_bundle(self):
        m = {"bundles": {"b": ["ghost"]}, "agents": {}}
        self.assertIn("bundle @b", al.validate_manifest(m, ALL_SKILLS)[0])

    def test_unknown_bundle_reported(self):
        m = {"bundles": {}, "agents": {"a": {"skills": ["@nope"]}}}
        self.assertIn("unknown bundle @nope", al.validate_manifest(m, ALL_SKILLS)[0])

    def test_bundle_cycle_reported(self):
        m = {"bundles": {"l": ["@l"]}, "agents": {}}
        self.assertIn("cycle", al.validate_manifest(m, ALL_SKILLS)[0])

    def test_alias_colliding_with_another_agent_key(self):
        m = {"bundles": {}, "agents": {"a": {"skills": []},
                                       "b": {"skills": [], "aliases": ["a"]}}}
        self.assertTrue(any("collides" in e for e in al.validate_manifest(m, set())))

    def test_reports_every_problem_not_just_the_first(self):
        m = {"bundles": {}, "agents": {"a": {"skills": ["g1", "g2"]}}}
        self.assertEqual(len(al.validate_manifest(m, ALL_SKILLS)), 2)


class PlanSyncTest(unittest.TestCase):
    def test_maps_every_agent_to_its_resolved_skills(self):
        plan = al.plan_sync(MANIFEST)
        self.assertEqual(set(plan), set(MANIFEST["agents"]))
        self.assertEqual(plan["go-developer"], ["git-commit", "tdd"])

    def test_bundle_only_name_gets_no_entry(self):
        # "developer" is a bundle, not an agent — it must not get a tree
        self.assertNotIn("developer", al.plan_sync(MANIFEST))


class _TmpAgents(unittest.TestCase):
    """Real symlinks in a throwaway agents_root."""

    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.root = os.path.join(self.tmp, "agents")
        for s in ALL_SKILLS:
            _touch(os.path.join(self.root, "skills", s, "SKILL.md"))

    def tearDown(self):
        shutil.rmtree(self.tmp, ignore_errors=True)

    def gen(self, key):
        return os.path.join(self.root, ".generated", key, ".claude", "skills")


class SyncGeneratedTest(_TmpAgents):
    def test_links_point_at_the_central_skills_dir(self):
        al.sync_agent("go", ["tdd"], self.root)
        self.assertEqual(os.readlink(os.path.join(self.gen("go"), "tdd")),
                         "../../../../skills/tdd")

    def test_link_actually_resolves_to_the_skill(self):
        al.sync_agent("go", ["tdd"], self.root)
        p = os.path.join(self.gen("go"), "tdd", "SKILL.md")
        self.assertTrue(os.path.isfile(p), "symlink must resolve to the real skill")

    def test_second_run_changes_nothing(self):
        al.sync_agent("go", ["tdd", "git-commit"], self.root)
        self.assertEqual(al.sync_agent("go", ["tdd", "git-commit"], self.root), [])

    def test_stale_link_is_pruned(self):
        al.sync_agent("go", ["tdd", "git-commit"], self.root)
        al.sync_agent("go", ["tdd"], self.root)
        self.assertEqual(os.listdir(self.gen("go")), ["tdd"])

    def test_mistargeted_link_is_repaired(self):
        al.sync_agent("go", ["tdd"], self.root)
        p = os.path.join(self.gen("go"), "tdd")
        os.unlink(p)
        os.symlink("../../../../skills/WRONG", p)
        al.sync_agent("go", ["tdd"], self.root)
        self.assertEqual(os.readlink(p), "../../../../skills/tdd")

    def test_dangling_link_is_repaired(self):
        d = self.gen("go")
        os.makedirs(d)
        os.symlink("../../../../skills/gone", os.path.join(d, "tdd"))
        al.sync_agent("go", ["tdd"], self.root)
        self.assertTrue(os.path.isfile(os.path.join(d, "tdd", "SKILL.md")))

    def test_removed_agent_tree_is_pruned(self):
        al.sync_generated({"go": ["tdd"], "old": ["tdd"]}, self.root)
        al.sync_generated({"go": ["tdd"]}, self.root)
        self.assertEqual(os.listdir(os.path.join(self.root, ".generated")), ["go"])

    def test_subagents_mirrored_only_when_source_exists(self):
        al.sync_agent("go", [], self.root)
        self.assertFalse(os.path.exists(
            os.path.join(self.root, ".generated", "go", ".claude", "agents")))
        _touch(os.path.join(self.root, "go", ".claude", "agents", "rev.md"))
        al.sync_agent("go", [], self.root)
        p = os.path.join(self.root, ".generated", "go", ".claude", "agents", "rev.md")
        self.assertTrue(os.path.isfile(p))

    def test_subagent_link_honours_dir_override(self):
        _touch(os.path.join(self.root, "real-dir", ".claude", "agents", "rev.md"))
        al.sync_agent("key", [], self.root, own_dir="real-dir")
        p = os.path.join(self.root, ".generated", "key", ".claude", "agents", "rev.md")
        self.assertTrue(os.path.isfile(p))

    def test_validation_error_writes_nothing(self):
        m = {"settings": {}, "bundles": {}, "agents": {"a": {"skills": ["ghost"]}}}
        rc = al.dispatch(["--sync"], m, self.root, "/cwd",
                         exec_fn=None, out=io.StringIO())
        self.assertEqual(rc, 2)
        self.assertFalse(os.path.exists(os.path.join(self.root, ".generated")))


class StalenessTest(_TmpAgents):
    def test_absent_dir_is_stale(self):
        self.assertTrue(al.generated_is_stale("go", ["tdd"], self.root))

    def test_fresh_after_sync(self):
        al.sync_agent("go", ["tdd"], self.root)
        self.assertFalse(al.generated_is_stale("go", ["tdd"], self.root))

    def test_added_skill_makes_it_stale(self):
        al.sync_agent("go", ["tdd"], self.root)
        self.assertTrue(al.generated_is_stale("go", ["tdd", "git-commit"], self.root))


class DispatchSyncTest(_TmpAgents):
    def _manifest(self):
        return {"settings": {}, "bundles": {"b": ["tdd"]},
                "agents": {"go": {"skills": ["@b", "git-commit"]}}}

    def test_sync_returns_zero_and_does_not_exec(self):
        calls = []
        rc = al.dispatch(["--sync"], self._manifest(), self.root, "/cwd",
                         exec_fn=lambda *a: calls.append(a), out=io.StringIO())
        self.assertEqual(rc, 0)
        self.assertEqual(calls, [])
        self.assertEqual(sorted(os.listdir(self.gen("go"))), ["git-commit", "tdd"])

    def test_sync_check_reports_drift_without_writing(self):
        out = io.StringIO()
        rc = al.dispatch(["--sync", "--check"], self._manifest(), self.root, "/cwd",
                         exec_fn=None, out=out)
        self.assertEqual(rc, 1)
        self.assertIn("stale: go", out.getvalue())
        self.assertFalse(os.path.exists(os.path.join(self.root, ".generated")))

    def test_sync_check_clean_after_sync(self):
        m = self._manifest()
        al.dispatch(["--sync"], m, self.root, "/cwd", exec_fn=None, out=io.StringIO())
        out = io.StringIO()
        rc = al.dispatch(["--sync", "--check"], m, self.root, "/cwd",
                         exec_fn=None, out=out)
        self.assertEqual((rc, "in sync" in out.getvalue()), (0, True))

    def test_validate_flag_returns_two_on_bad_manifest(self):
        m = {"settings": {}, "bundles": {}, "agents": {"a": {"skills": ["ghost"]}}}
        self.assertEqual(
            al.dispatch(["--validate"], m, self.root, "/cwd",
                        exec_fn=None, out=io.StringIO()), 2)

    def test_stale_agent_is_synced_before_exec(self):
        order = []
        m = self._manifest()
        m["settings"]["auto_sync"] = True
        al.dispatch(["go"], m, self.root, "/cwd",
                    exec_fn=lambda *a: order.append("exec"), out=io.StringIO(),
                    sync_fn=lambda *a: order.append("sync"))
        self.assertEqual(order, ["sync", "exec"])

    def test_auto_sync_skipped_when_disabled(self):
        order = []
        m = self._manifest()
        m["settings"]["auto_sync"] = False
        al.dispatch(["go"], m, self.root, "/cwd",
                    exec_fn=lambda *a: order.append("exec"), out=io.StringIO(),
                    sync_fn=lambda *a: order.append("sync"))
        self.assertEqual(order, ["exec"])


class ActivateTest(_TmpAgents):
    def setUp(self):
        super().setUp()
        self.home = os.path.join(self.tmp, "home-skills")
        self.skills_root = os.path.join(self.root, "skills")

    def test_activates_exactly_the_agents_skills(self):
        names = al.activation_names("backend-developer", MANIFEST, self.root)
        al.activate_global(names, self.skills_root, self.home)
        self.assertEqual(sorted(os.listdir(self.home)),
                         ["api-design", "git-commit", "tdd", "tree-shaking"])

    def test_all_links_every_skill_on_disk(self):
        names = al.activation_names("all", MANIFEST, self.root)
        al.activate_global(names, self.skills_root, self.home)
        self.assertEqual(set(os.listdir(self.home)), ALL_SKILLS)

    def test_none_clears_symlinks(self):
        al.activate_global(al.activation_names("all", MANIFEST, self.root),
                           self.skills_root, self.home)
        al.activate_global([], self.skills_root, self.home)
        self.assertEqual(os.listdir(self.home), [])

    def test_a_real_directory_is_never_removed(self):
        os.makedirs(os.path.join(self.home, "mine"))
        al.activate_global(["tdd"], self.skills_root, self.home)
        self.assertIn("mine", os.listdir(self.home))

    def test_links_resolve_to_the_central_skill(self):
        al.activate_global(["tdd"], self.skills_root, self.home)
        self.assertTrue(os.path.isfile(os.path.join(self.home, "tdd", "SKILL.md")))


class RealManifestTest(unittest.TestCase):
    """The real agents.json must name only skills that exist on disk."""

    ROOT = os.path.dirname(os.path.abspath(__file__))

    @unittest.skipUnless(os.path.isdir(os.path.join(ROOT, "skills")),
                         "agents/skills/ not present")
    def test_shipped_manifest_is_valid(self):
        m = al.load_manifest(os.path.join(self.ROOT, "agents.json"))
        self.assertEqual(al.validate_manifest(m, al.known_skills(self.ROOT)), [])

    @unittest.skipUnless(os.path.isdir(os.path.join(ROOT, "skills")),
                         "agents/skills/ not present")
    def test_shipped_dump_skill_plan_matches_resolved_skills(self):
        m = al.load_manifest(os.path.join(self.ROOT, "agents.json"))
        dump = al.dump_skill_plan(m, self.ROOT)
        self.assertEqual(set(dump), set(m["agents"]))
        spec = al.resolve_agent("backend-developer", m, self.ROOT)
        self.assertEqual(dump["backend-developer"]["skills"], spec.skills)


class DumpSkillPlanTest(unittest.TestCase):
    def test_resolves_skills_per_agent_from_bundles(self):
        dump = al.dump_skill_plan(MANIFEST, agents_root="/A")
        self.assertEqual(
            dump["backend-developer"]["skills"],
            ["api-design", "git-commit", "tdd", "tree-shaking"],
        )

    def test_mcp_path_omitted_when_file_does_not_exist(self):
        dump = al.dump_skill_plan(MANIFEST, agents_root="/A")
        self.assertIsNone(dump["backend-developer"]["mcp_path"])

    def test_mcp_path_omitted_when_agent_has_no_mcp(self):
        dump = al.dump_skill_plan(MANIFEST, agents_root="/A")
        self.assertIsNone(dump["database-manager"]["mcp_path"])

    def test_description_defaults_to_empty_string(self):
        dump = al.dump_skill_plan(MANIFEST, agents_root="/A")
        self.assertEqual(dump["backend-developer"]["description"], "")


class DumpSkillPlanWithRealMCPFileTest(_TmpAgents):
    def test_mcp_path_included_when_file_exists(self):
        mcp_path = os.path.join(self.root, "backend-developer", ".mcp.json")
        _touch(mcp_path)
        dump = al.dump_skill_plan(MANIFEST, self.root)
        self.assertEqual(dump["backend-developer"]["mcp_path"], mcp_path)

    def test_mcp_path_resolved_through_another_agents_dir(self):
        mcp_path = os.path.join(self.root, "backend-developer", ".mcp.json")
        _touch(mcp_path)
        dump = al.dump_skill_plan(MANIFEST, self.root)
        self.assertEqual(dump["node-developer"]["mcp_path"], mcp_path)


class AgentSummariesTest(unittest.TestCase):
    def test_shows_a_skill_count(self):
        lines = al._agent_summaries(MANIFEST)
        self.assertTrue(any("4 skills" in l for l in lines))

    def test_zero_skill_agent_does_not_crash(self):
        m = {"bundles": {}, "agents": {"empty": {"skills": []}}}
        self.assertIn("0 skills", al._agent_summaries(m)[0])

    def test_singular_for_one_skill(self):
        m = {"bundles": {}, "agents": {"one": {"skills": ["x"]}}}
        self.assertIn("1 skill ", al._agent_summaries(m)[0] + " ")
