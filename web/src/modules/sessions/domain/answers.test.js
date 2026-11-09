import { assert, file, test } from "../../../shared/testing/test.js";
import { makeInterview, questionDTO } from "../testing/interview-fixtures.js";
import { acceptRecommended, blankAnswers, isAnswered, progress, selectOwn, selectOption, toAnswers, withNote, withText } from "./answers.js";

file("sessions/domain/answers");

const multi = () => makeInterview({ questions: [questionDTO({ kind: "multiple", options: [{ label: "A", description: "" }, { label: "B", description: "" }, { label: "C", description: "" }], recommended: { option: 2 } })] });

test("nothing is answered at first, and a blank question is a skipped one", () => {
  const iv = makeInterview();
  const blank = blankAnswers(iv.questions);
  assert.equal(blank.length, 2);
  assert.deepEqual(progress(iv.questions, blank), { answered: 0, total: 2, skipped: 2 });
});

test("a single choice holds one option; choosing another replaces it, and Write my own replaces the options", () => {
  const q = makeInterview().questions[0];
  let a = selectOption(q, blankAnswers([q])[0], 1);
  assert.deepEqual(a.chosen, [1]);
  a = selectOption(q, a, 0);
  assert.deepEqual(a.chosen, [0]);
  a = selectOwn(q, a, true);
  assert.deepEqual([a.chosen, a.own], [[], true]);
  assert.equal(isAnswered(q, a), false, "Write my own with no text is not an answer yet");
  a = withText(a, "  a third way ");
  assert.equal(isAnswered(q, a), true);
  a = selectOption(q, a, 1);
  assert.deepEqual([a.chosen, a.own], [[1], false]);
});

test("a multiple choice holds a set of options; Write my own adds free text to the ticked ones", () => {
  const q = multi().questions[0];
  let a = selectOption(q, blankAnswers([q])[0], 2, true);
  a = selectOption(q, a, 0, true);
  assert.deepEqual([...a.chosen].sort(), [0, 2]);
  a = selectOption(q, a, 2, false);
  assert.deepEqual(a.chosen, [0]);
  a = withText(selectOwn(q, a, true), "and more");
  assert.deepEqual([a.chosen, a.own, a.text], [[0], true, "and more"]);
  assert.deepEqual(toAnswers([q], [a])[0], { chosen: [0], text: "and more", note: "", skipped: false });
});

test("a text question is answered by its text alone", () => {
  const q = makeInterview().questions[1];
  const a = blankAnswers([q])[0];
  assert.equal(isAnswered(q, withText(a, "   ")), false);
  assert.equal(isAnswered(q, withText(a, "in the web UI")), true);
});

test("a note is never an answer by itself, and is sent only with an answer", () => {
  const iv = makeInterview();
  const noted = withNote(blankAnswers(iv.questions)[0], "careful");
  assert.equal(isAnswered(iv.questions[0], noted), false);
  assert.deepEqual(toAnswers(iv.questions, [noted, blankAnswers(iv.questions)[1]])[0], { chosen: [], text: "", note: "", skipped: true });
  assert.deepEqual(toAnswers(iv.questions, [selectOption(iv.questions[0], noted, 1), blankAnswers(iv.questions)[1]])[0], { chosen: [1], text: "", note: "careful", skipped: false });
});

test("Write my own in a single choice sends its text and no option; the text of a hidden own box is not sent", () => {
  const q = makeInterview().questions[0];
  const own = withText(selectOwn(q, blankAnswers([q])[0], true), "mine");
  assert.deepEqual(toAnswers([q], [own])[0], { chosen: [], text: "mine", note: "", skipped: false });
  assert.deepEqual(toAnswers([q], [selectOption(q, own, 0)])[0], { chosen: [0], text: "", note: "", skipped: false });
});

test("accepting all recommended fills the blank questions that have one and never overwrites an answer", () => {
  const iv = makeInterview({ questions: [questionDTO(), questionDTO({ recommended: { option: 0 } }), questionDTO({ recommended: null }), questionDTO({ kind: "text", options: [], recommended: { text: "use SSE" } }), questionDTO({ kind: "multiple", recommended: { option: 1 } })] });
  const typed = withText(selectOwn(iv.questions[1], blankAnswers(iv.questions)[1], true), "mine");
  const states = blankAnswers(iv.questions);
  states[1] = typed;
  const after = acceptRecommended(iv.questions, states);
  assert.deepEqual(after[0].chosen, [1]);
  assert.equal(after[1], typed, "an answered question is untouched");
  assert.deepEqual(after[2].chosen, [], "no recommendation: stays blank");
  assert.equal(after[3].text, "use SSE");
  assert.deepEqual(after[4].chosen, [1]);
  assert.deepEqual(progress(iv.questions, after), { answered: 4, total: 5, skipped: 1 });
});

test("a note alone does not count as an answer for accept all: the question is still blank and gets the recommendation, keeping its note", () => {
  const iv = makeInterview();
  const states = [withNote(blankAnswers(iv.questions)[0], "hm"), blankAnswers(iv.questions)[1]];
  const after = acceptRecommended(iv.questions, states);
  assert.deepEqual([after[0].chosen, after[0].note], [[1], "hm"]);
});
