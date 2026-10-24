import { assert, file, test } from "../../../shared/testing/test.js";
import { MAX_TEXT, stepRequest } from "./steps.js";

file("writinghelper/domain/steps");

const base = {
  projectId: "p1",
  kind: "task_description",
  text: "rough",
  siblings: [
    { label: "Title", text: "A title" },
    { label: "Notes", text: "   " },
  ],
};

test("an analyze request names the field and sends only the siblings with text", () => {
  assert.deepEqual(stepRequest("analyze", base), {
    project_id: "p1",
    step: "analyze",
    field: { kind: "task_description", text: "rough" },
    siblings: [{ label: "Title", text: "A title" }],
  });
});

test("no siblings with text leaves the key out", () => {
  const body = stepRequest("analyze", { ...base, siblings: [] });
  assert.equal("siblings" in body, false);
});

test("a draft request carries the answers", () => {
  const body = stepRequest("draft", { ...base, answers: [{ question: "Q?", answer: "A" }] });
  assert.deepEqual(body.answers, [{ question: "Q?", answer: "A" }]);
  assert.equal("draft" in body, false);
});

test("a refine sends the current draft and the request", () => {
  const body = stepRequest("draft", { ...base, answers: [], draft: "Current", request: "Shorter" });
  assert.equal(body.draft, "Current");
  assert.equal(body.request, "Shorter");
  assert.equal("answers" in body, false);
});

test("a titles request sends what was offered as exclude", () => {
  const body = stepRequest("titles", { ...base, kind: "task_title", exclude: ["A", "B"] });
  assert.deepEqual(body.exclude, ["A", "B"]);
  assert.equal(body.field.kind, "task_title");
});

test("a text over the limit is refused before anything is sent", () => {
  const err = assert.throws(() => stepRequest("analyze", { ...base, text: "x".repeat(MAX_TEXT + 1) }), "INVALID_INPUT");
  assert.ok(err.message.includes("20,000"));
});

test("a sibling over the limit is cut, not refused: it is only context", () => {
  const body = stepRequest("analyze", { ...base, siblings: [{ label: "Title", text: "x".repeat(MAX_TEXT + 1) }] });
  assert.equal(body.siblings[0].text.length, MAX_TEXT);
});

test("more than four siblings are cut to four", () => {
  const siblings = Array.from({ length: 6 }, (_, i) => ({ label: `S${i}`, text: "t" }));
  assert.equal(stepRequest("analyze", { ...base, siblings }).siblings.length, 4);
});
