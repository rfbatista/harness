import { Codes } from "../../../../shared/domain/errors.js";
import { FeedStatus } from "../../../../shared/domain/feed.js";
import { fixedClock } from "../../../../shared/infrastructure/clock.js";
import { flush } from "../../../../shared/testing/doubles.js";
import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { PASSPHRASE, memoryCredentials } from "../../infrastructure/memory-gateway.js";
import { ENCRYPTED_KEY, PRIVATE_KEY, REPOSITORIES, SECRET_TOKEN, at, credentialDTO, makeCredential, makeKey } from "../../testing/fixtures.js";
import { gitCredentialsPage } from "./gitCredentialsPage.js";

file("credentials/presentation/gitCredentialsPage");

/** Inputs as the page's x-refs: real ones, so value behaves as in the browser. */
function secretInputs(doc) {
  const input = (type) => Object.assign(doc.createElement("input"), { type });
  return {
    token: input("password"),
    privateKey: doc.createElement("textarea"),
    passphrase: input("password"),
    // A file input's files cannot be set from a script: a stand-in with the same surface.
    keyFile: { value: "", files: [] },
    formFirst: input("text"),
    addButton: doc.createElement("button"),
  };
}

function setup({ credentials = [makeCredential(), makeKey()], identity = null, dataset = {}, world = {} } = {}) {
  const memory = memoryCredentials({ credentials, identity, repositories: REPOSITORIES.map((r) => ({ ...r })), now: () => at(90), ...world });
  const el = seededElement({ credentials: credentials.map(credentialDTO), identity });
  Object.assign(el.dataset, dataset);
  const copied = [];
  const mounted = mount(gitCredentialsPage({ gateway: memory.gateway, clock: fixedClock(at(120)), copy: async (t) => void copied.push(t) }), { el });
  const refs = secretInputs(el.ownerDocument);
  mounted.instance.$refs = refs;
  mounted.instance.init();
  mounted.tick();
  return { ...mounted, memory, refs, el, copied };
}

/** Everything the component holds, as text: no secret may be in it. */
const state = (instance) => JSON.stringify(Object.fromEntries(Object.keys(instance).filter((k) => !k.startsWith("$")).map((k) => [k, instance[k]])));

/** A menu-select event from one row's menu. */
function fromRow(el, id, action) {
  const row = el.ownerDocument.createElement("div");
  row.dataset.id = id;
  row.innerHTML = '<div class="menu"><button type="button">⋯</button></div>';
  el.append(row);
  return { target: row.querySelector(".menu"), detail: { action } };
}

test("starts from the seed: one group per host, rows with kind, default and meta", () => {
  const { instance } = setup({ credentials: [makeCredential({ lastUsedAt: at(0) }), makeKey(), makeCredential({ id: "gl", name: "gl", host: "gitlab.com", isDefault: false })] });
  assert.equal(instance.hasCredentials, true);
  assert.deepEqual(
    instance.groups.map((g) => [g.host, g.rows.map((r) => [r.name, r.kindLabel, r.isDefault, r.meta])]),
    [
      ["github.com", [["laptop-key", "SSH key", false, "git · SHA256:abcdef · never used"], ["work-token", "HTTPS token", true, "x-access-token · …1234 · last used 2h ago"]]],
      ["gitlab.com", [["gl", "HTTPS token", false, "x-access-token · …1234 · never used"]]],
    ],
  );
  assert.equal(instance.identityUnset, true);
});

test("an empty harness says so; a missing seed is read from the server", async () => {
  const empty = setup({ credentials: [] });
  assert.equal(empty.instance.isEmpty, true);

  const memory = memoryCredentials({ credentials: [makeCredential()], identity: { name: "Ada", email: "ada@example.com" } });
  const { instance } = mount(gitCredentialsPage({ gateway: memory.gateway, clock: fixedClock(at(0)) }), { el: document.createElement("main") });
  instance.init();
  await flush();
  assert.deepEqual([instance.credentials.length, instance.identityName], [1, "Ada"]);
});

test("adds a token: it is read from its input only to send, then the input is emptied", async () => {
  const { instance, refs, memory } = setup({ credentials: [] });
  instance.openAdd();
  assert.deepEqual([instance.formOpen, instance.showsToken, instance.form.isDefault], [true, true, true], "the first token of a host is offered as its default");
  Object.assign(instance.form, { name: "ci", host: "https://github.com/acme" });
  refs.token.value = SECRET_TOKEN;
  assert.ok(!state(instance).includes(SECRET_TOKEN), "typing never puts it in state");
  await instance.submit();
  const added = memory.find(instance.credentials[0].id);
  assert.deepEqual([added.name, added.host, added.isDefault, added.fingerprint], ["ci", "github.com", true, "…1234"]);
  assert.deepEqual([refs.token.value, instance.formOpen, instance.announcement], ["", false, "Added ci."]);
  assert.ok(!state(instance).includes(SECRET_TOKEN));
});

test("what is missing is said before asking: no token, no name", async () => {
  const { instance, memory } = setup({ credentials: [] });
  instance.openAdd({ host: "github.com" });
  instance.form.name = "ci";
  await instance.submit();
  assert.ok(instance.formError?.message.includes("token"));
  assert.equal((await memory.gateway.list()).length, 0);
});

test("a taken name shows under Name and the pasted token stays in its input only", async () => {
  const { instance, refs } = setup();
  instance.openAdd({ host: "github.com" });
  instance.form.name = "WORK-TOKEN";
  refs.token.value = SECRET_TOKEN;
  await instance.submit();
  assert.equal(instance.nameError?.code, Codes.GIT_CREDENTIAL_NAME_TAKEN);
  assert.equal(refs.token.value, SECRET_TOKEN, "kept for the next try");
  assert.ok(!state(instance).includes(SECRET_TOKEN));
  instance.cancelForm();
  assert.equal(refs.token.value, "", "cancel empties it");
});

test("generates an SSH key and shows its public key with what to do next", async () => {
  const { instance, memory } = setup({ credentials: [] });
  instance.openAdd({ host: "github.com", kind: "ssh_key" });
  assert.deepEqual([instance.showsKeySource, instance.showsKeyInput, instance.submitLabel], [true, false, "Generate key"]);
  instance.form.name = "laptop";
  await instance.submit();
  const id = instance.credentials[0].id;
  assert.ok(memory.find(id).publicKey);
  const row = instance.groups[0].rows[0];
  assert.deepEqual([row.keyOpen, row.hasPublicKey, row.highlighted], [true, true, true]);
  assert.ok(instance.notice.includes("github.com"));
  await instance.copyKey(id);
  assert.ok(instance.announcement.startsWith("Copied"));
});

test("a key that does not open is refused, and the key and passphrase are emptied", async () => {
  const { instance, refs } = setup();
  instance.openAdd({ host: "github.com", kind: "ssh_key" });
  Object.assign(instance.form, { name: "imported", keySource: "paste" });
  refs.privateKey.value = ENCRYPTED_KEY;
  refs.passphrase.value = "wrong";
  await instance.submit();
  assert.equal(instance.formError?.code, Codes.GIT_CREDENTIAL_INVALID_KEY);
  assert.deepEqual([refs.privateKey.value, refs.passphrase.value], ["", ""]);

  refs.privateKey.value = ENCRYPTED_KEY;
  refs.passphrase.value = PASSPHRASE;
  await instance.submit();
  assert.equal(instance.formOpen, false);
  assert.ok(instance.credentials.some((c) => c.name === "imported"));
  assert.ok(!state(instance).includes("PRIVATE KEY") && !state(instance).includes(PASSPHRASE));
});

test("a key file is read only to send", async () => {
  const { instance, refs, memory } = setup({ credentials: [] });
  instance.openAdd({ host: "github.com", kind: "ssh_key" });
  Object.assign(instance.form, { name: "from-file", keySource: "file" });
  refs.keyFile.files = [{ text: async () => PRIVATE_KEY }];
  await instance.submit();
  assert.equal((await memory.gateway.list())[0].name, "from-file");
  assert.equal(refs.keyFile.value, "");
});

test("an edit sends only what changed and never a secret", async () => {
  const { instance, memory } = setup();
  const sent = [];
  const update = memory.gateway.update;
  memory.gateway.update = (input) => (sent.push(input), update(input));
  instance.openEdit("gc-work");
  assert.deepEqual([instance.showsDetails, instance.showsToken, instance.formTitle], [true, false, "Edit work-token"]);
  instance.form.name = "Work";
  await instance.submit();
  assert.deepEqual(sent, [{ credentialId: "gc-work", name: "Work" }]);
  assert.equal(instance.credentials.find((c) => c.id === "gc-work").name, "Work");
});

test("a host change on a pinned credential names the repositories to unpin", async () => {
  const { instance, memory } = setup();
  await memory.gateway.setRepositoryCredential("r-https", "gc-work");
  instance.openEdit("gc-work");
  instance.form.host = "gitlab.com";
  await instance.submit();
  assert.equal(instance.formError?.code, Codes.GIT_CREDENTIAL_HOST_MISMATCH);
  assert.deepEqual(instance.pinnedRepositories, [{ id: "r-https", name: "harness" }]);
  assert.equal(instance.formOpen, true, "the form stays to fix");
});

test("replacing a secret asks only for it and changes the fingerprint", async () => {
  const { instance, refs } = setup();
  instance.openReplace("gc-work");
  assert.deepEqual([instance.showsDetails, instance.showsToken, instance.formTitle], [false, true, "Replace the token of work-token"]);
  await instance.submit();
  assert.ok(instance.formError?.message, "an empty token is not a replacement");
  refs.token.value = "ghp_new9999";
  await instance.submit();
  assert.equal(instance.credentials.find((c) => c.id === "gc-work").fingerprint, "…9999");
  assert.equal(refs.token.value, "");
});

test("a locked store is learned from a refusal: add, replace and test turn off; the rest still works", async () => {
  const { instance, memory, refs } = setup();
  memory.lock();
  instance.openAdd({ host: "github.com" });
  instance.form.name = "ci";
  refs.token.value = SECRET_TOKEN;
  await instance.submit();
  assert.deepEqual([instance.locked, instance.formError?.code], [true, Codes.GIT_CREDENTIAL_STORE_LOCKED]);
  assert.equal(refs.token.value, SECRET_TOKEN, "not a refusal of the secret itself");
  assert.deepEqual([instance.cannotAdd, instance.cannotSubmit], [true, true]);
  assert.ok(instance.lockedText.includes("HARNESS_SECRET_KEY"));
  instance.cancelForm();
  instance.openReplace("gc-work");
  assert.equal(instance.formOpen, false);
  instance.openEdit("gc-work");
  instance.form.name = "renamed";
  assert.equal(instance.cannotSubmit, false, "a rename needs no secret");
  await instance.submit();
  assert.equal(instance.credentials.find((c) => c.id === "gc-work").name, "renamed");
});

test("the row menu sets and clears the default, and deletes after the confirm", async () => {
  const { instance, el, dispatched, memory } = setup({ credentials: [makeCredential(), makeCredential({ id: "spare", name: "spare", isDefault: false })] });
  await instance.rowAction(fromRow(el, "spare", "default"));
  assert.deepEqual(instance.credentials.map((c) => [c.id, c.isDefault]), [["spare", true], ["gc-work", false]]);
  await instance.rowAction(fromRow(el, "spare", "undefault"));
  assert.equal(memory.find("spare").isDefault, false);

  const ask = fromRow(el, "spare", "delete");
  instance.rowAction(ask);
  assert.deepEqual(dispatched.at(-1), { name: "dialog-open", detail: { id: "credential-delete", anchor: ask.target.querySelector("button") } });
  assert.ok(instance.deleteText.startsWith("Delete spare?"));
  await instance.dialogAction({ detail: { id: "credential-delete", action: "delete" } });
  assert.deepEqual(instance.credentials.map((c) => c.id), ["gc-work"]);
  assert.equal(instance.notice, "", "our own delete is not news from elsewhere");
  assert.equal(instance.announcement, "Deleted spare.");
});

test("Test says what the host answered, as the server words it", async () => {
  const { instance } = setup({ credentials: [makeCredential(), makeCredential({ id: "gl", name: "gl", host: "gitlab.com", isDefault: false })] });
  await instance.test("gc-work");
  await instance.test("gl");
  const rows = instance.groups.flatMap((g) => g.rows);
  const line = (id) => rows.find((r) => r.id === id);
  assert.deepEqual([line("gc-work").testOk, line("gc-work").tested], [true, true]);
  assert.equal(line("gl").testOk, false);
  assert.ok(line("gl").testLine.startsWith("pick a repository to test this token against"));
});

test("changes made elsewhere arrive live; a credential deleted while edited closes the form and says so", async () => {
  const { instance, memory } = setup();
  memory.upsert(makeCredential({ id: "new", name: "from-elsewhere", isDefault: false }));
  assert.ok(instance.credentials.some((c) => c.name === "from-elsewhere"));
  instance.openEdit("gc-key");
  await memory.gateway.remove("gc-key");
  assert.deepEqual([instance.formOpen, instance.notice], [false, "This credential was deleted elsewhere."]);
});

test("a resynced feed reads the list again", async () => {
  const { instance, memory } = setup();
  memory.reportStatus(FeedStatus.RESYNCED);
  await flush();
  assert.equal(instance.credentials.length, 2);
});

test("the identity saves trimmed, says what is wrong first, and follows saves elsewhere unless edited", async () => {
  const { instance, memory } = setup();
  instance.identityName = "Ada";
  instance.identityEmail = "ada";
  assert.ok(instance.identityProblemText);
  assert.equal(instance.cannotSaveIdentity, true);
  instance.identityEmail = " ada@example.com ";
  await instance.saveIdentity();
  assert.deepEqual([instance.identity, instance.identityEmail, instance.identityDirty], [{ name: "Ada", email: "ada@example.com" }, "ada@example.com", false]);

  await memory.gateway.updateIdentity({ name: "Grace", email: "grace@example.com" });
  assert.equal(instance.identityName, "Grace");
  instance.identityName = "Typing";
  await memory.gateway.updateIdentity({ name: "Linus", email: "l@example.com" });
  assert.deepEqual([instance.identityName, instance.identity.name], ["Typing", "Linus"], "an edit in progress is kept");
});

test("a link names a host to add for, or a credential to show", () => {
  const add = setup({ dataset: { host: "gitlab.example.com", kind: "ssh_key" } });
  assert.deepEqual([add.instance.form.mode, add.instance.form.host, add.instance.form.kind], ["add", "gitlab.example.com", "ssh_key"]);
  const show = setup({ dataset: { credential: "gc-key" } });
  assert.deepEqual([show.instance.formOpen, show.instance.highlightId], [false, "gc-key"]);
});

test("leaving the page empties every secret input", () => {
  const { instance, refs } = setup();
  instance.openAdd();
  refs.token.value = SECRET_TOKEN;
  refs.privateKey.value = PRIVATE_KEY;
  refs.passphrase.value = PASSPHRASE;
  instance.clearSecrets();
  assert.deepEqual([refs.token.value, refs.privateKey.value, refs.passphrase.value], ["", "", ""]);
});
