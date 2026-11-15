import { assert, file, test } from "../../../shared/testing/test.js";
import { agoText, orderProposals, proposalRow, rowNote, sessionText, stateLabel, stateTone } from "./instructionProposals.js";

file("architecture/domain/instructionProposals");

const NOW = new Date("2026-11-15T12:00:00Z");
const at = (minutesAgo) => new Date(NOW.getTime() - minutesAgo * 60_000).toISOString();

const dto = (over = {}) => ({
  id: "p1",
  repository_id: "r1",
  state: "open",
  summary: "Verification skill",
  skills: ["verify-harness", "features-harness"],
  files: 7,
  task: { id: "t1", title: "Verification Skill" },
  session: { id: "s1", agent: "software-architect", mode: "architect" },
  created_at: at(25),
  decided_at: null,
  note: null,
  ...over,
});

test("each state has a label and a tone: attention for open, success for approved, danger for rejected, none for sent back", () => {
  assert.equal(stateLabel("open"), "Open");
  assert.equal(stateLabel("approved"), "Approved");
  assert.equal(stateLabel("rejected"), "Rejected");
  assert.equal(stateLabel("sent_back"), "Sent back");
  assert.equal(stateTone("open"), "attention");
  assert.equal(stateTone("approved"), "success");
  assert.equal(stateTone("rejected"), "danger");
  assert.equal(stateTone("sent_back"), "");
});

test("an unknown state is shown as it came, with no tone", () => {
  assert.equal(stateLabel("parked"), "parked");
  assert.equal(stateTone("parked"), "");
});

test("a proposal row reads the wire shape", () => {
  const row = proposalRow(dto());
  assert.equal(row.id, "p1");
  assert.equal(row.state, "open");
  assert.deepEqual(row.skills, ["verify-harness", "features-harness"]);
  assert.equal(row.files, 7);
  assert.deepEqual(row.task, { id: "t1", title: "Verification Skill" });
  assert.equal(row.note, "");
  assert.equal(row.decidedAt, "");
});

test("a row tolerates missing task, session and skills", () => {
  const row = proposalRow({ id: "p2", state: "open" });
  assert.deepEqual(row.skills, []);
  assert.equal(row.task, null);
  assert.equal(row.session, null);
  assert.equal(row.files, 0);
});

test("open proposals come first, each group newest first", () => {
  const rows = [
    proposalRow(dto({ id: "old-decided", state: "approved", created_at: at(5000) })),
    proposalRow(dto({ id: "new-decided", state: "rejected", created_at: at(3000) })),
    proposalRow(dto({ id: "old-open", created_at: at(120) })),
    proposalRow(dto({ id: "new-open", created_at: at(25) })),
  ];
  assert.deepEqual(orderProposals(rows).map((r) => r.id), ["new-open", "old-open", "new-decided", "old-decided"]);
});

test("ago words: just now, minutes, hours, yesterday, days", () => {
  assert.equal(agoText(at(0), NOW), "just now");
  assert.equal(agoText(at(25), NOW), "25 min ago");
  assert.equal(agoText(at(120), NOW), "2 h ago");
  assert.equal(agoText(at(60 * 30), NOW), "yesterday");
  assert.equal(agoText(at(60 * 24 * 3), NOW), "3 days ago");
});

test("the row note names the files and when it was proposed, and when it was decided", () => {
  assert.equal(rowNote(proposalRow(dto()), NOW), "7 files · proposed 25 min ago");
  assert.equal(rowNote(proposalRow(dto({ files: 1 })), NOW), "1 file · proposed 25 min ago");
  const decided = proposalRow(dto({ state: "sent_back", created_at: at(60 * 30), decided_at: at(60 * 25) }));
  assert.equal(rowNote(decided, NOW), "7 files · proposed yesterday · sent back yesterday");
});

test("the session is its agent and mode", () => {
  assert.equal(sessionText(proposalRow(dto())), "software-architect · architect");
  assert.equal(sessionText(proposalRow({ id: "x", state: "open" })), "");
});
