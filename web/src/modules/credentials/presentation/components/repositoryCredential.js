// One repository's credential on the Repositories page: what its pushes use,
// a Listbox select to pin a credential or go back to automatic, and Test.
// Inside each repository row (the projects module renders the rows; this
// module never imports it):
//
//   <div x-data="repositoryCredential" x-bind:data-repository-id="r.id">
//     <div class="[ dropdown ]" x-data="dropdown" data-size="sm" x-bind:data-error="pickError" x-on:dropdown-retry="retry">
//       <select x-effect="fill($el)" x-on:change="pick($event.target)"></select>
//     </div>
//
// The options are written by fill, not by an x-for inside the select: in a
// row cloned from the page's x-for, inside the dropdown, an x-for there is
// never walked.
//   </div>
//
// The repository id is read when needed, not at init: under x-for the
// binding that sets it may run after this component starts.

import { candidatesFor, automaticFor, kindLabel } from "../../domain/credential.js";
import { describeError } from "../../../../shared/presentation/errors.js";
import { bindingLine, testLine } from "../view.js";

/** @param {{ store: ReturnType<typeof import("../repositoryStore.js").repositoryCredentialsStore> }} deps */
export const repositoryCredential = ({ store }) => () => {
  /** This component's element: $root names the nearest x-data, which inside the dropdown is the dropdown. */
  let root = null;
  return {
    busy: false,
    /** The last select a pick came from, for Retry. */
    select: null,
    pickError: "",
    lastPick: null,
    testing: false,
    testResult: null,

    get repositoryId() {
      return root?.dataset.repositoryId ?? "";
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
      return `Credential for ${root?.dataset.repositoryName || "this repository"}`;
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
      root = this.$el;
      store.start(root.ownerDocument);
      // A repository added after the page loaded has no binding yet.
      queueMicrotask(() => this.repositoryId && !this.known && store.refresh());
    },

    /**
     * Writes the options into the select the Listbox select wraps, keeping the
     * stored choice selected. Run by x-effect, so it follows the store.
     * @param {HTMLSelectElement} select
     */
    fill(select) {
      const options = this.options;
      const selected = this.selected;
      const doc = select.ownerDocument;
      const same = select.options.length === options.length && options.every((o, i) => {
        const el = select.options[i];
        return el.value === o.value && el.textContent === o.label && (el.dataset.description ?? "") === o.description && (el.dataset.meta ?? "") === o.meta;
      });
      if (!same) {
        select.replaceChildren(
          ...options.map((o) => {
            const el = doc.createElement("option");
            el.value = o.value;
            el.textContent = o.label;
            if (o.description) el.dataset.description = o.description;
            if (o.meta) el.dataset.meta = o.meta;
            return el;
          }),
        );
      }
      select.value = selected;
    },

    /**
     * The select changed: pin that credential, or "" for automatic. The select
     * comes with the event: it lives in the dropdown's scope, not this one's.
     * @param {HTMLSelectElement} select
     */
    async pick(select) {
      this.select = select;
      const credentialId = select.value || null;
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
        select.value = this.selected;
      }
    },

    /** dropdown-retry: the same choice again. */
    retry() {
      this.pickError = "";
      if (!this.select) return;
      this.select.value = this.lastPick ?? "";
      return this.pick(this.select);
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
  };
  };
