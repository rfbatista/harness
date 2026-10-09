import views from "../../../../testdata/views/pull-request.json" with { type: "json" };
import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { makeDraft, makePullRequest } from "../testing/fixtures.js";
import { blockerText, reasonOf, refusalOf, summaryLine, toChip, uncommittedLine } from "./view.js";

file("pullrequests/presentation/view");

const draft = makeDraft({ headBranch: views.draft.head_branch, baseBranch: views.draft.base_branch });

test("each blocker reads as the Go page paints it", () => {
  for (const c of views.blockers) assert.equal(blockerText(c.code, draft), c.text, c.code);
});

test("each state reads and tones as the Go page paints it", () => {
  for (const c of views.chips) {
    const chip = toChip(makePullRequest({ state: c.state, draft: c.draft }));
    assert.deepEqual([chip.state, chip.tone], [c.shown, c.tone], `${c.state} draft=${c.draft}`);
  }
  assert.deepEqual([toChip(makePullRequest()).label, toChip(makePullRequest()).short], ["#42", "PR #42"]);
});

test("the summary and the uncommitted warning read as the Go page paints them", () => {
  for (const c of views.summaries) assert.equal(summaryLine({ ...draft, ahead: c.ahead, remote: c.remote }), c.text);
  for (const c of views.uncommitted) assert.equal(uncommittedLine({ ...draft, uncommittedChanges: c.count }), c.text);
  assert.equal(summaryLine({ ...draft, ahead: 2, remote: null }, "develop"), "2 commits from agent/feed into develop", "the base the person typed");
});

test("a refusal leads with what happened and keeps the server's reason without its code", () => {
  const err = new StructuredError(Codes.PR_PUSH_FAILED, "PR_PUSH_FAILED: ! [rejected] (non-fast-forward)", 502);
  assert.equal(reasonOf(err), "! [rejected] (non-fast-forward)");
  assert.deepEqual(refusalOf(err), { lead: "Git refused the push.", reason: "! [rejected] (non-fast-forward)", code: Codes.PR_PUSH_FAILED });
  const net = refusalOf(new StructuredError(Codes.NETWORK, "The harness server is not reachable."));
  assert.deepEqual([net.lead, net.code], ["The harness server is not reachable.", Codes.NETWORK]);
});
