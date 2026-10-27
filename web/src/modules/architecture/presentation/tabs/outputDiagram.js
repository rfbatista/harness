// The diagram view of the Output tab: the diagram model built from the page's
// draft (so an unsaved change shows, marked as a draft), drawn as SVG, with
// agents that can be focused. A component nested in architectureOutputTab; it
// exists only while the diagram view is open.

import { describeError } from "../../../../shared/presentation/errors.js";
import { PLAIN, buildDiagram } from "../../domain/diagram.js";
import { diagramToMermaid } from "../../domain/mermaid.js";
import { diagramToSvg } from "../../domain/svg.js";

/** The agent names a record mentions on its own (the catalog may not have loaded). */
function mentioned(record) {
  const names = new Set();
  const scope = (xs) => (xs ?? []).forEach((n) => n !== "all" && names.add(n));
  const access = (a) => Object.keys(a?.agents ?? {}).forEach((n) => names.add(n));
  (record.layers ?? []).forEach((l) => access(l.access));
  access(record.unlayered);
  (record.rules ?? []).forEach((r) => scope(r.agents));
  (record.tools ?? []).forEach((t) => scope(t.used_by));
  (record.sandboxes ?? []).forEach((s) => scope(s.used_by));
  (record.instructions ?? []).forEach((i) => scope(i.applies_to));
  (record.orchestration?.routing ?? []).forEach((r) => r.agent && names.add(r.agent));
  (record.orchestration?.handoffs ?? []).forEach((h) => (names.add(h.from), names.add(h.to)));
  return [...names];
}

const list = (xs) => (xs.length === 0 ? "nothing" : xs.join(", "));

/** @param { gateway: ReturnType<typeof import("../../infrastructure/architecture-gateway.js").architectureGateway> } deps */
export const outputDiagram = ({ gateway, clipboard }) => () => ({
  /** The agent catalog's names; empty until discovery answers. */
  catalog: [],
  focus: "",
  /** What Copy Mermaid or Download last did, for a status line. */
  notice: "",
  /** The Show options: what the drawing includes besides the layers and the change edges. */
  showGuards: true,
  showSandboxes: true,
  showHandoffs: false,

  async init() {
    try {
      this.catalog = await gateway.agents(this.repositoryId);
    } catch {
      this.catalog = []; // the record's own agent names still draw
    }
  },

  get model() {
    if (!this.draft) return null;
    const agents = [...new Set([...this.catalog, ...mentioned(this.draft)])].filter((n) => n !== PLAIN);
    const focus = agents.includes(this.focus) || this.focus === PLAIN ? this.focus : "";
    return buildDiagram(this.draft, {
      agents,
      focus,
      draft: this.dirty,
      handoffs: this.showHandoffs,
      problems: { overlaps: this.overlapNames, unmatched: this.unmatchedCount },
      files: this.fileCounts,
    });
  },
  /** The model as drawn: without the guards or the sandbox regions the person turned off. */
  get shown() {
    const m = this.model;
    if (!m) return null;
    return { ...m, guards: this.showGuards ? m.guards : [], regions: this.showSandboxes ? m.regions : [] };
  },
  /** Nothing to draw: the record has no layers. */
  get isEmpty() {
    return (this.draft?.layers ?? []).length === 0;
  },
  /** What is wrong with the layers, in words, from the Layers tab's preview. */
  get problemList() {
    const out = [];
    const paths = this.overlapCount;
    const files = this.unmatchedCount;
    if (paths > 0) out.push(`${paths} ${paths === 1 ? "path matches" : "paths match"} more than one layer`);
    if (files > 0) out.push(`${files} ${files === 1 ? "file matches" : "files match"} no layer`);
    return out;
  },
  get hasProblems() {
    return this.problemList.length > 0;
  },
  get problemsTitle() {
    const n = this.problemList.length;
    return `${n} ${n === 1 ? "problem" : "problems"} in the architecture.`;
  },
  get problemsText() {
    return `${this.problemList.join("; ")}. The layers involved are marked on the diagram.`;
  },
  get focusTitle() {
    return this.model?.focus || "All agents";
  },
  get ready() {
    return this.model !== null;
  },
  get isDraft() {
    return !!this.model?.draft;
  },
  /** The picture on the page: painted by the design tokens, so it follows light and dark. */
  get svg() {
    return this.shown ? diagramToSvg(this.shown, { themed: true }) : "";
  },
  /** The picture as a file: literal colours, since a file has no page tokens. */
  get fileSvg() {
    return this.shown ? diagramToSvg(this.shown) : "";
  },
  get agentButtons() {
    return (this.model?.nodes.agents ?? []).map((a) => ({ name: a.name, pressed: a.focused }));
  },
  get hasFocus() {
    return !!this.model?.focus;
  },
  get focusName() {
    return this.model?.focus ?? "";
  },
  get focusChange() {
    return list((this.model?.edges ?? []).filter((e) => e.access === "change").map((e) => e.layer));
  },
  get focusRead() {
    return list((this.model?.edges ?? []).filter((e) => e.access === "read").map((e) => e.layer));
  },
  /** The warning rules a blocking one hides on the focused agent's edges: the picture shows one guard per edge. */
  get focusWarnings() {
    const guards = this.model?.guards ?? [];
    const hidden = guards.filter((g) => g.mode === "warning" && guards.some((b) => b.mode === "blocking" && b.agent === g.agent && b.layer === g.layer));
    return list(hidden.map((g) => `${g.rule} on ${g.layer}`));
  },
  get focusTools() {
    const tools = this.model?.tools ?? [];
    return list(tools.map((t) => `${t.name} (${t.kind}${t.writes ? ", writes" : ""})`));
  },
  get focusHandoffs() {
    return list((this.model?.handoffs ?? []).map((h) => `${h.from} → ${h.to}: ${h.when}`));
  },
  get focusInstructions() {
    const n = this.model?.instructionCount ?? 0;
    return `${n} ${n === 1 ? "file" : "files"}`;
  },
  get guardRules() {
    const seen = new Map();
    for (const g of this.model?.guards ?? []) seen.set(g.rule, g);
    return [...seen.values()].map((g) => `${g.glyph} ${g.mode === "blocking" ? "blocking" : "warning"}: ${g.rule}`);
  },

  /** Draws the SVG into the element it is set on; x-html is not in the CSP build. A redraw keeps the keyboard on the agent it was on. */
  paint() {
    const held = this.$el.contains(document.activeElement) ? document.activeElement.closest?.("g[data-agent]") : null;
    const name = held?.getAttribute("data-agent");
    this.$el.innerHTML = this.svg;
    if (name) [...this.$el.querySelectorAll("g[data-agent]")].find((g) => g.getAttribute("data-agent") === name)?.focus();
  },

  pick(name) {
    this.focus = this.focus === name ? "" : name;
  },
  /** A click on an agent drawn in the picture focuses it too. */
  pickFromPicture(event) {
    const node = event.target.closest?.("g[data-agent]");
    const name = node?.getAttribute("data-agent");
    if (name && node.closest("[data-diagram]")) this.pick(name);
  },
  /** Enter or Space on an agent drawn in the picture focuses it. */
  pickFromKey(event) {
    if (event.key !== "Enter" && event.key !== " ") return;
    const node = event.target.closest?.("g[data-agent]");
    const name = node?.getAttribute("data-agent");
    if (!name || !node.closest("[data-diagram]")) return;
    event.preventDefault();
    this.pick(name);
  },
  clearFocus() {
    this.focus = "";
  },
  /** The empty state's button: the Layers tab, where the layers are defined. */
  openLayers() {
    this.openTab("layers");
  },

  downloadSvg() {
    const blob = new Blob([this.fileSvg], { type: "image/svg+xml" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = this.isDraft ? "architecture-diagram-draft.svg" : "architecture-diagram.svg";
    document.body.appendChild(a);
    a.click();
    a.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
    this.notice = "Downloaded the diagram as SVG.";
  },

  async copyMermaid() {
    try {
      const ok = await clipboard.copyText(diagramToMermaid(this.model));
      this.notice = ok ? "Copied the diagram as Mermaid." : "The browser would not copy. Select the text and copy it by hand.";
    } catch (err) {
      this.notice = describeError(err).message;
    }
  },
});
