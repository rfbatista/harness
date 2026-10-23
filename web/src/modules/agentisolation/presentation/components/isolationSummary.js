// The summary chips at the top of the Agent isolation page: the state of the
// credential, the claude and the sandbox in one glance, each a link to its
// section. The sections own their state; each announces its chip on the
// window as an "isolation-chip" event whenever it changes, so these stay in
// step without asking the server again.
//
//   <nav x-data="isolationSummary" data-seed="isolation-summary-seed"
//        x-on:isolation-chip.window="take">

import { readSeed } from "../../../../shared/presentation/seed.js";

/**
 * @typedef {object} Chip
 * @property {string} text
 * @property {string} tone  "signal", "attention", "danger" or "".
 * @property {boolean} blocks  Agents cannot start until it is fixed.
 */

export const isolationSummary = () => () => ({
  /** @type {Chip} */
  credential: { text: "", tone: "", blocks: false },
  /** @type {Chip} */
  claude: { text: "", tone: "", blocks: false },
  /** @type {Chip} */
  sandbox: { text: "", tone: "", blocks: false },

  init() {
    const seed = readSeed(this.$el);
    this.credential = seed.credential;
    this.claude = seed.claude;
    this.sandbox = seed.sandbox;
  },

  /** @param {CustomEvent<Chip & { section: "credential" | "claude" | "sandbox" }>} event */
  take(event) {
    const { section, text, tone, blocks } = event.detail;
    if (section === "credential" || section === "claude" || section === "sandbox") this[section] = { text, tone, blocks };
  },

  get blocked() {
    return this.credential.blocks || this.claude.blocks || this.sandbox.blocks;
  },
});
