import views from "../../../../testdata/views/git-credentials.json" with { type: "json" };
import { assert, file, test } from "../../../shared/testing/test.js";
import { candidatesFor, remoteOf } from "../domain/credential.js";
import { toCredential } from "../infrastructure/dto.js";
import { at, makeCredential, makeKey } from "../testing/fixtures.js";
import { addHref, bindingLine, credentialHref, credentialRow, lastUsedLabel, testLine } from "./view.js";

file("credentials/presentation/view");

test("links to the page carry a host and kind to add, or a credential to show, and never a secret", () => {
  assert.equal(addHref("github.com", "https_token"), "/git-credentials?host=github.com&kind=https_token");
  assert.equal(addHref("git.example.com:2222"), "/git-credentials?host=git.example.com%3A2222");
  assert.equal(credentialHref("gc 1"), "/git-credentials?credential=gc+1");
});

test("a row reads its kind, default and meta; a key carries its public key", () => {
  const row = credentialRow(makeCredential({ lastUsedAt: at(0) }), at(3 * 24 * 60));
  assert.deepEqual([row.kindLabel, row.isDefault, row.meta, row.hasPublicKey, row.menuLabel], ["HTTPS token", true, "x-access-token · …1234 · last used 3d ago", false, "Actions for work-token"]);
  const key = credentialRow(makeKey(), at(0));
  assert.deepEqual([key.kindLabel, key.hasPublicKey, key.meta], ["SSH key", true, "git · SHA256:abcdef · never used"]);
  assert.equal(lastUsedLabel(at(0), at(0)), "last used just now");
});

test("a test result reads the server's message with what was checked", () => {
  assert.equal(testLine({ ok: true, message: "Signed in.", checkedUrl: "https://api.github.com/user" }), "Signed in. (https://api.github.com/user)");
  assert.equal(testLine({ ok: false, message: "", checkedUrl: "" }), "The host refused it.");
});

test("a binding reads pinned, automatic, local, or what to add", () => {
  const work = makeCredential();
  const remote = "https://github.com/acme/harness.git";
  assert.deepEqual(bindingLine({ repositoryId: "r", credentialId: "gc-work", resolved: work, pullRequests: work, remoteUrl: remote }), { state: "pinned", name: "work-token", text: "pinned", addHref: "", pullText: "Pull requests: work-token (pinned)", pullAddHref: "" });
  assert.equal(bindingLine({ repositoryId: "r", credentialId: null, resolved: work, remoteUrl: remote }).text, "automatic (default for github.com)");
  assert.equal(bindingLine({ repositoryId: "r", credentialId: null, resolved: null, remoteUrl: null }).state, "local");
  const none = bindingLine({ repositoryId: "r", credentialId: null, resolved: null, remoteUrl: "git@github.com:acme/infra.git" });
  assert.deepEqual([none.state, none.addHref], ["none", "/git-credentials?host=github.com&kind=ssh_key"]);
  assert.ok(none.text.startsWith("No SSH key for github.com"));
  assert.equal(bindingLine({ repositoryId: "r", credentialId: "gc-key", resolved: makeKey(), remoteUrl: null }).state, "pinned", "a repository without an origin can be pinned");
});

test("rows, origins and bindings read as the Go pages paint them", () => {
  const pick = (which) => (which === "key" ? toCredential(views.key) : toCredential(views.credential));
  const now = new Date("2026-10-10T18:00:00Z");
  for (const c of views.rows) {
    const cred = { ...pick(c.which), lastUsedAt: c.used_minutes_ago === null ? null : new Date(now.getTime() - c.used_minutes_ago * 60_000) };
    const row = credentialRow(cred, now);
    assert.deepEqual([row.kindLabel, row.meta], [c.kind_label, c.meta], `${c.which} row`);
  }
  for (const c of views.remotes) {
    const r = remoteOf(c.url);
    assert.deepEqual([r?.host ?? "", r?.kind ?? ""], [c.host, c.kind], c.url);
  }
  for (const c of views.bindings) {
    const resolved = c.resolved ? pick(c.resolved) : null;
    const pullRequests = c.pull_requests ? pick(c.pull_requests) : null;
    const line = bindingLine({ repositoryId: "r", credentialId: c.pinned && resolved ? resolved.id : null, resolved, pullRequests, remoteUrl: c.remote_url });
    assert.deepEqual(line, { state: c.line.state, name: c.line.name, text: c.line.text, addHref: c.line.add_href, pullText: c.line.pull_text, pullAddHref: c.line.pull_add_href }, JSON.stringify(c));
  }
  const all = [toCredential(views.credential), toCredential(views.key)];
  for (const c of views.pickers) {
    assert.deepEqual(candidatesFor(all, c.remote_url).map((x) => x.id), c.candidates, c.remote_url);
  }
});
