import { Codes, StructuredError } from "../../../../shared/domain/errors.js";
import { flush } from "../../../../shared/testing/doubles.js";
import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryCredentials } from "../../infrastructure/memory-gateway.js";
import { REPOSITORIES, credentialDTO, makeCredential, makeKey, repositoryCredentialDTO } from "../../testing/fixtures.js";
import { SEED_ID, repositoryCredentialsStore } from "../repositoryStore.js";
import { repositoryCredential } from "./repositoryCredential.js";

file("credentials/presentation/repositoryCredential");

const CREDENTIALS = [makeCredential(), makeKey(), makeCredential({ id: "spare", name: "spare", isDefault: false })];

/** The Repositories page: its seed, the shared store, and one row component per repository asked for. */
async function setup({ credentials = CREDENTIALS, world = {} } = {}) {
  const memory = memoryCredentials({ credentials, repositories: REPOSITORIES.map((r) => ({ ...r })), ...world });
  const seed = {
    project_id: "p1",
    credentials: credentials.map(credentialDTO),
    locked: world.locked === true,
    repositories: (await memory.gateway.listRepositoryCredentials("p1")).map(repositoryCredentialDTO),
  };
  const page = seededElement(seed, SEED_ID);
  const store = repositoryCredentialsStore({ gateway: memory.gateway });
  const rows = {};
  const row = (repositoryId) => {
    if (rows[repositoryId]) return rows[repositoryId];
    const el = page.ownerDocument.createElement("div");
    el.dataset.repositoryId = repositoryId;
    el.dataset.repositoryName = REPOSITORIES.find((r) => r.id === repositoryId)?.name ?? "";
    page.append(el);
    const mounted = mount(repositoryCredential({ store }), { el });
    mounted.instance.$refs = { select: page.ownerDocument.createElement("select") };
    mounted.instance.init();
    return (rows[repositoryId] = mounted.instance);
  };
  return { memory, store, row };
}

test("each row says what its pushes use: automatic, pinned, nothing fits, or local only", async () => {
  const { row } = await setup();
  assert.deepEqual([row("r-https").line.state, row("r-https").line.name, row("r-https").line.text], ["automatic", "work-token", "automatic (default for github.com)"]);
  assert.deepEqual([row("r-ssh").line.state, row("r-ssh").line.text], ["automatic", "automatic (the only SSH key for github.com)"]);
  const none = row("r-gitlab").line;
  assert.deepEqual([none.state, none.addHref], ["none", "/git-credentials?host=gitlab.example.com&kind=https_token"]);
  assert.ok(none.text.includes("gitlab.example.com"));
  assert.equal(row("r-local").line.state, "local");
});

test("the picker offers Automatic, then the credentials of the origin's host and kind only", async () => {
  const { row } = await setup();
  const options = row("r-https").options;
  assert.deepEqual(options.map((o) => [o.value, o.label, o.meta]), [["", "Automatic", ""], ["spare", "spare", ""], ["gc-work", "work-token", "default"]]);
  assert.equal(options[0].description, "Uses work-token now");
  assert.equal(options[2].description, "HTTPS token · x-access-token · …1234");
  assert.deepEqual(row("r-ssh").options.map((o) => o.value), ["", "gc-key"]);
  assert.equal(row("r-gitlab").options[0].description, "Nothing fits now");
});

test("picking pins it; Automatic unpins; the select shows what is stored", async () => {
  const { row, memory } = await setup();
  const r = row("r-https");
  await r.pick("spare");
  assert.deepEqual([r.selected, r.line.state, r.line.name, memory.repository("r-https").credentialId], ["spare", "pinned", "spare", "spare"]);
  await r.pick("");
  assert.deepEqual([r.selected, r.line.state], ["", "automatic"]);
});

test("a refused pin shows on the dropdown with Retry, and the stored choice stays", async () => {
  const { row, memory } = await setup();
  const r = row("r-https");
  const set = memory.gateway.setRepositoryCredential;
  let refuse = true;
  memory.gateway.setRepositoryCredential = async (...args) => {
    if (refuse) {
      throw new StructuredError(Codes.GIT_CREDENTIAL_KIND_MISMATCH, "GIT_CREDENTIAL_KIND_MISMATCH: origin takes a https_token", 400);
    }
    return set(...args);
  };
  await r.pick("spare");
  assert.ok(r.pickError.includes(Codes.GIT_CREDENTIAL_KIND_MISMATCH));
  assert.equal(r.selected, "", "still automatic");
  refuse = false;
  await r.retry();
  assert.deepEqual([r.pickError, r.selected], ["", "spare"]);
});

test("Test runs against the repository's origin with what it resolves to; nothing to test when nothing fits", async () => {
  const { row } = await setup({ world: { refuse: (c) => (c.id === "gc-key" ? "Permission denied (publickey)." : "") } });
  const https = row("r-https");
  await https.test();
  assert.equal(https.testOk, true);
  assert.ok(https.testText.endsWith("(https://github.com/acme/harness.git)"));
  const ssh = row("r-ssh");
  await ssh.test();
  assert.deepEqual([ssh.testOk, ssh.testText.startsWith("Permission denied")], [false, true]);
  assert.equal(row("r-gitlab").cannotTest, true);
});

test("a locked store turns Test off on every row", async () => {
  const { row, memory, store } = await setup();
  memory.lock();
  await row("r-https").test();
  assert.equal(row("r-https").testResult.ok, false);
  assert.ok(row("r-https").testText.includes(Codes.GIT_CREDENTIAL_STORE_LOCKED));
  assert.deepEqual([store.locked, row("r-ssh").cannotTest], [true, true]);
});

test("a credential change anywhere re-reads every binding in one request, however many arrive", async () => {
  const { row, memory, store } = await setup();
  row("r-https");
  row("r-gitlab");
  let reads = 0;
  const list = memory.gateway.listRepositoryCredentials;
  memory.gateway.listRepositoryCredentials = (id) => (reads++, list(id));
  // A burst: three changes stream before any read answers.
  await Promise.all([
    memory.gateway.create({ name: "gl", kind: "https_token", host: "gitlab.example.com", token: "t-1" }),
    memory.gateway.update({ credentialId: "spare", name: "spare-2" }),
    memory.gateway.update({ credentialId: "spare", name: "spare-3" }),
  ]);
  await flush();
  await flush();
  assert.equal(reads, 2, "one read, and one more for what came during it");
  assert.deepEqual([row("r-gitlab").line.state, row("r-gitlab").line.name], ["automatic", "gl"]);
  assert.ok(row("r-https").options.some((o) => o.label === "spare-3"), "the options follow the change itself");
});

test("a repository added after the page loaded reads its binding", async () => {
  const { row, memory } = await setup();
  row("r-https");
  memory.gateway.listRepositoryCredentials = async () => [Object.freeze({ repositoryId: "r-new", credentialId: null, resolved: null, remoteUrl: null })];
  const late = row("r-new");
  assert.equal(late.line.state, "unknown");
  await flush();
  await flush();
  assert.equal(late.line.state, "local");
});

test("a store locked on first paint turns Test off before anyone tries it", async () => {
  const { row, store } = await setup({ world: { locked: true } });
  const first = row("r-https"); // the first row starts the store from the seed
  assert.deepEqual([store.locked, first.cannotTest], [true, true]);
});
