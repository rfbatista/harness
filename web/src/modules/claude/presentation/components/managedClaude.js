// The Managed claude section of the Agent isolation page: the installed
// versions, install another, and switch to it with a separate action. The
// server paints the first view in [data-ssr] and embeds the seed; this takes
// over, and polls while an install runs.
//
//   <section id="claude" x-data="managedClaude" data-seed="claude-seed">

import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";

const MB = 1024 * 1024;

/** @param {number} bytes */
const megabytes = (bytes) => `${Math.max(1, Math.round(bytes / MB))} MB`;

/**
 * The sentence naming the running sessions that keep their version.
 * @param {number} n
 * @param {string} version
 */
const keepLine = (n, version) => (n > 0 ? ` ${n} running session${n === 1 ? " keeps" : "s keep"} ${version}.` : "");

/** @param {{ gateway: import("../../domain/ports.js").ClaudeGateway, pollMs?: number }} deps */
export const managedClaude = ({ gateway, pollMs = 400 }) => () => ({
  current: "",
  /** @type {import("../../domain/ports.js").ClaudeVersion[]} */
  installed: [],
  /** @type {import("../../domain/ports.js").ClaudeInstall | null} */
  installing: null,
  /** @type {{ version: string, message: string } | null} */
  failed: null,
  overridden: false,
  bin: "",
  /** "idle", "loading", "ok" or "failed". */
  releasesState: "idle",
  /** @type {{ label: string, version: string }[]} */
  offered: [],
  error: null,
  busy: false,
  announcement: "",
  /** What the last switch did, for the person to read: who keeps what. */
  switchLine: "",
  timer: 0,

  init() {
    this.apply(readSeed(this.$el));
    for (const node of this.$el.querySelectorAll("[data-ssr]")) node.remove();
    if (this.installing) this.poll();
    else if (this.installed.length > 0) this.loadReleases();
  },
  destroy() {
    clearTimeout(this.timer);
  },

  /** @param {import("../../domain/ports.js").ClaudeState} state */
  apply(state) {
    this.current = state.current;
    this.installed = state.installed;
    this.installing = state.installing;
    this.failed = state.failed;
    this.overridden = state.overridden;
    this.bin = state.bin;
    this.$dispatch("isolation-chip", { section: "claude", ...this.summary });
  },

  // ── What the section shows ──────────────────────────────────────────
  get isNone() {
    return this.installed.length === 0;
  },
  get hasVersions() {
    return this.installed.length > 0;
  },
  get showEmpty() {
    return this.installed.length === 0 && !this.installing;
  },
  get showList() {
    return this.installed.length > 0 || this.installing !== null;
  },
  get isMissing() {
    return this.isNone && !this.overridden && !this.installing;
  },
  get isInstalling() {
    return this.installing !== null;
  },
  get hasFailed() {
    return this.failed !== null && !this.installing;
  },
  get failedTitle() {
    return this.failed ? `Could not install ${this.failed.version}.` : "";
  },
  get failedMessage() {
    return this.failed ? this.failed.message : "";
  },
  get chipText() {
    return this.isNone ? "None" : this.current;
  },
  get chipTone() {
    return this.isNone ? "danger" : "signal";
  },
  get overrideText() {
    return `Agents run ${this.bin}. The version you choose below is saved but not used.`;
  },
  /** The page's summary chip for this section. */
  get summary() {
    if (this.isNone) {
      if (this.overridden) return { text: "claude: override", tone: "signal", blocks: false };
      if (this.installing) return { text: "claude: installing", tone: "", blocks: true };
      return { text: "claude: not installed", tone: "danger", blocks: true };
    }
    return { text: `claude: ${this.current}`, tone: "signal", blocks: false };
  },
  get hasSwitchLine() {
    return this.switchLine !== "";
  },
  get cancelLabel() {
    return this.installing ? `Cancel install of ${this.installing.version}` : "";
  },
  get rows() {
    return this.installed.map((v) => ({
      version: v.version,
      active: v.active,
      date: v.installedAt ? new Date(v.installedAt).toLocaleDateString("en-GB", { day: "numeric", month: "short" }) : "",
      canUse: !v.active,
      useLabel: `Use ${v.version}`,
      canRemove: !v.active,
      removeLabel: `Remove ${v.version}`,
    }));
  },
  get installingVersion() {
    return this.installing ? this.installing.version : "";
  },
  get installingLabel() {
    return this.installing ? `${this.installing.version} installing…` : "";
  },
  get progressKnown() {
    return this.installing !== null && this.installing.total > 0;
  },
  get progressText() {
    const i = this.installing;
    if (!i) return "";
    if (i.total > 0) return `${megabytes(i.done)} of ${megabytes(i.total)}`;
    return i.done > 0 ? megabytes(i.done) : "Starting…";
  },
  get progressValue() {
    return this.installing ? this.installing.done : 0;
  },
  get progressMax() {
    return this.installing ? this.installing.total : 0;
  },
  get picker() {
    return this.releasesState === "ok" || this.releasesState === "loading";
  },
  get loadingReleases() {
    return this.releasesState === "loading";
  },
  get releasesFailed() {
    return this.releasesState === "failed";
  },
  get options() {
    return this.offered.map((o) => {
      const have = this.installed.some((v) => v.version === o.version);
      return { version: o.version, label: have ? `${o.label} · installed` : o.label, taken: have };
    });
  },
  get cannotInstall() {
    return this.busy || this.isInstalling;
  },

  async run(call) {
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

  // ── Versions the server offers ──────────────────────────────────────
  async loadReleases() {
    this.releasesState = "loading";
    try {
      const r = await gateway.releases();
      const offered = [{ label: `latest · ${r.latest}`, version: r.latest }];
      if (r.stable !== r.latest) offered.push({ label: `stable · ${r.stable}`, version: r.stable });
      this.offered = offered;
      this.releasesState = "ok";
    } catch {
      this.releasesState = "failed";
    }
  },

  // ── Install, with its progress ──────────────────────────────────────
  async installLatest() {
    await this.start("latest");
  },
  async installPicked() {
    const select = /** @type {HTMLSelectElement | undefined} */ (this.$refs.version);
    const typed = /** @type {HTMLInputElement | undefined} */ (this.$refs.typed);
    await this.start((select?.value || typed?.value || "").trim());
  },
  async retry() {
    await this.start(this.failed ? this.failed.version : "latest");
  },
  /** @param {string} version */
  async start(version) {
    this.switchLine = "";
    if (await this.run(() => gateway.install(version))) this.poll();
  },
  poll() {
    clearTimeout(this.timer);
    if (!this.installing) return;
    this.timer = setTimeout(async () => {
      try {
        const before = this.installing?.version ?? "";
        this.apply(await gateway.get());
        if (!this.installing) {
          this.announcement = this.failed ? "" : `Installed ${this.current || before}.`;
          this.loadReleases();
          return;
        }
      } catch (err) {
        this.error = describeError(err);
      }
      this.poll();
    }, pollMs);
  },

  async cancel() {
    const version = this.installing?.version ?? "";
    if (await this.run(() => gateway.cancel())) {
      clearTimeout(this.timer);
      this.announcement = `Install of ${version} cancelled.`;
      this.loadReleases();
    }
  },

  // ── Switch ──────────────────────────────────────────────────────────
  /** @param {Event} event a row button's click */
  async use(event) {
    const row = /** @type {HTMLElement} */ (/** @type {HTMLElement} */ (event.currentTarget).closest("[data-version]"));
    const version = row.dataset.version ?? "";
    const before = this.current;
    let kept = 0;
    if (await this.run(async () => {
      const state = await gateway.use(version);
      kept = state.keepingSessions ?? 0;
      return state;
    })) {
      this.switchLine = `New sessions will use ${version}.${keepLine(kept, before)}`;
      this.loadReleases();
    }
  },
  /** @param {Event} event a row button's click */
  async remove(event) {
    const row = /** @type {HTMLElement} */ (/** @type {HTMLElement} */ (event.currentTarget).closest("[data-version]"));
    const version = row.dataset.version ?? "";
    if (await this.run(() => gateway.remove(version))) {
      this.switchLine = "";
      this.announcement = `Removed ${version}.`;
      this.loadReleases();
    }
  },

  dismissError() {
    this.error = null;
  },
});
