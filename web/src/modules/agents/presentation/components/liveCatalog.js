// The open Agents page follows the catalog (contract section 6): it re-reads
// on the page feed's `agent_catalog_changed` event, on the feed coming back
// after a drop, when the window regains focus, and on Reload from disk. The
// events and their coalescing are domain/liveRefresh.js; this is what the page
// does with a read: swap the catalog in only when it differs (a focus on a
// 475-skill list must not redraw it), flag the items that changed on disk, tell
// the open editors, and show the broken-catalog banner. Mixed into the Agents
// page, which provides the catalog, the route, `error` and the editors.
//
// A save this page made itself is not a change from outside: the page records
// the versions its own writes answered (trackWrites) and leaves those out.

import { describeError } from "../../../../shared/presentation/errors.js";
import { changeKey, changeSentence, KIND_OF_TAB, catalogChanges, movedFiles } from "../../domain/onDisk.js";
import { refreshScheduler } from "../../domain/liveRefresh.js";

const WRITE_VERB = /^(save|delete|undo)/;

/**
 * Wraps a gateway so the page knows what its own writes answered, and when
 * they are done. Dry runs answer the current versions, so they are not
 * writes.
 * @template {object} G
 * @param {G} gateway
 */
export function trackWrites(gateway) {
  const own = { hashes: new Set(), skills: new Set() };
  const inflight = new Set();
  const record = (result) => {
    if (!result?.change || result.change.id === "") return;
    for (const v of Object.values(result.versions ?? {})) own.hashes.add(v);
    for (const w of result.change.writes ?? []) {
      const m = /^skills\/([^/]+)/.exec(w.path);
      if (m) own.skills.add(m[1]);
    }
  };
  const wrapped = { ...gateway };
  for (const [name, fn] of Object.entries(gateway)) {
    if (typeof fn !== "function" || !WRITE_VERB.test(name)) continue;
    wrapped[name] = (...args) => {
      const done = Promise.resolve(fn.apply(gateway, args));
      const settled = done.then(record, () => {}).finally(() => inflight.delete(settled));
      inflight.add(settled);
      return done;
    };
  }
  return {
    gateway: /** @type {G} */ (wrapped),
    /** Resolves when every write started so far has answered. */
    async settled() {
      while (inflight.size > 0) await Promise.allSettled([...inflight]);
    },
    /** What this page wrote since the last call. */
    takeOwn() {
      const out = { hashes: new Set(own.hashes), skills: new Set(own.skills) };
      own.hashes.clear();
      own.skills.clear();
      return out;
    },
  };
}

/**
 * Settles one open editor after a re-read: when a file it writes moved and
 * its own item changed (or went), the server is asked and Save is blocked with
 * the conflict banner; when only something else in the file moved, the editor
 * takes the new versions and keeps its draft; when nothing moved, a preview
 * that was waiting for the catalog to parse is asked for.
 * @param {ReturnType<typeof import("./saveController.js").saveController>} saver
 * @param {import("../../domain/onDisk.js").Kind} kind
 * @param {string} name  the item being edited, "" for a new one
 * @param {{ versions: Record<string, string>, changes: Record<string, unknown> }} ctx
 */
export function settleEditor(saver, kind, name, ctx) {
  const based = saver.state.baseVersions;
  const moved = movedFiles(based, ctx.versions);
  if (moved.length === 0) {
    saver.resume();
    return;
  }
  if (saver.state.phase === "conflict") return;
  if (name !== "" && ctx.changes[changeKey(kind, name)]) {
    void saver.probe(ctx.versions);
    return;
  }
  saver.rebase(Object.fromEntries(moved.map((p) => [p, ctx.versions[p]])));
}

/**
 * @param {{
 *   gateway: Pick<import("../../domain/ports.js").CatalogGateway, "getCatalog" | "getSkill"> & Partial<Pick<import("../../domain/ports.js").CatalogGateway, "follow">>,
 *   tracker: ReturnType<typeof trackWrites>,
 *   timers?: { setTimeout: (fn: () => void, ms: number) => any, clearTimeout: (id: any) => void },
 * }} deps
 */
export function liveCatalog({ gateway, tracker, timers = globalThis }) {
  /** @type {ReturnType<typeof refreshScheduler> | null} */
  let scheduler = null;
  let unfollow = () => {};

  return {
    /** Items that changed on disk since the person last looked, by "kind:name" (domain/onDisk.js). */
    changed: {},
    refreshing: false,
    /** The page feed's status, for whoever watches the page (and the browser tests): connecting, live, paused, resynced. */
    feedStatus: "connecting",
    /** The version of the open skill's folder, as its detail read it. */
    skillVersion: "",

    /** Called from init, once the catalog is known. */
    startLive() {
      scheduler = refreshScheduler({
        reload: async () => {
          try {
            await this.refresh();
          } catch (err) {
            this.error = describeError(err);
          }
        },
        timers,
      });
      unfollow = gateway.follow?.({
        onEvent: (data) => scheduler?.event(data),
        onStatus: (status) => {
          this.feedStatus = status;
          scheduler?.status(status);
        },
      }) ?? (() => {});
    },
    stopLive() {
      unfollow();
      scheduler?.dispose();
    },
    /** Alpine calls this when the page's element is removed. */
    destroy() {
      this.stopLive();
    },
    /** The window regained focus: edits by hand show up. */
    windowFocused() {
      scheduler?.focus();
    },
    /** Reload from disk, and Check again: read now. */
    reloadFromDisk() {
      if (scheduler) scheduler.now();
      else void this.refresh();
    },
    get reloadLabel() {
      return this.refreshing ? "Reloading…" : "Reload from disk";
    },
    get checkCatalogLabel() {
      return this.refreshing ? "Checking…" : "Check again";
    },

    /** Reads the catalog again; swaps it in only when it differs, and tells what changed. */
    async refresh() {
      await tracker.settled();
      this.refreshing = true;
      try {
        let next;
        try {
          next = await gateway.getCatalog();
        } catch (err) {
          this.error = describeError(err);
          return;
        }
        this.error = null;
        const before = this.catalog;
        const own = tracker.takeOwn();
        const changes = catalogChanges(before, next, own);
        const skill = await this.readOpenSkill(own);
        if (skill?.change) changes[changeKey("skill", this.item)] = skill.change;
        if (before === null || JSON.stringify(before) !== JSON.stringify(next)) {
          this.catalog = next;
          this.checked = {};
        }
        if (Object.keys(changes).length > 0) this.changed = { ...this.changed, ...changes };
        this.catalogMoved?.({ catalog: next, changes, versions: { ...next.versions, ...(skill?.versions ?? {}) } });
      } finally {
        this.refreshing = false;
      }
    },

    /**
     * The open skill's folder, read again: its version is not in the catalog
     * read. Returns its versions, and the change when its folder moved under
     * an open detail pane.
     */
    async readOpenSkill(own) {
      const name = this.item;
      if (this.tab !== "skills" || !name || this.mode === "new" || this.catalog === null || !this.catalog.skills.some((s) => s.name === name)) return null;
      let read;
      try {
        read = await gateway.getSkill(name);
      } catch {
        return null;
      }
      if (this.item !== name || this.tab !== "skills") return null;
      const now = read.versions[`skills/${name}`] ?? "";
      if (this.mode !== "") return { versions: read.versions };
      const had = this.skillVersion;
      if (now !== had || this.skillData === null) this.skillData = read.skill;
      this.skillVersion = now;
      if (had !== "" && now !== had && !own.hashes.has(now) && !own.skills.has(name)) {
        return { versions: read.versions, change: { kind: "skill", name, gone: false, lines: ["SKILL.md or its files changed"] } };
      }
      return { versions: read.versions };
    },

    /** The open item's change, for the banner above its detail; null when it has none. */
    get disk() {
      const kind = KIND_OF_TAB[this.tab];
      if (!kind || !this.item || this.mode) return null;
      const c = this.changed[changeKey(kind, this.item)];
      return c ? { sentence: changeSentence(c), lines: c.lines.map((text) => ({ text })) } : null;
    },
    dismissDisk() {
      this.clearDisk(KIND_OF_TAB[this.tab], this.item);
    },
    /** @param {string} kind @param {string} name */
    clearDisk(kind, name) {
      const key = changeKey(/** @type {any} */ (kind), name);
      if (!(key in this.changed)) return;
      const { [key]: _gone, ...rest } = this.changed;
      this.changed = rest;
    },
    /** A list row's badge: "changed on disk" over the problem it would show. */
    diskBadge(kind, name, badge) {
      return this.changed[changeKey(kind, name)] ? { label: "changed on disk", tone: "attention" } : badge;
    },

    // A catalog that does not parse.

    get broken() {
      const b = this.catalog?.broken ?? null;
      if (!b) return null;
      return {
        title: `${b.path} does not parse`,
        where: `Line ${b.line}, column ${b.column}`,
        message: b.message,
        note: "The lists below are the last version that parsed. Saving and deleting are off until the file is fixed by hand.",
      };
    },
    /** Why saving is off, or "". */
    get brokenWhy() {
      const b = this.catalog?.broken ?? null;
      return b ? `${b.path} does not parse` : "";
    },
    get isBroken() {
      return this.brokenWhy !== "";
    },
  };
}
