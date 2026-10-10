// The repository code page: a repository at a branch, tag or commit, its
// Files and Map. A ref is fixed, so nothing follows the feed and no change
// marks show. Picking another ref is a navigation (?ref=), which keeps the
// open path; the server says when the path is not at the new ref.
//
//   <main x-data="codePage" data-seed="code-seed" x-on:popstate.window="popState" x-on:keydown="onKey">

import { readSeed } from "../../../../shared/presentation/seed.js";
import { hrefOf } from "../../domain/routes.js";
import { codeBrowser } from "./codeBrowser.js";
import { compose } from "./compose.js";

/** @param {import("./codeBrowser.js").BrowserDeps & { navigate: (url: string) => void }} deps */
export const codePage = (deps) => () => {
  let observer = null;
  return compose(codeBrowser(deps), {
    kind: "repository",
    pickedRef: "",

    init() {
      try {
        this.startFrom(deps.gateway.decodeSeed(readSeed(this.$el)));
      } catch (err) {
        this.problem = { code: "BAD_RESPONSE", as: "banner", title: "", text: String(err?.message ?? err), action: "", next: "" };
        return;
      }
      this.pickedRef = this.route.ref || this.branch;
      this.watchWidth();
      this.$nextTick(() => {
        for (const node of this.$el.querySelectorAll("[data-ssr]")) node.remove();
        this.enterView();
      });
    },

    destroy() {
      observer?.disconnect();
      this.stop();
    },

    /** The ref select: the same view and path at another ref, as a new page. */
    pickRef(event) {
      const ref = event?.target?.value ?? this.pickedRef;
      if (!ref || ref === (this.route.ref || this.branch)) return;
      deps.navigate(hrefOf({ ...this.route, view: this.view, path: this.view === "files" ? (this.file?.path ?? this.route.path) : "", ref }));
    },

    /** PATH_NOT_FOUND's and REF_NOT_FOUND's next step. */
    problemAction() {
      if (this.problem?.next === "root") this.showRoot();
      else if (this.problem?.next === "ref") this.$root.querySelector(".code-source-bar select")?.focus();
    },

    watchWidth() {
      const el = this.$refs?.layout;
      if (!el || typeof ResizeObserver !== "function") return;
      const rem = parseFloat(getComputedStyle(el.ownerDocument.documentElement).fontSize) || 16;
      observer = new ResizeObserver((entries) => this.measure(entries[0].contentRect.width, rem));
      observer.observe(el);
    },
  });
};
