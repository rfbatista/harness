import { StructuredError } from "../../../shared/domain/errors.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { agentsSettingsHref, catalogChangeTouches, notEnabledRefusal, pickerCount, pickerHint } from "./agentPicker.js";

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
