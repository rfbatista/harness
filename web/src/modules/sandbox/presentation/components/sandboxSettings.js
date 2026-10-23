// The Sandbox section of the Agent isolation page: the switch that runs
// agents in the OS sandbox, and the extra paths they may use. The server
// paints the first view in [data-ssr] and embeds the seed; this takes over.
//
//   <section id="sandbox" x-data="sandboxSettings" data-seed="sandbox-seed">
//
// Turning the sandbox off asks first (an inline alertdialog); turning it on
// does not. A path the server refuses stays in the field with the reason.

import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";

const MISSING = "This folder does not exist yet. It will apply once it does.";
const CREDENTIALS = "This folder usually holds credentials.";

/** @param {{ gateway: import("../../domain/ports.js").SandboxGateway }} deps */
export const sandboxSettings = ({ gateway }) => () => ({
  enabled: true,
  driver: "",
  /** @type {import("../../domain/ports.js").SandboxPath[]} */
  paths: [],
  newPath: "",
  /** "read" or "write". */
  newAccess: "read",
  /** Why the path in the field was refused. */
  pathError: "",
  error: null,
  confirming: false,
  busy: false,
  announcement: "",
  /** The path an "Allow this path" link brought: in the field, not saved. */
  prefilled: false,

  init() {
    const seed = readSeed(this.$el);
    this.apply(seed);
    if (seed.allowPath) {
      this.newPath = seed.allowPath;
      this.prefilled = true;
      this.$nextTick(() => this.$refs.path?.focus());
    }
    for (const node of this.$el.querySelectorAll("[data-ssr]")) node.remove();
  },

  /** @param {import("../../domain/ports.js").SandboxState} state */
  apply(state) {
    this.enabled = state.enabled;
    this.driver = state.driver;
    this.paths = state.paths;
    this.$dispatch("isolation-chip", { section: "sandbox", ...this.summary });
  },

  /** The page's summary chip for this section. */
  get summary() {
    if (!this.enabled) return { text: "Sandbox: off", tone: "attention", blocks: false };
    if (this.noDriver) return { text: "Sandbox: no driver", tone: "danger", blocks: true };
    return { text: "Sandbox: on", tone: "signal", blocks: false };
  },

  get rows() {
    return this.paths.map((p) => ({
      path: p.path,
      access: p.write ? "write" : "read",
      accessLabel: `Access for ${p.path}`,
      removeLabel: `Remove ${p.path}`,
      missingWarning: (p.warnings ?? []).includes("missing") ? MISSING : "",
      credentialsWarning: (p.warnings ?? []).includes("credentials") ? CREDENTIALS : "",
    }));
  },
  get isEmpty() {
    return this.paths.length === 0;
  },
  get hasPaths() {
    return this.paths.length > 0;
  },
  get isOff() {
    return !this.enabled;
  },
  get noDriver() {
    return this.driver === "";
  },
  get chipText() {
    if (!this.enabled) return "Off";
    return this.noDriver ? "No driver" : "On";
  },
  get chipTone() {
    if (!this.enabled) return "attention";
    return this.noDriver ? "danger" : "signal";
  },
  get pathInvalid() {
    return this.pathError !== "";
  },

  /** Runs one change against the gateway and takes its state, or shows why not. */
  async change(call) {
    if (this.busy) return false;
    this.busy = true;
    this.error = null;
    try {
      this.apply(await call());
      return true;
    } catch (err) {
      this.error = describeError(err);
      return false;
    } finally {
      this.busy = false;
    }
  },

  // ── The switch ──────────────────────────────────────────────────────
  async toggle() {
    if (this.enabled) {
      this.confirming = true;
      this.$nextTick(() => this.$refs.keep?.focus());
      return;
    }
    if (await this.change(() => gateway.setEnabled(true))) this.announcement = "Sandbox is on.";
  },
  keepOn() {
    this.confirming = false;
    this.$nextTick(() => this.$refs.switch?.focus());
  },
  async turnOff() {
    this.confirming = false;
    if (await this.change(() => gateway.setEnabled(false))) this.announcement = "Sandbox is off.";
    this.$nextTick(() => this.$refs.switch?.focus());
  },

  // ── The extra paths ─────────────────────────────────────────────────
  async add() {
    if (this.busy) return;
    this.busy = true;
    this.error = null;
    this.pathError = "";
    try {
      this.apply(await gateway.addPath(this.newPath, this.newAccess === "write"));
      this.newPath = "";
      this.prefilled = false;
      this.announcement = "Path added.";
    } catch (err) {
      const described = describeError(err);
      if (described.code === "INVALID_INPUT") this.pathError = described.message;
      else this.error = described;
    } finally {
      this.busy = false;
    }
  },
  /** @param {Event} event a row select's change */
  async setAccess(event) {
    const select = /** @type {HTMLSelectElement} */ (event.target);
    const path = /** @type {HTMLElement} */ (select.closest("[data-path]")).dataset.path ?? "";
    if (!(await this.change(() => gateway.setPathAccess(path, select.value === "write")))) {
      select.value = this.paths.find((p) => p.path === path)?.write ? "write" : "read";
    }
  },
  /** @param {Event} event a row button's click */
  async remove(event) {
    const row = /** @type {HTMLElement} */ (/** @type {HTMLElement} */ (event.currentTarget).closest("[data-path]"));
    const path = row.dataset.path ?? "";
    if (await this.change(() => gateway.removePath(path))) this.announcement = "Path removed.";
  },

  /** The person is editing the path again: the refusal no longer applies. */
  editPath() {
    this.pathError = "";
  },
  dismissError() {
    this.error = null;
  },
});
