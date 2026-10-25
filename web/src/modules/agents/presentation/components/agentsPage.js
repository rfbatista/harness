// The Agents page: the catalog's header (its root and the New button), the
// five tabs, and the tab panels. The server paints all of it from the catalog
// read (data-seed), and this takes over without a flash; with no seed it
// reads the catalog itself. Choosing a tab moves the address to the tab's
// route without a reload. The tab panels are filled by the lists and
// editors of the later tickets.
//
//   <main x-data="agentsPage" data-seed="agent-catalog-seed" data-tab="skills" …>

import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { AGENT_FILTERS, agentDetail, agentRows, agentsFooter } from "../../domain/library.js";
import { TABS, routeOf, tabAfterKey } from "../../domain/tabs.js";

const DEFAULT_ROOT = "<data dir>/agents/";

/** How many items a tab lists, from the catalog. */
const COUNTS = {
  agents: (c) => c.agents.length,
  skills: (c) => c.skills.length,
  bundles: (c) => c.bundles.length,
  mcp: (c) => c.mcpServers.length,
  tools: (c) => c.builtInTools.length,
};

/**
 * @param {{
 *   gateway: Pick<import("../../domain/ports.js").CatalogGateway, "decodeCatalog" | "getCatalog">,
 *   history: Pick<History, "pushState">,
 *   location: Pick<Location, "pathname">,
 *   clipboard?: Pick<Clipboard, "writeText">,
 * }} deps
 */
export const agentsPage = ({ gateway, history, location, clipboard }) => () => ({
  /** @type {import("../../domain/tabs.js").TabId} */
  tab: "agents",
  item: "",
  mode: "",
  /** @type {import("../../domain/ports.js").Catalog | null} */
  catalog: null,
  error: null,
  loading: false,
  /** The Agents tab's text filter and quick filter ("all", "mcp", "problems"). */
  query: "",
  filter: "all",
  /** Bundle groups the person opened or closed in the open agent's detail, by "@name". */
  opened: {},
  /** What the last action said ("Path copied"), for the live region. */
  notice: "",

  async init() {
    this.route(location.pathname, this.$el.dataset);
    try {
      this.catalog = gateway.decodeCatalog(readSeed(this.$el));
    } catch {
      await this.load();
    }
  },

  async load() {
    this.loading = true;
    try {
      this.catalog = await gateway.getCatalog();
      this.error = null;
    } catch (err) {
      this.error = describeError(err);
    } finally {
      this.loading = false;
    }
  },

  /** The tab, item and mode a path (or, at first, the server's data attributes) names. */
  route(pathname, fallback = {}) {
    const r = routeOf(pathname);
    this.tab = r?.tab ?? fallback.tab ?? "agents";
    this.item = r?.item ?? fallback.item ?? "";
    this.mode = r?.mode ?? fallback.mode ?? "";
    this.opened = {};
  },

  /** The browser's back and forward. */
  popped() {
    this.route(location.pathname);
  },

  /** A click on a tab: show it and move the address, unless the click opens a new window. */
  select(event) {
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || event.button > 0) return;
    event.preventDefault();
    this.show(event.currentTarget.dataset.tab);
  },

  show(id) {
    const tab = TABS.find((t) => t.id === id);
    if (!tab) return;
    this.tab = tab.id;
    this.item = "";
    this.mode = "";
    this.opened = {};
    history.pushState({}, "", tab.href);
  },

  /** Arrow keys, Home and End move to a tab, focus it and show it. */
  tabKey(event) {
    const next = tabAfterKey(event.currentTarget.dataset.tab, event.key);
    if (!next) return;
    event.preventDefault();
    this.show(next);
    event.currentTarget.parentElement.querySelector(`[data-tab="${next}"]`)?.focus();
  },

  get root() {
    return this.catalog?.root || DEFAULT_ROOT;
  },

  get current() {
    return TABS.find((t) => t.id === this.tab) ?? TABS[0];
  },

  get newLabel() {
    return this.current.newLabel;
  },
  get newHref() {
    return this.current.newHref;
  },
  get newHidden() {
    return this.current.newLabel === "";
  },

  /** Per tab: what the tab and its panel bind to. */
  get tabState() {
    const out = {};
    for (const t of TABS) {
      const selected = t.id === this.tab;
      out[t.id] = {
        selected,
        tabindex: selected ? "0" : "-1",
        hidden: !selected,
        count: this.catalog ? String(COUNTS[t.id](this.catalog)) : "",
      };
    }
    return out;
  },

  get noItem() {
    return this.item === "";
  },

  /** What the open panel says about the item a deep link names, until its detail exists. */
  get itemNote() {
    if (!this.item) return "";
    return this.mode ? `${this.mode === "new" ? "New" : "Edit"} ${this.item}` : this.item;
  },

  // The Agents tab: the list, its filters and the open agent's detail.

  get agentRowsView() {
    if (!this.catalog) return [];
    return agentRows(this.catalog, { query: this.query, filter: this.filter }).map((r) => ({
      ...r,
      current: r.name === this.item && this.mode === "",
      hasBadge: r.badge !== null,
      noBadge: r.badge === null,
      badgeLabel: r.badge?.label ?? "",
      badgeTone: r.badge?.tone ?? "",
    }));
  },

  get agentFilters() {
    return AGENT_FILTERS.map((f) => ({ ...f, pressed: f.id === this.filter, variant: f.id === this.filter ? "" : "ghost" }));
  },

  get agentsFoot() {
    return this.catalog ? agentsFooter(this.catalog) : "";
  },

  get noAgentRows() {
    return this.catalog !== null && this.agentRowsView.length === 0;
  },
  get emptyTitle() {
    const q = this.query.trim();
    return q ? `No agents match \u201c${q}\u201d.` : "No agents.";
  },

  /** The open agent's detail, or null: no item, an editor, or a name the catalog lacks. */
  get detail() {
    if (!this.catalog || this.tab !== "agents" || !this.item || this.mode) return null;
    return agentDetail(this.catalog, this.item);
  },
  get noDetail() {
    return this.detail === null;
  },
  /** An item the catalog does not have. */
  get unknownAgent() {
    return this.catalog !== null && this.tab === "agents" && this.item !== "" && this.mode === "" && this.detail === null;
  },
  get detailProblems() {
    return (this.detail?.problems ?? []).filter((p) => p.code !== "NO_DESCRIPTION");
  },
  /** The detail's bundle groups, each open or closed: one bundle starts open. */
  get groups() {
    const d = this.detail;
    if (!d) return [];
    return d.groups.map((g) => {
      const expanded = this.opened[g.name] ?? d.groups.length === 1;
      return { ...g, expanded, closed: !expanded };
    });
  },
  /** Narrow screens show the list, or with an agent open the detail. */
  get detailOpenAttr() {
    return this.detail !== null || this.unknownAgent ? "" : null;
  },

  setFilter(event) {
    this.filter = event.currentTarget.dataset.filter;
  },

  /** A click on an agent's row: show its detail and move the address, unless the click opens a new window. */
  openAgent(event) {
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || event.button > 0) return;
    event.preventDefault();
    this.item = event.currentTarget.dataset.key;
    this.mode = "";
    this.opened = {};
    history.pushState({}, "", event.currentTarget.getAttribute("href"));
  },

  /** The narrow screens' way back from the detail to the list. */
  backToList(event) {
    event.preventDefault();
    this.show("agents");
  },

  toggleGroup(event) {
    const name = event.currentTarget.dataset.group;
    const now = this.groups.find((g) => g.name === name)?.expanded ?? false;
    this.opened = { ...this.opened, [name]: !now };
  },

  /** The actions menu chose an item. */
  async menuSelect(event) {
    if (event.detail.action !== "copy-path" || !this.detail) return;
    try {
      await clipboard?.writeText(this.detail.copyPath);
      this.notice = "Path copied";
    } catch {
      this.notice = "Could not copy the path";
    }
  },
});
