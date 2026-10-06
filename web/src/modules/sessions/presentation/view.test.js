import fixture from "../../../../testdata/views/session-status.json" with { type: "json" };
import { assert, file, test } from "../../../shared/testing/test.js";
import { needsYou } from "../domain/session.js";
import { makeSession, T0 } from "../testing/fixtures.js";
import { agentLabel, runsAs, startedBy, statusView, summary, terminalView, toGroupViews } from "./view.js";

file("sessions/presentation/view");

test("status words match the shared fixture (parity with the BFF)", () => {
  for (const c of fixture.cases) {
    const session = makeSession({ status: c.status, pendingApprovals: c.pending_approvals });
    const label = `${c.status}+${c.pending_approvals}`;
    assert.deepEqual(statusView(session), { state: c.state, word: c.word }, label);
    assert.equal(needsYou(session), c.needs_you, `${label} needs_you`);
  }
});

test("group views carry labels, counts and the selection", () => {
  const now = new Date(T0.getTime() + 5 * 60_000);
  const groups = toGroupViews(
    [makeSession({ id: "a", status: "idle" }), makeSession({ id: "b", task: "" })],
    { selectedId: "b", now, agentNames: {} },
  );
  assert.deepEqual(groups.map((g) => `${g.label}:${g.count}`), ["Needs you:1", "Running:1"]);
  assert.deepEqual(groups.map((g) => g.tone), ["attention", null]);
  const row = groups[1].rows[0];
  assert.deepEqual([row.title, row.meta, row.selected, row.attention], ["Untitled session", "backend · 5m", true, false]);
});

test("the detail pane attaches only to a live terminal on the server, and says why otherwise", () => {
  assert.equal(terminalView(makeSession({ status: "idle" })).terminal, "attach");
  assert.equal(terminalView(makeSession({ status: "done" })).terminal, "ended");
  const tui = terminalView(makeSession({ runsOn: "tui", runnerHost: "laptop" }));
  assert.equal(tui.terminal, "elsewhere");
  assert.ok(tui.terminalNote.includes("laptop"));
  const headless = terminalView(makeSession({ interactive: false, runsOn: "", lastAction: "edited go.mod" }));
  assert.equal(headless.terminal, "headless");
  assert.ok(headless.terminalNote.includes("edited go.mod"));
});

test("an agent reads as its name, its id when unknown, plain claude without one", () => {
  assert.equal(agentLabel("a1", { a1: "Reviewer" }), "Reviewer");
  assert.equal(agentLabel("gone", { a1: "Reviewer" }), "gone");
  assert.equal(agentLabel("", {}), "plain claude");
});

test("a session started in a mode reads as its agent in that mode", () => {
  assert.equal(runsAs(makeSession({ agentId: "a1" }), { a1: "Reviewer" }), "Reviewer");
  assert.equal(runsAs(makeSession({ agentId: "", mode: "architect" })), "plain claude as architect");
  assert.equal(runsAs(makeSession({ agentId: "a1", mode: "design" }), { a1: "Reviewer" }), "Reviewer as design");
});

test("summary counts what is waiting", () => {
  assert.equal(summary([makeSession({ status: "idle" }), makeSession({ id: "b" })]), "2 sessions · 1 waiting");
  assert.equal(summary([makeSession()]), "1 session");
});

test("who started a session: its parent's agent, another session, or nobody", () => {
  const lead = makeSession({ id: "lead", agentId: "backend" });
  const peer = makeSession({ id: "peer", parentSessionId: "lead" });
  const orphan = makeSession({ id: "orphan", parentSessionId: "gone" });
  const names = { backend: "Backend dev" };
  assert.equal(startedBy(peer, [lead, peer], names), "Backend dev");
  assert.equal(startedBy(orphan, [lead], names), "another session");
  assert.equal(startedBy(lead, [lead], names), "");
  const [row] = toGroupViews([peer, lead], { selectedId: null, now: T0, agentNames: names })
    .flatMap((g) => g.rows)
    .filter((r) => r.id === "peer");
  assert.ok(row.meta.startsWith("Backend dev · started by Backend dev · "), row.meta);
});
