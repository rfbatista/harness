// The MCP servers section of a project's settings (UX spec section 7): a
// checklist of every catalog server, Save and Cancel while the selection
// changed, a toast with Undo after a save, the What a session gets example,
// and a banner naming a server the catalog took off the project. It follows
// the catalog like the Agents page does (domain/liveRefresh.js).
//
//   <section x-data="projectMcpServers" data-project-id="…" data-project-name="…">
//
// set_project_mcp_servers answers only the list (it makes no catalog change to
// undo), so Undo puts the list the save replaced back with the same verb.

import { describeError } from "../../../../shared/presentation/errors.js";
import { describeRefusal } from "../../domain/refusals.js";
import { refreshScheduler } from "../../domain/liveRefresh.js";
import {
  projectMcpRows, projectMcpSaved, removedFromProject, selectionChanged, sessionExample,
} from "../../domain/projectMcp.js";

const plural = (n, word) => `${n} ${word}${n === 1 ? "" : "s"}`;
const quoted = (names) => names.map((n) => `“${n}”`).join(", ");
const NOTE = "They reach sessions from their next start. Running sessions keep what they started with.";

/**
 * @param {{
 *   gateway: Pick<import("../../domain/ports.js").CatalogGateway, "getCatalog" | "getProjectMcpServers" | "setProjectMcpServers"> & Partial<Pick<import("../../domain/ports.js").CatalogGateway, "follow">>,
 *   timers?: { setTimeout: (fn: () => void, ms: number) => any, clearTimeout: (id: any) => void },
 * }} deps
 */
export function projectMcpServers({ gateway, timers = globalThis }) {
  return () => {
    let scheduler = null;
    let unfollow = () => {};
    return {
      projectId: "",
      projectName: "",
      /** @type {import("../../domain/ports.js").Catalog | null} */
      catalog: null,
      /** The list the project has saved, and the one ticked now. */
      saved: /** @type {string[]} */ ([]),
      selected: /** @type {string[]} */ ([]),
      loaded: false,
      saving: false,
      error: null,
      refusal: null,
      /** @type {{ names: string[] } | null} */
      removed: null,
      toast: null,
      feedStatus: "connecting",

      async init() {
        this.projectId = this.$el.dataset.projectId ?? "";
        this.projectName = this.$el.dataset.projectName ?? "";
        await this.load();
        scheduler = refreshScheduler({ reload: () => this.refresh(), timers });
        unfollow = gateway.follow?.({
          onEvent: (data) => scheduler?.event(data),
          onStatus: (status) => {
            this.feedStatus = status;
            scheduler?.status(status);
          },
        }) ?? (() => {});
      },
      destroy() {
        unfollow();
        scheduler?.dispose();
      },
      windowFocused() {
        scheduler?.focus();
      },

      async load() {
        try {
          const [catalog, names] = await Promise.all([gateway.getCatalog(), gateway.getProjectMcpServers(this.projectId)]);
          this.catalog = catalog;
          this.saved = projectMcpSaved(catalog, names);
          this.selected = [...this.saved];
          this.error = null;
        } catch (err) {
          this.error = describeError(err);
        } finally {
          this.loaded = true;
        }
      },

      /** The catalog or the project's list changed: read both, keep what was ticked, name what was taken off. */
      async refresh() {
        if (this.saving) return;
        let catalog;
        let names;
        try {
          [catalog, names] = await Promise.all([gateway.getCatalog(), gateway.getProjectMcpServers(this.projectId)]);
        } catch (err) {
          this.error = describeError(err);
          return;
        }
        this.error = null;
        const gone = removedFromProject(this.saved, catalog, names);
        const keep = selectionChanged(this.saved, this.selected);
        const next = projectMcpSaved(catalog, names);
        const ticked = projectMcpSaved(catalog, this.selected);
        if (JSON.stringify(catalog) !== JSON.stringify(this.catalog)) this.catalog = catalog;
        this.saved = next;
        this.selected = keep ? ticked : [...next];
        if (gone.length > 0) this.removed = { names: [...(this.removed?.names ?? []), ...gone.filter((n) => !(this.removed?.names ?? []).includes(n))] };
      },

      // What the section shows.

      get hasCatalog() {
        return this.catalog !== null;
      },
      get rows() {
        return this.catalog ? projectMcpRows(this.catalog, this.projectId, this.selected) : [];
      },
      get isEmpty() {
        return this.loaded && this.catalog !== null && this.catalog.mcpServers.length === 0;
      },
      get hasRows() {
        return this.rows.length > 0;
      },
      get isLoading() {
        return !this.loaded;
      },
      get changed() {
        return selectionChanged(this.saved, this.selected);
      },
      get unchanged() {
        return !this.changed;
      },
      get count() {
        return this.hasRows ? `${this.selected.length} of ${this.rows.length} selected` : "";
      },
      get brokenWhy() {
        const b = this.catalog?.broken ?? null;
        return b ? `${b.path} does not parse` : "";
      },
      get isBroken() {
        return this.brokenWhy !== "";
      },
      get brokenNote() {
        return this.isBroken ? `${this.brokenWhy}, so saving is off until the file is fixed by hand. The list below is the last version that parsed.` : "";
      },
      get cannotSave() {
        return !this.changed || this.saving || this.isBroken;
      },
      get saveLabel() {
        return this.saving ? "Saving…" : "Save";
      },
      get example() {
        const ex = this.catalog ? sessionExample(this.catalog, this.selected) : { agent: "", withAgent: [], withoutAgent: [] };
        return {
          ...ex,
          hasAgent: ex.agent !== "",
          asAgent: `As ${ex.agent}`,
          noneWithAgent: ex.withAgent.length === 0,
          noneWithoutAgent: ex.withoutAgent.length === 0,
        };
      },
      get bannerText() {
        const names = this.removed?.names ?? [];
        if (names.length === 0) return "";
        return `${quoted(names)} ${names.length === 1 ? "was" : "were"} deleted from the catalog and ${names.length === 1 ? "was" : "were"} taken off this project too. The list below is up to date.`;
      },
      get hasBanner() {
        return this.removed !== null;
      },
      get hasToast() {
        return this.toast !== null;
      },

      // What the person does.

      /** @param {string} name */
      toggle(name) {
        this.refusal = null;
        this.selected = this.selected.includes(name) ? this.selected.filter((n) => n !== name) : [...this.selected, name];
      },
      cancel() {
        this.refusal = null;
        this.selected = [...this.saved];
      },
      dismissBanner() {
        this.removed = null;
      },
      dismissToast() {
        this.toast = null;
      },

      async save() {
        if (this.cannotSave) return;
        await this.write(this.selected, "saved");
      },
      async undoSave() {
        const before = this.toast?.undoTo;
        if (!before) return;
        await this.write(before, "undone");
      },

      /** @param {string[]} names @param {"saved"|"undone"} kind */
      async write(names, kind) {
        const previous = this.saved;
        this.saving = true;
        this.refusal = null;
        try {
          const answer = await gateway.setProjectMcpServers(this.projectId, names);
          this.saved = projectMcpSaved(this.catalog, answer);
          this.selected = [...this.saved];
          const n = this.saved.length;
          this.toast = kind === "saved"
            ? { text: `Saved — ${this.projectName} now gives ${plural(n, "MCP server")} to its sessions.`, note: NOTE, tone: "", undoTo: previous, hasUndo: true }
            : { text: `Undone — ${this.projectName} gives ${plural(n, "MCP server")} again.`, note: "Running sessions keep what they started with.", tone: "", undoTo: null, hasUndo: false };
        } catch (err) {
          const r = describeRefusal(err);
          if (kind === "undone") {
            this.toast = { text: r.message, note: "", tone: "danger", undoTo: null, hasUndo: false };
          } else {
            this.refusal = r;
          }
          if (r.code === "UNKNOWN_REFERENCE") scheduler?.now();
        } finally {
          this.saving = false;
        }
      },
    };
  };
}
