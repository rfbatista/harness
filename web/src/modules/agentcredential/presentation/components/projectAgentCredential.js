// A project's own agent credential, in its settings page: what its sessions
// sign in with instead of the global Agent credential. Without one they fall
// back to the global credential, or to a Claude login in the agents' home.
//
//   <section x-data="projectAgentCredential" data-seed="project-credential-seed">
//
// The seed is {project_id, set, kind, updated, global: {set, kind}}. The value
// is typed into an uncontrolled password input, sent once, and cleared on save,
// cancel and pagehide; it is never held in component state and never shown
// again.

import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { kindOf } from "./agentCredential.js";

const KIND_NAMES = { oauth: "OAuth token", api_key: "Anthropic API key" };

/** @param {{ gateway: import("../../domain/ports.js").AgentCredentialGateway }} deps */
export const projectAgentCredential = ({ gateway }) => () => ({
  projectId: "",
  isSet: false,
  kind: "",
  updated: "",
  /** The global credential is set, so a project without its own still has one. */
  globalSet: false,
  /** The kind chosen in the paste form. */
  chosen: "oauth",
  pasting: false,
  confirming: false,
  busy: false,
  error: null,
  announcement: "",

  init() {
    const seed = readSeed(this.$el);
    this.projectId = seed.project_id;
    this.globalSet = seed.global?.set === true;
    this.apply(seed);
    for (const node of this.$el.querySelectorAll("[data-ssr]")) node.remove();
  },

  /** @param {import("../../domain/ports.js").CredentialStatus} status */
  apply(status) {
    this.isSet = status.set;
    this.kind = status.kind;
    this.updated = status.updated;
  },

  get oauthChosen() {
    return this.chosen === "oauth";
  },
  get apiKeyChosen() {
    return this.chosen === "api_key";
  },
  get notSet() {
    return !this.isSet;
  },
  get chipText() {
    return this.isSet ? "Own credential" : "Global";
  },
  get chipTone() {
    return this.isSet ? "signal" : "";
  },
  get kindName() {
    return KIND_NAMES[this.kind] ?? "";
  },
  get savedDate() {
    return this.updated ? this.updated.slice(0, 10) : "";
  },
  get pasteLabel() {
    return this.isSet ? "Replace with a pasted token or key" : "Paste a token or key";
  },
  get pasteTone() {
    return this.isSet ? "" : "primary";
  },
  /** What a session without an own credential signs in with. */
  get fallbackLine() {
    return this.globalSet
      ? "Sessions of this project use the global Agent credential, or a Claude login when the agents' home has one."
      : "Sessions of this project use a Claude login in the agents' home. No global Agent credential is set either.";
  },

  startPaste() {
    this.pasting = true;
    this.confirming = false;
    this.error = null;
    this.$nextTick(() => this.$refs.value?.focus());
  },
  cancelPaste() {
    this.clearValue();
    this.pasting = false;
    this.error = null;
    this.$nextTick(() => this.$refs.paste?.focus());
  },
  /** @param {Event} event the field's input */
  detectKind(event) {
    const kind = kindOf(/** @type {HTMLInputElement} */ (event.target).value);
    if (kind) this.chosen = kind;
  },
  chooseKind(event) {
    this.chosen = /** @type {HTMLInputElement} */ (event.target).value;
  },
  /** The form's submit. */
  async save() {
    if (this.busy) return;
    const field = /** @type {HTMLInputElement} */ (this.$refs.value);
    this.busy = true;
    this.error = null;
    try {
      this.apply(await gateway.setForProject(this.projectId, /** @type {any} */ (this.chosen), field.value));
      this.clearValue();
      this.pasting = false;
      this.announcement = "Project agent credential saved.";
      this.$nextTick(() => this.$refs.paste?.focus());
    } catch (err) {
      this.error = describeError(err);
    } finally {
      this.busy = false;
    }
  },
  clearValue() {
    const field = /** @type {HTMLInputElement | undefined} */ (this.$refs.value);
    if (field) field.value = "";
  },
  /** The page is going away: the value must not outlive it in a restored page. */
  pagehide() {
    this.clearValue();
  },

  askRemove() {
    this.confirming = true;
    this.pasting = false;
    this.$nextTick(() => this.$refs.keep?.focus());
  },
  keep() {
    this.confirming = false;
    this.$nextTick(() => this.$refs.remove?.focus());
  },
  async remove() {
    if (this.busy) return;
    this.busy = true;
    this.error = null;
    this.confirming = false;
    try {
      this.apply(await gateway.removeForProject(this.projectId));
      this.announcement = "Project agent credential removed. Sessions use the global credential again.";
    } catch (err) {
      this.error = describeError(err);
    } finally {
      this.busy = false;
    }
  },
  dismissError() {
    this.error = null;
  },
});
