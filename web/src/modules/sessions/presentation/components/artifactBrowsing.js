// What a list of artifacts with one previewed beside it shares, on the Design
// tab and on the project's design assets: the cards, the selected one's
// preview and bar, J/K, and the sandboxed frame. A component composes it with
// its own state and actions:
//
//   compose(artifactBrowsing(clock), { …own state, getters, actions })
//
// compose copies property descriptors, so getters stay getters (a spread
// would freeze them into values); later parts override earlier ones.

import { toCardView, toPreviewView } from "../artifactView.js";

/** @param {...object} parts */
export function compose(...parts) {
  const out = {};
  for (const part of parts) Object.defineProperties(out, Object.getOwnPropertyDescriptors(part));
  return out;
}

/** @param {import("../../../../shared/infrastructure/clock.js").Clock} clock */
export function artifactBrowsing(clock) {
  return {
    /** @type {import("../../domain/artifact.js").Artifact[]} */
    artifacts: [],
    selectedId: "",
    /** The person picked an artifact by hand. */
    chosen: false,
    freshIds: [],
    now: clock.now(),

    get cards() {
      const fresh = new Set(this.freshIds);
      return this.artifacts.map((a) => this.cardView(a, toCardView(a, { selectedId: this.selectedId, now: this.now, fresh: fresh.has(a.id) })));
    },
    /** A component adds to a card here (the library names its task). */
    cardView(_artifact, card) {
      return card;
    },
    get current() {
      const a = this.artifacts.find((x) => x.id === this.selectedId);
      return a ? toPreviewView(a, this.now) : null;
    },
    get hasCurrent() {
      return this.current !== null;
    },
    /** The preview as a one-item list keyed on id@revision, so a new revision remounts the element. */
    get frames() {
      return this.current ? [this.current] : [];
    },
    get currentKind() {
      return this.current?.kind ?? "";
    },
    get hasOpenHref() {
      return (this.current?.openHref ?? "") !== "";
    },
    get currentOpenHref() {
      return this.current?.openHref ?? "";
    },
    get canMoveCurrent() {
      return this.current?.canMove ?? false;
    },
    get currentScopeWord() {
      return this.current?.scopeWord ?? "";
    },
    get currentMoveLabel() {
      return this.current?.moveLabel ?? "";
    },
    get currentMoveAriaLabel() {
      return this.current?.moveAriaLabel ?? "";
    },
    /** The bar shows a stream's status only for a component that follows one. */
    followsFeed: false,

    /** Keeps the selection on a listed artifact: the first when it is gone. */
    keepSelection() {
      if (!this.artifacts.some((a) => a.id === this.selectedId)) this.selectedId = this.artifacts[0]?.id ?? "";
    },

    /**
     * Fills the static <iframe sandbox="allow-scripts"> inside `host` with the
     * revision's src and title. The Alpine CSP build refuses directives on an
     * iframe, so the markup keeps the frame (and its sandbox) literal and the
     * wrapper's x-init hands it here. The sandbox attribute is never touched.
     * @param {HTMLElement} host @param {{ src: string, frameTitle: string }} frame
     */
    loadFrame(host, frame) {
      const iframe = host.querySelector("iframe");
      if (!iframe) return;
      iframe.title = frame.frameTitle;
      iframe.setAttribute("src", frame.src);
    },

    select(id) {
      this.selectedId = id;
      this.chosen = true;
    },
    next() {
      this.step(1);
    },
    previous() {
      this.step(-1);
    },
    step(delta) {
      if (this.artifacts.length === 0) return;
      const at = this.artifacts.findIndex((a) => a.id === this.selectedId);
      const to = at === -1 ? 0 : Math.max(0, Math.min(this.artifacts.length - 1, at + delta));
      this.select(this.artifacts[to].id);
    },
  };
}
