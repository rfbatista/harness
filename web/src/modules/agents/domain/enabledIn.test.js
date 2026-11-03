import { assert, file, test } from "../../../shared/testing/test.js";
import { allProjects, enabledInView, listedIn, projectChangeFiles, projectChoices, projectModes, projectsChanged, projectsToRead, projectsToSend, repositoriesOpen, toggleProject } from "./enabledIn.js";

file("agents/domain/enabledIn");

const agent = (name, enabledIn) => ({ name, enabledIn });
// harness and notes use All agents; atlas lists ux-designer; orbit and lab list nothing of it.
const projects = [{ id: "p-orbit", name: "orbit-app" }, { id: "p-harness", name: "harness" }, { id: "p-atlas", name: "atlas-api" }, { id: "p-lab", name: "lab" }, { id: "p-notes", name: "notes-site" }];
const all = (id, name) => ({ id, name, mode: "all" });
const sel = (id, name) => ({ id, name, mode: "selected" });
const catalog = {
  agents: [
    agent("ux-designer", [all("p-harness", "harness"), all("p-notes", "notes-site"), sel("p-atlas", "atlas-api")]),
    agent("backend", [all("p-harness", "harness"), all("p-notes", "notes-site")]),
    agent("lonely", []),
  ],
};

test("Enabled in counts the projects the agent is enabled in against every project, All rows first by name", () => {
  const v = enabledInView(catalog, catalog.agents[0], projects);
  assert.equal(v.heading, "Enabled in · 3 of 5 projects");
  assert.deepEqual(v.rows.map((r) => [r.name, r.badge, r.auto]), [["atlas-api", "Selected agents", false], ["harness", "All agents", true], ["notes-site", "All agents", true]]);
  assert.equal(v.rows[1].href, "/projects/p-harness/settings");
  assert.ok(/automatically/.test(v.rows[1].note), "an All row says it is enabled automatically");
  assert.equal(v.rows[0].note, "On the project’s list");
  assert.equal(v.nowhere, false);
});

test("the hint names the Selected projects that do not list the agent, each with a link to its settings", () => {
  const v = enabledInView(catalog, catalog.agents[0], projects);
  assert.deepEqual(v.notListed.map((p) => [p.name, p.href, p.sep]), [["lab", "/projects/p-lab/settings", " and "], ["orbit-app", "/projects/p-orbit/settings", ""]]);
  assert.equal(v.hasNotListed, true);
  const onlyBackend = { agents: [catalog.agents[1]] };
  const none = enabledInView(onlyBackend, catalog.agents[1], projects.filter((p) => p.id === "p-harness" || p.id === "p-notes"));
  assert.equal(none.hasNotListed, false);
});

test("an agent enabled nowhere is the attention state; with no projects at all it is not", () => {
  const v = enabledInView(catalog, catalog.agents[2], projects);
  assert.equal(v.heading, "Enabled in · 0 of 5 projects");
  assert.equal(v.nowhere, true);
  assert.ok(/No session can start as lonely/.test(v.nowhereText));
  const empty = enabledInView({ agents: [catalog.agents[2]] }, catalog.agents[2], []);
  assert.deepEqual([empty.nowhere, empty.noProjects, empty.heading], [false, true, "Enabled in · 0 of 0 projects"]);
});

test("a project the agent is enabled in counts even when the project list did not carry it, and the list's name wins", () => {
  const list = allProjects(catalog, [{ id: "p-harness", name: "renamed" }]);
  assert.deepEqual(list.map((p) => p.name), ["atlas-api", "notes-site", "renamed"]);
});

test("a project is All when any agent is enabled there unlisted, else Selected; with no agent the server's answer says", () => {
  assert.deepEqual(projectModes(catalog, projects), { "p-atlas": "selected", "p-harness": "all", "p-lab": "selected", "p-notes": "all", "p-orbit": "selected" });
  assert.deepEqual(projectsToRead(catalog, projects), []);
  const bare = { agents: [] };
  assert.deepEqual(projectsToRead(bare, projects.slice(0, 2)), ["p-harness", "p-orbit"]);
  assert.equal(projectModes(bare, projects.slice(0, 2), { "p-harness": "all" })["p-harness"], "all");
});

test("the editor's rows: Selected projects tick from the agent's lists, All projects are fixed and ticked, the summary counts both", () => {
  const draft = { previousName: "ux-designer", projects: null };
  const c = projectChoices(catalog, draft, projects);
  assert.deepEqual(c.rows.map((r) => [r.name, r.fixed, r.checked, r.badge]), [
    ["atlas-api", false, true, "Selected agents"], ["harness", true, true, "All agents"], ["lab", false, false, "Selected agents"], ["notes-site", true, true, "All agents"], ["orbit-app", false, false, "Selected agents"],
  ]);
  assert.equal(c.summary, "Enabled in 3 of 5 projects · 2 by All agents, 1 Selected-agents list");
  assert.equal(c.tocCount, "3");
  assert.equal(c.rows[1].href, "/projects/p-harness/settings");
});

test("a new agent starts with nothing ticked, enabled only in the All projects", () => {
  const c = projectChoices(catalog, { previousName: "", projects: null }, projects);
  assert.deepEqual(c.rows.filter((r) => r.checked && !r.fixed), []);
  assert.equal(c.count, 2);
  assert.equal(projectChoices({ agents: [] }, { previousName: "", projects: null }, []).summary, "No projects yet");
});

test("ticking a project sets the full list; an untouched section sends no `projects`", () => {
  const draft = { previousName: "ux-designer", projects: null };
  assert.equal(projectsToSend(catalog, draft), undefined, "untouched: omitted, so the lists stay as they are");
  assert.equal(projectsChanged(catalog, draft), false);
  const ticked = { ...draft, projects: toggleProject(catalog, draft, "p-lab") };
  assert.deepEqual(ticked.projects, ["p-atlas", "p-lab"]);
  assert.deepEqual(projectsToSend(catalog, ticked), ["p-atlas", "p-lab"]);
  const back = { ...draft, projects: toggleProject(catalog, ticked, "p-lab") };
  assert.equal(projectsToSend(catalog, back), undefined, "put back as it was: nothing to send");
  const none = { ...draft, projects: toggleProject(catalog, draft, "p-atlas") };
  assert.deepEqual(projectsToSend(catalog, none), [], "unticking the last one sends an empty set, which removes the agent from it");
  assert.deepEqual(listedIn(catalog, "ux-designer"), ["p-atlas"]);
  assert.deepEqual(listedIn(catalog, ""), []);
});

test("Review change shows each project whose enabled agents change, with what was added and removed; an MCP-only change is left out", () => {
  const files = projectChangeFiles([
    { id: "p-lab", name: "lab", before: [], after: [], enabledAgentsBefore: ["backend"], enabledAgentsAfter: ["backend", "ux-designer"] },
    { id: "p-atlas", name: "atlas-api", before: ["github"], after: ["github", "linear"], enabledAgentsBefore: [], enabledAgentsAfter: [] },
    { id: "p-orbit", name: "orbit-app", before: [], after: [], enabledAgentsBefore: ["ux-designer"], enabledAgentsAfter: [] },
  ]);
  assert.deepEqual(files.map((f) => [f.path, f.label]), [["lab · enabled agents", "+1 −0"], ["orbit-app · enabled agents", "+0 −1"]]);
  assert.deepEqual(files[0].lines.map((l) => [l.kind, l.text]), [["add", "ux-designer"], ["same", "backend"]]);
  assert.equal(files[1].empty, true);
  assert.ok(/left empty/.test(files[1].emptyNote));
  assert.equal(files[0].emptyNote, "");
});

// ── repositories (ADR 0061) ──────────────────────────────────────────

const repo = (projectId, repositoryId, repositoryName, over = {}) => ({ projectId, repositoryId, repositoryName, unassigned: false, promptSet: false, listVersion: "l1", ...over });
const withRepos = (a, repositories) => ({ ...a, repositories });

test("each project row lists its repositories with the assignment, the prompt and the actions' names", () => {
  const a = withRepos(catalog.agents[0], [
    repo("p-harness", "r-web", "harness-web", { unassigned: true, promptSet: true }),
    repo("p-harness", "r-api", "harness-api"),
    repo("p-atlas", "r-atlas", "atlas-api", { promptSet: true }),
  ]);
  const v = enabledInView({ agents: [a] }, a, projects);
  const harness = v.rows.find((r) => r.name === "harness");
  assert.deepEqual(harness.repositories.map((r) => [r.name, r.badge, r.promptText]), [["harness-api", "Assigned", "Add prompt"], ["harness-web", "Unassigned", "Repository prompt set"]]);
  assert.equal(harness.repositories[1].promptHref, "/projects/p-harness/repositories/r-web/architecture/agents?prompt=ux-designer");
  assert.equal(harness.repositories[1].assignLabel, "Assign ux-designer again in harness-web");
  assert.equal(harness.repositories[0].unassignLabel, "Unassign ux-designer from harness-api");
  assert.equal(harness.repositories[1].listVersion, "l1");
  assert.equal(v.rows.find((r) => r.name === "notes-site").repositories.length, 0);
});

test("a project's repositories collapse when all are assigned and open while one is not, unless the person chose", () => {
  const all = withRepos(catalog.agents[0], [repo("p-harness", "r1", "a"), repo("p-harness", "r2", "b")]);
  const row = enabledInView({ agents: [all] }, all, projects).rows.find((r) => r.name === "harness");
  assert.equal(row.repositoriesSummary, "2 repositories · all assigned");
  assert.equal(repositoriesOpen(row, undefined), false);
  assert.equal(repositoriesOpen(row, true), true);
  const some = withRepos(catalog.agents[0], [repo("p-harness", "r1", "a", { unassigned: true }), repo("p-harness", "r2", "b")]);
  const open = enabledInView({ agents: [some] }, some, projects).rows.find((r) => r.name === "harness");
  assert.equal(open.repositoriesSummary, "2 repositories · 1 unassigned");
  assert.equal(repositoriesOpen(open, undefined), true);
  assert.equal(repositoriesOpen(open, false), false);
  assert.equal(enabledInView({ agents: [some] }, some, projects).rows.find((r) => r.name === "notes-site").hasRepositories, false);
});

test("the Enabled in heading counts the repositories the agent is unassigned in, only in projects that enable it", () => {
  const a = withRepos(catalog.agents[0], [repo("p-harness", "r1", "a", { unassigned: true }), repo("p-atlas", "r2", "b", { unassigned: true }), repo("p-lab", "r3", "c", { unassigned: true })]);
  assert.equal(enabledInView({ agents: [a] }, a, projects).heading, "Enabled in · 3 of 5 projects · Unassigned in 2 repositories");
  const one = withRepos(catalog.agents[0], [repo("p-harness", "r1", "a", { unassigned: true })]);
  assert.equal(enabledInView({ agents: [one] }, one, projects).heading, "Enabled in · 3 of 5 projects · Unassigned in 1 repository");
});
