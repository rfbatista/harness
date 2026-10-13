import { assert, file, test } from "../../../shared/testing/test.js";
import { historyLine } from "./historyView.js";

file("sessions/presentation/historyView");

const entry = (fields) => ({ cursor: "c1", at: null, kind: "assistant", text: "", tool: "", isError: false, truncated: false, ...fields });

test("people and the agent read in full; the time is the local HH:MM", () => {
  const at = new Date(2026, 9, 13, 9, 5, 30);
  const user = historyLine(entry({ kind: "user", text: "first\nsecond", at }));
  assert.deepEqual([user.kind, user.mark, user.time, user.fold, user.text], ["user", "›", "09:05", false, "first\nsecond"]);
  assert.equal(user.stamp, at.toLocaleString());
  const agent = historyLine(entry({ kind: "assistant", text: "<img src=x onerror=alert(1)>" }));
  assert.deepEqual([agent.kind, agent.mark, agent.time, agent.text], ["agent", "⏺", "", "<img src=x onerror=alert(1)>"]);
  assert.deepEqual([historyLine(entry({ kind: "system", text: "compacted" })).kind, historyLine(entry({ kind: "system" })).mark], ["system", "·"]);
});

test("a tool call is one line: its tool and the call's summary", () => {
  const call = historyLine(entry({ kind: "tool_use", tool: "Bash", text: "git status" }));
  assert.deepEqual([call.kind, call.mark, call.fold, call.tool, call.gist, call.meta, call.text], ["tool", "⌁", true, "Bash", "git status", "", "git status"]);
});

test("a result folds to its first line and size; a failed one is an error", () => {
  const out = historyLine(entry({ kind: "tool_result", tool: "Bash", text: "\n M a.js\n M b.js" }));
  assert.deepEqual([out.kind, out.mark, out.gist, out.meta], ["tool", "↳", "M a.js", "3 lines"]);
  const failed = historyLine(entry({ kind: "tool_result", tool: "Bash", text: "exit 1", isError: true }));
  assert.deepEqual([failed.kind, failed.mark, failed.gist, failed.meta], ["error", "✕", "exit 1", ""]);
  assert.equal(historyLine(entry({ kind: "tool_result", text: "" })).gist, "no output");
});

test("text the server cut says so", () => {
  assert.equal(historyLine(entry({ truncated: true })).cut, true);
  assert.equal(historyLine(entry({})).cut, false);
});
