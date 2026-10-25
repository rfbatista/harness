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
 * }} deps
 */
export const agentsPage = ({ gateway, history, location }) => () => ({
  /** @type {import("../../domain/tabs.js").TabId} */
  tab: "agents",
  item: "",
  mode: "",
  /** @type {import("../../domain/ports.js").Catalog | null} */
  catalog: null,
  error: null,
  loading: false,

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
});
