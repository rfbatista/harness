// The Agent credential section of the Agent isolation page: whether agents
// have a credential, its kind, and pasting, replacing and removing it. The
// server paints the first view in [data-ssr] and embeds the seed; this takes
// over.
//
//   <section id="credential" x-data="agentCredential" data-seed="credential-seed">
//
// The value is typed into an uncontrolled password input, sent once, and
// cleared on save, cancel and pagehide. It is never held in component state
// and never shown again.

import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";

const KIND_NAMES = { oauth: "OAuth token", api_key: "Anthropic API key" };

/** The kind a pasted value's prefix says it is, or "" when it does not say. */
export function kindOf(value) {
  const v = value.trim();
  if (v.startsWith("sk-ant-oat")) return "oauth";
  if (v.startsWith("sk-ant-api")) return "api_key";
  return "";
}

/** @param {{ gateway: import("../../domain/ports.js").AgentCredentialGateway }} deps */
export const agentCredential = ({ gateway }) => () => ({
  isSet: false,
  kind: "",
  updated: "",
  /** The kind chosen in the paste form. */
  chosen: "oauth",
  pasting: false,
  confirming: false,
  busy: false,
  error: null,
  announcement: "",

  init() {
    this.apply(readSeed(this.$el));
    for (const node of this.$el.querySelectorAll("[data-ssr]")) node.remove();
  },

  /** @param {import("../../domain/ports.js").CredentialStatus} status */
  apply(status) {
    this.isSet = status.set;
    this.kind = status.kind;
    this.updated = status.updated;
  },

  get notSet() {
    return !this.isSet;
  },
  get chipText() {
    return this.isSet ? "Set" : "Not set";
  },
  get chipTone() {
    return this.isSet ? "signal" : "danger";
  },
  get kindName() {
    return KIND_NAMES[this.kind] ?? "";
  },
  get savedDate() {
    return this.updated.slice(0, 10);
  },
  get pasteLabel() {
    return this.isSet ? "Replace with a pasted token or key" : "Paste a token or key";
  },
  get pasteTone() {
    return this.isSet ? "" : "primary";
  },
  get oauthChosen() {
    return this.chosen === "oauth";
  },
  get apiKeyChosen() {
    return this.chosen === "api_key";
  },
  get noPasteForm() {
    return !this.pasting;
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
  /** Called with the field in view: the form's submit. */
  async save() {
    if (this.busy) return;
    const field = /** @type {HTMLInputElement} */ (this.$refs.value);
    this.busy = true;
    this.error = null;
    try {
      this.apply(await gateway.set(/** @type {any} */ (this.chosen), field.value));
      this.clearValue();
      this.pasting = false;
      this.announcement = "Agent credential saved.";
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
      this.apply(await gateway.remove());
      this.announcement = "Agent credential removed.";
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
