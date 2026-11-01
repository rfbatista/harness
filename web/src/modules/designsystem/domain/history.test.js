import { assert, file, test } from "../../../shared/testing/test.js";
import { VERSIONS_PAGE, hasMore, mergeVersions, sourceText, summaryText, versionRow } from "./history.js";

file("designsystem/domain/history");

const entry = (version, over = {}) => ({
  version,
  at: "2026-10-01T10:00:00Z",
  by: "person",
  source: "manual",
  proposalId: "",
  message: "",
  summary: { tokens: "+0 ~0 -0", principles: "+0 ~0 -0", components: "+0 ~0 -0" },
  ...over,
});
const now = new Date("2026-10-01T10:30:00Z");

test("a summary names only the parts that changed, in the server's +added ~changed -removed", () => {
  assert.equal(summaryText({ tokens: "+2 ~1 -0", principles: "~1", components: "+0 ~0 -0" }), "tokens +2 ~1 · principles ~1");
  assert.equal(summaryText({ tokens: "+0 ~0 -0", principles: "+0 ~0 -0", components: "+1 ~0 -0" }), "components +1");
});

test("a summary with no change says so", () => {
  assert.equal(summaryText(entry(1).summary), "no change");
  assert.equal(summaryText(undefined), "no change");
});

test("a source is told in words", () => {
  assert.equal(sourceText("manual"), "Edited by hand");
  assert.equal(sourceText("import"), "Imported from a repository");
  assert.equal(sourceText("proposal"), "Agent proposal");
  assert.equal(sourceText("other"), "other");
});

test("a row says who, how long ago, the source, the message and the summary", () => {
  const row = versionRow(entry(4, { message: "Warmer signal", summary: { tokens: "+0 ~1 -0", principles: "+0 ~0 -0", components: "+0 ~0 -0" } }), now);
  assert.equal(row.version, 4);
  assert.equal(row.label, "Version 4");
  assert.equal(row.by, "you");
  assert.equal(row.when, "30m ago");
  assert.equal(row.source, "Edited by hand");
  assert.equal(row.message, "Warmer signal");
  assert.equal(row.hasMessage, true);
  assert.equal(row.summary, "tokens ~1");
});

test("an agent's save names the session, a version without a message has none", () => {
  const row = versionRow(entry(2, { by: "design session 7f3a", source: "proposal" }), now);
  assert.equal(row.by, "design session 7f3a");
  assert.equal(row.hasMessage, false);
});

test("a time that cannot be read leaves the row without one", () => {
  assert.equal(versionRow(entry(1, { at: "" }), now).when, "");
});

test("a fresh first page replaces the head of the list and keeps the older versions already loaded", () => {
  const current = [entry(5), entry(4), entry(3), entry(2)];
  const merged = mergeVersions(current, [entry(7), entry(6), entry(5), entry(4)]);
  assert.deepEqual(merged.map((v) => v.version), [7, 6, 5, 4, 3, 2]);
});

test("an empty fresh page leaves the list as it was", () => {
  assert.deepEqual(mergeVersions([entry(2)], []).map((v) => v.version), [2]);
});

test("a full page that does not reach version 1 means there are older versions", () => {
  const full = Array.from({ length: VERSIONS_PAGE }, (_, i) => entry(100 - i));
  assert.equal(hasMore(full), true);
  assert.equal(hasMore(full.slice(0, 5)), false);
  assert.equal(hasMore(Array.from({ length: VERSIONS_PAGE }, (_, i) => entry(VERSIONS_PAGE - i))), false);
});
