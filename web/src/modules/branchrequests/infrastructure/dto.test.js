import { assert, file, test } from "../../../shared/testing/test.js";
import { isPending, upsertRequest } from "../domain/request.js";
import { toRequest, toRequestEvent, toRequests } from "./dto.js";
import { detailLineOf, startLineOf, titleOf } from "../presentation/view.js";

file("branchrequests/infrastructure/dto");

const wire = (over = {}) => ({
  id: "r1",
  session_id: "s1",
  task_id: "t1",
  repository_id: "repo1",
  session_branch: "agent/work",
  action: "create",
  branch: "feature-x",
  new_name: null,
  start_point: { kind: "branch", ref: "agent/work", commit: "0123456789abcdef" },
  command: "git checkout -b feature-x",
  via: "git",
  reason: "to try it",
  state: "pending",
  note: "",
  requested_at: "2026-11-07T10:00:00Z",
  answered_at: null,
  ...over,
});

test("a request, a list and a feed message read the same way", () => {
  const r = toRequest(wire());
  assert.equal(r.branch, "feature-x");
  assert.equal(isPending(r), true);
  assert.equal(toRequests({ requests: [wire(), wire({ id: "r2" })] }).length, 2);
  assert.equal(toRequestEvent({ branch_request: wire() }).request.id, "r1");
  assert.equal(toRequestEvent({ session: {} }), null);
});

test("a request with an unknown state or no id is refused", () => {
  const refuses = (fn) => {
    try {
      fn();
    } catch {
      return true;
    }
    return false;
  };
  assert.ok(refuses(() => toRequest(wire({ state: "maybe" }))));
  assert.ok(refuses(() => toRequest(wire({ id: "" }))));
});

test("requests stay newest first and a changed one replaces itself", () => {
  let list = upsertRequest([], toRequest(wire({ id: "a", requested_at: "2026-11-07T10:00:00Z" })));
  list = upsertRequest(list, toRequest(wire({ id: "b", requested_at: "2026-11-07T10:05:00Z" })));
  list = upsertRequest(list, toRequest(wire({ id: "a", requested_at: "2026-11-07T10:00:00Z", state: "approved" })));
  assert.deepEqual(list.map((r) => `${r.id}:${r.state}`), ["b:pending", "a:approved"]);
});

test("a card says the action, the branch and what it starts from", () => {
  assert.equal(titleOf(toRequest(wire())), "Create branch feature-x");
  assert.equal(titleOf(toRequest(wire({ action: "rename", branch: "a", new_name: "b" }))), "Rename branch a to b");
  assert.equal(titleOf(toRequest(wire({ action: "delete", branch: "old" }))), "Delete branch old");
  assert.equal(startLineOf(toRequest(wire())), "Starts from branch agent/work (01234567)");
});

test("a push card says the branch, the remote with its URL, the remote branch and the commits", () => {
  const push = toRequest(
    wire({
      action: "push",
      branch: "agent/work",
      start_point: null,
      remote: { name: "origin", url: "git@github.com:acme/widgets.git", new_name: null },
      remote_branch: "review/x",
      commits: 3,
      via: "push_branch",
    }),
  );
  assert.equal(titleOf(push), "Push branch agent/work");
  assert.equal(detailLineOf(push), "To origin (git@github.com:acme/widgets.git) as review/x · 3 commits");
  assert.equal(startLineOf(push), "");
  const one = toRequest(wire({ action: "push", branch: "b", remote: { name: "origin", url: "/o.git", new_name: null }, remote_branch: "b", commits: 1 }));
  assert.equal(detailLineOf(one), "To origin (/o.git) as b · 1 commit");
  const unknown = toRequest(wire({ action: "push", branch: "b", remote: { name: "origin", url: "/o.git", new_name: null }, remote_branch: "b", commits: null }));
  assert.equal(detailLineOf(unknown), "To origin (/o.git) as b");
});

test("a remote card says the remote and the new URL or name", () => {
  const remote = (action, over = {}) => toRequest(wire({ action, branch: "", start_point: null, remote: { name: "review", url: "", new_name: null }, ...over }));
  const add = remote("remote_add", { remote: { name: "review", url: "https://example.com/x.git", new_name: null } });
  assert.equal(titleOf(add), "Add remote review");
  assert.equal(detailLineOf(add), "New URL https://example.com/x.git");
  const set = remote("remote_set_url", { remote: { name: "origin", url: "/elsewhere", new_name: null } });
  assert.equal(titleOf(set), "Change the URL of remote origin");
  assert.equal(detailLineOf(set), "New URL /elsewhere");
  const rename = remote("remote_rename", { remote: { name: "review", url: "", new_name: "reviewed" } });
  assert.equal(titleOf(rename), "Rename remote review to reviewed");
  assert.equal(titleOf(remote("remote_remove")), "Remove remote review");
  assert.equal(detailLineOf(remote("remote_remove")), "");
});
