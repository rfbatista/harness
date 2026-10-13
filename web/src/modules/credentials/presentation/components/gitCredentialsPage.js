// The Git credentials page, /git-credentials: the author identity, every
// credential grouped by host, and the inline form that adds one, edits it or
// replaces its secret. The server paints the first rows in [data-ssr] and
// embeds the seed; this takes over and follows the catalog feed.
//
//   <main x-data="gitCredentialsPage" data-seed="credentials-seed"
//         data-host="github.com" data-kind="https_token" data-credential="gc-1"
//         x-on:menu-select="rowAction" x-on:dialog-action="dialogAction"
//         x-on:pagehide.window="clearSecrets">
//
// Secrets never enter this component's state. The token, private key and
// passphrase inputs are uncontrolled (x-ref, no x-model): submit reads them
// into the request and they are emptied on success, cancel, close and
// pagehide, and when the server refuses the secret itself. On any other
// refusal the person's paste stays in its input, never anywhere else.

import { Codes, codeOf } from "../../../../shared/domain/errors.js";
import { FeedStatus } from "../../../../shared/domain/feed.js";
import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { DEFAULT_USERNAME, applyChange, formProblem, groupByHost, identityProblem, kindLabel, normalizeHost } from "../../domain/credential.js";
import { credentialRow, testLine } from "../view.js";

const SECRET_REFS = ["token", "privateKey", "passphrase", "keyFile"];
const DELETE_DIALOG = "credential-delete";

const emptyForm = () => ({ mode: "", id: "", kind: "https_token", name: "", host: "", username: "", isDefault: false, keySource: "generate" });

/**
 * @param {{
 *   gateway: import("../../domain/ports.js").CredentialGateway,
 *   clock: import("../../../../shared/infrastructure/clock.js").Clock,
 *   copy?: (text: string) => Promise<void>,
 * }} deps
 */
export const gitCredentialsPage = ({ gateway, clock, copy = (text) => globalThis.navigator.clipboard.writeText(text) }) => () => {
  let unfollow = () => {};
  /** Ids this page is changing: their echoes are not news from elsewhere. */
  const pending = new Set();

  return {
    /** @type {import("../../domain/credential.js").Credential[]} */
    credentials: [],
    /** @type {import("../../domain/credential.js").Identity|null} */
    identity: null,
    identityName: "",
    identityEmail: "",
    savingIdentity: false,
    identityNotice: "",
    identityError: null,
    form: emptyForm(),
    defaultTouched: false,
    submitting: false,
    formError: null,
    nameError: null,
    /** Repositories pinned to the credential whose host change was refused. */
    pinnedRepositories: [],
    locked: false,
    /** Per credential id: { testing, ok, line }. */
    tests: {},
    /** Per credential id: its public key is shown. */
    openKeys: {},
    highlightId: "",
    deleteId: "",
    deleting: false,
    notice: "",
    announcement: "",
    error: null,
    ready: false,
    now: clock.now(),

    get groups() {
      return groupByHost(this.credentials).map((g) => ({
        host: g.host,
        headingId: `host-${g.host.replace(/[^a-z0-9]+/g, "-")}`,
        rows: g.credentials.map((c) => {
          const t = this.tests[c.id];
          return {
            ...credentialRow(c, this.now),
            testing: !!t?.testing,
            tested: !!t && !t.testing,
            testOk: !!t?.ok,
            testLine: t?.line ?? "",
            keyOpen: !!this.openKeys[c.id],
            highlighted: c.id === this.highlightId,
            canSetDefault: !c.isDefault,
          };
        }),
      }));
    },
    get hasCredentials() {
      return this.ready && this.credentials.length > 0;
    },
    get isEmpty() {
      return this.ready && this.credentials.length === 0;
    },

    // ── The form ────────────────────────────────────────────────────────
    get formOpen() {
      return this.form.mode !== "";
    },
    get editing() {
      return this.form.id ? (this.credentials.find((c) => c.id === this.form.id) ?? null) : null;
    },
    get formTitle() {
      if (this.form.mode === "add") return "Add a credential";
      const name = this.editing?.name ?? this.form.name;
      if (this.form.mode === "secret") return this.form.kind === "ssh_key" ? `Replace the key of ${name}` : `Replace the token of ${name}`;
      return `Edit ${name}`;
    },
    get submitLabel() {
      if (this.form.mode === "add") return this.form.kind === "ssh_key" && this.form.keySource === "generate" ? "Generate key" : "Add credential";
      return this.form.mode === "secret" ? "Replace" : "Save changes";
    },
    get showsKindChoice() {
      return this.form.mode === "add";
    },
    get showsDetails() {
      return this.form.mode === "add" || this.form.mode === "edit";
    },
    get showsToken() {
      return this.form.kind === "https_token" && (this.form.mode === "add" || this.form.mode === "secret");
    },
    get showsKeySource() {
      return this.form.mode === "add" && this.form.kind === "ssh_key";
    },
    /** Replacing a key: from a file or pasted. */
    get showsRekeySource() {
      return this.form.mode === "secret" && this.form.kind === "ssh_key";
    },
    get showsKeyInput() {
      return this.form.kind === "ssh_key" && (this.form.mode === "secret" || (this.form.mode === "add" && this.form.keySource !== "generate"));
    },
    get showsPaste() {
      return this.showsKeyInput && this.form.keySource === "paste";
    },
    get showsFile() {
      return this.showsKeyInput && this.form.keySource !== "paste";
    },
    get usernamePlaceholder() {
      return DEFAULT_USERNAME[this.form.kind];
    },
    get defaultLabel() {
      const host = normalizeHost(this.form.host) || "this host";
      return `Make it the default ${kindLabel(this.form.kind)} for ${host}`;
    },
    get needsSecretStore() {
      return this.form.mode === "secret" || this.form.mode === "add";
    },
    get cannotSubmit() {
      return this.submitting || (this.locked && this.needsSecretStore);
    },
    get hasPinnedRepositories() {
      return this.pinnedRepositories.length > 0;
    },
    get nameInvalid() {
      return this.nameError !== null;
    },

    // ── Locked store ────────────────────────────────────────────────────
    get lockedText() {
      return "The server can't read stored secrets: its encryption key is missing or wrong. Nothing was deleted. Set HARNESS_SECRET_KEY, or restore secret.key in the server's data directory, then restart it. Until then you can rename, set defaults, delete and pin, but not add, replace or test.";
    },
    get cannotAdd() {
      return this.locked || !this.ready;
    },

    // ── Identity ────────────────────────────────────────────────────────
    get identityUnset() {
      return this.ready && this.identity === null;
    },
    get identityDirty() {
      return this.identityName !== (this.identity?.name ?? "") || this.identityEmail !== (this.identity?.email ?? "");
    },
    get identityProblemText() {
      return this.identityDirty ? identityProblem({ name: this.identityName, email: this.identityEmail }) : "";
    },
    get cannotSaveIdentity() {
      return !this.ready || this.savingIdentity || !this.identityDirty || this.identityProblemText !== "";
    },

    // ── Delete ──────────────────────────────────────────────────────────
    get deleteName() {
      return this.credentials.find((c) => c.id === this.deleteId)?.name ?? "";
    },
    get deleteText() {
      return `Delete ${this.deleteName}? Repositories pinned to it go back to automatic. Pushes that used it fail until another credential fits.`;
    },

    init() {
      const el = this.$el;
      try {
        const seed = gateway.decodePageSeed(readSeed(el));
        this.credentials = seed.credentials;
        this.locked = seed.locked;
        this.setIdentity(seed.identity);
      } catch {
        // The server could not read them (or sent no seed): read them here.
        this.reload();
      }
      this.$nextTick(() => {
        for (const node of el.querySelectorAll("[data-ssr]")) node.remove();
        this.ready = true;
        const { host, kind, credential } = el.dataset;
        if (credential) this.highlight(credential);
        else if (host || kind) this.openAdd({ host: host ?? "", kind: kind ?? "" });
      });
      unfollow = gateway.follow(
        (change) => this.apply(change),
        (status) => status === FeedStatus.RESYNCED && this.reload(),
      );
    },

    destroy() {
      unfollow();
      this.clearSecrets();
    },

    async reload() {
      try {
        const [{ credentials, locked }, identity] = await Promise.all([gateway.list(), gateway.getIdentity()]);
        this.credentials = credentials;
        this.locked = locked;
        if (!this.identityDirty) this.setIdentity(identity);
        else this.identity = identity;
      } catch (err) {
        this.error = describeError(err);
      }
    },

    /** @param {import("../../domain/credential.js").Change} change */
    apply(change) {
      this.now = clock.now();
      if (change.kind === "identity") {
        if (this.savingIdentity) return; // our own save answers for itself
        const wasDirty = this.identityDirty;
        this.identity = change.identity;
        if (!wasDirty) this.setIdentity(change.identity);
        return;
      }
      this.credentials = applyChange(this.credentials, change);
      if (change.kind === "deleted" && !pending.has(change.id)) {
        if (this.form.id === change.id) {
          this.closeForm();
          this.notice = "This credential was deleted elsewhere.";
        }
        if (this.deleteId === change.id) this.deleteId = "";
      }
    },

    setIdentity(identity) {
      this.identity = identity;
      this.identityName = identity?.name ?? "";
      this.identityEmail = identity?.email ?? "";
    },

    async saveIdentity() {
      if (this.cannotSaveIdentity) return;
      this.savingIdentity = true;
      this.identityError = null;
      this.identityNotice = "";
      try {
        this.setIdentity(await gateway.updateIdentity({ name: this.identityName, email: this.identityEmail }));
        this.identityNotice = "Saved. New commits in harness worktrees use it.";
      } catch (err) {
        this.identityError = describeError(err);
      } finally {
        this.savingIdentity = false;
      }
    },

    resetIdentity() {
      this.setIdentity(this.identity);
      this.identityError = null;
      this.identityNotice = "";
    },

    // ── Opening and closing the form ────────────────────────────────────
    openAdd({ host = "", kind = "" } = {}) {
      if (this.cannotAdd) return;
      this.startForm({ ...emptyForm(), mode: "add", kind: kind === "ssh_key" ? "ssh_key" : "https_token", host: normalizeHost(host) });
      this.suggestDefault();
    },
    openEdit(id) {
      const c = this.credentials.find((x) => x.id === id);
      if (!c) return;
      this.startForm({ ...emptyForm(), mode: "edit", id, kind: c.kind, name: c.name, host: c.host, username: c.username, isDefault: c.isDefault });
    },
    openReplace(id) {
      const c = this.credentials.find((x) => x.id === id);
      if (!c || this.locked) return;
      this.startForm({ ...emptyForm(), mode: "secret", id, kind: c.kind, name: c.name, host: c.host, keySource: "file" });
    },
    startForm(form) {
      this.clearSecrets();
      this.form = form;
      this.defaultTouched = false;
      this.formError = this.nameError = null;
      this.pinnedRepositories = [];
      this.notice = "";
      // The first field the form shows: the name, or the secret being replaced.
      const first = form.mode !== "secret" ? "formFirst" : form.kind === "https_token" ? "token" : "keyFile";
      this.$nextTick(() => this.$refs[first]?.focus());
    },
    cancelForm() {
      this.closeForm();
      this.$nextTick(() => this.$refs.addButton?.focus());
    },
    closeForm() {
      this.clearSecrets();
      this.form = emptyForm();
      this.formError = this.nameError = null;
      this.pinnedRepositories = [];
    },
    setKind(kind) {
      this.form.kind = kind;
      this.suggestDefault();
    },
    touchDefault() {
      this.defaultTouched = true;
    },
    /** A new credential is pre-checked as default when no other of its host and kind exists. */
    suggestDefault() {
      if (this.form.mode !== "add" || this.defaultTouched) return;
      const host = normalizeHost(this.form.host);
      this.form.isDefault = !this.credentials.some((c) => c.host === host && c.kind === this.form.kind);
    },

    /** Empties every secret input, wherever the form is. */
    clearSecrets() {
      for (const name of SECRET_REFS) {
        const input = this.$refs?.[name];
        if (input) input.value = "";
      }
    },

    /** What the secret inputs hold now, read only to send. */
    async readSecrets() {
      const value = (name) => this.$refs?.[name]?.value ?? "";
      const file = this.$refs?.keyFile?.files?.[0];
      const privateKey = this.form.keySource === "paste" ? value("privateKey") : file ? await file.text() : "";
      return { token: value("token"), privateKey, passphrase: value("passphrase") };
    },

    async submit() {
      if (this.cannotSubmit) return;
      const secrets = await this.readSecrets();
      const given = this.form.kind === "https_token" ? secrets.token !== "" : secrets.privateKey !== "";
      const problem = formProblem({ ...this.form, secret: given });
      if (problem) {
        this.formError = { message: problem, code: "", next: "" };
        return;
      }
      this.submitting = true;
      this.formError = this.nameError = null;
      this.pinnedRepositories = [];
      const { mode, id } = this.form;
      if (id) pending.add(id);
      try {
        const saved = await this.send(secrets);
        this.credentials = applyChange(this.credentials, { kind: "upsert", credential: saved });
        this.closeForm();
        this.announcement = mode === "add" ? `Added ${saved.name}.` : `Saved ${saved.name}.`;
        if (saved.publicKey && mode !== "edit") {
          this.openKeys = { ...this.openKeys, [saved.id]: true };
          this.notice = `Add this public key to ${saved.host} (your SSH keys, or the repository's deploy keys), then Test.`;
        }
        this.highlight(saved.id);
      } catch (err) {
        this.refused(err);
      } finally {
        if (id) pending.delete(id);
        this.submitting = false;
      }
    },

    send({ token, privateKey, passphrase }) {
      const f = this.form;
      if (f.mode === "add") {
        const generate = f.kind === "ssh_key" && f.keySource === "generate";
        return gateway.create({
          name: f.name,
          kind: f.kind,
          host: f.host,
          username: f.username,
          isDefault: f.isDefault,
          ...(f.kind === "https_token" ? { token } : generate ? { generate: true } : { privateKey, passphrase }),
        });
      }
      if (f.mode === "secret") {
        return gateway.update({ credentialId: f.id, ...(f.kind === "https_token" ? { token } : { privateKey, passphrase }) });
      }
      const old = this.editing;
      const changed = {};
      if (f.name !== old?.name) changed.name = f.name;
      if (normalizeHost(f.host) !== old?.host) changed.host = f.host;
      if (f.username !== old?.username) changed.username = f.username;
      if (f.isDefault !== old?.isDefault) changed.isDefault = f.isDefault;
      return gateway.update({ credentialId: f.id, ...changed });
    },

    refused(err) {
      const view = describeError(err);
      switch (codeOf(err)) {
        case Codes.GIT_CREDENTIAL_NAME_TAKEN:
          this.nameError = view;
          return;
        case Codes.GIT_CREDENTIAL_INVALID_KEY:
          for (const name of ["privateKey", "passphrase", "keyFile"]) if (this.$refs?.[name]) this.$refs[name].value = "";
          this.formError = view;
          this.$nextTick(() => (this.$refs.privateKey ?? this.$refs.keyFile)?.focus());
          return;
        case Codes.GIT_CREDENTIAL_HOST_MISMATCH:
          this.pinnedRepositories = (err.details?.repositories ?? []).map((r) => ({ id: r.id, name: r.name || r.id }));
          this.formError = { ...view, message: "Repositories are pinned to this credential, so its host cannot change.", next: "unpin them on their project's Repositories page first, then change the host" };
          return;
        case Codes.GIT_CREDENTIAL_STORE_LOCKED:
          this.locked = true;
          this.formError = view;
          return;
        case Codes.GIT_CREDENTIAL_NOT_FOUND:
          this.closeForm();
          this.notice = "This credential was deleted elsewhere.";
          return;
        default:
          this.formError = view;
      }
    },

    // ── Row actions ─────────────────────────────────────────────────────
    /** @param {CustomEvent<{ action: string }>} event  a row's menu (they bubble up here) */
    rowAction(event) {
      const id = event.target?.closest?.("[data-id]")?.dataset.id ?? "";
      if (!this.credentials.some((c) => c.id === id)) return;
      switch (event.detail?.action) {
        case "edit":
          return this.openEdit(id);
        case "replace":
          return this.openReplace(id);
        case "default":
          return this.setDefault(id, true);
        case "undefault":
          return this.setDefault(id, false);
        case "delete":
          this.deleteId = id;
          this.$dispatch("dialog-open", { id: DELETE_DIALOG, anchor: event.target?.closest?.(".menu")?.querySelector(":scope > button") });
      }
    },

    async setDefault(id, isDefault) {
      pending.add(id);
      this.error = null;
      try {
        const saved = await gateway.update({ credentialId: id, isDefault });
        this.credentials = applyChange(this.credentials, { kind: "upsert", credential: saved });
        this.announcement = isDefault ? `${saved.name} is now the default for ${saved.host}.` : `${saved.name} is no longer a default.`;
      } catch (err) {
        this.error = describeError(err);
      } finally {
        pending.delete(id);
      }
    },

    /** @param {CustomEvent<{ id: string, action: string }>} event  the delete confirm's buttons */
    dialogAction(event) {
      if (event.detail?.id === DELETE_DIALOG && event.detail.action === "delete") return this.confirmDelete();
    },

    async confirmDelete() {
      const id = this.deleteId;
      if (!id || this.deleting) return;
      const name = this.deleteName;
      this.deleting = true;
      this.error = null;
      pending.add(id);
      try {
        await gateway.remove(id);
        this.credentials = applyChange(this.credentials, { kind: "deleted", id });
        if (this.form.id === id) this.closeForm();
        this.announcement = `Deleted ${name}.`;
      } catch (err) {
        if (codeOf(err) === Codes.GIT_CREDENTIAL_NOT_FOUND) this.credentials = applyChange(this.credentials, { kind: "deleted", id });
        else this.error = describeError(err);
      } finally {
        pending.delete(id);
        this.deleting = false;
        this.deleteId = "";
      }
    },

    async test(id) {
      if (this.locked || this.tests[id]?.testing) return;
      this.tests = { ...this.tests, [id]: { testing: true, ok: false, line: "" } };
      try {
        const result = await gateway.test(id);
        this.tests = { ...this.tests, [id]: { testing: false, ok: result.ok, line: testLine(result) } };
      } catch (err) {
        if (codeOf(err) === Codes.GIT_CREDENTIAL_STORE_LOCKED) this.locked = true;
        const view = describeError(err);
        this.tests = { ...this.tests, [id]: { testing: false, ok: false, line: `${view.message} ${view.code} · ${view.next}` } };
      }
    },

    toggleKey(id) {
      this.openKeys = { ...this.openKeys, [id]: !this.openKeys[id] };
    },

    async copyKey(id) {
      const c = this.credentials.find((x) => x.id === id);
      if (!c?.publicKey) return;
      try {
        await copy(c.publicKey);
        this.announcement = `Copied the public key of ${c.name}.`;
      } catch {
        this.announcement = "Could not copy: select the key and copy it yourself.";
      }
    },

    /** Scrolls to and marks one credential (a link from a refusal elsewhere). */
    highlight(id) {
      if (!this.credentials.some((c) => c.id === id)) return;
      this.highlightId = id;
      // $root: this.$el is the element whose expression called the method.
      this.$nextTick(() => this.$root.querySelector?.(`[data-id="${CSS.escape(id)}"]`)?.scrollIntoView?.({ block: "center" }));
    },

    dismissError() {
      this.error = null;
    },
    dismissNotice() {
      this.notice = "";
    },
  };
};
