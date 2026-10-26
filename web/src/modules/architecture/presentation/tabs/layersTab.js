// The Layers tab of the Architecture page: a component nested in
// architecturePage, so `this.draft`, `this.repositoryId` and `this.errorFor(path)`
// reach the page's draft and its field errors.
//
// It edits `draft.layers`. A debounced preview asks the server what the draft
// would match; the preview never saves, and an answer that arrives after a
// newer request was sent is dropped.

import { describeError } from "../../../../shared/presentation/errors.js";
import {
  fileCountText,
  isDuplicateName,
  matchKind,
  matchText,
  moved,
  newLayer,
  overlapText,
  patternProblem,
} from "../../domain/layers.js";

const PREVIEW_DELAY_MS = 250;
const SAMPLE_SHOWN = 5;
const UNMATCHED_SHOWN = 10;
const LAYER_NAME = /^[a-z0-9-]+$/;

/** @param { gateway: ReturnType<typeof import("../../infrastructure/architecture-gateway.js").architectureGateway> } deps */
export const layersTab = ({ gateway }) => () => ({
  /** The newest preview answer, with the match each layer had when it was asked. */
  preview: null,
  previewFailed: "",
  seq: 0,
  timer: 0,
  /** The project's bounded context names, once the server answered. */
  contexts: [],

  get layers() {
    return this.draft?.layers ?? [];
  },
  get isEmpty() {
    return !!this.draft && this.layers.length === 0;
  },
  /** Changes when something the preview depends on changes: each layer's name and match. */
  get previewKey() {
    return this.draft ? JSON.stringify(this.layers.map((l) => [l.name, l.match])) : "";
  },
  get hasPreview() {
    return this.preview !== null;
  },
  get unmatchedText() {
    return fileCountText(this.preview?.unlayered.fileCount ?? 0);
  },
  get unmatchedSample() {
    return (this.preview?.unlayered.sample ?? []).slice(0, UNMATCHED_SHOWN);
  },
  get overlaps() {
    return (this.preview?.overlaps ?? []).map((o) => ({ path: o.path, text: overlapText(o) }));
  },
  get overlapTitle() {
    const n = this.overlaps.length;
    return n === 0 ? "No overlaps" : `${n} ${n === 1 ? "path matches" : "paths match"} more than one layer`;
  },

  /** One entry per layer, with what the editor shows for it. */
  get rows() {
    const tab = this;
    return this.layers.map((layer, i) => {
      const kind = matchKind(layer);
      const matchPath = `/layers/${i}/match/${kind}`;
      const answer = tab.previewFor(layer, i);
      const title = layer.name || "Unnamed layer";
      const shown = answer ? answer.sample.slice(0, SAMPLE_SHOWN) : [];
      const more = answer ? answer.fileCount - shown.length : 0;
      return {
        i,
        layer,
        kind,
        title,
        first: i === 0,
        last: i === tab.layers.length - 1,
        ids: {
          name: `architecture-layer-${i}-name`,
          matchBy: `architecture-layer-${i}-match-by`,
          pattern: `architecture-layer-${i}-pattern`,
          context: `architecture-layer-${i}-context`,
          purpose: `architecture-layer-${i}-purpose`,
        },
        moveUpLabel: `Move ${title} up`,
        moveDownLabel: `Move ${title} down`,
        removeLabel: `Remove ${title}`,
        namePath: `/layers/${i}/name`,
        nameError: tab.nameError(i),
        patternPath: matchPath,
        patternError:
          patternProblem(kind, matchText(layer)) ||
          answer?.error?.message ||
          tab.errorFor(matchPath) ||
          tab.errorFor(`/layers/${i}/match`),
        patternLabel: kind === "regex" ? "Regular expression (matched against the repository-relative path)" : "Folder (relative to the repository root)",
        patternHint: kind === "regex" ? "RE2 syntax, not anchored." : "Includes everything below it.",
        placeholder: kind === "regex" ? "^internal/(domain|ports)/" : "internal/domain",
        contextPath: `/layers/${i}/bounded_context`,
        contextError: tab.errorFor(`/layers/${i}/bounded_context`),
        countText: !answer ? "" : answer.error ? "invalid" : fileCountText(answer.fileCount),
        sample: shown,
        more,
        moreText: `and ${more} more`,
        get pattern() {
          return matchText(layer);
        },
        set pattern(text) {
          layer.match = { [kind]: text };
        },
        contextOptions: tab.contextOptions(layer),
        get purpose() {
          return layer.purpose ?? "";
        },
        set purpose(text) {
          if (text === "") delete layer.purpose;
          else layer.purpose = text;
        },
      };
    });
  },

  async init() {
    this.$watch("previewKey", () => this.schedulePreview());
    this.schedulePreview();
    try {
      this.contexts = await gateway.boundedContexts(this.projectId);
    } catch {
      this.contexts = []; // the select still offers none and the name the layer holds
    }
  },

  /** The choices for a layer's bounded context: none, the project's contexts, then a saved name that is no longer among them. */
  contextOptions(layer) {
    const saved = layer.bounded_context ?? "";
    const stray = saved !== "" && !this.contexts.includes(saved);
    return [
      { value: "", label: "None", selected: saved === "" },
      ...this.contexts.map((name) => ({ value: name, label: name, selected: name === saved })),
      ...(stray ? [{ value: saved, label: `${saved} (not in the list)`, selected: true }] : []),
    ];
  },

  setContext(i, event) {
    const name = event.target.value;
    if (name === "") delete this.draft.layers[i].bounded_context;
    else this.draft.layers[i].bounded_context = name;
  },

  destroy() {
    clearTimeout(this.timer);
  },

  /** The preview's entry for a layer, as long as the layer still matches as it did when asked. */
  previewFor(layer, i) {
    const p = this.preview;
    if (!p || p.keys[i] !== JSON.stringify(layer.match)) return null;
    return p.layers[i] ?? null;
  },

  nameError(i) {
    const layer = this.layers[i];
    const saved = this.errorFor(`/layers/${i}/name`);
    if (saved) return saved;
    if (layer.name === "") return "";
    if (isDuplicateName(this.layers, i)) return `Another layer is already called ${layer.name}.`;
    return LAYER_NAME.test(layer.name) ? "" : "Use lower-case letters, digits and -.";
  },

  add(kind) {
    if (!this.draft) return;
    this.draft.layers.push(newLayer(this.draft.layers, kind));
  },

  remove(i) {
    this.draft.layers.splice(i, 1);
  },

  move(i, delta) {
    this.draft.layers = moved(this.draft.layers, i, delta);
  },

  /** Switches how a layer matches, keeping the text typed so far. */
  setKind(i, kind) {
    const layer = this.draft.layers[i];
    layer.match = { [kind]: matchText(layer) };
  },

  schedulePreview() {
    clearTimeout(this.timer);
    if (!this.draft) return;
    this.timer = setTimeout(() => this.runPreview(), PREVIEW_DELAY_MS);
  },

  async runPreview() {
    const n = ++this.seq;
    const layers = JSON.parse(JSON.stringify(this.draft.layers));
    const unlayered = JSON.parse(JSON.stringify(this.draft.unlayered ?? {}));
    try {
      const answer = await gateway.previewLayers(this.repositoryId, layers, unlayered);
      if (n !== this.seq) return; // a newer request was sent: this answer is out of date
      this.preview = { ...answer, keys: layers.map((l) => JSON.stringify(l.match)) };
      this.overlapCount = answer.overlaps.length; // the page's overview counts them
      this.previewFailed = "";
    } catch (err) {
      if (n !== this.seq) return;
      this.previewFailed = `The preview is not available: ${describeError(err).message}`;
    }
  },
});
