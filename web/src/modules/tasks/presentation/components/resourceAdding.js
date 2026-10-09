// Adding resources, a part of the task's resources page (taskResources.js):
// files from the picker, dropped on the Resources column or pasted, each a
// row at once that waits for a slot and uploads with progress; and the
// inline Add link form. Drag and paste are shortcuts: Add files is the whole
// path for the keyboard.

import { Codes, codeOf } from "../../../../shared/domain/errors.js";
import { linkClaim, uploadClaim } from "../../domain/items.js";
import { linkProblem, pastedTitle, titleProblem } from "../../domain/resources.js";
import { failed, newUpload, progressed, retried, startable, started, uploadTitle, UploadState } from "../../domain/uploads.js";
import { uploadRow } from "../resourceView.js";

const LINK_REFUSED = "Use an address that starts with http:// or https://.";

/** A clipboard image with no name of its own ("image.png") gets the time it was pasted as its title. */
const unnamed = (file) => !file.name || /^image\.\w+$/i.test(file.name);

const hasFiles = (event) => [...(event.dataTransfer?.types ?? [])].includes("Files");

const isField = (el) => el instanceof HTMLElement && (el.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName));

/**
 * @param {{
 *   gateway: import("../../domain/ports.js").ResourceGateway,
 *   clock: import("../../../../shared/infrastructure/clock.js").Clock,
 *   guards: { resources: ReturnType<typeof import("../../domain/items.js").echoGuard> },
 * }} deps
 */
export function resourceAdding({ gateway, clock, guards }) {
  let nextKey = 1;
  /** Row key → its File and, while it runs, the controller that cancels it. Not reactive. */
  const files = new Map();
  const controllers = new Map();
  let windowDepth = 0;
  let zoneDepth = 0;

  return {
    /** @type {import("../../domain/uploads.js").UploadRow[]} */
    uploads: [],
    dropArmed: false,
    dropOver: false,

    linkOpen: false,
    linkUrl: "",
    linkTitle: "",
    linkError: "",
    linkTitleError: "",
    linkBusy: false,

    destroyAdding() {
      this.cancelUploads();
    },

    get uploadRows() {
      return this.uploads.map(uploadRow);
    },
    get cannotAdd() {
      return this.taskGone;
    },

    // ── files ──────────────────────────────────────────────────────────
    pickFiles() {
      if (!this.taskGone) this.pageRoot()?.querySelector("[data-file-input]")?.click();
    },
    /** The picker's change event. */
    picked(event) {
      const input = event.target;
      this.addFiles([...(input.files ?? [])]);
      input.value = ""; // the same file again is a new add
    },

    /**
     * @param {File[]} list
     * @param {(file: File) => string} [titleOf]  the title to send; "" lets the server use the name
     */
    addFiles(list, titleOf = () => "") {
      if (this.taskGone || list.length === 0) return;
      const rows = list.map((file) => {
        const key = `up-${nextKey++}`;
        files.set(key, file);
        return newUpload(key, file, titleOf(file));
      });
      this.uploads = [...this.uploads, ...rows];
      const sending = rows.filter((r) => r.state !== UploadState.FAILED).length;
      if (sending > 0) this.announce(sending === 1 ? `Uploading ${uploadTitle(rows.find((r) => r.state !== UploadState.FAILED))}.` : `Uploading ${sending} files.`);
      this.pump();
    },

    /** Starts what the free slots allow. */
    pump() {
      for (const row of startable(this.uploads)) this.startUpload(row);
    },

    replaceUpload(key, fn) {
      this.uploads = this.uploads.map((u) => (u.key === key ? fn(u) : u));
    },
    dropUpload(key) {
      this.uploads = this.uploads.filter((u) => u.key !== key);
      files.delete(key);
      controllers.delete(key);
    },

    async startUpload(row) {
      const file = files.get(row.key);
      if (!file) return;
      this.replaceUpload(row.key, started);
      const controller = new AbortController();
      controllers.set(row.key, controller);
      const claim = guards.resources.claim(uploadClaim(uploadTitle(row), row.size));
      try {
        const resource = await gateway.upload({
          ticketId: this.ticketId,
          file,
          title: row.title,
          signal: controller.signal,
          onProgress: (loaded) => this.replaceUpload(row.key, (u) => progressed(u, loaded)),
        });
        for (const twin of guards.resources.settle(claim, resource.id)) this.showResource(twin);
        this.dropUpload(row.key);
        this.showResource(resource);
        this.toast(`Added “${resource.title}”. Sessions running on this task are told about it.`);
      } catch (err) {
        for (const held of guards.resources.settle(claim, "")) this.showResource(held);
        controllers.delete(row.key);
        if (err?.name === "AbortError") {
          this.dropUpload(row.key);
          this.announce(`Cancelled ${uploadTitle(row)}.`);
        } else {
          this.replaceUpload(row.key, (u) => failed(u, codeOf(err), err?.message ?? ""));
          if (codeOf(err) === Codes.TICKET_NOT_FOUND) this.loseTask();
        }
      } finally {
        this.pump();
      }
    },

    cancelUpload(key) {
      const running = controllers.get(key);
      if (running) running.abort();
      else this.dropUpload(key);
    },
    retryUpload(key) {
      this.replaceUpload(key, retried);
      this.pump();
    },
    dismissUpload(key) {
      this.dropUpload(key);
    },
    /** The task is gone or the page is leaving: nothing more is sent. */
    cancelUploads() {
      for (const c of controllers.values()) c.abort();
      this.uploads = this.uploads.filter((u) => u.state === UploadState.FAILED);
    },

    // ── drag and drop ──────────────────────────────────────────────────
    // Window-level handlers arm the column while files are dragged anywhere
    // over the page and keep a drop outside it from opening the file.
    windowDragEnter(event) {
      if (!hasFiles(event) || this.taskGone) return;
      windowDepth++;
      this.dropArmed = true;
    },
    windowDragOver(event) {
      if (hasFiles(event)) event.preventDefault();
    },
    windowDragLeave(event) {
      if (!hasFiles(event)) return;
      windowDepth = Math.max(0, windowDepth - 1);
      if (windowDepth === 0) this.disarm();
    },
    windowDrop(event) {
      if (hasFiles(event)) event.preventDefault();
      this.disarm();
    },
    zoneDragEnter(event) {
      if (!hasFiles(event) || this.taskGone) return;
      zoneDepth++;
      this.dropOver = true;
    },
    zoneDragLeave(event) {
      if (!hasFiles(event)) return;
      zoneDepth = Math.max(0, zoneDepth - 1);
      if (zoneDepth === 0) this.dropOver = false;
    },
    zoneDrop(event) {
      if (!hasFiles(event)) return;
      event.preventDefault();
      const list = [...(event.dataTransfer?.files ?? [])];
      this.disarm();
      this.addFiles(list);
    },
    disarm() {
      windowDepth = 0;
      zoneDepth = 0;
      this.dropArmed = false;
      this.dropOver = false;
    },

    // ── paste ──────────────────────────────────────────────────────────
    /** ⌘V / Ctrl V with files on the clipboard, while focus is not in a field. */
    pasted(event) {
      if (isField(event.target) || this.taskGone) return;
      const list = [...(event.clipboardData?.files ?? [])];
      if (list.length === 0) return;
      event.preventDefault();
      const at = clock.now();
      this.addFiles(list, (file) => (file.type.startsWith("image/") && unnamed(file) ? pastedTitle(at, file.type) : ""));
    },

    // ── add a link ─────────────────────────────────────────────────────
    toggleLink() {
      if (this.linkOpen) this.cancelLink();
      else this.openLink();
    },
    openLink() {
      if (this.taskGone) return;
      this.linkOpen = true;
      this.linkError = "";
      this.focusLater("[data-link-url]");
    },
    cancelLink() {
      this.linkOpen = false;
      this.linkUrl = "";
      this.linkTitle = "";
      this.linkError = "";
      this.linkTitleError = "";
      this.focusLater("[data-add-link]");
    },
    async submitLink() {
      if (this.linkBusy || this.taskGone) return;
      const url = this.linkUrl.trim();
      this.linkError = linkProblem(url);
      this.linkTitleError = this.linkTitle.trim() ? titleProblem(this.linkTitle) : "";
      if (this.linkError || this.linkTitleError) {
        this.focusLater(this.linkError ? "[data-link-url]" : "[data-link-title]");
        return;
      }
      this.linkBusy = true;
      const claim = guards.resources.claim(linkClaim(url));
      try {
        const resource = await gateway.addLink({ ticketId: this.ticketId, url, title: this.linkTitle });
        for (const twin of guards.resources.settle(claim, resource.id)) this.showResource(twin);
        this.showResource(resource);
        this.toast(`Added “${resource.title}”. Sessions running on this task are told about it.`);
        this.cancelLink();
      } catch (err) {
        for (const held of guards.resources.settle(claim, "")) this.showResource(held);
        if (codeOf(err) === Codes.TICKET_NOT_FOUND) this.loseTask();
        this.linkError = codeOf(err) === Codes.INVALID_INPUT ? LINK_REFUSED : `The link was not added: ${err?.message ?? "something went wrong"}.`;
      } finally {
        this.linkBusy = false;
      }
    },
    get linkInvalid() {
      return this.linkError !== "";
    },
    get linkTitleInvalid() {
      return this.linkTitleError !== "";
    },
  };
}
