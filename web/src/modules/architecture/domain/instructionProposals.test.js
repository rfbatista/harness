import { assert, file, test } from "../../../shared/testing/test.js";
import {
  agoText,
  approveLabel,
  approvedText,
  fileDiff,
  fileHead,
  opSummary,
  orderProposals,
  proposalFile,
  proposalRow,
  refusalTitle,
  refusedFiles,
  rowNote,
  sessionText,
  sendBackNoteError,
  staleCountText,
  stateLabel,
  stateTone,
} from "./instructionProposals.js";

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

// ── a proposal's files and their diffs ──────────────────────────────────────

const fileDto = (over = {}) => ({
  path: ".claude/skills/verify-harness/SKILL.md",
  op: "change",
  base_version: 3,
  current_version: 3,
  stale: false,
  base_content: "# Verify\n\nStep one.\nStep two.\n",
  content: "# Verify\n\nStep one.\nStep 2.\nStep three.\n",
  ...over,
});

test("a file reads the wire shape, with null contents as empty text", () => {
  const f = proposalFile(fileDto({ op: "create", base_version: 0, current_version: 0, base_content: null }));
  assert.equal(f.op, "create");
  assert.equal(f.baseContent, "");
  assert.equal(f.baseVersion, 0);
  assert.equal(f.stale, false);
  assert.equal(proposalFile(fileDto({ op: "remove", content: null })).content, "");
});

test("a file is stale when its current version differs from the one the draft read", () => {
  assert.equal(proposalFile(fileDto({ current_version: 4, stale: true })).stale, true);
  assert.equal(proposalFile(fileDto({ current_version: 4, stale: false })).stale, true, "the versions decide, not only the flag");
  assert.equal(proposalFile(fileDto()).stale, false);
});

test("a create is all additions and a remove all deletions, whole, with no hunk rows", () => {
  const created = fileDiff(proposalFile(fileDto({ op: "create", base_version: 0, base_content: null, content: "a\nb\n" })));
  assert.deepEqual(created.rows.map((r) => [r.type, r.kind, r.old, r.new, r.text]), [
    ["diff", "add", null, 1, "a"],
    ["diff", "add", null, 2, "b"],
  ]);
  assert.equal(created.added, 2);
  assert.equal(created.deleted, 0);
  const removed = fileDiff(proposalFile(fileDto({ op: "remove", content: null, base_content: "x\n" })));
  assert.deepEqual(removed.rows.map((r) => [r.kind, r.old, r.new, r.text]), [["del", 1, null, "x"]]);
  assert.equal(removed.deleted, 1);
});

test("a change shows its changed lines with two lines of context, in hunks", () => {
  const base = Array.from({ length: 20 }, (_, i) => `line ${i + 1}`).join("\n");
  const content = base.replace("line 10", "line ten");
  const d = fileDiff(proposalFile(fileDto({ base_content: base, content })));
  assert.equal(d.added, 1);
  assert.equal(d.deleted, 1);
  const kinds = d.rows.map((r) => (r.type === "hunk" ? "hunk" : r.kind));
  assert.deepEqual(kinds, ["hunk", "context", "context", "del", "add", "context", "context"]);
  assert.equal(d.rows[0].range, "@@ -8,5 +8,5 @@");
  assert.equal(d.rows[3].old, 10);
  assert.equal(d.rows[4].new, 10);
});

test("a change with identical texts has no rows", () => {
  const d = fileDiff(proposalFile(fileDto({ base_content: "same\n", content: "same\n" })));
  assert.deepEqual(d.rows, []);
});

test("a file's head names its op, its stale state and its versions", () => {
  const stale = fileHead(proposalFile(fileDto({ current_version: 4, stale: true })), "open");
  assert.equal(stale.op, "change");
  assert.equal(stale.staleText, "stale · v3 read, v4 saved");
  assert.equal(stale.againstText, "");
  const fresh = fileHead(proposalFile(fileDto()), "open");
  assert.equal(fresh.staleText, "");
  assert.equal(fresh.againstText, "against v3");
  assert.equal(fileHead(proposalFile(fileDto({ op: "create", base_version: 0, base_content: null })), "open").againstText, "");
});

test("staleness only matters while the proposal is open", () => {
  const f = proposalFile(fileDto({ current_version: 4, stale: true }));
  assert.equal(fileHead(f, "approved").staleText, "");
  assert.equal(fileHead(f, "approved").againstText, "against v3");
});

test("the op summary counts files by op, skipping ops with none", () => {
  const files = [
    proposalFile(fileDto({ op: "create" })),
    proposalFile(fileDto({ op: "create" })),
    proposalFile(fileDto({ op: "change" })),
    proposalFile(fileDto({ op: "remove" })),
  ];
  assert.equal(opSummary(files), "2 create · 1 change · 1 remove");
  assert.equal(opSummary([proposalFile(fileDto({ op: "change" }))]), "1 change");
});

test("stale files are counted in words, and the approve button names how many files it saves", () => {
  assert.equal(staleCountText(0), "");
  assert.equal(staleCountText(1), "1 file changed since the draft read it.");
  assert.equal(staleCountText(3), "3 files changed since the draft read them.");
  assert.equal(approveLabel(7), "Approve 7 files");
  assert.equal(approveLabel(1), "Approve 1 file");
});

test("the refusal names the stale files with the versions read and saved", () => {
  const files = [
    proposalFile(fileDto({ path: "a.md", base_version: 3, current_version: 4, stale: true })),
    proposalFile(fileDto({ path: "b.md" })),
    proposalFile(fileDto({ path: "c.md", base_version: 2, current_version: 3, stale: true })),
  ];
  assert.deepEqual(refusedFiles(["a.md", "c.md"], files), [
    { path: "a.md", text: "v3 read, v4 saved" },
    { path: "c.md", text: "v2 read, v3 saved" },
  ]);
  assert.deepEqual(refusedFiles(["gone.md"], files), [{ path: "gone.md", text: "" }], "a path the pane does not know is still named");
  assert.equal(refusalTitle(2), "Approval refused: 2 files were saved since the draft read them. Nothing was saved.");
  assert.equal(refusalTitle(1), "Approval refused: 1 file was saved since the draft read it. Nothing was saved.");
});

test("an approval says what it saved: new versions and removals", () => {
  const files = [proposalFile(fileDto({ op: "create" })), proposalFile(fileDto({ op: "change" })), proposalFile(fileDto({ op: "remove" }))];
  assert.equal(approvedText(files), "2 files saved as new versions, 1 removed. Sessions you start from now on receive them.");
  assert.equal(approvedText([proposalFile(fileDto({ op: "create" }))]), "1 file saved as a new version. Sessions you start from now on receive them.");
});

test("a send back needs a note with something in it; a reject needs none", () => {
  assert.equal(sendBackNoteError(""), "A note is required: tell the session what to change.");
  assert.equal(sendBackNoteError("  \n "), "A note is required: tell the session what to change.");
  assert.equal(sendBackNoteError("Wait for the server."), "");
});
