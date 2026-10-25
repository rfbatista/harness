import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { CHANGED_ON_DISK, describeRefusal, problemBadges, rowBadge } from "./refusals.js";

file("agents/domain/refusals");

const refusal = (code, message, details, status = 400) => new StructuredError(code, message, status, details);

test("INVALID_INPUT shows the server's message at the field it names", () => {
  const d = describeRefusal(refusal("INVALID_INPUT", "description is required", { field: "description" }));
  assert.equal(d.code, "INVALID_INPUT");
  assert.equal(d.message, "description is required");
  assert.equal(d.field, "description");
});

test("INVALID_INPUT without a field is a message for the whole form", () => {
  assert.equal(describeRefusal(refusal("INVALID_INPUT", "bad server")).field, "");
});

test("NAME_TAKEN names the free name the server suggests, on the name field", () => {
  const d = describeRefusal(refusal("NAME_TAKEN", "skill tdd exists", { suggestion: "tdd-2" }, 409));
  assert.equal(d.field, "name");
  assert.equal(d.suggestion, "tdd-2");
  assert.equal(d.message, "That name is taken. Try tdd-2.");
});

test("NAME_TAKEN without a suggestion only says it is taken", () => {
  assert.equal(describeRefusal(refusal("NAME_TAKEN", "x", undefined, 409)).message, "That name is taken.");
});

test("CATALOG_CHANGED carries the versions and the changed files for the conflict banner", () => {
  const details = { current_versions: { "agents.json": "sha256:2" }, changed: [{ path: "agents.json", theirs: "{}" }] };
  const d = describeRefusal(refusal("CATALOG_CHANGED", "changed", details, 409));
  assert.equal(d.message, "A file changed on disk since you opened this. Reload to see it, or overwrite it with your version.");
  assert.deepEqual(d.currentVersions, { "agents.json": "sha256:2" });
  assert.deepEqual(d.changed, [{ path: "agents.json", theirs: "{}" }]);
});

test("CATALOG_CHANGED on an undo says the change cannot be put back", () => {
  const d = describeRefusal(refusal("CATALOG_CHANGED", "changed", undefined, 409), { undo: true });
  assert.equal(d.message, "A file changed since this save, so Undo would lose that change. Nothing was undone.");
});

test("CATALOG_BROKEN keeps the server's message (file, line, column) and says saving is off", () => {
  const d = describeRefusal(refusal("CATALOG_BROKEN", "agents.json: line 12, column 4: unexpected }", undefined, 409));
  assert.equal(d.message, "agents.json: line 12, column 4: unexpected }");
  assert.equal(d.next, "Saving is off until the file is fixed by hand. Then check again.");
});

test("UNKNOWN_REFERENCE lists the names that do not exist", () => {
  const d = describeRefusal(refusal("UNKNOWN_REFERENCE", "x", { names: ["a", "@b"] }));
  assert.equal(d.message, "These do not exist: a, @b.");
  assert.deepEqual(d.names, ["a", "@b"]);
});

test("BUNDLE_CYCLE says the bundles would include each other", () => {
  assert.equal(describeRefusal(refusal("BUNDLE_CYCLE", "x")).message, "Bundles cannot include each other, directly or through another bundle.");
});

test("CHANGE_NOT_UNDOABLE says only the latest change can be undone", () => {
  assert.equal(describeRefusal(refusal("CHANGE_NOT_UNDOABLE", "x", undefined, 409)).message, "That change can no longer be undone: only the latest change since the server started can.");
});

test("each 404 says what is gone and to reload from disk", () => {
  const kinds = { AGENT_NOT_FOUND: "agent", SKILL_NOT_FOUND: "skill", BUNDLE_NOT_FOUND: "bundle", MCP_SERVER_NOT_FOUND: "MCP server", PROJECT_NOT_FOUND: "project" };
  for (const [code, kind] of Object.entries(kinds)) {
    const d = describeRefusal(refusal(code, "gone", undefined, 404));
    assert.equal(d.message, `That ${kind} no longer exists.`);
    assert.equal(d.next, "Reload from disk to see what is there now.");
  }
});

test("an unknown code or a network failure keeps its own message", () => {
  assert.equal(describeRefusal(new StructuredError(Codes.NETWORK, "The harness server is not reachable.")).message, "The harness server is not reachable.");
  assert.equal(describeRefusal(new Error("boom")).code, Codes.UNKNOWN);
});

test("each problem code has its badge text", () => {
  const texts = (problems) => problemBadges(problems).map((b) => b.text);
  assert.deepEqual(texts([{ code: "NO_DESCRIPTION", message: "" }]), ["No description"]);
  assert.deepEqual(texts([{ code: "MISSING_SKILL" }]), ["1 missing skill"]);
  assert.deepEqual(texts([{ code: "MISSING_SKILL" }, { code: "MISSING_SKILL" }, { code: "MISSING_SKILL" }]), ["3 missing skills"]);
  assert.deepEqual(texts([{ code: "UNKNOWN_BUNDLE" }]), ["1 unknown bundle"]);
  assert.deepEqual(texts([{ code: "UNKNOWN_BUNDLE" }, { code: "UNKNOWN_BUNDLE" }]), ["2 unknown bundles"]);
  assert.deepEqual(texts([{ code: "BUNDLE_CYCLE" }]), ["Bundle cycle"]);
  assert.deepEqual(texts([{ code: "UNKNOWN_MCP_SERVER" }]), ["1 unknown MCP server"]);
  assert.deepEqual(texts([{ code: "SECRET_NOT_SET" }]), ["secret not set"]);
  assert.deepEqual(texts([{ code: "SECRET_NOT_SET" }, { code: "SECRET_NOT_SET" }]), ["2 secrets not set"]);
  assert.deepEqual(texts([{ code: "INVALID_SKILL" }]), ["Invalid SKILL.md"]);
});

test("a problem code the page does not know shows the server's message", () => {
  assert.deepEqual(problemBadges([{ code: "NEW_THING", message: "Something new" }]).map((b) => b.text), ["Something new"]);
});

test("a row's badge is the most serious problem; an item without problems has none", () => {
  assert.equal(rowBadge([]), null);
  assert.equal(rowBadge(undefined), null);
  const b = rowBadge([{ code: "NO_DESCRIPTION" }, { code: "MISSING_SKILL" }]);
  assert.equal(b.text, "1 missing skill");
  assert.equal(b.tone, "danger");
  assert.equal(rowBadge([{ code: "NO_DESCRIPTION" }]).tone, "attention");
  assert.equal(rowBadge([{ code: "SECRET_NOT_SET" }]).tone, "attention");
});

test("the badge for a live update that changes the open item", () => {
  assert.equal(CHANGED_ON_DISK.text, "changed on disk");
});
