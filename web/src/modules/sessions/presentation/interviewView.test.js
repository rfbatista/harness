import { assert, file, test } from "../../../shared/testing/test.js";
import { makeInterview, makeList, questionDTO } from "../testing/interview-fixtures.js";
import { toInterview } from "../infrastructure/interviews-dto.js";
import { arrivalText, bodyHtml, cardView, clockTime, headWho, panelView, peekText, questionView, toggleLabel } from "./interviewView.js";

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
