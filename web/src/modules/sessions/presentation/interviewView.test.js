import { assert, file, test } from "../../../shared/testing/test.js";
import { makeInterview, makeList, questionDTO } from "../testing/interview-fixtures.js";
import { toInterview } from "../infrastructure/interviews-dto.js";
import { answersText, arrivalText, bodyHtml, cardView, clockTime, closedText, footerView, headWho, historyRowView, noteSummary, optionForKey, panelView, peekText, questionView, toggleLabel } from "./interviewView.js";

file("sessions/presentation/interviewView");

test("the recommended option is drawn first, labelled and never checked; the rest keep their order", () => {
  const iv = makeInterview({ questions: [questionDTO({ options: [{ label: "A", description: "" }, { label: "B", description: "b" }, { label: "C", description: "" }], recommended: { option: 2 } })] });
  const q = questionView(iv.questions[0], 0, iv.id);
  assert.deepEqual(q.options.map((o) => `${o.key}:${o.label}:${o.value}:${o.recommended}`), ["1:C:2:true", "2:A:0:false", "3:B:1:false"]);
  assert.equal(q.options[2].description, "b");
  assert.ok(q.options.every((o) => !("checked" in o)), "nothing is preselected");
});

test("no recommendation: the options keep the order the agent gave", () => {
  const iv = makeInterview({ questions: [questionDTO({ recommended: null })] });
  assert.deepEqual(questionView(iv.questions[0], 0, iv.id).options.map((o) => o.label), ["A file per interview", "The harness database"]);
});

test("a choice question ends with Write my own; a text question has a box and no options", () => {
  const iv = makeInterview();
  const single = questionView(iv.questions[0], 0, iv.id);
  assert.equal(single.own.label, "Write my own");
  assert.equal(single.multiple, false);
  const text = questionView(iv.questions[1], 1, iv.id);
  assert.equal(text.text, true);
  assert.equal(text.own, null);
  assert.equal(text.number, "2");
  const multi = questionView(toInterview({ ...makeInterviewDTO(), questions: [questionDTO({ kind: "multiple" })] }).questions[0], 0, "x");
  assert.equal(multi.hint, "Choose any number");
  assert.equal(multi.multiple, true);
});

function makeInterviewDTO() {
  return { id: "i", session_id: "s1", agent_name: "a", asked_at: "2026-11-08T14:32:00Z", state: "open", questions: [] };
}

test("a text recommendation is carried as text for the recommendation box", () => {
  const iv = makeInterview({ questions: [questionDTO({ kind: "text", options: [], recommended: { text: "interviews" } })] });
  assert.equal(questionView(iv.questions[0], 0, iv.id).recommendedText, "interviews");
});

test("a card names who asks and how many, and when", () => {
  const card = cardView(makeInterview({ asked_at: new Date(2026, 10, 8, 14, 32).toISOString() }));
  assert.equal(card.heading, "architect asks 2 questions");
  assert.equal(card.label, "Open interview from architect");
  assert.equal(card.asked, "Asked 14:32");
  assert.equal(cardView(makeInterview({ questions: [questionDTO()] })).heading, "architect asks 1 question");
});

test("the panel lists the open interviews newest first and counts their questions", () => {
  const list = makeList(3, makeInterview({ id: "a" }), makeInterview({ id: "b", state: "sent" }), makeInterview({ id: "c", agent_name: "product", questions: [questionDTO()] }));
  const panel = panelView(list);
  assert.deepEqual(panel.cards.map((c) => c.id), ["c", "a"]);
  assert.equal(panel.questions, 3);
  assert.equal(panel.openLabel, "3 open");
  assert.equal(panel.who, "architect, product · 2 open");
  assert.equal(panel.peek, "3 questions from product", "all the open questions, named for the newest interview's agent");
  assert.equal(panelView(makeList(0)).open, false);
});

test("the study's words: toggle name, peek strip, arrival announcements, head", () => {
  assert.equal(toggleLabel(5), "Questions, 5 open");
  assert.equal(peekText(5, "architect"), "5 questions from architect");
  const iv = makeInterview({ questions: [questionDTO(), questionDTO(), questionDTO()] });
  assert.equal(arrivalText(iv, false), "architect asks 3 questions");
  assert.equal(arrivalText(iv, true), "architect asked 3 more questions");
  assert.equal(headWho([makeInterview()]), "architect");
  assert.equal(headWho([]), "");
  assert.equal(clockTime(new Date(2026, 10, 8, 9, 5)), "09:05");
});

test("a body is markdown, and raw HTML in it is shown as text", () => {
  const html = bodyHtml("**bold** and `code`\n\n<script>alert(1)</script>\n\n[a](javascript:x) [b](https://example.com)");
  assert.ok(html.includes("<strong>bold</strong>") && html.includes("<code>code</code>"));
  assert.ok(!html.includes("<script>") && html.includes("&lt;script&gt;"));
  assert.ok(!html.includes("javascript:"));
  assert.ok(html.includes('href="https://example.com"') && html.includes('target="_blank"'));
  assert.equal(bodyHtml(""), "");
});

test("the footer says how far the interview is, and the Send button says what happens", () => {
  assert.equal(footerView({ answered: 3, total: 5, skipped: 2 }).text, "3 of 5 answered · 2 will be skipped");
  assert.equal(footerView({ answered: 3, total: 5, skipped: 2 }).send, "Send · 2 skipped");
  assert.equal(footerView({ answered: 3, total: 5, skipped: 2 }).bar, "inline-size: 60%");
  assert.equal(footerView({ answered: 0, total: 5, skipped: 5 }).text, "0 of 5 answered · 5 will be skipped");
  assert.equal(footerView({ answered: 0, total: 5, skipped: 5 }).send, "Send, all skipped");
  assert.equal(footerView({ answered: 5, total: 5, skipped: 0 }).text, "5 of 5 answered");
  assert.equal(footerView({ answered: 5, total: 5, skipped: 0 }).send, "Send answers");
});

test("number keys pick the row in the drawn order: the recommendation is 1, the options stop at 8", () => {
  const iv = makeInterview();
  const q = questionView(iv.questions[0], 0, iv.id);
  assert.equal(optionForKey(q, "1").label, "The harness database");
  assert.equal(optionForKey(q, "2").label, "A file per interview");
  assert.equal(optionForKey(q, "3"), null, "Write my own has no number");
  assert.equal(optionForKey(q, "9"), null);
  assert.equal(optionForKey(q, "a"), null);
  assert.equal(optionForKey(q, "12"), null);
});

test("the card counts the questions the agent recommends an answer to", () => {
  const iv = makeInterview({ questions: [questionDTO(), questionDTO({ recommended: null }), questionDTO({ kind: "text", options: [], recommended: { text: "x" } })] });
  assert.equal(cardView(iv).recommended, 2);
  assert.equal(cardView(iv).questions[2].at, 2);
});

// ── stack and history ───────────────────────────────────────────────────────

const three = () => [questionDTO({ title: "First?" }), questionDTO({ title: "Second?" }), questionDTO({ title: "Third?" })];
const sentAnswers = [
  { chosen: [1], text: "", note: "because", skipped: false },
  { chosen: [], text: "", note: "", skipped: true },
];
const sent = (over = {}) => makeInterview({ id: "sent", state: "sent", ended_at: "2026-11-08T14:41:00Z", delivery: "delivered", delivered_at: "2026-11-08T14:41:00Z", answers: sentAnswers, ...over });

test("the newest open interview is expanded, older ones are folded with how far the person got, and each can be told otherwise", () => {
  const list = makeList(3, makeInterview({ id: "a", questions: three().map((q) => q) }), makeInterview({ id: "b" }));
  const progress = (id) => (id === "a" ? { answered: 2, total: 3, skipped: 1 } : { answered: 0, total: 2, skipped: 2 });
  const panel = panelView(list, { progress });
  assert.deepEqual(panel.cards.map((c) => [c.id, c.folded]), [["b", false], ["a", true]]);
  const a = panel.cards[1];
  assert.equal(a.heading, "architect asks 3 questions");
  assert.equal(a.foldedSub, "First?", "a card is named by who asked and its first question");
  assert.equal(a.foldedBadge, "2 of 3");
  assert.equal(a.foldLabel, "Open interview from architect, folded");
  const told = panelView(list, { progress, folded: (id) => (id === "b" ? true : id === "a" ? false : undefined) });
  assert.deepEqual(told.cards.map((c) => [c.id, c.folded]), [["b", true], ["a", false]], "the person's choice wins over the order");
});

test("a lone open interview is never folded", () => {
  assert.equal(panelView(makeList(1, makeInterview())).cards[0].folded, false);
});

test("the note's summary says Note once it holds text and Add a note while it is empty", () => {
  assert.equal(noteSummary(""), "Add a note");
  assert.equal(noteSummary("   \n"), "Add a note", "blank is empty");
  assert.equal(noteSummary("because"), "Note");
});

test("a folded history row says the state, who and when, the counts and how it was delivered", () => {
  const at = clockTime(new Date("2026-11-08T14:32:00Z"));
  const delivered = clockTime(new Date("2026-11-08T14:41:00Z"));
  const row = historyRowView(sent(), sentAnswers);
  assert.equal(row.title, `Sent · architect · ${at}`);
  assert.equal(row.sub, `1 answered, 1 skipped · delivered ${delivered}`);
  assert.equal(row.badge, "2 questions");
  assert.equal(row.tone, "success");
  assert.equal(row.label, `Sent interview from architect at ${at}`);
  assert.equal(row.copy, false);
  assert.equal(historyRowView(sent({ delivery: "queued", delivered_at: null }), sentAnswers).sub, "1 answered, 1 skipped · queued: the agent is mid-turn");
  assert.equal(historyRowView(sent({ answers: [{ chosen: [0], text: "", note: "", skipped: false }, { chosen: [], text: "x", note: "", skipped: false }] }), null).sub.startsWith("2 answered · "), true, "nothing skipped: no skipped count");
});

test("a dismissed row says when and that the agent was told; an expired one when the session ended and that the agent never read it", () => {
  const ended = clockTime(new Date("2026-11-08T12:44:00Z"));
  const dismissed = historyRowView(makeInterview({ id: "d", state: "dismissed", ended_at: "2026-11-08T12:44:00Z" }), null);
  assert.equal(dismissed.title.startsWith("Dismissed · architect · "), true);
  assert.equal(dismissed.sub, `Dismissed ${ended} · the agent was told`);
  assert.equal(dismissed.tone, null);
  const expired = historyRowView(makeInterview({ id: "x", state: "expired", ended_at: "2026-11-08T12:44:00Z" }), null);
  assert.equal(expired.title.startsWith("Expired · architect · "), true);
  assert.equal(expired.sub, `Session ended ${ended} · the agent never read it`);
  assert.equal(expired.tone, "danger");
  assert.equal(expired.copy, true, "only an expired interview offers Copy answers");
});

test("an opened row reads each question with its answer, its note, or Skipped", () => {
  const row = historyRowView(sent(), sentAnswers);
  assert.deepEqual(row.questions.map((q) => [q.number, q.title, q.answer, q.skipped, q.note]), [
    ["1", "Which store holds interviews?", "The harness database", false, "because"],
    ["2", "Where is it read?", "Skipped", true, ""],
  ]);
});

test("an answer reads as the options chosen, then the text; a text question as its text", () => {
  const multi = { ...makeInterview().questions[0], kind: "multiple" };
  const [textQ] = [makeInterview().questions[1]];
  assert.equal(historyRowView(sent({ questions: [{ ...multi, title: "m" }, questionDTO()], answers: [{ chosen: [0, 1], text: "and a third", note: "", skipped: false }, { chosen: [0], text: "", note: "", skipped: false }] }), null).questions[0].answer, "A file per interview, The harness database, and a third");
  assert.equal(textQ.kind, "text");
  assert.equal(historyRowView(sent({ answers: [{ chosen: [], text: "", note: "", skipped: true }, { chosen: [], text: "in the page seed", note: "", skipped: false }] }), null).questions[1].answer, "in the page seed");
});

test("a dismissed interview opened shows its questions skipped, with the dismiss note if there is one", () => {
  const row = historyRowView(makeInterview({ id: "d", state: "dismissed", ended_at: "2026-11-08T12:44:00Z", dismiss_note: "not now" }), null);
  assert.deepEqual(row.questions.map((q) => q.skipped), [true, true]);
  assert.equal(row.dismissNote, "not now");
});

test("the copied answers are plain text: who, each question with its answer and note", () => {
  const text = answersText(makeInterview({ id: "x", state: "expired", ended_at: "2026-11-08T12:44:00Z" }), [
    { chosen: [1], text: "", note: "ok", skipped: false },
    { chosen: [], text: "", note: "", skipped: true },
  ]);
  assert.equal(text, ["1. Which store holds interviews?", "The harness database", "Note: ok", "", "2. Where is it read?", "Skipped"].join("\n"));
});

test("the panel lists open interviews, then Earlier: the most recently ended first, folded unless opened", () => {
  const list = makeList(
    4,
    makeInterview({ id: "open" }),
    sent({ id: "a", ended_at: "2026-11-08T13:00:00Z" }),
    makeInterview({ id: "b", state: "expired", ended_at: "2026-11-08T15:00:00Z" }),
  );
  const panel = panelView(list, { opened: (id) => id === "a", typed: () => [{ chosen: [1], text: "", note: "", skipped: false }, { chosen: [], text: "typed", note: "", skipped: false }] });
  assert.equal(panel.history.label, "Earlier · 2");
  assert.deepEqual(panel.history.rows.map((r) => [r.id, r.opened]), [["b", false], ["a", true]]);
  assert.equal(panel.empty, false);
  const expired = panel.history.rows[0];
  assert.equal(expired.questions[1].answer, "typed", "an expired interview reads back what was typed");
});

test("with nothing open and history, the panel says so; with neither there is no panel", () => {
  const only = panelView(makeList(1, sent()));
  assert.equal(only.open, false);
  assert.equal(only.empty, true);
  assert.equal(only.emptyTitle, "No questions right now");
  assert.equal(only.emptyText, "When an agent asks you questions, they appear here, and your answers go back to it automatically.");
  assert.equal(only.who, "nothing open");
  assert.equal(only.hasHistory, true);
  assert.equal(panelView(makeList(0)).hasHistory, false);
});

test("what a change from another tab says", () => {
  assert.equal(closedText(sent()), "Interview answered in another tab");
  assert.equal(closedText(makeInterview({ state: "dismissed" })), "Interview dismissed in another tab");
  assert.equal(closedText(makeInterview({ state: "expired" })), "Interview expired: the session ended");
});

test("what the markup binds: the read-back heading and whether it holds an answer, the fold button's name, and that only a stack can fold", () => {
  const row = historyRowView(sent(), sentAnswers);
  assert.deepEqual(row.questions.map((q) => [q.heading, q.answered]), [["1 · Which store holds interviews?", true], ["2 · Where is it read?", false]]);
  const stack = panelView(makeList(2, makeInterview({ id: "a" }), makeInterview({ id: "b", agent_name: "product" })));
  assert.deepEqual(stack.cards.map((c) => [c.id, c.foldable, c.foldButton]), [["b", true, "Fold the interview from product"], ["a", true, "Fold the interview from architect"]]);
  assert.equal(panelView(makeList(1, makeInterview())).cards[0].foldable, false);
});
