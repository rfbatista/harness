// The Hooks tab of the Architecture page: a component nested in
// architecturePage, so `this.draft`, `this.errorFor(path)` and `this.isInvalid(path)`
// reach the page's draft and its field errors. It shows the rule list and the
// editor of one rule, as the published hook-rules design does.

import {
  LIFECYCLE_POINTS,
  MODES,
  RULE_KINDS,
  duplicateRule,
  groupRules,
  hasEditRule,
  issueText,
  joinList,
  newCommandRule,
  newEditRule,
  outcome,
  previewBar,
  previewText,
  ruleSummary,
  ruleTitle,
  splitList,
  triggerLabel,
  withKind,
} from "../../domain/hooks.js";
import { agentChoices, toggled } from "../../domain/descriptive.js";
import { agentSlot } from "../../domain/layers.js";
import { agentPicker } from "./agentPicker.js";

const RULE_PATH = /^\/rules\/(\d+)\/([^/]+)/;

/** @param { gateway: ReturnType<typeof import("../../infrastructure/architecture-gateway.js").architectureGateway> } deps */
export const hooksTab = ({ gateway }) => () => ({
  ...agentPicker({ gateway }),
  lifecyclePoints: LIFECYCLE_POINTS,
  modes: ["blocking", "warning"].map((value) => ({ value, ...MODES[value] })),
  /** The position of the rule whose editor is open. */
  selected: 0,

  get rules() {
    return this.draft?.rules ?? [];
  },
  get noRules() {
    return this.rules.length === 0;
  },
  get selectedIndex() {
    return Math.max(0, Math.min(this.selected, this.rules.length - 1));
  },

  /** The list: the non-empty lifecycle groups with their rules. */
  get groups() {
    return groupRules(this.rules).map((g) => ({
      key: g.key,
      title: g.title,
      count: g.rules.length,
      rules: g.rules.map(({ rule, index }) => ({
        index,
        title: ruleTitle(rule),
        summary: ruleSummary(rule),
        modeText: MODES[rule.mode]?.text ?? rule.mode,
        // the Alpine CSP build has no ternary or ??, so the template reads these ready-made
        tone: MODES[rule.mode]?.tone ?? "attention",
        current: index === this.selectedIndex,
        hasError: this.ruleIssues(index).length > 0,
      })),
    }));
  },

  /** The open rule, in a list of one (or none) for x-for. */
  get editing() {
    const rule = this.rules[this.selectedIndex];
    return rule ? [this.rowFor(rule, this.selectedIndex)] : [];
  },

  rowFor(rule, index) {
    const isEdit = rule.kind === "edit_outside_layer";
    const atTool = rule.at === "before_tool";
    return {
      index,
      rule,
      isEdit,
      isCommand: !isEdit,
      atTool,
      showTools: isEdit || atTool,
      title: ruleTitle(rule),
      modeText: MODES[rule.mode]?.text ?? rule.mode,
      tone: MODES[rule.mode]?.tone ?? "attention",
      trigger: triggerLabel(rule),
      id: rule.id ?? "",
      command: rule.command ?? "",
      message: rule.message ?? "",
      paths: rule.paths ?? "",
      tools: joinList(rule.tools),
      timeout: rule.timeout_seconds ?? "",
      kinds: RULE_KINDS.map((k) => ({
        ...k,
        selected: k.value === rule.kind,
        disabled: k.value === "edit_outside_layer" && k.value !== rule.kind && hasEditRule(this.rules),
      })),
      agentChips: agentChoices(this.catalog, Array.isArray(rule.agents) ? rule.agents : [], false).map((c) => ({ ...c, slot: agentSlot(c.value) })),
      preview: previewText(rule, this.draft.layers),
      previewBar: previewBar(rule),
      outcome: outcome(rule),
      removeLabel: `Delete rule ${ruleTitle(rule)}`,
    };
  },

  /** The problems the server found on one rule, in the words a person acts on. */
  ruleIssues(index) {
    const rule = this.rules[index];
    return this.errors
      .map((e) => ({ match: RULE_PATH.exec(e.path), message: e.message }))
      .filter((e) => e.match && Number(e.match[1]) === index)
      .map((e) => issueText(rule, e.match[2], e.message));
  },
  get problems() {
    return this.rules.map((rule, index) => ({ title: ruleTitle(rule), messages: this.ruleIssues(index) })).filter((p) => p.messages.length > 0);
  },
  get hasProblems() {
    return this.problems.length > 0;
  },
  /** The banner's words: the first rule that cannot be saved and why, then how many more. */
  get problemText() {
    const [first, ...rest] = this.problems;
    if (!first) return "";
    const more = rest.length === 0 ? "" : ` ${rest.length} more ${rest.length === 1 ? "rule has" : "rules have"} problems.`;
    return `“${first.title}” cannot be saved. ${first.messages.join(" ")}${more}`;
  },

  pointer(index, field) {
    return `/rules/${index}/${field}`;
  },
  fieldError(index, field) {
    const message = this.errorFor(`/rules/${index}/${field}`);
    return message === "" ? "" : issueText(this.rules[index], field, message);
  },
  fieldInvalid(index, field) {
    return this.isInvalid(`/rules/${index}/${field}`);
  },

  select(index) {
    this.selected = index;
  },
  addEdit() {
    this.draft.rules = [...this.rules, newEditRule(this.rules)];
    this.selected = this.draft.rules.length - 1;
  },
  addCommand() {
    this.draft.rules = [...this.rules, newCommandRule(this.rules)];
    this.selected = this.draft.rules.length - 1;
  },
  duplicate(index) {
    this.draft.rules = [...this.rules, duplicateRule(this.rules, this.rules[index])];
    this.selected = this.draft.rules.length - 1;
  },
  remove(index) {
    this.draft.rules = this.rules.filter((_, i) => i !== index);
    this.selected = Math.max(0, Math.min(this.selected, this.draft.rules.length - 1));
  },
  set(index, field, value) {
    this.draft.rules[index][field] = value;
    if (field === "at" && value !== "before_tool") delete this.draft.rules[index].tools;
  },
  /** The When select: the rule becomes the other kind. */
  setKind(index, kind) {
    this.draft.rules[index] = withKind(this.draft.rules[index], kind);
  },
  setList(index, field, value) {
    const list = splitList(value);
    if (list.length === 0) delete this.draft.rules[index][field];
    else this.draft.rules[index][field] = list;
  },
  setText(index, field, value) {
    if (value === "") delete this.draft.rules[index][field];
    else this.draft.rules[index][field] = value;
  },
  /** An agent chip was ticked or unticked; no agent chosen leaves the field out (every agent). */
  toggleChip(index, name, on) {
    const list = toggled(this.rules[index].agents ?? [], name, on);
    if (list.length === 0) delete this.draft.rules[index].agents;
    else this.draft.rules[index].agents = list;
  },
  setTimeout(index, value) {
    const n = Number(value);
    if (value === "" || !Number.isFinite(n)) delete this.draft.rules[index].timeout_seconds;
    else this.draft.rules[index].timeout_seconds = n;
  },
});
