import views from "../../../../testdata/views/pull-request.json" with { type: "json" };
import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { makeDraft, makePullRequest } from "../testing/fixtures.js";
import { blockerLink, blockerText, reasonOf, refusalOf, summaryLine, toChip, uncommittedLine } from "./view.js";

file("pullrequests/presentation/view");

const draft = makeDraft({ headBranch: views.draft.head_branch, baseBranch: views.draft.base_branch });

test("each blocker reads, and links to its fix, as the Go page paints it", () => {
  for (const c of views.blockers) {
    const d = { ...draft, remote: c.remote ?? null };
    assert.equal(blockerText(c.code, d), c.text, c.code);
    assert.equal(blockerLink(c.code, d)?.href ?? "", c.href ?? "", `${c.code} link`);
  }
});

test("a credential refusal names the host and links to where it is fixed", () => {
  const missing = refusalOf(new StructuredError(Codes.GIT_CREDENTIAL_MISSING, "GIT_CREDENTIAL_MISSING: no token resolves for github.com", 400, { host: "github.com", kind: "https_token" }));
  assert.deepEqual(missing, { lead: "No token stored for github.com.", reason: "no token resolves for github.com", code: Codes.GIT_CREDENTIAL_MISSING, href: "/git-credentials?host=github.com&kind=https_token", linkText: "Add one in Git credentials" });
  const key = refusalOf(new StructuredError(Codes.GIT_CREDENTIAL_MISSING, "GIT_CREDENTIAL_MISSING: x", 400, { host: "github.com", kind: "ssh_key" }));
  assert.equal(key.lead, "No SSH key stored for github.com.");
  const rejected = refusalOf(new StructuredError(Codes.GIT_CREDENTIAL_REJECTED, "GIT_CREDENTIAL_REJECTED: Bad credentials", 502, { host: "github.com", credential_id: "gc-1" }));
  assert.deepEqual([rejected.lead, rejected.reason, rejected.href], ["github.com refused the stored credential.", "Bad credentials", "/git-credentials?credential=gc-1"]);
  const locked = refusalOf(new StructuredError(Codes.GIT_CREDENTIAL_STORE_LOCKED, "GIT_CREDENTIAL_STORE_LOCKED: no key", 503));
  assert.deepEqual([locked.code, locked.href], [Codes.GIT_CREDENTIAL_STORE_LOCKED, "/git-credentials"]);
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
