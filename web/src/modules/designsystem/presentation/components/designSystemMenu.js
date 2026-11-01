// The ⋯ menu of a design system's header: Rename, Edit description, Delete. Each opens an inline
// panel under the header (no dialog). It lives inside designSystemPage and reads the design
// system's id, name, description and version from it; the header follows the change through the
// project feed, so the menu writes nothing back.
//
//   <div x-data="designSystemMenu" data-seed="design-system-seed">

import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { faultOf } from "../../domain/identity.js";

/** @param {{ gateway: ReturnType<typeof import("../../infrastructure/designsystem-gateway.js").designSystemGateway>, navigate: (url: string) => void }} deps */
export const designSystemMenu = ({ gateway, navigate }) => () => ({
  indexHref: "",
  /** The open panel: "", "rename", "description" or "delete". */
  panel: "",
  draftName: "",
  draftDescription: "",
  /** The name typed to confirm a delete. */
  confirm: "",
  fault: null,
  error: null,
  /** The repositories (names) that refused a delete, once refused. */
  usedBy: [],
  saving: false,
  /** The agents the project enables, as the page seeds them, and the spawn form. */
  spawnAgents: [],
  spawnAgentId: "",
  spawnPrompt: "",
  /** Design sessions already running on this design system, read when the form opens. */
  spawnRunning: 0,
  /** The session just started, "" before. */
  spawnedId: "",

  get spawning() {
    return this.panel === "spawn";
  },
  get spawnNoAgents() {
    return this.spawnAgents.length === 0;
  },
  get spawnHasRunning() {
    return this.spawnRunning > 0;
  },
  get spawnRunningText() {
    return this.spawnRunning === 1 ? "A design session is already running on this design system." : `${this.spawnRunning} design sessions are already running on this design system.`;
  },
  get canSpawn() {
    return !this.saving && this.spawnAgentId !== "" && this.spawnPrompt.trim() !== "";
  },
  get spawnStarted() {
    return this.spawnedId !== "";
  },
  get renaming() {
    return this.panel === "rename";
  },
  get describing() {
    return this.panel === "description";
  },
  get deleting() {
    return this.panel === "delete";
  },
  get canSave() {
    if (this.saving) return false;
    if (this.renaming) return this.draftName.trim() !== "" && this.draftName.trim() !== this.name;
    return this.draftDescription.trim() !== this.description;
  },
  get canDelete() {
    return !this.saving && this.confirm.trim() === this.name;
  },
  get deleteNote() {
    return this.version > 0 ? `This removes its ${this.version} saved ${this.version === 1 ? "version" : "versions"} for good.` : "It has no saved versions yet.";
  },
  get refused() {
    return this.usedBy.length > 0;
  },
  get refusedTitle() {
    return `“${this.name}” cannot be deleted while repositories use it.`;
  },
  get refusedRepositories() {
    return this.usedBy.join(", ");
  },

  init() {
    const seed = readSeed(this.$el);
    this.indexHref = seed.index_href;
    this.spawnAgents = Array.isArray(seed.agents) ? seed.agents : [];
    this.spawnAgentId = this.spawnAgents[0]?.id ?? "";
    if (new URLSearchParams(globalThis.location?.search ?? "").get("spawn") === "1") this.openSpawn();
  },

  /** Opens the spawn form, after reading how many design sessions already run here. */
  async openSpawn() {
    this.close();
    this.spawnedId = "";
    this.panel = "spawn";
    try {
      this.spawnRunning = await gateway.runningDesignSessions(this.designSystemId);
    } catch {
      this.spawnRunning = 0;
    }
    this.$nextTick(() => this.$el.querySelector("[data-menu-panel] textarea")?.focus());
  },

  typeSpawnAgent(event) {
    this.spawnAgentId = event.target.value;
  },

  typeSpawnPrompt(event) {
    this.spawnPrompt = event.target.value;
  },

  /** Starts the design session; a running one is only warned about, never a refusal. */
  async startDesignSession() {
    if (!this.canSpawn) return;
    this.saving = true;
    this.error = null;
    try {
      const { sessionId } = await gateway.startDesignSession(this.designSystemId, this.spawnAgentId, this.spawnPrompt.trim());
      this.spawnPrompt = "";
      this.spawnedId = sessionId;
      this.panel = "";
    } catch (err) {
      this.error = describeError(err);
    } finally {
      this.saving = false;
    }
  },

  menuSelect(event) {
    const action = event.detail.action;
    this.close();
    if (action === "rename") this.draftName = this.name;
    if (action === "description") this.draftDescription = this.description;
    this.panel = action;
    this.$nextTick(() => this.$el.querySelector("[data-menu-panel] input, [data-menu-panel] textarea")?.focus());
  },

  close() {
    this.panel = "";
    this.confirm = "";
    this.fault = null;
    this.error = null;
    this.usedBy = [];
  },

  typeName(event) {
    this.draftName = event.target.value;
    this.fault = null;
  },

  typeDescription(event) {
    this.draftDescription = event.target.value;
    this.fault = null;
  },

  typeConfirm(event) {
    this.confirm = event.target.value;
    this.usedBy = [];
    this.error = null;
  },

  /** Stores the new name or description; the slug never changes. */
  async save() {
    if (!this.canSave) return;
    this.saving = true;
    this.fault = null;
    this.error = null;
    try {
      await gateway.update(this.designSystemId, this.renaming ? { name: this.draftName.trim() } : { description: this.draftDescription.trim() });
      this.close();
    } catch (err) {
      const fault = faultOf(err);
      if (fault.field === "") this.error = describeError(err);
      else this.fault = fault;
    } finally {
      this.saving = false;
    }
  },

  /** Deletes the design system once its name is typed, then goes back to the list. */
  async remove() {
    if (!this.canDelete) return;
    this.saving = true;
    this.error = null;
    this.usedBy = [];
    try {
      await gateway.remove(this.designSystemId, this.confirm.trim());
      navigate(this.indexHref);
    } catch (err) {
      const used = err?.code === "DESIGN_SYSTEM_IN_USE" && Array.isArray(err.details?.repositories) ? err.details.repositories : null;
      if (used) this.usedBy = used.map((r) => r.name);
      else this.error = describeError(err);
    } finally {
      this.saving = false;
    }
  },
});
