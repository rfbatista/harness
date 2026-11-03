import { StructuredError } from "../../../shared/domain/errors.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { agentsSettingsHref, catalogChangeTouches, notAssignedRefusal, notEnabledRefusal, notOfferedRows, pickerCount, pickerHint, repositoryAgentsHref } from "./agentPicker.js";

file("sessions/domain/agentPicker");

test("the count says how many agents the picker offers", () => {
  assert.equal(pickerCount(4), "4 enabled");
  assert.equal(pickerCount(0), "");
});

test("a Selected-agents hint names the project and the catalog size", () => {
  assert.equal(pickerHint({ count: 4, total: 10, mode: "selected", project: "atlas-api" }), "Showing the 4 agents enabled in atlas-api of 10 in the catalog.");
  assert.equal(pickerHint({ count: 1, total: 10, mode: "selected", project: "atlas-api" }), "Showing the 1 agent enabled in atlas-api of 10 in the catalog.");
});

test("an All-agents hint says new agents appear automatically", () => {
  const hint = pickerHint({ count: 10, total: 10, mode: "all", project: "atlas-api" });
  assert.ok(hint.includes("All agents are enabled in atlas-api"), hint);
  assert.ok(hint.includes("new agents appear automatically"), hint);
});

test("the settings link points at the project's Enabled agents section", () => {
  assert.equal(agentsSettingsHref("p 1"), "/projects/p%201/settings#settings-agents-enabled");
});

test("a catalog event touches the picker when it names the project, names no path, or names the catalog itself", () => {
  const ev = (...paths) => ({ agent_catalog_changed: { paths } });
  assert.equal(catalogChangeTouches(ev("projects/p1"), "p1"), true);
  assert.equal(catalogChangeTouches(ev("projects/p2"), "p1"), false, "another project's list");
  assert.equal(catalogChangeTouches(ev("agents.json"), "p1"), true, "an agent was added, renamed or deleted");
  assert.equal(catalogChangeTouches(ev("skills/tdd"), "p1"), true);
  assert.equal(catalogChangeTouches(ev(), "p1"), true);
  assert.equal(catalogChangeTouches({ project: { id: "p1" } }, "p1"), false, "other feed messages");
  assert.equal(catalogChangeTouches(null, "p1"), false);
});

test("AGENT_NOT_ENABLED becomes the banner: the sentence, the code, the way out and the link", () => {
  const err = new StructuredError("AGENT_NOT_ENABLED", "AGENT_NOT_ENABLED: agent ux-designer is not enabled in project atlas-api", 409);
  const refusal = notEnabledRefusal(err, { agent: "ux-designer", project: "atlas-api", projectId: "p1" });
  assert.equal(refusal.message, "The session was not started: ux-designer is not enabled in atlas-api.");
  assert.equal(refusal.code, "AGENT_NOT_ENABLED");
  assert.ok(refusal.explanation.includes("Your prompt is kept"), refusal.explanation);
  assert.equal(refusal.linkText, "Open atlas-api's Agents settings");
  assert.equal(refusal.href, "/projects/p1/settings#settings-agents-enabled");
  assert.equal(notEnabledRefusal(new Error("boom"), { agent: "a", project: "p", projectId: "p1" }), null, "any other error is not this refusal");
});

test("with agents unassigned from the repository the hint and the count name the assigned ones", () => {
  assert.equal(
    pickerHint({ count: 3, total: 6, mode: "selected", project: "atlas", repository: "atlas-api", unassigned: 2 }),
    "Showing the 3 agents assigned to atlas-api, of 5 enabled in atlas. 2 are unassigned here.",
  );
  assert.equal(pickerHint({ count: 1, total: 6, mode: "all", project: "atlas", repository: "atlas-api", unassigned: 1 }), "Showing the 1 agent assigned to atlas-api, of 2 enabled in atlas. 1 is unassigned here.");
  assert.ok(pickerHint({ count: 6, total: 6, mode: "all", project: "atlas", repository: "atlas-api", unassigned: 0 }).includes("All agents are enabled in atlas"), "nothing unassigned: the project's hint stands");
  assert.equal(pickerCount(3, 2), "3 of 5 enabled");
  assert.equal(pickerCount(3), "3 enabled");
});

test("the missing agents say why and where to fix it", () => {
  const rows = notOfferedRows([{ name: "reviewer", reason: "unassigned" }, { name: "qa", reason: "not_enabled" }], { repository: "atlas-api", repositoryId: "r1", project: "atlas", projectId: "p1" });
  assert.deepEqual(rows, [
    { name: "reviewer", reason: "unassigned from atlas-api", linkText: "Manage agents in atlas-api", href: "/projects/p1/repositories/r1/architecture/agents" },
    { name: "qa", reason: "not enabled in atlas", linkText: "Enable in atlas", href: "/projects/p1/settings#settings-agents-enabled" },
  ]);
  assert.equal(repositoryAgentsHref("p 1", "r/1"), "/projects/p%201/repositories/r%2F1/architecture/agents");
});

test("a repository agents event touches the picker of its project", () => {
  const ev = (project_id) => ({ repository_agents_changed: { project_id, repository_id: "r1", agents: ["reviewer"] } });
  assert.equal(catalogChangeTouches(ev("p1"), "p1"), true);
  assert.equal(catalogChangeTouches(ev("p2"), "p1"), false);
  assert.equal(catalogChangeTouches({ repository_agents_changed: {} }, "p1"), true, "an event that names no project may be ours");
});

test("AGENT_NOT_ASSIGNED becomes a banner laid out like AGENT_NOT_ENABLED, with the link to the Agents tab", () => {
  const err = new StructuredError("AGENT_NOT_ASSIGNED", "AGENT_NOT_ASSIGNED: reviewer is not assigned to atlas-api.", 409, { agent: "reviewer", repository_id: "r1", repository_name: "atlas-api", assigned: ["backend"] });
  const refusal = notAssignedRefusal(err, { agent: "reviewer", repository: "", repositoryId: "", projectId: "p1" });
  assert.equal(refusal.message, "The session was not started: reviewer is not assigned to atlas-api.");
  assert.equal(refusal.code, "AGENT_NOT_ASSIGNED");
  assert.ok(refusal.explanation.includes("Your prompt is kept"), refusal.explanation);
  assert.equal(refusal.linkText, "Open atlas-api's Agents settings");
  assert.equal(refusal.href, "/projects/p1/repositories/r1/architecture/agents");
  assert.equal(notAssignedRefusal(new StructuredError("AGENT_NOT_ENABLED", "x", 409), { agent: "a", repository: "r", repositoryId: "r1", projectId: "p1" }), null);
});
