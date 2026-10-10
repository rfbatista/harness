// A diff on the history pages: the open commit against its first parent, or
// a session's uncommitted changes (scope working), under the commit's facts.
// It is the Changes view's diff without a scope switch, the source bar or
// the feed: collapse, Load diff, unified or split, ] and [, Go to line.
//
//   <section x-data="codeDiff" data-seed="code-diff-seed" x-on:keydown="onKey">

import { readSeed } from "../../../../shared/presentation/seed.js";
import { repositoryHref } from "../../domain/routes.js";
import { codeBrowser } from "./codeBrowser.js";
import { compose } from "./compose.js";

/** @param {import("./codeBrowser.js").BrowserDeps} deps */
export const codeDiff = (deps) => () => {
  let observer = null;
  return compose(codeBrowser(deps), {
    kind: "history",
    commitHash: "",
    repositoryId: "",
    workspaceId: "",

    init() {
      try {
        const raw = readSeed(this.$el);
        const seed = deps.gateway.decodeSeed(raw);
        this.commitHash = typeof raw?.commit === "string" ? raw.commit : "";
        this.repositoryId = seed.source.repositoryId;
        this.workspaceId = seed.source.workspaceId;
        this.startFrom(seed);
        this.route = { ...this.route, view: "changes" };
      } catch (err) {
        this.problem = { code: "BAD_RESPONSE", as: "banner", title: "", text: String(err?.message ?? err), action: "", next: "" };
        return;
      }
      this.watchWidth();
      this.$nextTick(() => {
        for (const node of this.$el.querySelectorAll("[data-ssr]")) node.remove();
      });
    },

    destroy() {
      observer?.disconnect();
      this.stop();
    },

    get view() {
      return "changes";
    },
    urlOwned() {
      return false;
    },
    historyQuery() {
      return this.commitHash ? { repositoryId: this.repositoryId, commit: this.commitHash } : { workspaceId: this.workspaceId, scope: "working" };
    },
    viewFileHref(path) {
      return this.commitHash ? repositoryHref(this.projectId, this.repositoryId, this.commitHash, path) : "";
    },
    enterView() {},

    watchWidth() {
      const el = this.$refs?.layout ?? this.$el;
      if (typeof ResizeObserver !== "function") return;
      const rem = parseFloat(getComputedStyle(el.ownerDocument.documentElement).fontSize) || 16;
      observer = new ResizeObserver((entries) => this.measure(entries[0].contentRect.width, rem));
      observer.observe(el);
    },
  });
};
