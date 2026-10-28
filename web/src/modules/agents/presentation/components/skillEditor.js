// The skill form, a full page at /agents/skills/new and /agents/skills/<name>/edit:
// the name with its skills/ prefix, the description with its meter and coach,
// the Markdown body (Write and Preview) with Start from, extra files, Who gets
// it, the rename banner, and beside them What will be written, the server's
// dry run as the person types. The save pipeline (Review change, conflict,
// toast, leave guard) is the one the other editors share.
//
// Opened from the agent editor's "Create skill…", it has a way back:
// ?return=<editor path>. A save goes there with ?added=<skill> before the
// #fragment, Cancel goes there without it; the editor restores its own draft
// either way. Mixed into the Agents page, which provides the catalog, the
// route (tab, item, mode), `load()`, `navigateTo()` and the toast.
//
// The page never builds file bytes: the server's dry run is the preview.
// Every member is prefixed sf, except the ones the save pipeline's markup binds
// to by name (see editors.js).

import { describeError } from "../../../../shared/presentation/errors.js";
import { renderMarkdown } from "../../domain/markdown.js";
import { openSave, saveView, savedToast } from "../../domain/savePipeline.js";
import {
  START_FROM, checkSkillDraft, coachDescription, decodeUpload, descriptionMeter, normalizeNewPath, normalizeUploadPath,
  previewModel, reachOf, renameNote, returnHref, safeReturn, sizeText, skillChanges, skillDraft, skillFieldErrors, skillSave,
  startFromBody,
} from "../../domain/skillForm.js";
import { settleEditor } from "./liveCatalog.js";
import { saveController } from "./saveController.js";

const enc = encodeURIComponent;
const plural = (n, one, many = `${one}s`) => `${n} ${n === 1 ? one : many}`;
const byName = (a, b) => a.name.localeCompare(b.name);
const TEMPLATES = ["", startFromBody("workflow"), startFromBody("reference")];

/**
 * @param {{
 *   gateway: Pick<import("../../domain/ports.js").CatalogGateway, "getSkill" | "saveSkill">,
 *   location: { pathname: string, search?: string },
 *   timers?: { setTimeout: (fn: () => void, ms: number) => any, clearTimeout: (id: any) => void },
 * }} deps
 */
export function skillEditor({ gateway, location, timers }) {
  /** @type {any} */
  let page;
  /** @type {ReturnType<typeof saveController>} */
  let saver;
  // The diff models are worked out once per preview, not on every read.
  const models = new WeakMap();

  /** The versions the form read: agents.json from the catalog, the skill's folder from its own read. */
  const versionsOf = (catalog, own) => ({
    ...(catalog.versions["agents.json"] ? { "agents.json": catalog.versions["agents.json"] } : {}),
    ...Object.fromEntries(Object.entries(own).filter(([path]) => path.startsWith("skills/"))),
  });

  const editor = {
    sfOpen: false,
    /** @type {import("../../domain/skillForm.js").SkillDraft | null} */
    sfDraft: null,
    sfSave: openSave({}),
    /** The last preview the server answered, kept while the next is on its way. */
    sfShown: null,
    sfTouched: false,
    sfLoadError: "",
    sfMissing: false,
    /** The way back to the agent editor, if the person came from one. */
    sfBack: "",
    sfBodyTab: "write",
    sfStart: "blank",
    sfCopyFrom: "",
    sfNote: "",
    /** The extra file open for editing, by path. */
    sfEditing: "",
    sfNewOpen: false,
    sfNewPath: "",
    sfNewProblem: "",
    sfUploadNote: "",
    sfSeq: 0,

    setupEditor() {
      page = this;
      saver = saveController({
        run: (options) => gateway.saveSkill(skillSave(page.sfDraft), options),
        onState: (state) => {
          page.sfSave = state;
          if (state.preview) page.sfShown = state.preview;
        },
        blocked: () => page.brokenWhy !== "",
        timers,
      });
    },

    /** Opens, or closes, the form the route names. Called after every move of the route and the first read. */
    syncEditor() {
      const wanted = this.catalog !== null && this.tab === "skills" && this.mode !== "";
      if (!wanted) {
        // Only this form's own state: another editor may have just opened for the same route.
        if (this.sfOpen) this.sfClose();
        return;
      }
      this.sfBegin();
    },

    /** Starts the form from the route: empty, a copy, or the skill read from the server. */
    async sfBegin() {
      const seq = ++this.sfSeq;
      const params = new URLSearchParams(location.search ?? "");
      this.sfOpen = true;
      this.sfDraft = null;
      this.sfShown = null;
      this.sfTouched = false;
      this.sfLoadError = "";
      this.sfMissing = false;
      this.sfBack = safeReturn(params.get("return"));
      this.sfBodyTab = "write";
      this.sfStart = "blank";
      this.sfCopyFrom = "";
      this.sfNote = "";
      this.sfEditing = "";
      this.sfNewOpen = false;
      this.sfNewPath = "";
      this.sfNewProblem = "";
      this.sfUploadNote = "";
      this.reviewOpen = false;
      this.departing = false;
      saver.open({});

      const edit = this.mode === "edit";
      if (edit && !this.catalog.skills.some((s) => s.name === this.item)) {
        this.sfMissing = true;
        return;
      }
      const wanted = edit ? this.item : params.get("from") ?? "";
      let read = null;
      if (wanted && this.catalog.skills.some((s) => s.name === wanted)) {
        try {
          read = await gateway.getSkill(wanted);
        } catch (err) {
          if (seq !== this.sfSeq) return;
          if (edit) {
            this.sfLoadError = describeError(err).message;
            return;
          }
        }
        if (seq !== this.sfSeq) return;
      }
      if (edit && !read) {
        this.sfMissing = true;
        return;
      }
      const draft = edit ? skillDraft(this.catalog, { previous: read.skill }) : skillDraft(this.catalog, { name: params.get("name") ?? "", from: read?.skill });
      this.sfDraft = draft;
      saver.open(versionsOf(this.catalog, edit ? read.versions : {}));
      if (skillChanges(draft) > 0 && !edit) this.sfChanged();
    },

    closeEditor() {
      this.sfClose();
    },
    sfClose() {
      this.sfSeq++;
      saver.open({});
      this.sfOpen = false;
      this.sfDraft = null;
      this.sfShown = null;
      this.sfMissing = false;
      this.reviewOpen = false;
      this.leaving = null;
    },

    // The page.

    get skillEditing() {
      return this.sfOpen;
    },
    get sfReady() {
      return this.sfDraft !== null;
    },
    get sfLoading() {
      return this.sfOpen && this.sfDraft === null && !this.sfMissing && this.sfLoadError === "";
    },
    get sfHasLoadError() {
      return this.sfLoadError !== "";
    },
    get sfIsEdit() {
      return this.sfDraft ? this.sfDraft.previousName !== "" : this.mode === "edit";
    },
    get skillTitle() {
      return this.sfIsEdit ? `Edit ${this.sfDraft?.previousName || this.item}` : "New skill";
    },
    get sfSubtitle() {
      return this.sfIsEdit
        ? "Changes the folder in the catalog, skills/. Agents pick the change up at their next session start."
        : "Creates a folder in the catalog, skills/, with a SKILL.md. Agents get it through the bundles or agents you pick at the end.";
    },
    get sfBackLabel() {
      return this.sfBack ? "← Back to the agent" : "← Skills";
    },
    get sfBackHref() {
      return this.sfBack || (this.sfIsEdit ? `/agents/skills/${enc(this.sfDraft?.previousName || this.item)}` : "/agents/skills");
    },
    get editorHref() {
      return `${location.pathname}${location.search ?? ""}`;
    },
    get saveButtonLabel() {
      if (this.sv.saving) return "Saving…";
      return this.sfIsEdit ? "Save skill" : "Create skill";
    },
    get sfPathChip() {
      const name = this.sfDraft?.previousName || this.sfDraft?.name || "<name>";
      return `skills/${name}/SKILL.md`;
    },

    // Checks.

    get sfCheck() {
      return this.sfDraft && this.catalog
        ? checkSkillDraft(this.sfDraft, this.catalog, { touched: this.sfTouched || this.sfDraft.previousName !== "" })
        : { valid: false, nameProblem: "", taken: null, descriptionProblem: "" };
    },
    get sfChangeCount() {
      return this.sfDraft ? skillChanges(this.sfDraft) : 0;
    },
    /** The name another skill already has: the form knows at once, the server's NAME_TAKEN covers a catalog that moved. */
    get sfTaken() {
      if (this.sfCheck.taken) return this.sfCheck.taken;
      const refusal = this.sfSave.refusal;
      if (refusal?.code !== "NAME_TAKEN" || !this.sfDraft) return null;
      return { name: this.sfDraft.name, href: `/agents/skills/${enc(this.sfDraft.name)}`, suggestion: refusal.suggestion };
    },
    get sfWrites() {
      const d = this.sfDraft;
      if (!d || !this.catalog) return ["skills/"];
      const readers = d.bundles.some((b) => !d.base.bundles.includes(b)) || d.base.bundles.some((b) => !d.bundles.includes(b))
        || d.agents.some((a) => !d.base.agents.includes(a)) || d.base.agents.some((a) => !d.agents.includes(a));
      const renamed = renameNote(this.catalog, d.previousName, d.name).places > 0;
      return [`skills/${d.name || "<name>"}/`, ...(readers || renamed ? ["agents.json"] : [])];
    },
    get sv() {
      const view = saveView(this.sfSave, { valid: this.sfCheck.valid, changes: this.sfChangeCount, writes: this.sfWrites, blocked: this.brokenWhy });
      if (view.blocked) return view;
      const verb = this.sfIsEdit ? "save" : "create";
      if (view.conflict) return { ...view, footer: "Saving is off until you choose Reload or Overwrite." };
      if (!view.dirty) return this.sfIsEdit ? view : { ...view, footer: "Nothing is written until you create the skill." };
      if (!this.sfCheck.valid) {
        const d = this.sfDraft;
        const bad = [d?.name === "" || this.sfCheck.nameProblem !== "", (d?.description.trim() ?? "") === "" || this.sfCheck.descriptionProblem !== ""].filter(Boolean).length;
        return { ...view, footer: `Fix ${plural(Math.max(bad, 1), "field")} to ${verb} the skill.` };
      }
      return this.sfIsEdit ? { ...view, footer: `${view.footer}. Agents pick it up at their next session start.` } : view;
    },
    get reviewFiles() {
      return this.sv.files;
    },
    get conflictFiles() {
      return this.sv.conflictFiles;
    },

    get fieldErrors() {
      const server = skillFieldErrors(this.sfSave.refusal);
      return {
        name: this.sfTaken ? `A skill named ${this.sfTaken.name} already exists.` : this.sfCheck.nameProblem || server.name,
        description: this.sfCheck.descriptionProblem || server.description,
        files: server.files,
        form: server.form,
      };
    },
    get formInvalid() {
      return this.fieldErrors.form !== "";
    },
    get refusalNext() {
      return this.sfSave.refusal?.next ?? "";
    },
    get refusalCode() {
      return this.sfSave.refusal?.code ?? "";
    },
    get sfNameInvalid() {
      return this.fieldErrors.name !== "";
    },
    get sfDescInvalid() {
      return this.fieldErrors.description !== "";
    },
    get sfFilesInvalid() {
      return this.fieldErrors.files !== "";
    },
    get sfNameDescribedBy() {
      return this.sfNameInvalid ? "sf-name-hint sf-name-error" : "sf-name-hint";
    },
    get sfDescDescribedBy() {
      return this.sfDescInvalid ? "sf-desc-hint sf-desc-coach sf-desc-error" : "sf-desc-hint sf-desc-coach";
    },
    get sfFilesDescribedBy() {
      return this.sfFilesInvalid ? "sf-files-hint sf-files-error" : "sf-files-hint";
    },
    get sfHasSuggestion() {
      return this.sfTaken !== null && this.sfTaken.suggestion !== "";
    },
    get sfTakenHref() {
      return this.sfTaken?.href ?? "";
    },
    get sfSuggestion() {
      return this.sfTaken?.suggestion ?? "";
    },

    // Description.

    get sfCoach() {
      if (!this.sfDraft || !this.catalog) return [];
      return coachDescription(this.sfDraft.description, { names: this.catalog.skills.map((s) => s.name), self: this.sfDraft.previousName || this.sfDraft.name })
        .map((c) => ({ ...c, mark: c.ok ? "✓" : "○", state: c.ok ? "ok" : "no", sr: c.ok ? "Done:" : "Not yet:" }));
    },
    get sfMeter() {
      return descriptionMeter(this.sfDraft?.description ?? "");
    },
    get sfMeterText() {
      return this.sfMeter.text;
    },
    get sfMeterStyle() {
      return `width: ${this.sfMeter.percent}%`;
    },
    get sfMeterOver() {
      return this.sfMeter.over ? "true" : null;
    },

    // The rename banner.

    get sfRename() {
      return this.sfDraft && this.catalog ? renameNote(this.catalog, this.sfDraft.previousName, this.sfDraft.name) : { show: false, places: 0, text: "" };
    },
    get sfRenameShown() {
      return this.sfRename.show;
    },
    get sfRenameText() {
      return this.sfRename.text;
    },

    // The body.

    get sfStartOptions() {
      return START_FROM;
    },
    get sfCopyOptions() {
      return this.catalog ? [...this.catalog.skills].sort(byName).filter((s) => s.name !== this.sfDraft?.previousName).map((s) => ({ name: s.name })) : [];
    },
    get sfCopying() {
      return this.sfStart === "copy";
    },
    get sfWriteShown() {
      return this.sfBodyTab === "write";
    },
    get sfPreviewShown() {
      return this.sfBodyTab === "preview";
    },
    get sfWritePressed() {
      return String(this.sfBodyTab === "write");
    },
    get sfPreviewPressed() {
      return String(this.sfBodyTab === "preview");
    },
    get sfHasNote() {
      return this.sfNote !== "";
    },
    showWrite() {
      this.sfBodyTab = "write";
    },
    showPreview() {
      this.sfBodyTab = "preview";
    },
    /** Puts the body, as Markdown, into its element. The text is escaped on the way. Called from x-effect. @param {HTMLElement} el */
    paintBodyPreview(el) {
      el.innerHTML = this.sfDraft && this.sfBodyTab === "preview" ? renderMarkdown(this.sfDraft.body) || "<p>Nothing to preview yet.</p>" : "";
    },

    /** Start from changed: a template fills an empty body; a copy asks which skill. */
    sfStartChanged(event) {
      const id = event.currentTarget.value;
      this.sfNote = "";
      this.sfStart = id;
      if (id === "copy") return;
      this.sfApplyBody(startFromBody(id));
    },
    /** A skill was chosen to copy: its body, its description when empty, and its files. */
    async sfCopyChosen(event) {
      const name = event.currentTarget.value;
      this.sfCopyFrom = name;
      if (!name) return;
      const seq = this.sfSeq;
      try {
        const { skill } = await gateway.getSkill(name);
        if (seq !== this.sfSeq || !this.sfDraft) return;
        const d = this.sfDraft;
        if (this.sfApplyBody(skill.body) && d.description.trim() === "") d.description = skill.description;
        for (const f of skill.files.filter((x) => !x.dir)) this.sfPutFile(f.path, f.content, f.encoding, f.size);
        this.sfChanged();
      } catch (err) {
        this.sfNote = describeError(err).message;
      }
    },
    /** Replaces the body when it is empty or still a template, else says it was kept. Returns whether it did. */
    sfApplyBody(text) {
      const d = this.sfDraft;
      if (!d) return false;
      if (d.body.trim() !== "" && !TEMPLATES.includes(d.body)) {
        this.sfNote = "Your instructions are not empty, so they were kept. Clear them first to start from a template.";
        this.sfStart = "blank";
        return false;
      }
      d.body = text;
      this.sfChanged();
      return true;
    },

    // Extra files.

    get sfFileRows() {
      return (this.sfDraft?.files ?? []).map((f) => ({
        key: f.path,
        path: f.path,
        size: sizeText(f.size),
        badge: f.origin === "added" ? "new" : f.content !== f.original ? "edited" : "",
        hasBadge: f.origin === "added" || f.content !== f.original,
        text: f.encoding === "utf8",
        binary: f.encoding !== "utf8",
        editing: this.sfEditing === f.path,
        content: f.encoding === "utf8" ? f.content : "",
        editLabel: this.sfEditing === f.path ? "Done" : "Edit",
        editAria: `${this.sfEditing === f.path ? "Close" : "Edit"} ${f.path}`,
        removeAria: `Remove ${f.path}`,
      }));
    },
    get sfNoFiles() {
      return (this.sfDraft?.files.length ?? 0) === 0;
    },
    get sfNewProblemShown() {
      return this.sfNewProblem !== "";
    },
    get sfUploadNoteShown() {
      return this.sfUploadNote !== "";
    },
    /** Sets a file's content, or adds it. A path the person removed earlier comes back as a new one. */
    sfPutFile(path, content, encoding, size) {
      const d = this.sfDraft;
      const have = d.files.find((f) => f.path === path);
      const bytes = size ?? new TextEncoder().encode(content).length;
      if (have) {
        have.content = content;
        have.encoding = encoding === "base64" ? "base64" : "utf8";
        have.size = bytes;
        return;
      }
      d.deleted = d.deleted.filter((p) => p !== path);
      d.files.push({ path, content, encoding: encoding === "base64" ? "base64" : "utf8", size: bytes, origin: "added", original: null });
    },
    sfNewFileOpen() {
      this.sfNewOpen = true;
      this.sfNewPath = "";
      this.sfNewProblem = "";
      this.$nextTick?.(() => document.getElementById("sf-new-path")?.focus());
    },
    sfNewFileCancel() {
      this.sfNewOpen = false;
      this.sfNewProblem = "";
    },
    sfNewFileAdd() {
      const { path, problem } = normalizeNewPath(this.sfNewPath);
      if (problem) {
        this.sfNewProblem = problem;
        return;
      }
      if (this.sfDraft.files.some((f) => f.path === path)) {
        this.sfNewProblem = `${path} is already there.`;
        return;
      }
      this.sfPutFile(path, "", "utf8", 0);
      this.sfNewOpen = false;
      this.sfNewProblem = "";
      this.sfEditing = path;
      this.sfChanged();
    },
    sfToggleFile(event) {
      const path = event.currentTarget.dataset.path;
      this.sfEditing = this.sfEditing === path ? "" : path;
    },
    sfFileInput(event) {
      const f = this.sfDraft.files.find((x) => x.path === event.currentTarget.dataset.path);
      if (!f) return;
      f.content = event.currentTarget.value;
      f.size = new TextEncoder().encode(f.content).length;
      this.sfChanged();
    },
    sfRemoveFile(event) {
      const path = event.currentTarget.dataset.path;
      const d = this.sfDraft;
      const f = d.files.find((x) => x.path === path);
      if (!f) return;
      d.files = d.files.filter((x) => x.path !== path);
      if (f.origin === "existing") d.deleted.push(path);
      if (this.sfEditing === path) this.sfEditing = "";
      this.sfChanged();
    },
    /** The buttons that open the file chooser, and the folder chooser. */
    sfChooseFiles() {
      document.getElementById("sf-upload-files")?.click();
    },
    sfChooseFolder() {
      document.getElementById("sf-upload-folder")?.click();
    },
    /** Files or a folder were chosen: each lands inside the skill, text as text and the rest as base64. */
    async sfUpload(event) {
      const input = event.currentTarget;
      const folder = input.hasAttribute("webkitdirectory");
      const chosen = [...input.files];
      const seq = this.sfSeq;
      let added = 0;
      let skipped = 0;
      for (const file of chosen) {
        const path = normalizeUploadPath(folder ? file.webkitRelativePath || file.name : file.name, { folder });
        if (path === null) {
          skipped++;
          continue;
        }
        const { content, encoding } = decodeUpload(new Uint8Array(await file.arrayBuffer()));
        if (seq !== this.sfSeq || !this.sfDraft) return;
        this.sfPutFile(path, content, encoding, file.size);
        added++;
      }
      input.value = "";
      this.sfUploadNote = `Added ${plural(added, "file")}${skipped ? `, skipped ${skipped} (SKILL.md, hidden or outside the folder)` : ""}.`;
      if (added) this.sfChanged();
    },

    // Who gets it.

    get sfBundleRows() {
      if (!this.catalog || !this.sfDraft) return [];
      return [...this.catalog.bundles].sort(byName).map((b) => ({
        name: b.name,
        label: `@${b.name}`,
        checked: this.sfDraft.bundles.includes(b.name),
        hint: `→ ${plural(reachOf(this.catalog, { bundles: [b.name], agents: [] }).agents.length, "agent")}`,
      }));
    },
    get sfAgentRows() {
      if (!this.catalog || !this.sfDraft) return [];
      return [...this.catalog.agents].sort(byName).map((a) => ({ name: a.name, label: a.name, checked: this.sfDraft.agents.includes(a.name), hint: "" }));
    },
    get sfNoBundles() {
      return (this.catalog?.bundles.length ?? 0) === 0;
    },
    get sfNoAgents() {
      return (this.catalog?.agents.length ?? 0) === 0;
    },
    get sfReach() {
      return this.sfDraft && this.catalog ? reachOf(this.catalog, this.sfDraft) : { agents: [], none: true, text: "" };
    },
    get sfReachText() {
      return this.sfReach.text;
    },
    sfToggleBundle(event) {
      const name = event.currentTarget.dataset.name;
      const d = this.sfDraft;
      d.bundles = d.bundles.includes(name) ? d.bundles.filter((b) => b !== name) : [...d.bundles, name];
      this.sfChanged();
    },
    sfToggleAgent(event) {
      const name = event.currentTarget.dataset.name;
      const d = this.sfDraft;
      d.agents = d.agents.includes(name) ? d.agents.filter((a) => a !== name) : [...d.agents, name];
      this.sfChanged();
    },

    // What will be written.

    get sfPreviewFiles() {
      const p = this.sfShown;
      if (!p) return [];
      if (!models.has(p)) models.set(p, previewModel(p.writes));
      return models.get(p);
    },
    get sfHasPreview() {
      return this.sfShown !== null;
    },
    get sfPreviewStale() {
      return this.sfShown !== null && this.sfSave.preview === null && this.sfSave.phase !== "saved" ? "true" : null;
    },
    get sfPreviewCount() {
      const n = this.sfPreviewFiles.length;
      if (n === 0) return "";
      return `${plural(n, "file")}${this.sfPreviewFiles.some((f) => f.path === "agents.json") ? " · agents.json changes" : ""}`;
    },
    /** What the panel says while it has no preview: why not, and what to do. */
    get sfPreviewHint() {
      if (this.sfShown !== null) return "";
      if (this.sfSave.phase === "previewing") return "Asking the server what it would write…";
      if (this.sfSave.refusal && !this.formInvalid) return "No preview until the error above is fixed.";
      if (this.sfSave.refusal) return "No preview: the server refused. Its answer is above.";
      if (!this.sfCheck.valid) return "Give the skill a name and a description, and the files it will write show here.";
      return this.sfIsEdit ? "No changes to write." : "Type a name and a description to see the files.";
    },
    get sfPreviewBusy() {
      return this.sfSave.phase === "previewing" || (this.sfSave.phase === "dirty" && this.sfSave.preview === null && this.sfCheck.valid) ? "true" : "false";
    },

    // Changing the form.

    /** The form changed: the name loses a typed skills/, and the pipeline schedules the preview. */
    sfNameInput() {
      const d = this.sfDraft;
      if (d.name.startsWith("skills/")) d.name = d.name.slice("skills/".length);
      this.sfChanged();
    },
    /** The suggested name, taken. */
    sfUseSuggestion() {
      this.sfDraft.name = this.sfSuggestion;
      this.sfChanged();
    },
    sfChanged() {
      this.sfTouched = true;
      saver.edited({ dirty: this.sfChangeCount > 0, valid: this.sfCheck.valid });
    },
    /** Moves the keyboard to the first field the form or the server found wrong. */
    sfFocusFirstInvalid() {
      this.$nextTick?.(() => document.querySelector("[data-skill-form] [aria-invalid='true']")?.focus());
    },

    // Saving, and the way out.

    async saveEditor() {
      if (!this.sv.canSave) return;
      await this.sfFinish(await saver.save());
    },
    async overwrite() {
      await this.sfFinish(await saver.save({ force: true }));
    },
    /** A saved skill: read the catalog again, show the toast, and go back to the editor it was made for, or to the skill. */
    async sfFinish({ ok, result }) {
      if (!ok || !result) {
        this.sfFocusFirstInvalid();
        return;
      }
      const name = this.sfDraft.name;
      const back = this.sfBack;
      this.showToast(savedToast({ id: result.change.id, summary: result.change.summary }));
      await this.load();
      this.closeEditor();
      this.navigateTo(back ? returnHref(back, name) : `/agents/skills/${enc(name)}`);
    },
    /** The conflict banner's Reload: drop my edits and read the skill again. */
    async reloadEditor() {
      this.clearDisk("skill", this.sfDraft?.previousName ?? "");
      await this.load();
      await this.sfBegin();
    },
    /** The catalog was read again: settle this editor against what is on disk now (liveCatalog.js). */
    catalogMoved(ctx) {
      if (this.sfOpen && this.sfDraft) settleEditor(saver, "skill", this.sfDraft.previousName ?? "", ctx);
    },
    cancelEditor(event) {
      event.preventDefault();
      const go = this.sfBack ? returnHref(this.sfBack, "") : this.sfBackHref;
      this.guarded(() => this.navigateTo(go));
    },
    /** The back link: the same as Cancel. */
    sfBackClick(event) {
      this.cancelEditor(event);
    },
  };

  return editor;
}
