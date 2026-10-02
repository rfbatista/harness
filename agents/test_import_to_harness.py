"""Tests for import_to_harness.py against a fake harness HTTP API (no network)."""

import os
import json
import tempfile
import unittest
from unittest import mock

import import_to_harness as ih


def _touch(path, content="x"):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        f.write(content)


class FakeHarness:
    """In-memory stand-in for harness's /api/{list,import,create,update,delete}_*
    endpoints, matching just enough of their real request/response shape."""

    def __init__(self):
        self.skills = {}   # id -> {id, name, published_path}
        self.agents = {}   # id -> {id, name, description, skill_ids, mcp_server_ids}
        self.mcp_servers = {}  # name -> {id, name}
        self._next = 0
        self.calls = []

    def _id(self, prefix):
        self._next += 1
        return f"{prefix}{self._next}"

    def __call__(self, base_url, method, path, body=None):
        self.calls.append((method, path, body))
        if (method, path) == ("GET", "/list_skills"):
            return {"skills": list(self.skills.values())}
        if (method, path) == ("GET", "/list_agents"):
            return {"agents": list(self.agents.values())}
        if (method, path) == ("POST", "/import_skill_from_path"):
            name = os.path.basename(body["path"])
            sid = self._id("skill")
            self.skills[sid] = {"id": sid, "name": name, "published_path": None}
            return {"skill": self.skills[sid]}
        if (method, path) == ("POST", "/delete_skill"):
            del self.skills[body["skill_id"]]
            return {}
        if (method, path) == ("POST", "/import_mcp_servers"):
            content = json.loads(body["content"])
            out = []
            for name in content.get("mcpServers", {}):
                existing = self.mcp_servers.get(name)
                sid = existing["id"] if existing else self._id("mcp")
                self.mcp_servers[name] = {"id": sid, "name": name}
                out.append(self.mcp_servers[name])
            return {"mcp_servers": out}
        if (method, path) == ("POST", "/create_agent"):
            aid = self._id("agent")
            self.agents[aid] = {"id": aid, **body}
            return {"agent": self.agents[aid]}
        if (method, path) == ("POST", "/update_agent"):
            aid = body["agent_id"]
            self.agents[aid] = {**self.agents[aid], **body}
            return {"agent": self.agents[aid]}
        raise AssertionError(f"unhandled fake call: {method} {path}")


class ImportSkillsTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        _touch(os.path.join(self.tmp, "alpha", "SKILL.md"))
        _touch(os.path.join(self.tmp, "beta", "SKILL.md"))
        _touch(os.path.join(self.tmp, "not-a-skill", "README.md"))
        self.api = FakeHarness()

    def test_imports_every_skill_dir_with_a_skill_md(self):
        with mock.patch.object(ih, "_request", self.api):
            ids = ih.import_skills("http://x", self.tmp)
        self.assertEqual(set(ids), {"alpha", "beta"})

    def test_skips_directories_without_skill_md(self):
        with mock.patch.object(ih, "_request", self.api):
            ih.import_skills("http://x", self.tmp)
        called_paths = [b["path"] for m, p, b in self.api.calls if p == "/import_skill_from_path"]
        self.assertFalse(any("not-a-skill" in p for p in called_paths))

    def test_rerun_deletes_and_reimports_existing_skill_by_name(self):
        with mock.patch.object(ih, "_request", self.api):
            first = ih.import_skills("http://x", self.tmp)
            second = ih.import_skills("http://x", self.tmp)
        self.assertNotEqual(first["alpha"], second["alpha"])
        deletes = [b["skill_id"] for m, p, b in self.api.calls if p == "/delete_skill"]
        self.assertIn(first["alpha"], deletes)

    def test_warns_when_deleting_a_published_skill(self):
        with mock.patch.object(ih, "_request", self.api):
            ids = ih.import_skills("http://x", self.tmp)
            self.api.skills[ids["alpha"]]["published_path"] = "/pub/alpha"
            buf = __import__("io").StringIO()
            ih.import_skills("http://x", self.tmp, out=buf)
        self.assertIn("alpha", buf.getvalue())
        self.assertIn("published", buf.getvalue())


class ImportAgentsTest(unittest.TestCase):
    def setUp(self):
        self.api = FakeHarness()

    def test_creates_agent_with_resolved_skill_ids(self):
        plan = {"go-developer": {"skills": ["alpha", "beta"], "mcp_path": None, "description": ""}}
        skill_ids = {"alpha": "skill1", "beta": "skill2"}
        with mock.patch.object(ih, "_request", self.api):
            ih.import_agents("http://x", plan, skill_ids)
        agent = next(iter(self.api.agents.values()))
        self.assertEqual(agent["name"], "go-developer")
        self.assertEqual(set(agent["skill_ids"]), {"skill1", "skill2"})

    def test_rerun_updates_existing_agent_instead_of_duplicating(self):
        plan = {"go-developer": {"skills": ["alpha"], "mcp_path": None, "description": ""}}
        skill_ids = {"alpha": "skill1"}
        with mock.patch.object(ih, "_request", self.api):
            ih.import_agents("http://x", plan, skill_ids)
            ih.import_agents("http://x", plan, {"alpha": "skill1-v2"})
        self.assertEqual(len(self.api.agents), 1)
        agent = next(iter(self.api.agents.values()))
        self.assertEqual(agent["skill_ids"], ["skill1-v2"])

    def test_warns_on_unresolved_skill_name(self):
        plan = {"go-developer": {"skills": ["missing"], "mcp_path": None, "description": ""}}
        buf = __import__("io").StringIO()
        with mock.patch.object(ih, "_request", self.api):
            ih.import_agents("http://x", plan, {}, out=buf)
        self.assertIn("missing", buf.getvalue())

    def test_imports_mcp_servers_when_mcp_path_present(self):
        tmp = tempfile.mkdtemp()
        mcp_path = os.path.join(tmp, ".mcp.json")
        _touch(mcp_path, json.dumps({"mcpServers": {"github": {"command": "gh-mcp",
                                                                 "env": {"TOKEN": "${GITHUB_TOKEN}"}}}}))
        plan = {"backend-developer": {"skills": [], "mcp_path": mcp_path, "description": ""}}
        with mock.patch.object(ih, "_request", self.api):
            ih.import_agents("http://x", plan, {})
        agent = next(iter(self.api.agents.values()))
        self.assertEqual(len(agent["mcp_server_ids"]), 1)
        import_calls = [b for m, p, b in self.api.calls if p == "/import_mcp_servers"]
        self.assertIn("${GITHUB_TOKEN}", import_calls[0]["content"])


class RunIntegrationTest(unittest.TestCase):
    def test_end_to_end_against_a_small_manifest(self):
        tmp = tempfile.mkdtemp()
        agents_dir = os.path.join(tmp, "agents")
        _touch(os.path.join(agents_dir, "skills", "tdd", "SKILL.md"))
        manifest = {
            "settings": {},
            "bundles": {},
            "agents": {"go-developer": {"skills": ["tdd"]}},
        }
        with open(os.path.join(agents_dir, "agents.json"), "w") as f:
            json.dump(manifest, f)

        api = FakeHarness()
        with mock.patch.object(ih, "_request", api):
            ih.run("http://x", agents_dir, out=__import__("io").StringIO())

        self.assertEqual(len(api.skills), 1)
        self.assertEqual(len(api.agents), 1)


if __name__ == "__main__":
    unittest.main()
