import { assert, file, test } from "../../../shared/testing/test.js";
import { jsonText, jsonTree, setFolds } from "./json-tree.js";

file("usage/presentation/json-tree");

const render = (value) => {
  const el = document.createElement("div");
  el.append(jsonTree(document, value));
  return el;
};

test("an object reads as indented JSON, each object and array folding on its own", () => {
  const el = render({ model: "m", n: 3, ok: true, none: null, messages: [{ role: "user" }] });
  assert.equal(el.textContent.replace(/\s+/g, ""), '{5keys"model":"m","n":3,"ok":true,"none":null,"messages":[1item{1key"role":"user"}]}');
  const folds = el.querySelectorAll("details");
  assert.equal(folds.length, 3, "the root, the array and the object in it");
  assert.ok([...folds].every((d) => d.open), "everything starts open");
});

test("each kind of value has its own class, for colour", () => {
  const el = render({ s: "x", n: 1, b: false, z: null });
  assert.deepEqual(
    [".json-key", ".json-string", ".json-number", ".json-boolean", ".json-null"].map((s) => el.querySelectorAll(s).length),
    [4, 1, 1, 1, 1],
  );
});

test("a string is shown escaped and whole, never as markup", () => {
  const long = "<b>" + "a".repeat(5000) + '"\n';
  const el = render({ text: long });
  assert.equal(el.querySelector(".json-string").textContent, JSON.stringify(long));
  assert.equal(el.querySelector("b"), null);
});

test("an empty object or array, or a bare value, does not fold", () => {
  assert.equal(render({}).textContent, "{}");
  assert.equal(render([]).textContent, "[]");
  assert.equal(render("cut off").textContent, '"cut off"');
  assert.equal(render({}).querySelector("details"), null);
});

test("a search highlights each match and folds what holds none", () => {
  const el = document.createElement("div");
  el.append(jsonTree(document, { model: "m", request: { messages: [{ content: "find the Needle here" }] }, response: { text: "nothing" } }, { query: "needle" }));
  const marks = [...el.querySelectorAll("mark.json-match")];
  assert.deepEqual(marks.map((m) => m.textContent), ["Needle"]);
  const folds = [...el.querySelectorAll("details")];
  const open = (d) => d.open;
  assert.deepEqual(folds.map(open), [true, true, true, true, false], "root, request, messages, the message open; response folded");
  assert.equal(el.querySelector(".json-string mark").closest(".json-string").textContent, '"find the Needle here"');
});

test("a search matches keys too", () => {
  const el = document.createElement("div");
  el.append(jsonTree(document, { usage: { input_tokens: 3 } }, { query: "INPUT" }));
  assert.deepEqual([...el.querySelectorAll("mark")].map((m) => m.textContent), ["input"]);
});

test("collapse all folds every node but keeps the root's own fold reachable; expand all opens them", () => {
  const el = render({ a: { b: [1, 2] }, c: { d: 1 } });
  setFolds(el, false);
  assert.ok([...el.querySelectorAll("details")].every((d) => !d.open));
  setFolds(el, true);
  assert.ok([...el.querySelectorAll("details")].every((d) => d.open));
});

test("one node folds without touching its siblings", () => {
  const el = render({ a: { x: 1 }, b: { y: 2 } });
  const folds = el.querySelectorAll("details");
  folds[1].open = false;
  assert.deepEqual([...folds].map((d) => d.open), [true, false, true]);
});

test("copy gives the value as indented JSON", () => {
  assert.equal(jsonText({ a: [1] }), '{\n  "a": [\n    1\n  ]\n}');
  assert.equal(jsonText(null), "null");
  assert.equal(jsonText("x"), '"x"');
});
