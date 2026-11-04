// The Agent attribution section of the Agent isolation page: how the commit
// trailer and the pull request line of agent work are credited. The server
// paints the saved preview in [data-ssr] and embeds the seed; this takes over.
//
//   <section id="attribution" x-data="agentAttribution" data-seed="attribution-seed">
//
// The preview is built here from the controls, with the rule the server uses
// for the launch (domain.AgentAttribution.Launch): None is "", Custom text is
// the trimmed text, Claude default is left out. The browser test pins the two
// together. A save the server refuses leaves the page on the saved values.

import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { toState } from "../../infrastructure/attribution-gateway.js";

/** @param {import("../../domain/ports.js").AttributionChoice} c @returns {string | null} */
const lineOf = (c) => (c.mode === "none" ? "" : c.mode === "custom" ? c.text.trim() : null);

/** @param {import("../../domain/ports.js").AttributionChoice} commit @param {import("../../domain/ports.js").AttributionChoice} pr */
export function previewOf(commit, pr) {
  /** @type {Record<string, string>} */
  const out = {};
  const c = lineOf(commit);
  const p = lineOf(pr);
  if (c !== null) out.commit = c;
  if (p !== null) out.pr = p;
  return Object.keys(out).length === 0 ? "{}" : JSON.stringify({ attribution: out });
}

/** @param {import("../../domain/ports.js").AttributionChoice} c */
const keyOf = (c) => (c.mode === "custom" ? `custom:${c.text.trim()}` : c.mode);

/** @param {{ gateway: import("../../domain/ports.js").AttributionGateway }} deps */
export const agentAttribution = ({ gateway }) => () => ({
  ready: false,
  /** @type {import("../../domain/ports.js").AttributionChoice} */
  commit: { mode: "default", text: "" },
  /** @type {import("../../domain/ports.js").AttributionChoice} */
  pr: { mode: "default", text: "" },
  /** The saved values, as the server answered them. */
  saved: { commit: { mode: "default", text: "" }, pr: { mode: "default", text: "" } },
  error: null,
  busy: false,
  announcement: "",

  init() {
    this.apply(toState(readSeed(this.$el)));
    for (const node of this.$el.querySelectorAll("[data-ssr]")) node.remove();
    this.ready = true;
  },

  /** @param {import("../../domain/ports.js").AttributionState} state */
  apply(state) {
    this.saved = { commit: { ...state.commit }, pr: { ...state.pr } };
    this.revert();
  },

  /** Puts the controls back on the saved values. */
  revert() {
    this.commit = { ...this.saved.commit };
    this.pr = { ...this.saved.pr };
  },

  get preview() {
    return previewOf(this.commit, this.pr);
  },
  get commitCustom() {
    return this.commit.mode === "custom";
  },
  get prCustom() {
    return this.pr.mode === "custom";
  },
  get cannotSave() {
    return this.busy || (keyOf(this.commit) === keyOf(this.saved.commit) && keyOf(this.pr) === keyOf(this.saved.pr));
  },

  async save() {
    if (this.busy) return;
    this.busy = true;
    this.error = null;
    this.announcement = "";
    try {
      this.apply(await gateway.save(this.commit, this.pr));
      this.announcement = "Saved.";
    } catch (err) {
      this.error = describeError(err);
      this.revert();
    } finally {
      this.busy = false;
    }
  },
  dismissError() {
    this.error = null;
  },
});
