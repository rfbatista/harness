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
