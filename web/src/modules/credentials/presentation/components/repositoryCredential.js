// One repository's credential on the Repositories page: what its pushes use,
// a Listbox select to pin a credential or go back to automatic, and Test.
// Inside each repository row (the projects module renders the rows; this
// module never imports it):
//
//   <div x-data="repositoryCredential" x-bind:data-repository-id="r.id">
//     <div class="[ dropdown ]" x-data="dropdown" data-size="sm" x-bind:data-error="pickError" x-on:dropdown-retry="retry">
//       <select x-ref="select" x-on:change="pick($event.target.value)">…options…</select>
//     </div>
//   </div>
//
// The repository id is read when needed, not at init: under x-for the
// binding that sets it may run after this component starts.

import { candidatesFor, automaticFor, kindLabel } from "../../domain/credential.js";
import { describeError } from "../../../../shared/presentation/errors.js";
import { bindingLine, testLine } from "../view.js";

/** @param {{ store: ReturnType<typeof import("../repositoryStore.js").repositoryCredentialsStore> }} deps */
export const repositoryCredential = ({ store }) => () => ({
  busy: false,
  pickError: "",
  lastPick: null,
  testing: false,
  testResult: null,

  // $root: this.$el is the element whose expression asked.
  get repositoryId() {
    return this.$root.dataset.repositoryId ?? "";
  },
  get rc() {
    return store.byRepository[this.repositoryId] ?? null;
  },
  get known() {
    return this.rc !== null;
  },
  get line() {
    return this.rc ? bindingLine(this.rc) : { state: "unknown", name: "", text: "Reading…", addHref: "" };
  },
  get selected() {
    return this.rc?.credentialId ?? "";
  },
  /** Automatic first, then what the origin can be pinned to. */
  get options() {
    if (!this.rc) return [];
    const auto = automaticFor(store.credentials, this.rc.remoteUrl);
    const first = { value: "", label: "Automatic", description: auto ? `Uses ${auto.name} now` : "Nothing fits now", meta: "" };
    const pins = candidatesFor(store.credentials, this.rc.remoteUrl).map((c) => ({
      value: c.id,
      label: c.name,
      description: [kindLabel(c.kind), c.username, c.fingerprint].filter(Boolean).join(" · "),
      meta: c.isDefault ? "default" : "",
    }));
    return [first, ...pins];
  },
  get pickLabel() {
    return `Credential for ${this.$root.dataset.repositoryName || "this repository"}`;
  },
  get cannotTest() {
    return this.testing || store.locked || !this.rc?.resolved;
  },
  get testOk() {
    return !!this.testResult?.ok;
  },
  get testText() {
    return this.testResult?.line ?? "";
  },

  init() {
    store.start(this.$root.ownerDocument);
    // A repository added after the page loaded has no binding yet.
    queueMicrotask(() => this.repositoryId && !this.known && store.refresh());
  },

  /** The select changed: pin that credential, or "" for automatic. */
  async pick(value) {
    const credentialId = value || null;
    if (credentialId === (this.rc?.credentialId ?? null) || this.busy) return;
    this.busy = true;
    this.pickError = "";
    this.lastPick = credentialId;
    try {
      await store.pin(this.repositoryId, credentialId);
      this.testResult = null;
    } catch (err) {
      const view = describeError(err);
      this.pickError = `${view.message} (${view.code}: ${view.next})`;
    } finally {
      this.busy = false;
      // The select shows what is stored, never a choice the server refused.
      if (this.$refs.select) this.$refs.select.value = this.selected;
    }
  },

  /** dropdown-retry: the same choice again. */
  retry() {
    this.pickError = "";
    return this.pick(this.lastPick ?? "");
  },

  async test() {
    if (this.cannotTest) return;
    this.testing = true;
    this.testResult = null;
    try {
      const result = await store.test(this.repositoryId);
      if (result) this.testResult = { ok: result.ok, line: testLine(result) };
    } catch (err) {
      const view = describeError(err);
      this.testResult = { ok: false, line: `${view.message} ${view.code} · ${view.next}` };
    } finally {
      this.testing = false;
    }
  },
});
