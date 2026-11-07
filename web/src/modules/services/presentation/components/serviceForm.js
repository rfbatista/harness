// The service form, in a side drawer over the Services page: the kind first
// (command or image), then only that kind's fields, the variables and the .env
// export. The server validates; a refusal lands under the field it names, or in
// a banner. Editing a running service says "Restart to apply" and offers Save
// and restart. Values of the variables live only in this form and are never
// logged (contract: Harness server ↔ Web UI — services, section 1).
//
//   <section x-data="serviceForm"> … x-init="setup(formService, projectId)" …

import { isActive } from "../../domain/service.js";
import { describeError } from "../../../../shared/presentation/errors.js";
import {
  addRow, canSave, draftFromService, fieldOf, formTitle, newDraft, removeRow, setKind, toInput,
} from "../../domain/serviceForm.js";

/** @param {{ gateway: import("../../domain/ports.js").ServicesGateway }} deps */
export const serviceForm = ({ gateway }) => () => {
  let root = null;
  /** The service being edited; null for a new one. */
  let service = null;
  let projectId = "";
  let runtime = { name: "docker", available: true };

  return {
    /** @type {import("../../domain/serviceForm.js").Draft} */
    draft: newDraft(),
    saving: false,
    /** The refusals under their fields: field → message. */
    messages: {},
    /** The row a list refusal names: field → index. */
    badRows: {},
    /** A refusal that names no field: {title, message, code, next, links}. */
    banner: null,

    /** Called once by the template, with what to edit. */
    setup(editing, project, containerRuntime = runtime) {
      root = this.$root;
      service = editing;
      projectId = project;
      runtime = containerRuntime;
      this.draft = editing ? draftFromService(editing) : newDraft();
      this.$nextTick(() => root?.querySelector("#sf-name")?.focus());
    },

    // ── what the markup binds ────────────────────────────────────────────
    get title() {
      return formTitle(service);
    },
    get editing() {
      return service !== null;
    },
    get kindLocked() {
      return service !== null;
    },
    get isCommand() {
      return this.draft.kind === "command";
    },
    get isImage() {
      return this.draft.kind === "image";
    },
    get running() {
      return service !== null && isActive(service);
    },
    get restartBanner() {
      return this.running;
    },
    get restartTitle() {
      return `${service?.name ?? ""} is running.`;
    },
    get saveLabel() {
      return service ? "Save" : "Create service";
    },
    get saveState() {
      return this.running ? "Changes apply at the next start" : "";
    },
    get ready() {
      return canSave(this.draft);
    },
    get saveDisabled() {
      return this.saving || !this.ready;
    },
    get canExport() {
      return this.draft.repositoryId !== "";
    },
    get exportHint() {
      return this.canExport
        ? "Adds the .env saved for the repository (Settings → Repositories) under the variables above."
        : "Pick a repository first.";
    },
    get hasSubdir() {
      return this.draft.repositoryId !== "";
    },
    /** An image service cannot start without its runtime: said in the form, where the kind is chosen. */
    get runtimeNotice() {
      return this.isImage && !runtime.available ? `${runtime.name} was not found on this machine. Image services cannot start until it is installed, or you choose another runtime in the server settings.` : "";
    },
    get hasRuntimeNotice() {
      return this.runtimeNotice !== "";
    },
    get hasBanner() {
      return this.banner !== null;
    },

    fieldMessage(field) {
      return this.messages[field] ?? "";
    },
    hasMessage(field) {
      return (this.messages[field] ?? "") !== "";
    },
    invalid(field) {
      return this.hasMessage(field);
    },
    /** A list row a refusal names. */
    rowInvalid(field, index) {
      return this.badRows[field] === index;
    },

    // ── editing ──────────────────────────────────────────────────────────
    /** @param {{ currentTarget: { value: string } }} event the kind card */
    chooseKind(event) {
      if (this.kindLocked) return;
      this.draft = setKind(this.draft, /** @type {"command" | "image"} */ (event.currentTarget.value));
    },
    /** Typing in a field takes its refusal away. */
    touch(field) {
      if (!(field in this.messages) && !(field in this.badRows)) return;
      const { [field]: _m, ...messages } = this.messages;
      const { [field]: _r, ...badRows } = this.badRows;
      this.messages = messages;
      this.badRows = badRows;
    },
    addPort() { this.draft = addRow(this.draft, "ports"); },
    addArg() { this.draft = addRow(this.draft, "args"); },
    addMapping() { this.draft = addRow(this.draft, "mappings"); },
    /** @param {{ currentTarget: HTMLElement }} event a row's remove button: the list and index are on it */
    removeRow(event) {
      const { list, index } = event.currentTarget.dataset;
      this.draft = removeRow(this.draft, list, Number(index));
    },

    // ── saving ───────────────────────────────────────────────────────────
    async save() {
      await this.submit(false);
    },
    async saveAndRestart() {
      await this.submit(true);
    },

    async submit(restart) {
      if (this.saving) return;
      this.saving = true;
      this.banner = null;
      this.messages = {};
      this.badRows = {};
      let saved = null;
      try {
        const input = toInput(this.draft);
        if (service) {
          const answer = await gateway.updateService(service.id, input);
          saved = { service: answer.service, restartNeeded: answer.restartNeeded, created: false };
        } else {
          saved = { service: await gateway.createService(projectId, input), restartNeeded: false, created: true };
        }
        if (restart && service) {
          try {
            saved = { ...saved, service: await gateway.restartService(service.id), restartNeeded: false };
          } catch (err) {
            // The change is saved; only the restart was refused.
            this.banner = { ...describeError(err), title: `${saved.service.name} was saved but did not restart.` };
          }
        }
        this.$dispatch("service-saved", saved);
      } catch (err) {
        this.refused(err);
      } finally {
        this.saving = false;
      }
    },

    /** A refusal: under its field when it names one, otherwise in the banner. */
    refused(err) {
      const shown = describeError(err);
      const { field, index } = shown.code === "INVALID_INPUT" ? fieldOf(shown.message) : { field: "", index: -1 };
      if (field === "") {
        this.banner = { ...shown, title: "The service was not saved." };
        return;
      }
      this.messages = { [field]: shown.message };
      if (index >= 0) this.badRows = { [field]: index };
      this.$nextTick(() => root?.querySelector(`#sf-${field}`)?.focus());
    },

    cancel() {
      this.$dispatch("service-form-close");
    },
  };
};
