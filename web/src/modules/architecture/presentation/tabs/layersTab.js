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
  otherLayers,
  overlapText,
  overlapsOf,
  layerPattern,
  patternProblem,
  folderChoices,
  isFolderPicked,
  withFolderPicked,
  accessSentence,
  presetAdditions,
} from "../../domain/layers.js";

const PREVIEW_DELAY_MS = 250;
const SAMPLE_SHOWN = 20;
const OVERLAPS_SHOWN = 5;
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
  /** The position of the layer whose editor is open. */
  selected: 0,
  /** The folder picker: open or not, what loading the folders came to, and the folders. */
  pickerOpen: false,
  foldersState: "idle",
  foldersError: "",
  folderTree: null,

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

  /** The position of the open layer, kept inside the list. */
  get selectedIndex() {
    return Math.max(0, Math.min(this.selected, this.layers.length - 1));
  },
  /** The open layer's entry, or null when there are no layers. */
  get current() {
    const layer = this.layers[this.selectedIndex];
    return layer ? this.rowFor(layer, this.selectedIndex) : null;
  },
  /** One entry per layer, with what the list shows for it. */
  get rows() {
    return this.layers.map((layer, i) => this.rowFor(layer, i));
  },
  /** The folders to pick from: the checkout's directories, minus .git and the repository's ignored paths. */
  get folders() {
    return folderChoices(this.folderTree, this.ignoredPaths, this.rootDir).map((path) => ({
      path,
      checked: isFolderPicked(this.layers[this.selectedIndex], path),
    }));
  },
  get showPicker() {
    return this.pickerOpen && !!this.current && this.current.kind === "folder";
  },
  get foldersLoading() {
    return this.foldersState === "loading";
  },
  get foldersFailed() {
    return this.foldersState === "failed";
  },
  get foldersNone() {
    return this.foldersState === "ready" && this.folders.length === 0;
  },
  get unlayeredSentence() {
    return `Unmatched paths use the repository default: ${accessSentence(this.draft?.unlayered)}.`;
  },
  get overlapTone() {
    return this.overlaps.length === 0 ? undefined : "attention";
  },
  get overlapRule() {
    return "Rule: when layers overlap, the narrower pattern wins (a regex over a folder, a deeper folder over a shallower one, the first regex over a later one).";
  },
  get overlapsShown() {
    return this.overlaps.slice(0, OVERLAPS_SHOWN);
  },
  get overlapsMore() {
    return Math.max(0, this.overlaps.length - OVERLAPS_SHOWN);
  },
  get overlapsMoreText() {
    return `and ${this.overlapsMore} more`;
  },
  /** The layers with something wrong, each with what is wrong. */
  get problems() {
    return this.rows
      .map((r) => ({ title: r.title, messages: [r.nameError, r.patternError, r.contextError].filter((m) => m !== "") }))
      .filter((p) => p.messages.length > 0);
  },
  get hasProblems() {
    return this.problems.length > 0;
  },
  /** The banner's words: the first layer that cannot be saved and why, then how many more. */
  get problemText() {
    const [first, ...rest] = this.problems;
    if (!first) return "";
    const more = rest.length === 0 ? "" : ` ${rest.length} more ${rest.length === 1 ? "layer has" : "layers have"} problems.`;
    return `“${first.title}” cannot be saved. ${first.messages.join(" ")}${more}`;
  },

  /** What the list and the editor show for one layer. */
  rowFor(layer, i) {
    const tab = this;
    const kind = matchKind(layer);
    const matchPath = `/layers/${i}/match/${kind}`;
    const answer = tab.previewFor(layer, i);
    const title = layer.name || "Unnamed layer";
    const shown = answer ? answer.sample.slice(0, SAMPLE_SHOWN) : [];
    const more = answer ? answer.fileCount - shown.length : 0;
    const overlaps = overlapsOf(tab.preview?.overlaps, layer.name);
    const nameError = tab.nameError(i);
    const patternError =
      patternProblem(kind, matchText(layer)) ||
      answer?.error?.message ||
      tab.errorFor(matchPath) ||
      tab.errorFor(`/layers/${i}/match`);
    const contextError = tab.errorFor(`/layers/${i}/bounded_context`);
    const overlapPaths = new Map(overlaps.map((o) => [o.path, otherLayers(o, layer.name).join(", ")]));
    return {
      i,
      layer,
      kind,
      title,
      first: i === 0,
      last: i === tab.layers.length - 1,
      current: i === tab.selectedIndex,
      listPattern: layerPattern(layer),
      hasOverlap: overlaps.length > 0,
      hasError: nameError !== "" || patternError !== "" || contextError !== "",
      ids: {
        name: `architecture-layer-${i}-name`,
        matchBy: `architecture-layer-${i}-match-by`,
        pattern: `architecture-layer-${i}-pattern`,
        context: `architecture-layer-${i}-context`,
        purpose: `architecture-layer-${i}-purpose`,
        tree: `architecture-layer-${i}-tree`,
        treeLabel: `architecture-layer-${i}-tree-label`,
      },
      moveUpLabel: `Move ${title} up`,
      moveDownLabel: `Move ${title} down`,
      removeLabel: `Delete layer ${title}`,
      namePath: `/layers/${i}/name`,
      nameError,
      patternPath: matchPath,
      patternError,
      patternLabel: kind === "regex" ? "Regular expression (matched against the repository-relative path)" : "Folder (relative to the repository root)",
      patternHint: kind === "regex" ? "RE2 syntax, not anchored." : "Includes everything below it.",
      placeholder: kind === "regex" ? "^internal/(domain|ports)/" : "internal/domain",
      contextPath: `/layers/${i}/bounded_context`,
      contextError,
      countText: !answer ? "" : answer.error ? "invalid" : fileCountText(answer.fileCount),
      cannotPreview: patternError !== "",
      shownText: shown.length === 0 ? "" : `${shown.length} of ${answer.fileCount} shown`,
      overlapText: `${overlaps.length} overlap another layer`,
      unmatchedText: `${tab.preview?.unlayered.fileCount ?? 0} unmatched`,
      emptyText: patternError !== "" ? "Fix the pattern to see matches." : answer ? "No paths match." : "Looking for matches…",
      sample: shown.map((path) => ({ path, also: overlapPaths.has(path) ? `also ${overlapPaths.get(path)}` : "" })),
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

  select(i) {
    this.selected = i;
  },

  /** Opens or closes the folder tree; opening it reads the checkout's folders unless they are already here. */
  togglePicker() {
    this.pickerOpen = !this.pickerOpen;
    if (this.pickerOpen && this.foldersState !== "ready") return this.loadFolders();
  },

  async loadFolders() {
    this.foldersState = "loading";
    this.foldersError = "";
    try {
      this.folderTree = await gateway.listTree(this.rootDir);
      this.foldersState = "ready";
    } catch (err) {
      this.foldersState = "failed";
      this.foldersError = `The folders could not be listed: ${describeError(err).message}`;
    }
  },

  /** A folder was checked or unchecked in the tree: the layer's folder follows. */
  pickFolder(path, event) {
    const layer = this.layers[this.selectedIndex];
    layer.match = withFolderPicked(layer, path, event.target.checked).match;
  },

  /** Adds a layer for a folder, with the default access, and opens its editor. */
  add(kind = "folder") {
    if (!this.draft) return;
    this.draft.layers.push(newLayer(this.draft.layers, kind));
    this.selected = this.draft.layers.length - 1;
  },

  /** Adds the hexagonal layers the repository lacks, each with the default access. */
  addPreset() {
    if (!this.draft) return;
    const added = presetAdditions(this.draft.layers);
    if (added.length === 0) return;
    this.selected = this.draft.layers.length;
    this.draft.layers.push(...added);
  },

  /** Deletes the open layer; the one now at its place (or the last) opens. */
  removeCurrent() {
    this.remove(this.selectedIndex);
  },

  remove(i) {
    this.draft.layers.splice(i, 1);
    this.selected = Math.max(0, Math.min(this.selected, this.draft.layers.length - 1));
  },

  /** Moves a layer by a place; the editor stays on it. */
  move(i, delta) {
    const layers = moved(this.draft.layers, i, delta);
    if (layers === this.draft.layers) return;
    this.draft.layers = layers;
    if (this.selected === i) this.selected = i + delta;
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
      this.overlapNames = [...new Set(answer.overlaps.flatMap((o) => o.layers))]; // the diagram marks them
      this.unmatchedCount = answer.unlayered.fileCount;
      this.previewFailed = "";
    } catch (err) {
      if (n !== this.seq) return;
      this.previewFailed = `The preview is not available: ${describeError(err).message}`;
    }
  },
});
