import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { makeState, T0, UPSTREAMS } from "../testing/fixtures.js";
import { chipOf, fetchAge, fetchedPhrase, freshLine, panelOf, pushLabel, pushQuestion, refusalOf, uncommittedLine, upstreamLabel } from "./view.js";

file("sync/presentation/view");

const minutesAgo = (n) => new Date(T0.getTime() - n * 60000);
const chip = (upstream, o = {}) => chipOf(makeState({ upstream, ...o }), { now: T0 });

test("every upstream state gives the chip the study's words", () => {
  const read = (c) => [c.state, c.upstream, c.hasCounts ? `↑${c.ahead} ↓${c.behind}` : "", c.word];
  assert.deepEqual(read(chip("in_sync")), ["in-sync", "origin", "", "in sync"]);
  assert.deepEqual(read(chip("ahead")), ["ahead", "origin", "↑3 ↓0", ""]);
  assert.deepEqual(read(chip("behind")), ["behind", "origin", "↑0 ↓2", ""]);
  assert.deepEqual(read(chip("diverged")), ["diverged", "origin", "↑3 ↓2", ""]);
  assert.deepEqual(read(chip("none")), ["none", "", "", "not pushed"]);
  assert.deepEqual(read(chip("gone")), ["gone", "origin", "", "upstream deleted"]);
  assert.deepEqual(read(chip("unknown")), ["unknown", "origin", "", "not fetched"]);
});

test("the chip shows the remote alone, and the full ref when the upstream is not origin/<branch>", () => {
  assert.equal(upstreamLabel(makeState({ upstream: "ahead" })), "origin");
  const other = { state: "ahead", ref: "origin/main", remote: "origin", ahead: 1, behind: 0 };
  assert.equal(upstreamLabel(makeState({ upstream: other })), "origin/main");
  assert.equal(chip(other).title, "origin/main");
  assert.equal(chip("ahead").title, "origin/agent/sync-1234", "the full ref is the title");
  assert.equal(chip("none").title, "no upstream");
  assert.equal(upstreamLabel(makeState({ upstream: "none" })), "");
});

test("a fetch or a push in flight puts its word on the chip and keeps the counts", () => {
  const c = chipOf(makeState({ upstream: "ahead" }), { now: T0, busy: "fetching" });
  assert.deepEqual([c.word, c.loading, c.hasCounts, c.ahead], ["Fetching…", true, true, 3]);
  assert.equal(chipOf(makeState({ upstream: "ahead" }), { now: T0, busy: "pushing" }).word, "Pushing…");
  assert.equal(chip("ahead").loading, false);
});

test("the fetch age reads in minutes, hours and days, and is amber only past an hour", () => {
  const age = (n) => fetchAge(minutesAgo(n), T0);
  assert.deepEqual([age(0).ago, age(12).ago, age(59).ago, age(60).ago, age(180).ago, age(24 * 60).ago, age(3 * 24 * 60).ago], ["just now", "12 min ago", "59 min ago", "1 h ago", "3 h ago", "1 day ago", "3 days ago"]);
  assert.deepEqual([age(59).old, age(60).old, age(61).old], [false, false, true]);
  assert.deepEqual([fetchAge(null, T0).never, fetchAge(null, T0).old], [true, true]);
  assert.equal(freshLine(minutesAgo(12), T0), "Remote last fetched 12 min ago");
  assert.equal(freshLine(null, T0), "Remote not fetched yet");
  assert.equal(fetchedPhrase(minutesAgo(12), T0), "fetched 12 min ago");
});

test("a branch pushed on a never fetched repository reads as pushed, not as unknown", () => {
  assert.equal(freshLine(null, T0, minutesAgo(3)), "Pushed 3 min ago · never fetched");
  assert.equal(fetchedPhrase(null, T0, minutesAgo(3)), "pushed 3 min ago · never fetched");
  assert.equal(freshLine(minutesAgo(12), T0, minutesAgo(3)), "Remote last fetched 12 min ago", "both set: the fetch age as before");
  assert.deepEqual([fetchAge(null, T0, minutesAgo(3)).old, fetchAge(null, T0, minutesAgo(90)).old], [false, true]);
  const state = makeState({ upstream: "in_sync", fetchedAt: null, pushedAt: minutesAgo(3) });
  assert.deepEqual([chipOf(state, { now: T0 }).word, panelOf(state, { now: T0 }).fresh.line], ["in sync", "Pushed 3 min ago · never fetched"]);
});

test("counts are underlined as old only when there are counts and the fetch is over an hour old", () => {
  assert.equal(chip("ahead", { fetchedAt: minutesAgo(180) }).old, true);
  assert.equal(chip("ahead", { fetchedAt: minutesAgo(12) }).old, false);
  assert.equal(chip("in_sync", { fetchedAt: minutesAgo(180) }).old, false, "no counts to underline");
});

test("the chip describes itself in words for a screen reader", () => {
  assert.equal(chip("ahead").describe, "Upstream origin/agent/sync-1234: 3 commits ahead, 0 commits behind, fetched 12 min ago.");
  assert.equal(chip("none", { fetchedAt: null }).describe, "No upstream: not pushed.");
});

test("the push button and its question name the commits and the remote branch", () => {
  assert.equal(pushLabel(makeState({ upstream: "ahead" })), "Push 3 commits");
  assert.equal(pushLabel(makeState({ upstream: { ...UPSTREAMS.ahead, ahead: 1 } })), "Push 1 commit");
  assert.equal(pushQuestion(makeState({ upstream: "ahead" })), "Push 3 commits to origin/agent/sync-1234?");
  const unpushed = makeState({ upstream: "none", base: { ref: "main", ahead: 2, behind: 0 } });
  assert.equal(pushQuestion(unpushed), "Push 2 commits to origin/agent/sync-1234?");
  assert.equal(pushQuestion(makeState({ upstream: "none", base: { ref: "main", gone: true, ahead: 0, behind: 0 } })), "Push the branch to origin/agent/sync-1234?");
});

test("the panel has the branch and short commit, a row per comparison, the age and Push only when offered", () => {
  const p = panelOf(makeState({ upstream: "ahead", fetchedAt: minutesAgo(180) }), { now: T0 });
  assert.deepEqual([p.branch, p.commit], ["agent/sync-1234", "ab6f51b"]);
  assert.deepEqual([p.base.prefix, p.base.ref, p.base.chip.state, p.base.chip.ahead, p.base.chip.behind], ["vs base", "main", "diverged", 3, 8]);
  assert.deepEqual([p.upstream.prefix, p.upstream.ref, p.upstream.chip.state], ["vs upstream", "origin/agent/sync-1234", "ahead"]);
  assert.deepEqual([p.fresh.line, p.fresh.old, p.offersPush, p.pushLabel], ["Remote last fetched 3 h ago", true, true, "Push 3 commits"]);
  const behind = panelOf(makeState({ upstream: "behind" }), { now: T0 });
  assert.deepEqual([behind.offersPush, behind.question], [false, ""]);
});

test("the panel reads Fetching… and Pushing… on its buttons while busy", () => {
  const fetching = panelOf(makeState({ upstream: "ahead" }), { now: T0, busy: "fetching" });
  assert.deepEqual([fetching.busy, fetching.fetchLabel, fetching.pushLabel], [true, "Fetching…", "Push 3 commits"]);
  const pushing = panelOf(makeState({ upstream: "ahead" }), { now: T0, busy: "pushing" });
  assert.deepEqual([pushing.fetchLabel, pushing.pushLabel], ["↻ Fetch", "Pushing…"]);
});

test("the base row reads in sync, missing or by its counts", () => {
  assert.equal(panelOf(makeState({ base: { ref: "main", ahead: 0, behind: 0 } }), { now: T0 }).base.chip.word, "in sync");
  assert.equal(panelOf(makeState({ base: { ref: "main", gone: true, ahead: 0, behind: 0 } }), { now: T0 }).base.chip.state, "gone");
  assert.deepEqual([panelOf(makeState({ base: { ref: "main", gone: true, ahead: 0, behind: 0 } }), { now: T0 }).base.chip.word, panelOf(makeState({ base: { ref: "main", gone: true, ahead: 0, behind: 0 } }), { now: T0 }).base.hint], ["base gone", "The base branch main no longer exists."]);
});

test("uncommitted work reads 3 changed · 2 new, or clean", () => {
  assert.equal(uncommittedLine({ tracked: 3, untracked: 2 }), "3 changed · 2 new");
  assert.equal(uncommittedLine({ tracked: 0, untracked: 2 }), "2 new");
  assert.equal(uncommittedLine({ tracked: 3, untracked: 0 }), "3 changed");
  assert.equal(uncommittedLine({ tracked: 0, untracked: 0 }), "clean");
});

const refused = (code, details, reason = "git says no") => new StructuredError(code, `${code}: ${reason}`, 409, details);

test("a credential error names the host and links to the credentials page, with no reason and no code", () => {
  const missing = refusalOf(refused(Codes.GIT_CREDENTIAL_MISSING, { host: "github.com", kind: "https_token" }, "secret-looking text"));
  assert.deepEqual(missing, { tone: "banner", lead: "No credential stored for github.com.", reason: "", code: "", href: "/git-credentials?host=github.com&kind=https_token", linkText: "Add one in Git credentials" });
  assert.equal(refusalOf(refused(Codes.GIT_CREDENTIAL_MISSING, { host: "h", kind: "ssh_key" })).lead, "No SSH key stored for h.");
  const rejected = refusalOf(refused(Codes.GIT_CREDENTIAL_REJECTED, { host: "github.com", credential_id: "c1" }, "Bad credentials"));
  assert.deepEqual([rejected.tone, rejected.lead, rejected.reason, rejected.code, rejected.href], ["banner", "github.com refused the stored credential.", "", "", "/git-credentials?credential=c1"]);
  const locked = refusalOf(refused(Codes.GIT_CREDENTIAL_STORE_LOCKED));
  assert.deepEqual([locked.tone, locked.reason, locked.href], ["banner", "", "/git-credentials"]);
});

test("a push refused because the branch is behind or diverged says which, and nothing is forced", () => {
  const rejected = (upstream) => refusalOf(refused(Codes.PUSH_REJECTED, { state: makeState({ upstream }) }, "! [rejected] (non-fast-forward)"));
  const behind = rejected("behind");
  assert.deepEqual([behind.tone, behind.code, behind.lead, behind.reason], ["danger", "PUSH_REJECTED", "The remote refused the push: ! [rejected] (non-fast-forward). Nothing was pushed.", ""]);
  assert.equal(refusalOf(refused(Codes.PUSH_REJECTED, undefined, "pre-receive hook declined\nsecond line")).lead, "The remote refused the push: pre-receive hook declined. Nothing was pushed.");
  for (const r of [behind]) assert.ok(!/force/i.test(r.lead), "no force is offered or named");
});

test("each other refusal code maps to its words and keeps its code", () => {
  const of = (code, reason) => refusalOf(refused(code, undefined, reason));
  assert.deepEqual([of(Codes.PUSH_STALE).lead, of(Codes.PUSH_STALE).code], ["The branch moved since you confirmed. Check the new commits and push again.", "PUSH_STALE"]);
  assert.deepEqual([of(Codes.PUSH_FAILED, "remote hung up").lead, of(Codes.PUSH_FAILED, "remote hung up").reason], ["The push failed: remote hung up.", ""]);
  assert.deepEqual([of(Codes.FETCH_FAILED, "no route").lead, of(Codes.FETCH_FAILED, "no route").reason], ["The fetch failed: no route. Counts are as of the last fetch.", ""]);
  assert.equal(of(Codes.NO_REMOTE).lead, "This repository has no origin remote.");
  assert.equal(of(Codes.WORKSPACE_MISSING).lead, "The worktree is gone, so its branch cannot be pushed from here.");
  assert.equal(of(Codes.UNAVAILABLE).lead, "Git actions are not available on this server.");
  assert.equal(of(Codes.PUSH_STALE).tone, "danger");
});

test("an error the module does not know is described as the app describes any error", () => {
  const r = refusalOf(new StructuredError(Codes.NETWORK, "The harness server is not reachable."));
  assert.deepEqual([r.tone, r.lead, r.code], ["danger", "The harness server is not reachable.", "NETWORK"]);
});
