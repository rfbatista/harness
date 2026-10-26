// The Agents page: the catalog's header (its root and the New button), the
// five tabs, and the tab panels. The server paints all of it from the catalog
// read (data-seed), and this takes over without a flash; with no seed it
// reads the catalog itself. Choosing a tab moves the address to the tab's
// route without a reload. The Bundles tab and its editor are mixed in from
// bundlesTab.js, bundleEditor.js and saveUi.js; the other tabs' editors come
// with the later tickets.
//
//   <main x-data="agentsPage" data-seed="agent-catalog-seed" data-tab="skills" …>

import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { AGENT_FILTERS, agentDetail, agentRows, agentsFooter } from "../../domain/library.js";
import { renderMarkdown } from "../../domain/markdown.js";
import { MCP_FILTERS, mcpDetail, mcpFooter, mcpRows } from "../../domain/mcp.js";
import { SKILL_FILTERS, SKILL_PAGE, skillFiles, skillReach, skillRows, skillSummary, skillsFooter } from "../../domain/skills.js";
import { TABS, routeOf, tabAfterKey } from "../../domain/tabs.js";
import { TOOL_FILTERS, toolDetail, toolRows, toolsFooter } from "../../domain/tools.js";
import { agentEditor } from "./agentEditor.js";
import { bundleEditor } from "./bundleEditor.js";
import { bundlesTab } from "./bundlesTab.js";
import { compose } from "./compose.js";
import { editors } from "./editors.js";
import { mcpEditor } from "./mcpEditor.js";
import { saveUi } from "./saveUi.js";
import { skillEditor } from "./skillEditor.js";

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
 *   gateway: Pick<import("../../domain/ports.js").CatalogGateway, "decodeCatalog" | "getCatalog" | "getSkill" | "checkEnvVars" | "saveAgent" | "saveBundle" | "saveSkill" | "saveMcpServer" | "testMcpServer" | "listProjects" | "undoChange">,
 *   history: Pick<History, "pushState"> & Partial<Pick<History, "replaceState">>,
 *   location: Pick<Location, "pathname" | "search"> & Partial<Pick<Location, "hash" | "assign">>,
 *   clipboard?: Pick<Clipboard, "writeText">,
 *   timers?: { setTimeout: (fn: () => void, ms: number) => any, clearTimeout: (id: any) => void },
 * }} deps
 */
export const agentsPage = ({ gateway, history, location, clipboard, timers = globalThis }) => () => compose({
  /** @type {import("../../domain/tabs.js").TabId} */
  tab: "agents",
  item: "",
  mode: "",
  /** The path the page shows now: a move within one page (a #fragment) is not a navigation. */
  shown: "",
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
  /** The Skills tab: text filter, quick filter, how many rows are mounted, and the open skill as the server read it. */
  skillQuery: "",
  skillFilter: "all",
  skillLimit: SKILL_PAGE,
  /** @type {import("../../domain/ports.js").SkillDetail["skill"] | null} */
  skillData: null,
  skillLoading: false,
  skillError: null,
  /** The MCP servers tab: filters, and what Check again last answered per variable. */
  mcpQuery: "",
  mcpFilter: "all",
  /** @type {Record<string, boolean>} */
  checked: {},
  checking: false,
  /** The Tools tab: filters and the open tool (not in the address). */
  toolQuery: "",
  toolFilter: "all",
  toolItem: "",

  async init() {
    this.setupEditor();
    this.route(location.pathname, this.$el.dataset);
    try {
      this.catalog = gateway.decodeCatalog(readSeed(this.$el));
    } catch {
      await this.load();
    }
    this.syncEditor();
    this.toolItem = location.hash ? decodeURIComponent(location.hash.slice(1)) : "";
    await this.loadSkill();
  },

  async load() {
    this.loading = true;
    try {
      this.catalog = await gateway.getCatalog();
      this.error = null;
      this.checked = {};
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
    this.shown = pathname;
    this.opened = {};
    this.skillData = null;
    this.skillError = null;
    this.syncEditor();
  },

  /** Moves the address to a page of this page without a reload. */
  push(href) {
    history.pushState({}, "", href);
    this.shown = href.split(/[?#]/)[0];
  },

  /** The browser's back and forward. A dirty editor puts the address back and asks first. */
  popped() {
    const target = location.pathname;
    if (target === this.shown) return;
    if (this.leaveGuarded) {
      history.pushState({}, "", this.editorHref);
      this.leaving = { go: () => this.landOn(target) };
      return;
    }
    this.landOn(target);
  },
  landOn(pathname) {
    this.route(pathname);
    this.loadSkill();
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
    this.guarded(() => {
      this.tab = tab.id;
      this.item = "";
      this.mode = "";
      this.opened = {};
      this.toolItem = "";
      this.skillData = null;
      this.push(tab.href);
      this.syncEditor();
    });
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

  /** A full-page editor or form is open: it takes the place of the header, the tabs and the lists. */
  get fullPage() {
    return this.agentEditing || this.skillEditing || this.mfOpen;
  },

  get noItem() {
    return this.item === "";
  },
  /** The deep link's note shows only until the catalog is known. */
  get itemNoteHidden() {
    return this.item === "" || this.catalog !== null;
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
    return this.catalog !== null && this.tab === "agents" && this.item !== "" && (this.mode === "" || this.mode === "edit") && this.detail === null && this.agentDraft === null;
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
  /** Narrow screens show the list, or with an item open the detail. */
  get detailOpenAttr() {
    const open = {
      agents: this.detail !== null || this.unknownAgent,
      skills: this.skill !== null || this.unknownSkill,
      mcp: this.mcp !== null || this.unknownMcp,
      bundles: this.bundle !== null || this.unknownBundle || this.editing,
      tools: this.toolItem !== "",
    }[this.tab];
    return open ? "" : null;
  },

  setFilter(event) {
    this.filter = event.currentTarget.dataset.filter;
  },

  /** A click on a row: show its detail and move the address, unless the click opens a new window. */
  openAgent(event) {
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || event.button > 0) return;
    event.preventDefault();
    const key = event.currentTarget.dataset.key;
    const href = event.currentTarget.getAttribute("href");
    this.guarded(() => {
      this.item = key;
      this.mode = "";
      this.opened = {};
      this.skillData = null;
      this.skillError = null;
      this.push(href);
      this.syncEditor();
      this.loadSkill();
    });
  },

  /** The narrow screens' way back from the detail to the list. */
  backToList(event) {
    event.preventDefault();
    this.show(this.tab);
  },

  toggleGroup(event) {
    const name = event.currentTarget.dataset.group;
    const now = this.groups.find((g) => g.name === name)?.expanded ?? false;
    this.opened = { ...this.opened, [name]: !now };
  },

  // The Skills tab: the list a page at a time, and the open skill.

  get skillRowsResult() {
    if (!this.catalog) return { rows: [], total: 0, hidden: 0 };
    return skillRows(this.catalog, { query: this.skillQuery, filter: this.skillFilter, limit: this.skillLimit });
  },
  get skillRowsView() {
    return this.skillRowsResult.rows.map((r) => ({
      ...r,
      current: r.name === this.item && this.tab === "skills" && this.mode === "",
      hasBadge: r.badge !== null,
      noBadge: r.badge === null,
      badgeLabel: r.badge?.label ?? "",
      badgeTone: r.badge?.tone ?? "",
    }));
  },
  get skillFilters() {
    return SKILL_FILTERS.map((f) => ({ ...f, pressed: f.id === this.skillFilter, variant: f.id === this.skillFilter ? "" : "ghost" }));
  },
  get skillsFoot() {
    return this.catalog ? skillsFooter(this.catalog) : "";
  },
  get noSkillRows() {
    return this.catalog !== null && this.skillRowsResult.total === 0;
  },
  get skillEmptyTitle() {
    const q = this.skillQuery.trim();
    return q ? `No skills match \u201c${q}\u201d.` : "No skills.";
  },
  get skillsHidden() {
    return this.skillRowsResult.hidden;
  },
  get noSkillsHidden() {
    return this.skillRowsResult.hidden === 0;
  },
  get skillMoreLabel() {
    return `Show ${Math.min(SKILL_PAGE, this.skillRowsResult.hidden)} more`;
  },
  get skillMoreNote() {
    return `${this.skillRowsResult.hidden} more not shown`;
  },

  setSkillFilter(event) {
    this.skillFilter = event.currentTarget.dataset.filter;
    this.skillLimit = SKILL_PAGE;
  },
  /** The text filter changed: start again from the first page. */
  skillQueryChanged() {
    this.skillLimit = SKILL_PAGE;
  },
  showMoreSkills() {
    this.skillLimit += SKILL_PAGE;
  },

  /** The open skill as the catalog read lists it, or null: no item, an editor, or a name the catalog lacks. */
  get skill() {
    if (!this.catalog || this.tab !== "skills" || !this.item || this.mode) return null;
    return skillSummary(this.catalog, this.item);
  },
  get unknownSkill() {
    return this.catalog !== null && this.tab === "skills" && this.item !== "" && this.mode === "" && this.skill === null;
  },
  get skillReady() {
    return this.skillData !== null;
  },
  get skillPending() {
    return this.skillData === null && this.skillError === null;
  },
  get skillErrorText() {
    return this.skillError?.message ?? "";
  },
  get noSkillError() {
    return this.skillError === null;
  },
  get skillFrontmatter() {
    const s = this.skillData;
    return s ? `---\nname: ${s.name}\ndescription: ${s.description}\n---` : "";
  },
  get skillFilesView() {
    return skillFiles((this.skillData?.files ?? []).filter((f) => f.path !== "SKILL.md"));
  },
  get skillReach() {
    return skillReach(this.skillData ?? { agents: [], bundles: [], agentCount: 0 });
  },
  get hasReachAgents() {
    return this.skillReach.agents.length > 0;
  },
  get hasReachBundles() {
    return this.skillReach.bundles.length > 0;
  },
  get skillFileHeading() {
    const n = this.skillFilesView.length;
    return `Files · ${n}`;
  },

  /** Reads the open skill's body, files and reach. Answers that arrive after the person moved on are dropped. */
  async loadSkill() {
    const name = this.item;
    if (this.tab !== "skills" || !name || this.mode || this.skill === null) return;
    this.skillError = null;
    try {
      const { skill } = await gateway.getSkill(name);
      if (this.item === name && this.tab === "skills") this.skillData = skill;
    } catch (err) {
      if (this.item === name) this.skillError = describeError(err);
    }
  },

  /** Puts the open skill's SKILL.md into its element. The text is escaped on the way: embedded HTML shows as text. Called from x-effect. */
  paintBody(el) {
    el.innerHTML = renderMarkdown(this.skillData?.body ?? "");
  },

  // The MCP servers tab.

  get mcpRowsView() {
    if (!this.catalog) return [];
    return mcpRows(this.catalog, { query: this.mcpQuery, filter: this.mcpFilter }).map((r) => ({
      ...r,
      current: r.name === this.item && this.tab === "mcp" && this.mode === "",
      hasBadge: r.badge !== null,
      noBadge: r.badge === null,
      badgeLabel: r.badge?.label ?? "",
      badgeTone: r.badge?.tone ?? "",
    }));
  },
  get mcpFilters() {
    return MCP_FILTERS.map((f) => ({ ...f, pressed: f.id === this.mcpFilter, variant: f.id === this.mcpFilter ? "" : "ghost" }));
  },
  get mcpFoot() {
    return this.catalog ? mcpFooter(this.catalog) : "";
  },
  get noMcpRows() {
    return this.catalog !== null && this.mcpRowsView.length === 0;
  },
  get mcpEmptyTitle() {
    const q = this.mcpQuery.trim();
    return q ? `No MCP servers match \u201c${q}\u201d.` : "No MCP servers.";
  },
  setMcpFilter(event) {
    this.mcpFilter = event.currentTarget.dataset.filter;
  },
  get mcp() {
    if (!this.catalog || this.tab !== "mcp" || !this.item || this.mode) return null;
    return mcpDetail(this.catalog, this.item, this.checked);
  },
  get unknownMcp() {
    return this.catalog !== null && this.tab === "mcp" && this.item !== "" && this.mode === "" && this.mcp === null;
  },
  get mcpUnset() {
    return (this.mcp?.unset.length ?? 0) > 0;
  },
  get mcpAllSet() {
    return !this.mcpUnset;
  },
  get mcpUsed() {
    return this.mcp !== null && !this.mcp.unused;
  },
  get mcpHasAgents() {
    return (this.mcp?.agents.length ?? 0) > 0;
  },
  get mcpHasProjects() {
    return (this.mcp?.projects.length ?? 0) > 0;
  },
  get mcpHasRefs() {
    return (this.mcp?.refs.length ?? 0) > 0;
  },
  get mcpHasEntries() {
    return (this.mcp?.entries.length ?? 0) > 0;
  },
  get checkLabel() {
    return this.checking ? "Checking\u2026" : "Check again";
  },

  /** Asks the server which of the open server's variables are set now. Never a value. */
  async checkAgain() {
    const names = this.mcp?.refNames ?? [];
    if (names.length === 0) return;
    this.checking = true;
    try {
      this.checked = { ...this.checked, ...(await gateway.checkEnvVars(names)) };
      this.notice = "Checked the variables again";
    } catch (err) {
      this.notice = describeError(err).message;
    } finally {
      this.checking = false;
    }
  },

  // The Tools tab: read-only.

  get toolRowsView() {
    if (!this.catalog) return [];
    return toolRows(this.catalog, { query: this.toolQuery, filter: this.toolFilter }).map((r) => ({ ...r, current: r.name === this.toolItem }));
  },
  get toolFilters() {
    return TOOL_FILTERS.map((f) => ({ ...f, pressed: f.id === this.toolFilter, variant: f.id === this.toolFilter ? "" : "ghost" }));
  },
  get toolsFoot() {
    return toolsFooter();
  },
  get noToolRows() {
    return this.catalog !== null && this.toolRowsView.length === 0;
  },
  setToolFilter(event) {
    this.toolFilter = event.currentTarget.dataset.filter;
  },
  get tool() {
    if (!this.catalog || this.tab !== "tools" || !this.toolItem) return null;
    return toolDetail(this.catalog, this.toolItem);
  },
  get noTool() {
    return this.tool === null;
  },
  openTool(event) {
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || event.button > 0) return;
    event.preventDefault();
    const key = event.currentTarget.dataset.key;
    const href = event.currentTarget.getAttribute("href");
    this.guarded(() => {
      this.toolItem = key;
      this.push(href);
    });
  },

  /** The actions menu chose an item. */
  async menuSelect(event) {
    const open = this.detail ?? this.skill ?? this.mcp ?? this.bundle;
    if (event.detail.action !== "copy-path" || !open) return;
    try {
      await clipboard?.writeText(open.copyPath);
      this.notice = "Path copied";
    } catch {
      this.notice = "Could not copy the path";
    }
  },
}, bundlesTab({ history }), editors(
  { part: bundleEditor({ gateway, location, timers }) },
  // The MCP form's drawer opens over the agent editor: while it does, the shared members answer for the drawer.
  { part: agentEditor({ gateway, location, history, timers }), isOpen: (p) => p.agentDraft !== null && !p.mfOpen },
  { part: skillEditor({ gateway, location, timers }), isOpen: (p) => p.sfOpen },
  { part: mcpEditor({ gateway, location, timers }), isOpen: (p) => p.mfOpen },
), saveUi({ gateway }));
