// The access credentials of a service, beside its log: each label with its value
// masked until the person shows it, and a copy button (ADR 0004). The values come
// with the service the page already holds and stay in memory: nothing is logged,
// stored or put in a URL, and closing the panel drops the reveal state.
//
//   <section x-data="serviceCredentials()" x-effect="credentialsChanged(logView.credentials)"> …

import { MASK } from "../../domain/mask.js";

/**
 * @param {{ clipboard?: { writeText(text: string): Promise<void> } }} [deps]
 */
export const serviceCredentials = ({ clipboard } = {}) => () => ({
  /** @type {{ label: string, value: string }[]} */
  credentials: [],
  /** Labels shown, in memory only. */
  revealed: {},
  /** The label last copied. */
  copied: "",

  get rows() {
    return this.credentials.map((c) => {
      const open = this.revealed[c.label] === true;
      return {
        label: c.label,
        shown: open ? c.value : MASK,
        masked: !open,
        toggleText: open ? "Hide" : "Show",
        toggleName: `${open ? "Hide" : "Show"} ${c.label}`,
        copyName: `Copy ${c.label}`,
        copyText: this.copied === c.label ? "Copied" : "Copy",
      };
    });
  },
  get hasRows() {
    return this.credentials.length > 0;
  },
  get summary() {
    const n = this.credentials.length;
    return `${n} credential${n === 1 ? "" : "s"}`;
  },

  /** The service's credentials changed (x-effect): what no longer exists is forgotten. */
  credentialsChanged(list) {
    this.credentials = list ?? [];
    const labels = new Set(this.credentials.map((c) => c.label));
    this.revealed = Object.fromEntries(Object.entries(this.revealed).filter(([label]) => labels.has(label)));
    if (!labels.has(this.copied)) this.copied = "";
  },

  /** @param {{ currentTarget: { dataset: { label: string } } }} event a row's Show or Hide */
  toggle(event) {
    const label = event.currentTarget.dataset.label;
    this.revealed = { ...this.revealed, [label]: this.revealed[label] !== true };
  },

  /** @param {{ currentTarget: { dataset: { label: string } } }} event a row's Copy */
  async copy(event) {
    const label = event.currentTarget.dataset.label;
    const c = this.credentials.find((x) => x.label === label);
    if (!c) return;
    try {
      await (clipboard ?? navigator.clipboard).writeText(c.value);
      this.copied = label;
    } catch {
      this.copied = ""; // the browser refused: the button keeps saying Copy
    }
  },
});
