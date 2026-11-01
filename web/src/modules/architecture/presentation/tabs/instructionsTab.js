// The Instructions & rules tab of the Architecture page: a component nested in
// architecturePage, so `this.draft`, `this.errorFor(path)` and `this.isInvalid(path)`
// reach the page's draft and its field errors. Rows are edited in a drawer
// (listEditing); a path that discovery did not find is tagged missing. The
// rows are the harness's instruction files merged with the record's entries; a
// row whose file the harness holds opens it in the editor pane beside the list.
// The file's text is the harness's own copy, apart from the page's draft: it is
// never part of the page's save bar. It is saved on its own against the version
// it started from; a refusal, a feed event and a tab switch all keep the text.

import { renderMarkdown } from "../../../../shared/domain/markdown.js";
import { describeError } from "../../../../shared/presentation/errors.js";
import { INSTRUCTION_KINDS, LOADED, blankInstruction, isMissingPath, kindLabel, loadedLabel, rowsAt } from "../../domain/descriptive.js";
import {
  decideOnFeed,
  fieldMessages,
  isDirty,
  lineDiff,
  listItems,
  mergeMetadata,
  notFoundText,
  pathAfterKindChange,
  prefillPath,
  refusalText,
} from "../../domain/instructionFiles.js";
import { agentPicker } from "./agentPicker.js";
import { listEditing, noDrawer } from "./listEditing.js";

/** @param { gateway: ReturnType<typeof import("../../infrastructure/architecture-gateway.js").architectureGateway> } deps */
export const instructionsTab = ({ gateway }) => () => {
  const picker = agentPicker({ gateway });
  const files = gateway.instructionFiles;
  /** The entry the next Add makes: a file the record does not describe, taken into the draft with its defaults. */
  let pending = null;
  let leave = null;
  let unfollow = null;
  /** Feed events that arrived while our own save was in flight, replayed once it has answered. */
  let held = [];
  const editing = listEditing(["instructions"], () => pending ?? blankInstruction(), "editing", (row) => row.path ?? "");
  return {
    ...picker,
    kinds: INSTRUCTION_KINDS,
    loadedModes: LOADED,
    kindLabel: (kind) => kindLabel("instruction", kind),
    loadedLabel,
    /** The drawer: which row it is on, and its working copy. */
    editing: noDrawer(),
    /** The paths discovery found in the checkout, once asked; null until then, and for good if it failed. */
    found: null,
    asked: false,
    ...editing,

    /** The harness's files (metadata), once listed. */
    files: [],
    /** The open file with its text, the path being opened, and why a path could not be shown. */
    file: null,
    opening: "",
    notFound: "",
    openError: "",
    /** Which view of the open file shows: "text" or "preview". */
    fileView: "text",
    /** The person's text in the editor, apart from the file's loaded text. */
    body: "",
    /** A new file's kind and path, and the kind whose prefill the path still holds. */
    newKind: "memory",
    newPath: "",
    prefilledKind: "memory",
    fileBusy: false,
    /** The rename form (open, and the path in it) and the delete confirmation: both are asked in the page, never in a native dialog. */
    renaming: false,
    renamePath: "",
    confirmingDelete: false,
    /** Whether the file was deleted elsewhere (its text stays). */
    fileDeleted: false,
    /** Someone saved a newer version: whether to say so, which, its text once read, and whether the diff shows. */
    newerShown: false,
    newerVersion: 0,
    newer: null,
    showChanges: false,
    /** What the last save said: next to the path, next to the text, or elsewhere. */
    pathError: "",
    bodyError: "",
    saveError: "",

    async init() {
      this.$watch("errors", () => this.openForErrors());
      this.$watch("tab", (tab) => {
        if (tab !== "instructions") return;
        this.shown();
        if (this.file !== null && !this.file.isNew) this.setAddress(this.file.path);
      });
      if (this.isTab("instructions")) {
        this.shown();
        this.openLinked();
      }
      leave = (event) => this.leaving(event);
      window.addEventListener("beforeunload", leave);
      unfollow = files.follow(this.projectId, this.repositoryId, { onChanged: (change) => this.instructionChanged(change) });
      await picker.init.call(this);
    },
    destroy() {
      window.removeEventListener("beforeunload", leave);
      unfollow?.();
    },
    /** Leaving the page with unsaved text asks the browser to confirm. */
    leaving(event) {
      if (!this.fileDirty) return;
      event.preventDefault();
      event.returnValue = "";
    },

    /** The tab was shown: read what the harness holds and what discovery found. */
    shown() {
      this.lookUp();
      this.loadFiles();
    },
    async loadFiles() {
      try {
        this.files = (await files.list(this.repositoryId)).files;
      } catch {
        this.files = []; // the rows are the record's, as before
      }
    },
    /** A deep link `…/instructions?path=<path>` opens that file. */
    openLinked() {
      const path = new URLSearchParams(window.location.search).get("path") ?? "";
      if (path !== "") this.openPath(path);
    },

    /** The list's rows: the record's entries, then each harness file the record does not describe, with the defaults. */
    rows() {
      return mergeMetadata(this.files, rowsAt(this.draft, ["instructions"]));
    },
    /** What the list draws: the rows, with a skill's files under a folder header. */
    items() {
      return listItems(this.rows());
    },
    /** Edit on a file the record does not describe takes it into the draft first (Cancel drops it again). */
    edit(index) {
      if (index < rowsAt(this.draft, ["instructions"]).length) {
        editing.edit.call(this, index);
        return;
      }
      const row = this.rows()[index];
      pending = { path: row.path, kind: row.kind, applies_to: ["all"], loaded: "always" };
      editing.add.call(this);
      pending = null;
    },

    openLabel(row) {
      return `Open ${row.path}`;
    },
    isViewing(row) {
      return !!row && this.file !== null && this.file.path === row.path;
    },
    openRow(row) {
      if (!this.canLeave()) return;
      return this.openPath(row.path);
    },
    /** Reads a file and shows it in the pane; a later choice wins over a slower earlier one. */
    async openPath(path) {
      this.opening = path;
      this.notFound = "";
      this.openError = "";
      try {
        const file = await files.get(this.repositoryId, path);
        if (this.opening !== path) return;
        this.take(file);
        this.fileView = "text";
        this.setAddress(path);
      } catch (err) {
        if (this.opening !== path) return;
        this.file = null;
        if (err?.code === "INSTRUCTION_NOT_FOUND") this.notFound = notFoundText(path);
        else this.openError = describeError(err).message;
      } finally {
        if (this.opening === path) this.opening = "";
      }
    },
    closeFile() {
      if (!this.canLeave()) return;
      this.opening = "";
      this.file = null;
      this.body = "";
      this.clearRefusals();
      this.closeAsks();
      this.notFound = "";
      this.openError = "";
      this.setAddress("");
    },
    /** Puts the open file's path in the address (replacing, so Back keeps leaving the page), or takes it out. */
    setAddress(path) {
      const url = new URL(window.location.href);
      if (path === "") url.searchParams.delete("path");
      else url.searchParams.set("path", path);
      window.history.replaceState(null, "", url);
    },

    get hasFile() {
      return this.file !== null;
    },
    get isOpening() {
      return this.opening !== "";
    },
    get hasNotFound() {
      return this.notFound !== "";
    },
    get hasOpenError() {
      return this.openError !== "";
    },
    /** Whether the pane shows anything: a file, one being opened, or why none could be. */
    paneOpen() {
      return this.hasFile || this.isOpening || this.hasNotFound || this.hasOpenError;
    },
    /** Whether the column beside the list shows anything: the pane or the metadata drawer. */
    sideOpen() {
      return this.paneOpen() || this.drawerOpen();
    },
    get paneTitle() {
      if (this.isNewFile) return "New instruction file";
      return this.file !== null ? this.file.path : this.opening;
    },
    get versionText() {
      if (this.file === null) return "";
      return this.fileDeleted ? "Deleted" : `Version ${this.file.version}`;
    },
    get isNewFile() {
      return this.file !== null && this.file.isNew === true;
    },
    get fileDirty() {
      return this.file !== null && isDirty(this.file.content, this.body);
    },
    get canSave() {
      return this.file !== null && !this.fileBusy && (this.fileDirty || this.isNewFile || this.fileDeleted);
    },
    get cannotSave() {
      return !this.canSave;
    },
    get cannotDiscard() {
      return !this.fileDirty || this.fileBusy;
    },
    /** Whether rename and delete apply: a file the harness holds, not one being made or already gone. */
    get canChange() {
      return this.file !== null && !this.isNewFile && !this.fileDeleted && !this.fileBusy;
    },
    get cannotChange() {
      return !this.canChange;
    },
    get renameShown() {
      return this.renaming && this.file !== null && !this.fileDeleted;
    },
    get deleteAsked() {
      return this.confirmingDelete && this.file !== null;
    },
    get deleteText() {
      const path = this.file === null ? "" : this.file.path;
      const lost = this.fileDirty ? " Your unsaved changes to it are lost too." : "";
      return `Delete ${path}? It leaves the harness, and agents stop getting it.${lost}`;
    },
    get renameDirtyText() {
      return "You have unsaved changes. They stay in the editor on the new path; only the saved text moves.";
    },
    get hasPathError() {
      return this.pathError !== "";
    },
    get hasBodyError() {
      return this.bodyError !== "";
    },
    get hasSaveError() {
      return this.saveError !== "";
    },
    get pathInvalid() {
      return String(this.hasPathError);
    },
    get bodyInvalid() {
      return String(this.hasBodyError);
    },
    get newerText() {
      const which = this.newerVersion > 0 ? `version ${this.newerVersion}` : "a newer version";
      return `Someone saved ${which} of this file. Your text is kept; Reload loads theirs and drops yours.`;
    },
    get canShowDiff() {
      return this.newer !== null;
    },
    get diffLabel() {
      return this.showChanges ? "Hide changes" : "Show changes";
    },
    get diffShown() {
      return this.newerShown && this.showChanges && this.newer !== null;
    },
    /** Your text against the newest version, line by line: removed lines are yours alone, added lines are theirs alone (what Reload would do to your text). */
    get diffLines() {
      return this.newer === null ? [] : lineDiff(this.body, this.newer.content);
    },
    get textShown() {
      return this.fileView === "text";
    },
    get previewShown() {
      return this.fileView === "preview";
    },
    get textPressed() {
      return String(this.textShown);
    },
    get previewPressed() {
      return String(this.previewShown);
    },
    showText() {
      this.fileView = "text";
    },
    showPreview() {
      this.fileView = "preview";
    },
    /** Puts the file's Markdown into its element, escaped on the way. Called from x-effect. @param {HTMLElement} el */
    paintPreview(el) {
      if (this.file !== null && this.fileView === "preview") el.innerHTML = renderMarkdown(this.body) || "<p>Nothing to preview yet.</p>";
      else el.innerHTML = "";
    },

    /** Takes a file read from the harness as the open one: its text is the editor's text, and nothing from an earlier refusal stays. */
    take(file) {
      this.file = file;
      this.body = file.content;
      this.clearRefusals();
      this.closeAsks();
    },
    closeAsks() {
      this.renaming = false;
      this.confirmingDelete = false;
    },
    clearRefusals() {
      this.pathError = "";
      this.bodyError = "";
      this.saveError = "";
      this.fileDeleted = false;
      this.newerShown = false;
      this.newerVersion = 0;
      this.newer = null;
      this.showChanges = false;
    },
    /** Whether the open file may be left: not with unsaved text, which the person saves or discards first. */
    canLeave() {
      if (!this.fileDirty) return true;
      this.saveError = "This file has unsaved changes. Save or Discard them before leaving it.";
      return false;
    },

    /** Add file: an empty editor for a new file, with kind and path to choose (the path prefilled from the kind). */
    addFile() {
      if (!this.canLeave()) return;
      this.closeDrawer();
      this.opening = "";
      this.notFound = "";
      this.openError = "";
      this.take({ path: "", kind: "memory", version: 0, content: "", isNew: true });
      this.newKind = "memory";
      this.prefilledKind = "memory";
      this.newPath = prefillPath("memory");
      this.fileView = "text";
      this.setAddress("");
    },
    /** Add file to skill: a new skill file whose path starts in the skill's folder. */
    addToSkill(item) {
      if (!this.canLeave()) return;
      this.addFile();
      this.newKind = "skill";
      this.prefilledKind = "skill";
      this.newPath = `${item.folderPath}/`;
    },
    /** Add SKILL.md: the same, with the file's name filled in. */
    addSkillMd(item) {
      if (!this.canLeave()) return;
      this.addToSkill(item);
      this.newPath = `${item.folderPath}/SKILL.md`;
    },
    skillMdLabel(item) {
      return `Add SKILL.md to skill ${item.name}`;
    },
    skillLabel(item) {
      return `Add file to skill ${item.name}`;
    },
    kindChanged(event) {
      const kind = event.target.value;
      this.newPath = pathAfterKindChange(this.prefilledKind, this.newPath, kind);
      this.prefilledKind = kind;
      this.newKind = kind;
    },

    /** Saves the text against the version it started from (0 creates); a refusal keeps the text and says why. */
    async saveFile() {
      if (!this.canSave) return;
      const isNew = this.isNewFile;
      const sent = this.body;
      const request = {
        path: isNew ? this.newPath.trim() : this.file.path,
        kind: isNew ? this.newKind : this.file.kind,
        baseVersion: this.file.version,
        content: sent,
      };
      this.clearRefusals();
      this.fileBusy = true;
      held = [];
      try {
        const meta = await files.save(this.repositoryId, request);
        this.file = { path: meta.path, kind: meta.kind, version: meta.version, content: sent, isNew: false };
        if (isNew) this.setAddress(meta.path);
        this.loadFiles();
      } catch (err) {
        this.refused(err);
      } finally {
        this.fileBusy = false;
        const later = held;
        held = [];
        later.forEach((change) => this.applyChange(change));
      }
    },
    refused(err) {
      if (err?.code === "INSTRUCTION_INVALID") {
        const said = fieldMessages(err.fieldErrors ?? []);
        this.pathError = said.path;
        this.bodyError = said.content;
        this.saveError = said.other || (said.path || said.content ? "" : refusalText(err.code));
      } else if (err?.code === "INSTRUCTION_EXISTS") {
        this.pathError = refusalText(err.code);
      } else if (err?.code === "INSTRUCTION_CHANGED") {
        this.noteNewer(0);
      } else if (err?.code === "INSTRUCTION_NOT_FOUND") {
        this.markDeleted();
      } else {
        this.saveError = refusalText(err?.code) || describeError(err).message;
      }
    },
    /** Rename: asks for the new path in the page; the file's text moves with it, and any unsaved text stays in the editor. */
    askRename() {
      if (!this.canChange) return;
      this.clearRefusals();
      this.confirmingDelete = false;
      this.renamePath = this.file.path;
      this.renaming = true;
    },
    cancelRename() {
      this.renaming = false;
      this.pathError = "";
    },
    async renameFile() {
      if (!this.canChange || !this.renaming) return;
      const to = this.renamePath.trim();
      if (to === this.file.path) {
        this.cancelRename();
        return;
      }
      const request = { path: this.file.path, newPath: to, baseVersion: this.file.version };
      this.clearRefusals();
      this.fileBusy = true;
      held = [];
      try {
        const meta = await files.rename(this.repositoryId, request);
        this.file = { ...this.file, path: meta.path, kind: meta.kind, version: meta.version };
        this.renaming = false;
        this.setAddress(meta.path);
        this.loadFiles();
      } catch (err) {
        this.refused(err);
      } finally {
        this.fileBusy = false;
        const later = held;
        held = [];
        later.forEach((change) => this.applyChange(change));
      }
    },
    /** Delete: asks first, naming the file; a refusal keeps the file. */
    askDelete() {
      if (!this.canChange) return;
      this.clearRefusals();
      this.renaming = false;
      this.confirmingDelete = true;
    },
    cancelDelete() {
      this.confirmingDelete = false;
    },
    async deleteFile() {
      if (!this.canChange || !this.confirmingDelete) return;
      const { path, version } = this.file;
      this.clearRefusals();
      this.fileBusy = true;
      held = [];
      try {
        await files.remove(this.repositoryId, { path, baseVersion: version });
        this.file = null;
        this.body = "";
        this.closeAsks();
        this.setAddress("");
        this.loadFiles();
        this.instructionDeleted(path);
      } catch (err) {
        this.confirmingDelete = false;
        this.refused(err);
      } finally {
        this.fileBusy = false;
        const later = held;
        held = [];
        later.forEach((change) => this.applyChange(change));
      }
    },
    /** Discard: the text goes back to what was loaded. */
    discardText() {
      if (this.file === null) return;
      this.body = this.file.content;
      this.clearRefusals();
    },

    /** Someone saved a newer version: say so, keep the text, and read that version for Reload and the diff. */
    noteNewer(version) {
      this.newerShown = true;
      this.newerVersion = version;
      this.newer = null;
      this.showChanges = false;
      this.readNewer(this.file.path);
    },
    async readNewer(path) {
      try {
        const newest = await files.get(this.repositoryId, path);
        if (this.file === null || this.file.path !== path || !this.newerShown) return;
        this.newer = newest;
        this.newerVersion = newest.version;
      } catch (err) {
        if (err?.code === "INSTRUCTION_NOT_FOUND" && this.file !== null && this.file.path === path) this.markDeleted();
      }
    },
    toggleChanges() {
      this.showChanges = !this.showChanges;
    },
    /** Reload: the newest version replaces the editor's text. */
    async reloadFile() {
      if (this.file === null) return;
      const path = this.file.path;
      try {
        const newest = await files.get(this.repositoryId, path);
        if (this.file === null || this.file.path !== path) return;
        this.take(newest);
      } catch (err) {
        if (err?.code === "INSTRUCTION_NOT_FOUND") this.markDeleted();
        else this.saveError = describeError(err).message;
      }
    },
    /** The file is gone from the harness; its text stays, and Save puts it back. */
    markDeleted() {
      if (this.file === null) return;
      this.file = { ...this.file, version: 0 };
      this.newerShown = false;
      this.newer = null;
      this.fileDeleted = true;
    },

    /** A change announced on the project feed: the list follows, and so does the open file unless our own save is in flight (its answer comes with the version; the echo must not read as someone else's change). */
    instructionChanged(change) {
      this.loadFiles();
      if (this.fileBusy) {
        held.push(change);
        return;
      }
      this.applyChange(change);
    },
    applyChange(change) {
      if (this.file === null || this.file.isNew) return;
      const verdict = decideOnFeed({ path: this.file.path, version: this.file.version, dirty: this.fileDirty }, change);
      if (verdict === "reload") this.reloadFile();
      else if (verdict === "notice") this.noteNewer(change.version);
      else if (verdict === "removed") this.markDeleted();
    },

    /** Reads which instruction files discovery found, once, when the tab is first shown (the page's one discovery, shared with the agent pickers). A failure leaves every row unmarked. */
    async lookUp() {
      if (this.asked) return;
      this.asked = true;
      try {
        const found = await gateway.discovered(this.repositoryId);
        this.found = found.instructions.map((i) => i.path);
      } catch {
        this.found = null;
      }
    },
    isMissing(row) {
      if (row.file) return false; // the harness holds it: whether the repository does is not what the tag says
      return isMissingPath(row.path, this.found === null ? null : new Set(this.found));
    },
  };
};
