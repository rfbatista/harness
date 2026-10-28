import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryCatalog } from "../../infrastructure/memory-gateway.js";
import { makeWorld } from "../../testing/fixtures.js";
import { agentsPage } from "./agentsPage.js";

file("agents/presentation/components/enabledInPage");

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// harness (p1) uses All agents; atlas-api (p2) is Selected and lists backend only.
async function open(path) {
  const world = makeWorld();
  world.projects.p2 = [];
  world.projectNames.p2 = "atlas-api";
  world.projectAgents = { p2: { mode: "selected", agents: ["backend"] } };
  const { gateway, server } = memoryCatalog(world);
  const location = { pathname: path, search: "", hash: "" };
  const history = { pushState: (_s, _t, url) => { location.pathname = url.split("?")[0]; } };
  const seed = await server.api.get("/get_agent_catalog");
  const m = mount(agentsPage({ gateway, history, location }), { el: seededElement(seed) });
  await m.instance.init();
  await sleep(10); // the project list the open agent asked for
  return { ...m, server, page: m.instance };
}
const tick = (id) => ({ currentTarget: { dataset: { id } } });
const saves = (server) => server.requests.filter(([m, p]) => m === "POST" && p === "/save_catalog_agent" && !p.dry_run);
const menuEvent = () => ({ detail: { action: "delete" }, currentTarget: { querySelector: () => null } });

test("an agent's detail shows where it is enabled, and the project list is read only once an agent is open", async () => {
  const { page, server } = await open("/agents/ux-designer");
  assert.equal(server.requests.filter(([, p]) => p === "/list_projects").length, 1);
  assert.equal(page.detail.enabled.heading, "Enabled in · 1 of 2 projects");
  assert.deepEqual(page.detail.enabled.rows.map((r) => [r.name, r.badge]), [["harness", "All agents"]]);
  assert.deepEqual(page.detail.enabled.notListed.map((p) => p.name), ["atlas-api"]);
  const list = await open("/agents");
  assert.equal(list.server.requests.filter(([, p]) => p === "/list_projects").length, 0, "the library does not read the projects");
});

test("the editor's Projects section ticks the Selected projects, fixes the All ones, and counts in the contents list", async () => {
  const { page } = await open("/agents/backend/edit");
  assert.deepEqual(page.projectRowsChoice.map((r) => [r.name, r.fixed, r.checked]), [["atlas-api", false, true], ["harness", true, true]]);
  assert.equal(page.projectsSummary, "Enabled in 2 of 2 projects · 1 by All agents, 1 Selected-agents list");
  assert.equal(page.toc.find((s) => s.id === "projects").count, "2");
  page.toggleProject(tick("p2"));
  assert.equal(page.projectRowsChoice[0].checked, false);
  assert.equal(page.agentChangeCount, 1, "the section counts as a change");
  assert.equal(page.agentTouched, true);
});

test("a new agent starts with nothing ticked", async () => {
  const { page } = await open("/agents/new");
  assert.deepEqual(page.projectRowsChoice.filter((r) => r.checked && !r.fixed), []);
  assert.equal(page.agentChangeCount, 0);
});

test("the save sends `projects` only when the section was touched", async () => {
  const touched = await open("/agents/ux-designer/edit");
  touched.page.toggleProject(tick("p2"));
  await touched.page.saveEditor();
  const [, , body] = saves(touched.server)[0];
  assert.deepEqual(body.projects, ["p2"]);
  assert.deepEqual((await touched.server.api.get("/get_project_agents", { project_id: "p2" })).agents, ["backend", "ux-designer"]);

  const plain = await open("/agents/ux-designer/edit");
  plain.page.agentDraft.description = "Designs screens well.";
  plain.page.agentChanged();
  await plain.page.saveEditor();
  const [, , sent] = saves(plain.server)[0];
  assert.equal("projects" in sent, false, "untouched: the lists stay as they are");
});

test("a refusal about `projects` shows at the Projects section and links to it by aria-describedby", async () => {
  const { page } = await open("/agents/ux-designer/edit");
  page.agentDraft.projects = ["p1"];
  page.agentChanged();
  await page.saveEditor();
  assert.ok(page.agentProjectsInvalid, "the section is invalid");
  assert.ok(page.agentErrors.projects.includes("harness"), page.agentErrors.projects);
  assert.equal(page.agentProjectsDescribedBy, "agent-projects-hint agent-projects-error");
  assert.equal(page.agentErrors.form, "", "not shown as a form-wide error");
});

test("Review change shows the project list the change sets", async () => {
  const { page } = await open("/agents/ux-designer/edit");
  page.toggleProject(tick("p2"));
  await sleep(450);
  assert.deepEqual(page.reviewProjects.map((f) => [f.path, f.label]), [["atlas-api · enabled agents", "+1 −0"]]);
  assert.deepEqual(page.reviewProjects[0].lines.map((l) => [l.kind, l.text]), [["add", "ux-designer"], ["same", "backend"]]);
});

test("the delete dialog lists the Selected projects, says when one is left empty, and follows a change on the live feed", async () => {
  const { page, server } = await open("/agents/backend");
  await page.menuSelect(menuEvent());
  assert.deepEqual(page.del.groups[0].items.map((i) => i.name), ["atlas-api"]);
  assert.ok(/atlas-api is left with no agent enabled/.test(page.del.groups[0].note));
  assert.ok(/harness/.test(page.del.total));
  await server.api.post("/set_project_agents", { project_id: "p2", mode: "selected", agents: ["backend", "ux-designer"] });
  await sleep(450);
  assert.equal(page.del.groups[0].note, "", "another agent keeps the list from emptying");
});

test("a project's list changing on the feed refreshes the detail", async () => {
  const { page, server } = await open("/agents/ux-designer");
  assert.equal(page.detail.enabled.heading, "Enabled in · 1 of 2 projects");
  await server.api.post("/set_project_agents", { project_id: "p2", mode: "all", agents: [] });
  await sleep(450);
  assert.equal(page.detail.enabled.heading, "Enabled in · 2 of 2 projects");
});
