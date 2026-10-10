import { assert, file, test } from "../../../shared/testing/test.js";
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
  assert.deepEqual(bindingLine({ repositoryId: "r", credentialId: "gc-work", resolved: work, remoteUrl: remote }), { state: "pinned", name: "work-token", text: "pinned", addHref: "" });
  assert.equal(bindingLine({ repositoryId: "r", credentialId: null, resolved: work, remoteUrl: remote }).text, "automatic (default for github.com)");
  assert.equal(bindingLine({ repositoryId: "r", credentialId: null, resolved: null, remoteUrl: null }).state, "local");
  const none = bindingLine({ repositoryId: "r", credentialId: null, resolved: null, remoteUrl: "git@github.com:acme/infra.git" });
  assert.deepEqual([none.state, none.addHref], ["none", "/git-credentials?host=github.com&kind=ssh_key"]);
  assert.ok(none.text.startsWith("No SSH key for github.com"));
  assert.equal(bindingLine({ repositoryId: "r", credentialId: "gc-key", resolved: makeKey(), remoteUrl: null }).state, "pinned", "a repository without an origin can be pinned");
});
