// The variables a service's run was given, beside its log: each with its source,
// masked until revealed (ADR 0077). They are read when the log panel opens and
// again when the run changes, never cached across runs. Values stay in this
// component's memory: nothing is logged, stored or put in a URL, and closing the
// panel drops them with the reveal state.
//
//   <section x-data="serviceVariables(id)" x-effect="runChanged(logView.runId)"> …

import { describeError } from "../../../../shared/presentation/errors.js";
import { MASK } from "../../domain/mask.js";

const SOURCE_WORDS = { repository_env: "repository .env", service: "service" };

const sourceOf = (v) => (v.source === "override" ? `override by ${v.by || "an agent"}` : SOURCE_WORDS[v.source]);

/**
 * @param {{ gateway: import("../../domain/ports.js").ServicesGateway }} deps
 */
export const serviceVariables = ({ gateway }) => (id = "") => {
  let serviceId = id;

  return {
    /** The run the variables are those of ("" when the service never ran). */
    runId: "",
    /** @type {import("../../domain/ports.js").RunVariables["variables"]} */
    variables: [],
    /** Names revealed, in memory only. */
    revealed: {},
    loading: false,
    error: null,
    opened: false,
    /** Counts reads, so an answer for a run that is no longer current is dropped. */
    token: 0,

    // ── what the markup binds ────────────────────────────────────────────
    get rows() {
      return this.variables.map((v) => {
        const open = this.revealed[v.name] === true;
        return {
          name: v.name,
          source: sourceOf(v),
          override: v.source === "override",
          shown: open ? v.value : MASK,
          masked: !open,
          toggleText: open ? "Hide" : "Reveal",
          toggleName: `${open ? "Hide" : "Reveal"} ${v.name}`,
        };
      });
    },
    get neverRan() {
      return this.runId === "";
    },
    get empty() {
      return !this.neverRan && !this.loading && this.error === null && this.variables.length === 0;
    },
    get hasRows() {
      return this.variables.length > 0;
    },
    get summary() {
      if (this.neverRan) return "it has not run yet";
      if (this.error !== null) return "could not be read";
      if (this.variables.length === 0) return this.loading ? "reading…" : "no variables";
      return `${this.variables.length} variable${this.variables.length === 1 ? "" : "s"}`;
    },
    get allRevealed() {
      return this.hasRows && this.variables.every((v) => this.revealed[v.name] === true);
    },
    get allLabel() {
      return this.allRevealed ? "Hide all" : "Reveal all";
    },

    // ── lifecycle ────────────────────────────────────────────────────────
    init() {
      serviceId = id || (this.$el.dataset.serviceId ?? "");
    },

    /** The run on screen changed (x-effect): its variables are read, and nothing stays revealed. */
    runChanged(runId) {
      if (runId === this.runId) return;
      this.runId = runId;
      this.variables = [];
      this.revealed = {};
      this.error = null;
      this.token++;
      if (runId === "") {
        this.loading = false;
        return;
      }
      return this.load();
    },

    async load() {
      const token = ++this.token;
      this.loading = true;
      this.error = null;
      try {
        const read = await gateway.getRunVariables(serviceId);
        if (token !== this.token) return;
        this.variables = read.variables;
      } catch (err) {
        if (token !== this.token) return;
        this.error = describeError(err);
      } finally {
        if (token === this.token) this.loading = false;
      }
    },

    retry() {
      return this.load();
    },

    toggleSection() {
      this.opened = !this.opened;
    },

    /** @param {{ currentTarget: HTMLElement }} event a row's Reveal or Hide: the variable's name is on it */
    toggle(event) {
      const name = event.currentTarget.dataset.name;
      this.revealed = { ...this.revealed, [name]: this.revealed[name] !== true };
    },

    toggleAll() {
      const reveal = !this.allRevealed;
      this.revealed = reveal ? Object.fromEntries(this.variables.map((v) => [v.name, true])) : {};
    },
  };
};
