import { assert, file, test } from "../../../shared/testing/test.js";
import * as panel from "./panel.js";

file("writinghelper/domain/panel");

const questions = [
  { id: "q1", text: "Which fields?", why: "It says titles.", chips: ["Title and description", "Every field"] },
  { id: "q2", text: "Never do what?", why: "Limits matter.", chips: [] },
  { id: "q3", text: "Done when?", why: "No criteria.", chips: ["A person drafts in a minute"] },
];

/** A panel on a description that has text, with questions shown. */
const asking = () => panel.analyzed(panel.opened({ kind: "task_description", text: "rough text" }), { already_good: false, questions });

test("a closed panel has no phase to show", () => {
  assert.equal(panel.closed().phase, "closed");
});

test("an empty description starts at the start question", () => {
  assert.equal(panel.opened({ kind: "task_description", text: "  " }).phase, "start");
});

test("a description with text starts by reading", () => {
  const s = panel.opened({ kind: "task_description", text: "rough" });
  assert.equal(s.phase, "reading");
  assert.equal(s.step, "analyze");
});

test("a comment follows the same rules as a description", () => {
  assert.equal(panel.opened({ kind: "comment", text: "" }).phase, "start");
  assert.equal(panel.opened({ kind: "comment", text: "x" }).step, "analyze");
});

test("a title goes straight to reading its options, empty or not", () => {
  for (const text of ["", "A title"]) {
    const s = panel.opened({ kind: "task_title", text });
    assert.equal(s.phase, "reading");
    assert.equal(s.step, "titles");
  }
});

test("analyze with questions shows at most three", () => {
  const s = panel.analyzed(panel.opened({ kind: "comment", text: "x" }), { already_good: false, questions: [...questions, { id: "q4", text: "?", why: "", chips: [] }] });
  assert.equal(s.phase, "questions");
  assert.equal(s.questions.length, 3);
});

test("analyze with nothing missing says so", () => {
  assert.equal(panel.analyzed(panel.opened({ kind: "comment", text: "x" }), { already_good: true, questions: [] }).phase, "good");
});

test("analyze that finds no questions and no verdict counts as nothing missing", () => {
  assert.equal(panel.analyzed(panel.opened({ kind: "comment", text: "x" }), { already_good: false, questions: [] }).phase, "good");
});

test("answers are counted, and a blank one does not count", () => {
  let s = asking();
  assert.equal(panel.answeredCount(s), 0);
  s = panel.setAnswer(s, "q1", "Title and description");
  s = panel.setAnswer(s, "q2", "   ");
  assert.equal(panel.answeredCount(s), 1);
  assert.deepEqual(panel.answersFor(s), [{ question: "Which fields?", answer: "Title and description" }]);
});

test("picking a chip answers with it, and picking it again clears it", () => {
  let s = panel.pickChip(asking(), "q1", "Every field");
  assert.equal(s.answers.q1, "Every field");
  assert.equal(panel.chipPressed(s, "q1", "Every field"), true);
  assert.equal(panel.chipPressed(s, "q1", "Title and description"), false);
  s = panel.pickChip(s, "q1", "Every field");
  assert.equal(s.answers.q1, "");
});

test("answers are sent in question order", () => {
  let s = panel.setAnswer(panel.setAnswer(asking(), "q3", "c"), "q1", "a");
  assert.deepEqual(panel.answersFor(s).map((a) => a.answer), ["a", "c"]);
});

test("a draft streams in and then completes into a proposal", () => {
  let s = panel.drafting(asking(), { baseline: "rough text" });
  assert.equal(s.phase, "drafting");
  s = panel.delta(panel.delta(s, "Hel"), "lo");
  assert.equal(s.draft, "Hello");
  s = panel.drafted(s, "Hello there");
  assert.equal(s.phase, "proposal");
  assert.equal(s.draft, "Hello there");
  assert.equal(s.stale, false);
});

test("typing in the field after the draft started marks it stale", () => {
  let s = panel.drafted(panel.drafting(asking(), { baseline: "rough text" }), "Draft");
  s = panel.fieldChanged(s, "rough text, edited");
  assert.equal(s.stale, true);
  assert.equal(s.phase, "proposal");
});

test("typing back to the original text clears stale", () => {
  let s = panel.drafted(panel.drafting(asking(), { baseline: "rough text" }), "Draft");
  s = panel.fieldChanged(panel.fieldChanged(s, "rough text!"), "rough text");
  assert.equal(s.stale, false);
});

test("typing while the questions are shown does not mark anything stale", () => {
  const s = panel.fieldChanged(asking(), "other");
  assert.equal(s.stale, false);
});

test("typing while the draft streams marks it stale for when it lands", () => {
  let s = panel.fieldChanged(panel.drafting(asking(), { baseline: "rough text" }), "rough text more");
  s = panel.drafted(s, "Draft");
  assert.equal(s.stale, true);
});

test("a refine keeps the answers and the earlier draft until the new one lands", () => {
  let s = panel.setAnswer(asking(), "q1", "a");
  s = panel.drafted(panel.drafting(s, { baseline: "rough text" }), "First");
  s = panel.drafting(s, { baseline: "rough text", request: "Shorter" });
  assert.equal(s.phase, "drafting");
  assert.equal(s.draft, "");
  assert.equal(s.answers.q1, "a");
  assert.equal(s.request, "Shorter");
});

test("stopping a first draft goes back to the questions with the answers kept", () => {
  const s = panel.stopped(panel.drafting(panel.setAnswer(asking(), "q1", "a"), { baseline: "x" }));
  assert.equal(s.phase, "questions");
  assert.equal(s.answers.q1, "a");
});

test("stopping a refine goes back to the proposal it refined", () => {
  let s = panel.drafted(panel.drafting(asking(), { baseline: "x" }), "First");
  s = panel.stopped(panel.delta(panel.drafting(s, { baseline: "x", request: "Shorter" }), "Sho"));
  assert.equal(s.phase, "proposal");
  assert.equal(s.draft, "First");
});

test("stopping a draft from the start question goes back to it", () => {
  const s = panel.stopped(panel.drafting(panel.opened({ kind: "comment", text: "" }), { baseline: "" }));
  assert.equal(s.phase, "start");
});

test("stopping while reading closes the panel", () => {
  assert.equal(panel.stopped(panel.opened({ kind: "comment", text: "x" })).phase, "closed");
});

test("a failure keeps the answers and remembers what to retry", () => {
  let s = panel.setAnswer(asking(), "q1", "a");
  s = panel.failed(panel.drafting(s, { baseline: "x" }), { code: "WRITING_HELPER_FAILED", message: "stopped", next: "try again" });
  assert.equal(s.phase, "error");
  assert.equal(s.error.code, "WRITING_HELPER_FAILED");
  assert.equal(s.answers.q1, "a");
  assert.equal(s.retry, "draft");
});

test("retrying after a failed read retries the read", () => {
  const s = panel.failed(panel.opened({ kind: "comment", text: "x" }), { code: "X", message: "m", next: "n" });
  assert.equal(s.retry, "analyze");
});

test("titles are collected and remembered so that three more excludes them", () => {
  let s = panel.titled(panel.opened({ kind: "task_title", text: "" }), { titles: ["A", "B", "C"] });
  assert.equal(s.phase, "titles");
  assert.deepEqual(s.titles, ["A", "B", "C"]);
  s = panel.titled(panel.reading(s, "titles"), { titles: ["D", "E", "F"] });
  assert.deepEqual(s.titles, ["D", "E", "F"]);
  assert.deepEqual(s.offered, ["A", "B", "C", "D", "E", "F"]);
});

test("the view toggles between changes and clean", () => {
  assert.equal(asking().view, "changes");
  assert.equal(panel.setView(asking(), "clean").view, "clean");
});

test("the start question needs words before it can continue", () => {
  let s = panel.opened({ kind: "task_description", text: "" });
  assert.equal(panel.canContinue(s), false);
  s = panel.setIntent(s, "let people get AI help");
  assert.equal(panel.canContinue(s), true);
  assert.deepEqual(panel.answersFor(s), [{ question: "What is this task about?", answer: "let people get AI help" }]);
});

test("the placeholders of a draft are listed", () => {
  assert.deepEqual(panel.openGaps("Done when [open: criteria] and [open: owner]"), ["[open: criteria]", "[open: owner]"]);
  assert.deepEqual(panel.openGaps("nothing"), []);
});

test("the start question is worded for the field", async () => {
  const { startQuestion } = await import("./copy.js");
  assert.equal(startQuestion("task_description"), "What is this task about?");
  assert.equal(startQuestion("comment"), "What do you want to say?");
  const s = panel.setIntent(panel.opened({ kind: "comment", text: "" }), "keep the pen visible");
  assert.deepEqual(panel.answersFor(s), [{ question: "What do you want to say?", answer: "keep the pen visible" }]);
});
