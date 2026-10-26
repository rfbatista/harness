// The bundle editor in the Bundles tab's detail pane: New bundle and Edit.
// A name with the @ prefix, the skill-and-bundle picker with chips, the rename
// banner, and the save pipeline (preview, Review change, conflict, toast,
// leave guard) around them. Mixed into the Agents page, which provides the
// catalog, the route (tab, item, mode), `load()` and `navigateTo()`.
//
// The page never builds file bytes: the server's dry run is the preview.

import {
  bundleChanges, bundleDraft, bundleFieldErrors, bundlePickerOptions, checkBundleDraft, cleanBundleName, renameImpact,
} from "../../domain/bundles.js";
import { openSave, saveView, savedToast } from "../../domain/savePipeline.js";
import { entryPickerMixin } from "./entryPickerMixin.js";
import { saveController } from "./saveController.js";

const enc = encodeURIComponent;
const FILES = ["agents.json"];
const NO_OPTIONS = { options: [], hidden: 0, empty: true };

/** The versions of the files this editor writes, as the catalog read them. */
const versionsOf = (catalog) => Object.fromEntries(FILES.filter((f) => f in catalog.versions).map((f) => [f, catalog.versions[f]]));

/**
 * @param {{
 *   gateway: Pick<import("../../domain/ports.js").CatalogGateway, "saveBundle">,
 *   location: { pathname: string, search?: string },
 *   timers?: { setTimeout: (fn: () => void, ms: number) => any, clearTimeout: (id: any) => void },
 * }} deps
 */
export function bundleEditor({ gateway, location, timers }) {
  /** @type {any} */
  let page;
  /** @type {ReturnType<typeof saveController>} */
  let saver;

  const picker = entryPickerMixin("bundlePicker", {
    options: (p, query) => (p.catalog && p.draft ? bundlePickerOptions(p.catalog, { query, chosen: p.draft.entries, exclude: p.draft.previousName }) : NO_OPTIONS),
    chips: (p) => {
      if (!p.draft) return [];
      const skills = new Set(p.catalog.skills.map((s) => s.name));
      const bundles = new Set(p.catalog.bundles.map((b) => b.name));
      return p.draft.entries.map((e) => {
        const nested = e.startsWith("@");
        return { key: e, label: e, kind: nested ? "bundle" : "skill", missing: nested ? !bundles.has(e.slice(1)) : !skills.has(e) };
      });
    },
    add: (p, key) => {
      if (!p.draft || p.draft.entries.includes(key)) return;
      p.draft.entries.push(key);
      p.draftChanged();
    },
    remove: (p, key) => {
      if (!p.draft) return;
      p.draft.entries = p.draft.entries.filter((e) => e !== key);
      p.draftChanged();
    },
  });

  const editor = {
    /** The form: { previousName, name, entries }, or null when no editor is open. */
    draft: null,
    save: openSave({}),

    /** Called once, from the page's init. */
    setupEditor() {
      page = this;
      saver = saveController({
        run: (options) => gateway.saveBundle({ previousName: page.draft.previousName, bundle: { name: page.draft.name, entries: [...page.draft.entries] } }, options),
        onState: (state) => { page.save = state; },
        timers,
      });
    },

    /** Opens, or closes, the editor the route names. Called after every move of the route and the first read. */
    syncEditor() {
      const wanted = this.catalog !== null && this.tab === "bundles" && this.mode !== "";
      if (!wanted) {
        if (this.draft !== null) this.closeEditor();
        return;
      }
      const draft = bundleDraft(this.catalog, this.mode === "edit" ? this.item : "");
      if (draft && this.mode === "new") {
        const from = new URLSearchParams(location.search ?? "").get("from");
        const source = from ? this.catalog.bundles.find((b) => b.name === from) : undefined;
        if (source) draft.entries = [...source.entries];
      }
      this.draft = draft;
      this.reviewOpen = false;
      this.bundlePickerQuery = "";
      saver.open(versionsOf(this.catalog));
      if (draft && draft.entries.length > 0 && draft.previousName === "") this.draftChanged();
    },

    closeEditor() {
      saver.open({});
      this.draft = null;
      this.reviewOpen = false;
      this.leaving = null;
    },

    get editing() {
      return this.draft !== null;
    },
    get editorTitle() {
      return this.draft?.previousName ? `Edit @${this.draft.previousName}` : "New bundle";
    },
    get editorPath() {
      return this.draft?.previousName ? `agents.json › bundles.${this.draft.previousName}` : "will be added to agents.json › bundles";
    },
    get editorHref() {
      return this.draft?.previousName ? `/agents/bundles/${enc(this.draft.previousName)}/edit` : "/agents/bundles/new";
    },
    get saveLabel() {
      return this.draft?.previousName ? "Save bundle" : "Create bundle";
    },
    get saveButtonLabel() {
      return this.sv.saving ? "Saving…" : this.saveLabel;
    },

    get draftCheck() {
      return this.draft ? checkBundleDraft(this.draft) : { valid: false, nameProblem: "", entriesProblem: "" };
    },
    get draftChanges() {
      return this.draft && this.catalog ? bundleChanges(this.catalog, this.draft) : 0;
    },
    /** The save pipeline's view: footer text, Save on or off, the review and the conflict. */
    get sv() {
      return saveView(this.save, { valid: this.draftCheck.valid, changes: this.draftChanges, writes: FILES });
    },
    get reviewFiles() {
      return this.sv.files;
    },
    get conflictFiles() {
      return this.sv.conflictFiles;
    },

    /** Each field's error: what the form can tell at once, else what the server refused. */
    get fieldErrors() {
      const server = bundleFieldErrors(this.save.refusal);
      return { name: this.draftCheck.nameProblem || server.name, entries: server.entries, form: server.form };
    },
    get nameInvalid() {
      return this.fieldErrors.name !== "";
    },
    get entriesInvalid() {
      return this.fieldErrors.entries !== "";
    },
    get formInvalid() {
      return this.fieldErrors.form !== "";
    },
    get nameDescribedBy() {
      return this.nameInvalid ? "bundle-name-hint bundle-name-error" : "bundle-name-hint";
    },
    get entriesDescribedBy() {
      return this.entriesInvalid ? "bundle-entries-hint bundle-entries-error" : "bundle-entries-hint";
    },
    get refusalNext() {
      return this.save.refusal?.next ?? "";
    },
    get refusalCode() {
      return this.save.refusal?.code ?? "";
    },

    /** The banner that names what a rename rewrites. */
    get renameBanner() {
      if (!this.draft || !this.catalog) return { show: false, from: "", to: "", text: "" };
      const impact = renameImpact(this.catalog, this.draft.previousName, this.draft.name);
      return { show: impact.renamed, from: `@${this.draft.previousName}`, to: `@${this.draft.name}`, text: impact.text };
    },

    /** The form changed. The name loses a typed @, and the pipeline schedules the preview. */
    draftChanged() {
      this.draft.name = cleanBundleName(this.draft.name);
      saver.edited({ dirty: this.draftChanges > 0, valid: this.draftCheck.valid });
    },

    async saveEditor() {
      if (!this.sv.canSave) return;
      await this.finish(await saver.save());
    },
    /** The conflict banner's Overwrite: the same write again, with force. */
    async overwrite() {
      await this.finish(await saver.save({ force: true }));
    },
    /** A saved bundle: read the catalog again, show the toast, and land on the bundle. */
    async finish({ ok, result }) {
      if (!ok || !result) return;
      const name = this.draft.name;
      this.showToast(savedToast({ id: result.change.id, summary: result.change.summary }));
      await this.load();
      this.closeEditor();
      this.navigateTo(`/agents/bundles/${enc(name)}`);
    },
    /** The conflict banner's Reload: drop my edits and read the catalog again. */
    async reloadEditor() {
      const previous = this.draft.previousName;
      await this.load();
      const draft = this.catalog ? bundleDraft(this.catalog, previous) : null;
      if (!draft) {
        this.notice = "That bundle no longer exists";
        this.closeEditor();
        this.navigateTo("/agents/bundles");
        return;
      }
      this.draft = draft;
      this.reviewOpen = false;
      saver.reload(versionsOf(this.catalog));
    },
    cancelEditor(event) {
      event.preventDefault();
      const previous = this.draft?.previousName ?? "";
      this.guarded(() => this.navigateTo(previous ? `/agents/bundles/${enc(previous)}` : "/agents/bundles"));
    },
  };

  return Object.defineProperties({}, { ...Object.getOwnPropertyDescriptors(picker), ...Object.getOwnPropertyDescriptors(editor) });
}
