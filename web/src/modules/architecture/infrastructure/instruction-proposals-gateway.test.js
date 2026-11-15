import { StructuredError } from "../../../shared/domain/errors.js";
import { apiClient } from "../../../shared/infrastructure/api.js";
import { fakeFetch, jsonResponse } from "../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { instructionProposalsGateway } from "./instruction-proposals-gateway.js";

file("architecture/infrastructure/instruction-proposals-gateway");

const proposal = {
  id: "p1",
  repository_id: "r1",
  state: "open",
  summary: "Verification skill",
  skills: ["verify-harness"],
  files: 2,
  task: { id: "t1", title: "Verification Skill" },
  session: { id: "s1", agent: "architect", mode: "architect" },
  created_at: "2026-11-15T11:00:00Z",
  decided_at: null,
  note: null,
};

function rig(answer, followed = []) {
  const { fetch, calls } = fakeFetch((url, init) => answer(url.replace("/api/", ""), JSON.parse(init.body ?? "null")));
  const feed = { follow: (path, handlers) => (followed.push({ path, handlers }), () => followed.push("stopped")) };
  const gateway = instructionProposalsGateway(apiClient({ base: "/api", fetch }), feed);
  const sent = () => calls.map((c) => [c.url.replace("/api/", ""), JSON.parse(c.init.body)]);
  return { gateway, sent, followed };
}

test("list sends the repository and reads the proposals and the open count", async () => {
  const { gateway, sent } = rig(() => jsonResponse(200, { proposals: [proposal], open_count: 1 }));
  const out = await gateway.list("r1");
  assert.deepEqual(sent(), [["list_instruction_proposals", { repository_id: "r1" }]]);
  assert.equal(out.openCount, 1);
  assert.equal(out.proposals.length, 1);
  assert.equal(out.proposals[0].id, "p1");
  assert.equal(out.proposals[0].task.title, "Verification Skill");
});

test("list tolerates a missing list and count", async () => {
  const out = await rig(() => jsonResponse(200, {})).gateway.list("r1");
  assert.deepEqual(out, { proposals: [], openCount: 0 });
});

test("a refusal arrives with its code", async () => {
  const { gateway } = rig(() => jsonResponse(404, { error: "no such repository", code: "REPOSITORY_NOT_FOUND" }));
  try {
    await gateway.list("nope");
    assert.ok(false, "expected a refusal");
  } catch (err) {
    assert.ok(err instanceof StructuredError);
    assert.equal(err.code, "REPOSITORY_NOT_FOUND");
  }
});

test("follow reads the repository's instruction_proposal_changed events and ignores the others", () => {
  const followed = [];
  const { gateway } = rig(() => jsonResponse(200, {}), followed);
  const seen = [];
  const stop = gateway.follow("proj", "r1", { onChanged: (c) => seen.push(c) });
  assert.equal(followed[0].path, "/events?project_id=proj");
  const { onMessage } = followed[0].handlers;
  onMessage({ instruction_proposal_changed: { repository_id: "r1", proposal_id: "p1", state: "open", open_count: 2 } });
  onMessage({ instruction_proposal_changed: { repository_id: "other", proposal_id: "p9", state: "open", open_count: 5 } });
  onMessage({ instruction_file_changed: { repository_id: "r1", path: "x", version: 1 } });
  assert.deepEqual(seen, [{ proposalId: "p1", state: "open", openCount: 2 }]);
  stop();
  assert.equal(followed.at(-1), "stopped");
});

const wireFile = {
  path: ".claude/skills/verify-harness/SKILL.md",
  op: "change",
  base_version: 3,
  current_version: 4,
  stale: true,
  base_content: "old\n",
  content: "new\n",
};

test("get sends the proposal id and reads the proposal with its files", async () => {
  const { gateway, sent } = rig(() => jsonResponse(200, { proposal, files: [wireFile] }));
  const out = await gateway.get("p1");
  assert.deepEqual(sent(), [["get_instruction_proposal", { proposal_id: "p1" }]]);
  assert.equal(out.proposal.id, "p1");
  assert.equal(out.files.length, 1);
  assert.equal(out.files[0].op, "change");
  assert.equal(out.files[0].stale, true);
  assert.equal(out.files[0].baseContent, "old\n");
});

test("approve sends the proposal id and reads the decided proposal and the saved files", async () => {
  const { gateway, sent } = rig(() =>
    jsonResponse(200, { proposal: { ...proposal, state: "approved" }, files: [{ path: "CLAUDE.md", kind: "memory", version: 2, size: 4, updated_at: "2026-11-15T11:00:00Z" }] }),
  );
  const out = await gateway.approve("p1");
  assert.deepEqual(sent(), [["approve_instruction_proposal", { proposal_id: "p1" }]]);
  assert.equal(out.proposal.state, "approved");
  assert.deepEqual(out.saved, [{ path: "CLAUDE.md", version: 2 }]);
});

test("an INSTRUCTION_CHANGED refusal keeps the stale paths", async () => {
  const { gateway } = rig(() => jsonResponse(409, { error: "changed", code: "INSTRUCTION_CHANGED", details: { paths: ["a.md", "b.md"] } }));
  try {
    await gateway.approve("p1");
    assert.ok(false, "expected a refusal");
  } catch (err) {
    assert.equal(err.code, "INSTRUCTION_CHANGED");
    assert.deepEqual(err.paths, ["a.md", "b.md"]);
  }
});

test("a get that is not a proposal is a BAD_RESPONSE", async () => {
  const { gateway } = rig(() => jsonResponse(200, { nope: true }));
  try {
    await gateway.get("p1");
    assert.ok(false, "expected a refusal");
  } catch (err) {
    assert.equal(err.code, "BAD_RESPONSE");
  }
});

test("reject sends the note only when there is one", async () => {
  const { gateway, sent } = rig(() => jsonResponse(200, { proposal: { ...proposal, state: "rejected", note: "No." } }));
  const out = await gateway.reject("p1", "No.");
  await gateway.reject("p1", "");
  assert.deepEqual(sent(), [
    ["reject_instruction_proposal", { proposal_id: "p1", note: "No." }],
    ["reject_instruction_proposal", { proposal_id: "p1" }],
  ]);
  assert.equal(out.proposal.state, "rejected");
  assert.equal(out.proposal.note, "No.");
});

test("send back sends the proposal id and the note", async () => {
  const { gateway, sent } = rig(() => jsonResponse(200, { proposal: { ...proposal, state: "sent_back", note: "Wait for the server." } }));
  const out = await gateway.sendBack("p1", "Wait for the server.");
  assert.deepEqual(sent(), [["send_back_instruction_proposal", { proposal_id: "p1", note: "Wait for the server." }]]);
  assert.equal(out.proposal.state, "sent_back");
});

test("a decided proposal's refusal arrives with its code", async () => {
  const { gateway } = rig(() => jsonResponse(409, { error: "decided", code: "PROPOSAL_DECIDED" }));
  try {
    await gateway.reject("p1", "");
    assert.ok(false, "expected a refusal");
  } catch (err) {
    assert.equal(err.code, "PROPOSAL_DECIDED");
  }
});
