import { assert, file, test } from "../../../shared/testing/test.js";
import { chatItems, dayLabel, historyEntry, isWorking, LONG_TEXT, matches } from "./historyView.js";

file("sessions/presentation/historyView");

let seq = 0;
const entry = (fields) => historyEntry({ cursor: `c${++seq}`, at: null, kind: "assistant", text: "", tool: "", isError: false, truncated: false, html: "", ...fields });
const types = (items) => items.map((i) => i.type);

test("an entry keeps its text and the server's html; the time is the local HH:MM", () => {
  const at = new Date(2026, 9, 13, 9, 5, 30);
  const e = entry({ kind: "user", text: "**hi**", html: "<p><strong>hi</strong></p>", at });
  assert.deepEqual([e.time, e.day, e.text, e.html, e.lower], ["09:05", "2026-10-13", "**hi**", "<p><strong>hi</strong></p>", "**hi**"]);
  assert.equal(e.stamp, at.toLocaleString());
  assert.deepEqual([entry({}).time, entry({}).day], ["", ""]);
});

test("a person and the agent take turns; the agent's messages in a row are one turn", () => {
  const items = chatItems([entry({ kind: "user", text: "fix it" }), entry({ text: "On it." }), entry({ text: "Done." })], { agent: "backend" });
  assert.deepEqual(types(items), ["turn", "turn"]);
  const [person, agent] = items;
  assert.deepEqual([person.who, person.name, person.initial, person.parts.map((p) => p.text)], ["person", "You", "Y", ["fix it"]]);
  assert.deepEqual([agent.who, agent.name, agent.initial, agent.parts.map((p) => p.text)], ["agent", "backend", "B", ["On it.", "Done."]]);
});

test("the tool calls between two messages are one card, each call paired with its result", () => {
  const items = chatItems([
    entry({ text: "Looking." }),
    entry({ kind: "tool_use", tool: "Bash", text: "go test ./..." }),
    entry({ kind: "tool_use", tool: "Read", text: "a.go" }),
    entry({ kind: "tool_result", tool: "Bash", text: "FAIL\nexit 1", isError: true }),
    entry({ kind: "tool_result", tool: "Read", text: "package a" }),
    entry({ kind: "tool_use", tool: "Bash", text: "git status" }),
    entry({ kind: "tool_result", tool: "Bash", text: "" }),
    entry({ text: "Fixed." }),
  ]);
  assert.deepEqual(types(items), ["turn", "activity", "turn"]);
  const card = items[1];
  assert.deepEqual([card.summary, card.names, card.failed, card.failedLabel, card.running], ["3 tool calls", "Bash ×2, Read", 1, "1 failed", false]);
  assert.deepEqual(
    card.calls.map((c) => [c.tool, c.gist, c.output, c.state, c.mark, c.meta, c.cursors.length]),
    [
      ["Bash", "go test ./...", "FAIL\nexit 1", "error", "✕", "2 lines", 2],
      ["Read", "a.go", "package a", "ok", "✓", "", 2],
      ["Bash", "git status", "", "ok", "✓", "", 2],
    ],
  );
  assert.equal(card.calls[0].cursorList, card.calls[0].cursors.join(" "));
});

test("a result without its call is a row of its own; a call without its result runs while the session does", () => {
  const entries = [entry({ kind: "tool_result", tool: "Bash", text: "\n M a.js" }), entry({ kind: "tool_use", tool: "Edit", text: "a.js" })];
  const [card] = chatItems(entries, { live: true });
  assert.deepEqual(card.calls.map((c) => [c.tool, c.gist, c.input, c.hasOutput, c.state]), [
    ["Bash", "M a.js", "", true, "ok"],
    ["Edit", "a.js", "a.js", false, "running"],
  ]);
  assert.equal(card.running, true);
  assert.equal(chatItems(entries, { live: false })[0].calls[1].state, "none");
  assert.equal(chatItems([entry({ kind: "tool_result", tool: "Bash", text: "" })])[0].calls[0].gist, "no output");
});

test("only the newest card can still be running", () => {
  const items = chatItems([entry({ kind: "tool_use", tool: "Bash", text: "a" }), entry({ text: "hm" }), entry({ kind: "tool_use", tool: "Bash", text: "b" })], { live: true });
  assert.deepEqual(items.filter((i) => i.type === "activity").map((i) => i.calls[0].state), ["none", "running"]);
});

test("with tools hidden, only the conversation shows, but a card with a failure leaves its flag", () => {
  const items = chatItems(
    [
      entry({ kind: "user", text: "go" }),
      entry({ kind: "tool_use", tool: "Bash", text: "ok" }),
      entry({ kind: "tool_result", tool: "Bash", text: "fine" }),
      entry({ text: "next" }),
      entry({ kind: "tool_use", tool: "Bash", text: "bad" }),
      entry({ kind: "tool_result", tool: "Bash", text: "boom", isError: true }),
    ],
    { showTools: false },
  );
  assert.deepEqual(types(items), ["turn", "turn", "failed"]);
  assert.equal(items[2].summary, "1 tool call failed");
});

test("system notes are their own item; a new day starts with a divider", () => {
  const now = new Date(2026, 9, 21, 12, 0);
  const items = chatItems(
    [entry({ kind: "user", text: "a", at: new Date(2026, 9, 20, 17, 0) }), entry({ kind: "system", text: "Session resumed", at: new Date(2026, 9, 21, 9, 12) }), entry({ text: "b", at: new Date(2026, 9, 21, 9, 13) })],
    { now },
  );
  assert.deepEqual(types(items), ["day", "turn", "day", "system", "turn"]);
  assert.deepEqual([items[0].label, items[2].label, items[3].label, items[3].time], ["Yesterday", "Today", "Session resumed", "09:12"]);
  assert.equal(dayLabel("2026-10-21", now), "Today");
  assert.ok(dayLabel("2025-03-04", now).includes("2025"), "another year names it");
});

test("a long agent message folds; a person's never does; a cut one says so", () => {
  const long = "x".repeat(LONG_TEXT + 1);
  const [person, agent] = chatItems([entry({ kind: "user", text: long }), entry({ text: long, truncated: true })]);
  assert.deepEqual([person.parts[0].long, agent.parts[0].long, agent.parts[0].cut], [false, true, true]);
});

test("search finds entries by their text in any case, tool output only while tools show", () => {
  const entries = [entry({ kind: "user", text: "The Flaky test" }), entry({ kind: "tool_result", tool: "Bash", text: "flaky again" }), entry({ text: "fixed" })];
  assert.deepEqual(matches(entries, " FLAKY "), [entries[0].cursor, entries[1].cursor]);
  assert.deepEqual(matches(entries, "flaky", false), [entries[0].cursor]);
  assert.deepEqual(matches(entries, "  "), []);
});

test("the agent is working while the session runs and its newest entry is not an answer", () => {
  assert.equal(isWorking([entry({ kind: "user" })], true), true);
  assert.equal(isWorking([entry({ kind: "tool_result" })], true), true);
  assert.equal(isWorking([entry({ kind: "assistant" })], true), false);
  assert.equal(isWorking([entry({ kind: "user" })], false), false);
  assert.equal(isWorking([], true), false);
});
