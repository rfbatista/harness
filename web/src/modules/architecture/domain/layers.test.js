import { assert, file, test } from "../../../shared/testing/test.js";
import {
  cellLabel, matrixAgents, moved, newLayer, nextAccess, patternProblem, withAccess, withMatchKind, accessOf, DEFAULT_ROW,
  AGENT_SLOTS, accessSentence, agentSlot, layerPattern, otherLayers, overlapsOf, presetAdditions, withRowAccess,
  folderChoices, isFolderPicked, withFolderPicked,
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

test("a layer shows as its folder with /** or its regex between slashes", () => {
  assert.equal(layerPattern({ match: { folder: "internal/domain" } }), "internal/domain/**");
  assert.equal(layerPattern({ match: { regex: "^a/" } }), "/^a//");
  assert.equal(layerPattern({ match: { folder: "" } }), "");
});

test("an agent keeps its colour slot, and the slots are few", () => {
  assert.equal(agentSlot("go-developer"), agentSlot("go-developer"));
  for (const n of ["a", "software-architect", "frontend-developer", "plain", ""]) {
    const s = agentSlot(n);
    assert.ok(Number.isInteger(s) && s >= 0 && s < AGENT_SLOTS, n);
  }
});

test("the preset adds the hexagonal layers a repository does not have yet", () => {
  assert.equal(presetAdditions([]).length, 8);
  const rest = presetAdditions([{ name: "domain" }, { name: "web" }]);
  assert.deepEqual(rest.map((l) => l.name), ["ports", "application", "adapters-in", "adapters-out", "tests", "docs"]);
});

test("setting a whole row gives an agent one level on every layer", () => {
  const layers = [newLayer([]), { ...newLayer([{ name: "layer-1" }]), name: "b" }];
  const out = withRowAccess(layers, "go-developer", "change");
  assert.deepEqual(out.map((l) => accessOf(l.access, "go-developer")), ["change", "change"]);
  assert.equal(accessOf(layers[0].access, "go-developer"), "read"); // the input is left alone
  const dflt = withRowAccess(layers, DEFAULT_ROW, "none");
  assert.deepEqual(dflt.map((l) => l.access.default), ["none", "none"]);
});

test("the access of unlayered paths is told in words", () => {
  assert.equal(accessSentence({ default: "read", agents: { "software-architect": "change", devops: "change" } }), "everyone reads, only devops and software-architect change"); // names sorted, whatever order the record keeps them in
  assert.equal(accessSentence({ default: "read", agents: { devops: "change", "software-architect": "change" } }), "everyone reads, only devops and software-architect change");
  assert.equal(accessSentence({ default: "read", agents: {} }), "everyone reads, nobody changes");
  assert.equal(accessSentence({ default: "change", agents: {} }), "everyone reads and changes");
  assert.equal(accessSentence({ default: "none", agents: { plain: "read" } }), "nobody reads, only plain reads");
});

test("the paths that overlap a layer are the overlaps naming it", () => {
  const overlaps = [{ path: "a", layers: ["x", "y"], winner: "x" }, { path: "b", layers: ["y", "z"], winner: "z" }];
  assert.deepEqual(overlapsOf(overlaps, "x").map((o) => o.path), ["a"]);
  assert.deepEqual(overlapsOf(overlaps, "y").map((o) => o.path), ["a", "b"]);
  assert.deepEqual(overlapsOf(overlaps, "").map((o) => o.path), []);
  assert.deepEqual(otherLayers({ layers: ["x", "y"] }, "x"), ["y"]);
});

const tree = {
  path: "", name: ".", is_dir: true,
  children: [
    { path: ".git", name: ".git", is_dir: true, children: [{ path: ".git/hooks", name: "hooks", is_dir: true, children: [] }] },
    { path: "README.md", name: "README.md", is_dir: false },
    { path: "node_modules", name: "node_modules", is_dir: true, children: [{ path: "node_modules/x", name: "x", is_dir: true, children: [] }] },
    {
      path: "internal", name: "internal", is_dir: true,
      children: [
        { path: "internal/domain", name: "domain", is_dir: true, children: [{ path: "internal/domain/zone.go", name: "zone.go", is_dir: false }] },
        { path: "internal/app", name: "app", is_dir: true, children: [] },
      ],
    },
    { path: "web", name: "web", is_dir: true, children: [] },
  ],
};

test("the folder choices are the tree's directories, sorted, with no files, .git or ignored paths", () => {
  assert.deepEqual(folderChoices(tree, ["node_modules"]), ["internal", "internal/app", "internal/domain", "web"]);
  assert.deepEqual(folderChoices(tree, ["./internal/domain/", "node_modules"]), ["internal", "internal/app", "web"]);
  assert.deepEqual(folderChoices(null, []), []);
});

test("an ignored path hides the folders below it and an absolute one is read from the checkout", () => {
  assert.deepEqual(folderChoices(tree, ["internal"], "/src/harness"), ["node_modules", "node_modules/x", "web"]);
  assert.deepEqual(folderChoices(tree, ["/src/harness/internal", "node_modules"], "/src/harness"), ["web"]);
});

test("checked folders map to and from the layer's folder pattern", () => {
  const layer = { name: "core", match: { folder: "internal/domain" }, access: {} };
  assert.equal(isFolderPicked(layer, "internal/domain"), true);
  assert.equal(isFolderPicked(layer, "internal/app"), false);
  assert.equal(isFolderPicked({ name: "t", match: { regex: "internal/domain" } }, "internal/domain"), false);
  const moved = withFolderPicked(layer, "internal/app", true);
  assert.deepEqual(moved.match, { folder: "internal/app" });
  assert.equal(moved.name, "core");
  assert.deepEqual(withFolderPicked(moved, "internal/app", false).match, { folder: "" });
  assert.deepEqual(withFolderPicked(layer, "internal/app", false).match, { folder: "internal/domain" });
});
