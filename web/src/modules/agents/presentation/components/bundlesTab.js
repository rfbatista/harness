// The Bundles tab: the list with its filters and footer, the open bundle's
// detail, and the in-page links into it and its editor. Mixed into the
// Agents page (agentsPage.js), which owns the catalog, the route and the
// history.

import { BUNDLE_FILTERS, bundleDetail, bundleRows, bundlesFooter } from "../../domain/bundles.js";

const modified = (event) => event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || event.button > 0;

/** @param {{ history: Pick<History, "pushState"> }} deps */
export function bundlesTab({ history }) {
  return {
    bundleQuery: "",
    bundleFilter: "all",

    get bundleRowsView() {
      if (!this.catalog) return [];
      return bundleRows(this.catalog, { query: this.bundleQuery, filter: this.bundleFilter }).map((r) => ({
        ...r,
        current: r.key === this.item && this.tab === "bundles" && this.mode === "",
        disk: this.diskBadge("bundle", r.key, null) !== null,
        noDisk: this.diskBadge("bundle", r.key, null) === null,
      }));
    },
    get bundleFilters() {
      return BUNDLE_FILTERS.map((f) => ({ ...f, pressed: f.id === this.bundleFilter, variant: f.id === this.bundleFilter ? "" : "ghost" }));
    },
    get bundlesFoot() {
      return this.catalog ? bundlesFooter(this.catalog) : "";
    },
    get noBundleRows() {
      return this.catalog !== null && this.bundleRowsView.length === 0;
    },
    get bundleEmptyTitle() {
      const q = this.bundleQuery.trim();
      return q ? `No bundles match “${q}”.` : "No bundles.";
    },
    setBundleFilter(event) {
      this.bundleFilter = event.currentTarget.dataset.filter;
    },

    /** The open bundle's detail, or null: no item, an editor, or a name the catalog lacks. */
    get bundle() {
      if (!this.catalog || this.tab !== "bundles" || !this.item || this.mode) return null;
      return bundleDetail(this.catalog, this.item);
    },
    get unknownBundle() {
      if (this.catalog === null || this.tab !== "bundles" || this.item === "") return false;
      return this.mode === "new" ? false : bundleDetail(this.catalog, this.item) === null;
    },
    get bundleHasAgents() {
      return (this.bundle?.agents.length ?? 0) > 0;
    },
    get bundleHasBundles() {
      return (this.bundle?.bundles.length ?? 0) > 0;
    },

    /** Moves to an address of the page without a reload. */
    navigateTo(href) {
      history.pushState({}, "", href);
      this.route(href.split(/[?#]/)[0]);
      this.loadSkill();
    },

    /** A click on a bundle's row, an Edit link or a link to another bundle. */
    openBundleLink(event) {
      if (modified(event)) return;
      event.preventDefault();
      const href = event.currentTarget.getAttribute("href");
      this.guarded(() => this.navigateTo(href));
    },
    /** The header's New button, on the Bundles tab. */
    openNew(event) {
      if (this.tab !== "bundles" || modified(event)) return;
      event.preventDefault();
      const href = event.currentTarget.getAttribute("href");
      this.guarded(() => this.navigateTo(href));
    },
  };
}
