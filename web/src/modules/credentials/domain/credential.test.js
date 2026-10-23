import { assert, file, test } from "../../../shared/testing/test.js";
import { makeCredential, makeKey } from "../testing/fixtures.js";
import { applyChange, automaticFor, candidatesFor, formProblem, groupByHost, identityProblem, kindLabel, normalizeHost, remoteOf } from "./credential.js";

file("credentials/domain/credential");

test("a host is kept as the server stores it: lowercase, no scheme, userinfo or path, with its port", () => {
  const cases = [
    ["github.com", "github.com"],
    ["  GitHub.COM ", "github.com"],
    ["https://github.com/acme/harness.git", "github.com"],
    ["ssh://git@git.example.com:2222/acme/x", "git.example.com:2222"],
    ["git.example.com:8443", "git.example.com:8443"],
    ["https://", ""],
    ["", ""],
  ];
  for (const [input, want] of cases) assert.equal(normalizeHost(input), want, input);
});

test("an origin says which host and kind of credential it takes; a local one none", () => {
  const cases = [
    ["https://github.com/acme/harness.git", { host: "github.com", kind: "https_token" }],
    ["https://user@GitHub.com/acme/harness", { host: "github.com", kind: "https_token" }],
    ["https://git.example.com:8443/a/b.git", { host: "git.example.com:8443", kind: "https_token" }],
    ["git@github.com:acme/infra.git", { host: "github.com", kind: "ssh_key" }],
    ["github.com:acme/infra.git", { host: "github.com", kind: "ssh_key" }],
    ["ssh://git@git.example.com:2222/acme/x.git", { host: "git.example.com:2222", kind: "ssh_key" }],
    ["file:///src/harness", null],
    ["/src/harness", null],
    ["", null],
    [null, null],
  ];
  for (const [url, want] of cases) assert.deepEqual(remoteOf(url), want, String(url));
});

test("credentials group by host in the server's order", () => {
  const groups = groupByHost([
    makeCredential({ id: "b", name: "b", host: "gitlab.com" }),
    makeCredential({ id: "z", name: "Zed" }),
    makeKey({ id: "a", name: "alpha" }),
  ]);
  assert.deepEqual(groups.map((g) => [g.host, g.credentials.map((c) => c.id)]), [["github.com", ["a", "z"]], ["gitlab.com", ["b"]]]);
  assert.deepEqual(groupByHost([]), []);
});

test("a repository can be pinned to credentials of its origin's host and kind; without an origin, any", () => {
  const list = [makeCredential(), makeKey(), makeCredential({ id: "gl", name: "gl", host: "gitlab.com" })];
  const ids = (url) => candidatesFor(list, url).map((c) => c.id);
  assert.deepEqual(ids("https://github.com/a/b.git"), ["gc-key", "gc-work"], "an SSH key of the host serves an https origin");
  assert.deepEqual(candidatesFor([makeKey({ host: "git.example.com:8443" })], "https://git.example.com:8443/a/b.git").map((c) => c.id), [], "but not one with a port");
  assert.deepEqual(ids("git@github.com:a/b.git"), ["gc-key"]);
  assert.deepEqual(ids("https://bitbucket.org/a/b.git"), []);
  assert.deepEqual(ids(null), ["gc-key", "gc-work", "gl"]);
  assert.deepEqual(ids("file:///src/x"), [], "an unreadable origin takes none");
});

test("a change upserts or removes by id, and a new default clears the old one of the same host and kind only", () => {
  const work = makeCredential();
  const key = makeKey({ isDefault: true });
  const gl = makeCredential({ id: "gl", name: "gl", host: "gitlab.com", isDefault: true });
  let list = [work, key, gl];
  const next = makeCredential({ id: "new", name: "new", isDefault: true });
  list = applyChange(list, { kind: "upsert", credential: next });
  assert.deepEqual(list.map((c) => [c.id, c.isDefault]), [["gc-key", true], ["new", true], ["gc-work", false], ["gl", true]]);
  list = applyChange(list, { kind: "upsert", credential: { ...next, name: "renamed" } });
  assert.equal(list.length, 4);
  assert.equal(list.find((c) => c.id === "new").name, "renamed");
  list = applyChange(list, { kind: "deleted", id: "gl" });
  assert.deepEqual(list.map((c) => c.id), ["gc-key", "new", "gc-work"], "renamed sorts before work-token");
  assert.equal(applyChange(list, { kind: "identity", identity: { name: "a", email: "a@b" } }), list);
});

test("an identity needs a name and an email with exactly one @", () => {
  assert.equal(identityProblem({ name: "Ada", email: "ada@example.com" }), "");
  assert.ok(identityProblem({ name: " ", email: "ada@example.com" }));
  assert.ok(identityProblem({ name: "Ada", email: "" }));
  assert.ok(identityProblem({ name: "Ada", email: "ada.example.com" }));
  assert.ok(identityProblem({ name: "Ada", email: "a@b@c" }));
});

test("a credential form says what is missing before asking the server", () => {
  const base = { mode: "add", kind: "https_token", name: "ci", host: "github.com", secret: true };
  assert.equal(formProblem(base), "");
  assert.ok(formProblem({ ...base, name: " " }));
  assert.ok(formProblem({ ...base, host: "https://" }));
  assert.ok(formProblem({ ...base, secret: false }), "a token is required to add one");
  assert.equal(formProblem({ ...base, kind: "ssh_key", keySource: "generate", secret: false }), "", "a generated key needs no secret");
  assert.ok(formProblem({ ...base, kind: "ssh_key", keySource: "paste", secret: false }));
  assert.equal(formProblem({ ...base, mode: "edit", secret: false }), "", "an edit keeps the stored secret");
  assert.ok(formProblem({ ...base, mode: "secret", secret: false }));
  assert.equal(formProblem({ mode: "secret", kind: "ssh_key", name: "", host: "", secret: true }), "", "replacing a secret asks only for it");
  assert.deepEqual([kindLabel("https_token"), kindLabel("ssh_key")], ["HTTPS token", "SSH key"]);
});

test("without a pin an origin resolves to the default of its host and kind, else the only one", () => {
  const key = makeKey();
  const work = makeCredential({ isDefault: false });
  const spare = makeCredential({ id: "spare", name: "spare", isDefault: false });
  assert.equal(automaticFor([key, work], "https://github.com/a/b.git")?.id, "gc-work", "the only token");
  assert.equal(automaticFor([key, work, spare], "https://github.com/a/b.git")?.id, "gc-key", "two tokens, no default: the host's only SSH key");
  assert.equal(automaticFor([work, spare], "https://github.com/a/b.git"), null, "two tokens, no default, no key");
  assert.equal(automaticFor([key, spare, work].slice(0, 1), "https://github.com/a/b.git")?.id, "gc-key", "no token: the only SSH key");
  assert.equal(automaticFor([key, makeKey({ id: "k2", name: "k2", isDefault: true })], "https://github.com/a/b.git")?.id, "k2", "no token: the default SSH key");
  assert.equal(automaticFor([makeKey({ host: "git.example.com:8443" })], "https://git.example.com:8443/a/b.git"), null, "no key on an origin with a port");
  assert.equal(automaticFor([key, work, { ...spare, isDefault: true }], "https://github.com/a/b.git")?.id, "spare");
  assert.equal(automaticFor([key, work], "git@github.com:a/b.git")?.id, "gc-key");
  assert.equal(automaticFor([key, work], null), null);
});
