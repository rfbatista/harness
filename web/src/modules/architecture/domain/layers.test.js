import { assert, file, test } from "../../../shared/testing/test.js";
import {
  cellLabel, matrixAgents, moved, newLayer, nextAccess, patternProblem, withAccess, withMatchKind, accessOf, DEFAULT_ROW,
} from "./layers.js";

file("architecture/domain/layers");

test("a new layer starts with everyone reading and only software-architect changing", () => {
  const l = newLayer([{ name: "layer-1" }]);
  assert.equal(l.name, "layer-2");
  assert.deepEqual(l.access, { default: "read", agents: { "software-architect": "change" } });
});

test("a cell cycles none, read, change", () => {
  assert.deepEqual(["none", "read", "change"].map(nextAccess), ["read", "change", "none"]);
});

test("setting an agent to the default drops its entry; the default row sets the default", () => {
  let a = { default: "read", agents: { "go-developer": "change" } };
  a = withAccess(a, "go-developer", "read");
  assert.deepEqual(a.agents, {});
  assert.equal(accessOf(a, "go-developer"), "read");
  a = withAccess(a, DEFAULT_ROW, "none");
  assert.equal(accessOf(a, "devops"), "none");
});

test("an invalid regex or folder is named", () => {
  assert.ok(/does not compile/.test(patternProblem("regex", "(^tests/")));
  assert.equal(patternProblem("regex", "^tests/"), "");
  assert.ok(/no leading/.test(patternProblem("folder", "/etc")));
  assert.ok(/\.\./.test(patternProblem("folder", "a/../b")));
  assert.equal(patternProblem("folder", ""), "");
});

test("the matrix lists the catalog, agents only the record names, then plain", () => {
  const rec = { layers: [{ access: { agents: { ghost: "read", "go-developer": "change" } } }], unlayered: { agents: {} } };
  assert.deepEqual(matrixAgents(["go-developer", "devops"], rec), ["go-developer", "devops", "ghost", "plain"]);
});

test("moved swaps neighbours and stops at the ends; the match keeps its text across kinds", () => {
  assert.deepEqual(moved([1, 2, 3], 2, -1), [1, 3, 2]);
  assert.deepEqual(moved([1, 2, 3], 0, -1), [1, 2, 3]);
  assert.deepEqual(withMatchKind({ name: "x", match: { folder: "a/b" } }, "regex").match, { regex: "a/b" });
  assert.equal(cellLabel("go-developer", "domain", "change"), "go-developer on domain: change");
});
