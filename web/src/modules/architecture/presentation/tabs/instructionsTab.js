// The Instructions & rules tab of the Architecture page: a component nested in
// architecturePage, so `this.draft`, `this.errorFor(path)` and `this.isInvalid(path)`
// reach the page's draft and its field errors. Rows are edited in a drawer
// (listEditing); a path that discovery did not find is tagged missing. The
// rows are the harness's instruction files merged with the record's entries; a
// row whose file the harness holds opens it in the editor pane beside the list.
// The file's text is the harness's own copy, apart from the page's draft: it is
// never part of the page's save bar.

import { renderMarkdown } from "../../../../shared/domain/markdown.js";
import { describeError } from "../../../../shared/presentation/errors.js";
import { INSTRUCTION_KINDS, LOADED, blankInstruction, isMissingPath, kindLabel, loadedLabel, rowsAt } from "../../domain/descriptive.js";
import { mergeMetadata, notFoundText } from "../../domain/instructionFiles.js";
import { agentPicker } from "./agentPicker.js";
import { listEditing, noDrawer } from "./listEditing.js";

/** @param { gateway: ReturnType<typeof import("../../infrastructure/architecture-gateway.js").architectureGateway> } deps */
export const instructionsTab = ({ gateway }) => () => {
  const picker = agentPicker({ gateway });
  const files = gateway.instructionFiles;
  /** The entry the next Add makes: a file the record does not describe, taken into the draft with its defaults. */
  let pending = null;
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

    async init() {
      this.$watch("errors", () => this.openForErrors());
      this.$watch("tab", (tab) => {
        if (tab === "instructions") this.shown();
      });
      if (this.isTab("instructions")) {
        this.shown();
        this.openLinked();
      }
      await picker.init.call(this);
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
      return this.file !== null && this.file.path === row.path;
    },
    openRow(row) {
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
        this.file = file;
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
      this.opening = "";
      this.file = null;
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
      return this.file !== null ? this.file.path : this.opening;
    },
    get versionText() {
      return this.file !== null ? `Version ${this.file.version}` : "";
    },
    get fileText() {
      return this.file !== null ? this.file.content : "";
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
      if (this.file !== null && this.fileView === "preview") el.innerHTML = renderMarkdown(this.file.content) || "<p>Nothing to preview yet.</p>";
      else el.innerHTML = "";
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
