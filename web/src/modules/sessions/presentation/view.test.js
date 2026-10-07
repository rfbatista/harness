import fixture from "../../../../testdata/views/session-status.json" with { type: "json" };
import { assert, file, test } from "../../../shared/testing/test.js";
import { needsYou } from "../domain/session.js";
import { makeSession, T0 } from "../testing/fixtures.js";
import { agentLabel, runsAs, startedBy, statusView, summary, terminalView, toDetailView, toGroupViews } from "./view.js";

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

test("an ended interactive session offers Resume when the server allows it, and says why not otherwise", () => {
  const ended = (overrides) => makeSession({ status: "stopped", resumable: false, resumeBlocked: "", ...overrides });

  const resumable = terminalView(ended({ resumable: true }));
  assert.deepEqual([resumable.terminal, resumable.resumable], ["ended", true]);
  assert.ok(resumable.terminalNote.includes("Resume it"), resumable.terminalNote);

  const noTree = terminalView(ended({ resumeBlocked: "WORKSPACE_MISSING" }));
  assert.deepEqual([noTree.terminal, noTree.resumable], ["ended", false]);
  assert.ok(noTree.terminalNote.includes("worktree was removed"), noTree.terminalNote);

  const noChat = terminalView(ended({ status: "failed", resumeBlocked: "SESSION_TRANSCRIPT_MISSING" }));
  assert.equal(noChat.resumable, false);
  assert.ok(noChat.terminalNote.includes("no conversation"), noChat.terminalNote);

  const older = terminalView(ended({}));
  assert.equal(older.resumable, false, "a server that does not say: no Resume");
  assert.ok(older.terminalNote.includes("Start a new one"), older.terminalNote);
  assert.ok(!older.terminalNote.includes("TUI"), "no longer sends the person to the TUI");
});

test("a TUI session points to the TUI while it runs, and can come back on the server once it ended", () => {
  const live = terminalView(makeSession({ runsOn: "tui", runnerHost: "laptop" }));
  assert.deepEqual([live.terminal, live.resumable], ["elsewhere", false]);
  const over = terminalView(makeSession({ runsOn: "tui", runnerHost: "laptop", status: "done", resumable: true, resumeBlocked: "" }));
  assert.deepEqual([over.terminal, over.resumable], ["ended", true]);
});

test("headless and live sessions never offer Resume", () => {
  assert.equal(terminalView(makeSession({ interactive: false, runsOn: "", status: "done", resumable: false, resumeBlocked: "SESSION_NOT_INTERACTIVE" })).resumable, false);
  assert.equal(terminalView(makeSession({ status: "running" })).resumable, false);
  // Advisory even when the server says yes: a session still alive shows its terminal.
  assert.equal(terminalView(makeSession({ status: "running", resumable: true })).resumable, false);
});

test("the detail and the row both carry whether Resume shows", () => {
  const s = makeSession({ id: "r", status: "stopped", resumable: true, resumeBlocked: "" });
  assert.equal(toDetailView(s, T0, {}).resumable, true);
  const [group] = toGroupViews([s], { selectedId: null, now: T0, agentNames: {} });
  assert.equal(group.rows[0].resumable, true);
  assert.equal(group.rows[0].resumeLabel, "Resume Port tickets screen to httpclient", "the row's button names its session");
  const [running] = toGroupViews([makeSession()], { selectedId: null, now: T0, agentNames: {} });
  assert.equal(running.rows[0].resumable, false);
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
