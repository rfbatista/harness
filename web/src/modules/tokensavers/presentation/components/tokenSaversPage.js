// The Token savers page: rtk, caveman and codegraph with their install
// state, the installed and pinned versions, and Install, Update and Remove.
// An install runs on the server in the background; while one does, the page
// reads the list every half second for its progress and outcome.
//
//   <main x-data="tokenSaversPage" data-seed="token-savers-seed"> …

import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { canRemove, installLabel, stateLabel, stateTone, versionsText } from "../../domain/installation.js";

const POLL_MS = 500;

/** What to do about the server's token saver refusals and failures. */
const NEXT_STEP = {
  TOKEN_SAVER_UNSUPPORTED_PLATFORM: "run the harness on macOS or Linux to use token savers",
  TOKEN_SAVER_BUSY: "wait for the install to end, then retry",
  TOKEN_SAVER_CHECKSUM_MISMATCH: "retry; if it fails again, the release changed and the harness needs a new pin",
  TOKEN_SAVER_CHECKSUM_MISSING: "the release changed; the harness needs a new pin",
  TOKEN_SAVER_ASSET_MISSING: "the release changed; the harness needs a new pin",
  TOKEN_SAVER_DOWNLOAD_FAILED: "check the server's network access to github.com, then retry",
  TOKEN_SAVER_ARCHIVE_INVALID: "retry; if it fails again, the release changed and the harness needs a new pin",
};

function described(err) {
  const d = describeError(err);
  return { ...d, next: NEXT_STEP[d.code] ?? d.next };
}

/** @param {{ gateway: import("../../domain/installation-ports.js").TokenSaverInstallGateway }} deps */
export const tokenSaversPage = ({ gateway }) => () => ({
  /** @type {ReturnType<typeof toRow>[]} */
  rows: [],
  platform: "",
  supported: true,
  unsupported: "",
  error: null,
  announcement: "",
  timer: 0,

  async init() {
    try {
      this.apply(gateway.decodeSeed(readSeed(this.$el)));
    } catch {
      await this.refresh();
    }
    this.$nextTick(() => {
      for (const node of this.$el.querySelectorAll("[data-ssr]")) node.remove();
    });
    this.follow();
  },

  destroy() {
    clearTimeout(this.timer);
  },

  /** @param {import("../../domain/installation.js").SaverList} list */
  apply(list) {
    this.rows = list.savers.map(toRow);
    this.platform = list.platform;
    this.supported = list.supported;
    this.unsupported = list.unsupported;
  },

  async refresh() {
    try {
      this.apply(await gateway.list());
    } catch (err) {
      this.error = described(err);
    }
  },

  /** Reads the list again while an install runs. */
  follow() {
    clearTimeout(this.timer);
    if (!this.rows.some((r) => r.state === "installing")) return;
    this.timer = setTimeout(async () => {
      const before = new Map(this.rows.map((r) => [r.saver, r.state]));
      await this.refresh();
      for (const r of this.rows) {
        if (before.get(r.saver) !== "installing" || r.state === "installing") continue;
        this.announcement = r.state === "failed" ? `${r.saver} failed to install.` : `${r.saver} ${r.installedVersion} installed.`;
      }
      this.follow();
    }, POLL_MS);
  },

  async install(saver) {
    this.error = null;
    try {
      this.put(await gateway.install(saver));
      this.follow();
    } catch (err) {
      this.error = described(err);
    }
  },

  async remove(saver) {
    const row = this.rows.find((r) => r.saver === saver);
    if (!row || row.removing) return;
    row.removing = true;
    this.error = null;
    try {
      this.put(await gateway.remove(saver));
      this.announcement = `${saver} removed.`;
    } catch (err) {
      this.error = described(err);
      row.removing = false;
    }
  },

  /** @param {import("../../domain/installation.js").Saver} s */
  put(s) {
    this.rows = this.rows.map((r) => (r.saver === s.saver ? toRow(s) : r));
  },

  dismissError() {
    this.error = null;
  },
});

/** @param {import("../../domain/installation.js").Saver} s */
function toRow(s) {
  const failure = s.error ? { ...s.error, next: NEXT_STEP[s.error.code] ?? "retry, or check the server log" } : null;
  return {
    ...s,
    error: failure,
    stateLabel: stateLabel(s),
    tone: stateTone(s),
    versions: versionsText(s),
    installLabel: installLabel(s),
    canRemove: canRemove(s),
    removing: false,
  };
}
