// The Hooks tab of the Architecture page: a component nested in
// architecturePage, so `this.draft`, `this.errorFor(path)` and `this.isInvalid(path)`
// reach the page's draft and its field errors.

import {
  LIFECYCLE_POINTS,
  MODES,
  groupRules,
  hasEditRule,
  joinList,
  newCommandRule,
  newEditRule,
  outcome,
  previewText,
  splitList,
} from "../../domain/hooks.js";

/** @param { gateway: ReturnType<typeof import("../../infrastructure/architecture-gateway.js").architectureGateway> } deps */
export const hooksTab = ({ gateway }) => () => ({
  lifecyclePoints: LIFECYCLE_POINTS,
  modes: [
    { value: "blocking", ...MODES.blocking },
    { value: "warning", ...MODES.warning },
  ],

  get rules() {
    return this.draft?.rules ?? [];
  },
  get groups() {
    return groupRules(this.rules).map((g) => ({
      key: g.key,
      title: g.title,
      rules: g.rules.map(({ rule, index }) => ({
        index,
        rule,
        isEdit: rule.kind === "edit_outside_layer",
        atTool: rule.at === "before_tool",
        modeText: MODES[rule.mode]?.text ?? rule.mode,
        agents: joinList(rule.agents),
        paths: rule.paths ?? "",
        tools: joinList(rule.tools),
        timeout: rule.timeout_seconds ?? "",
        preview: previewText(rule, this.draft.layers),
        outcome: outcome(rule),
      })),
    }));
  },
  get noRules() {
    return this.rules.length === 0;
  },
  get cannotAddEdit() {
    return hasEditRule(this.rules);
  },

  pointer(index, field) {
    return `/rules/${index}/${field}`;
  },
  fieldError(index, field) {
    return this.errorFor(`/rules/${index}/${field}`);
  },
  fieldInvalid(index, field) {
    return this.isInvalid(`/rules/${index}/${field}`);
  },

  addEdit() {
    this.draft.rules = [...this.rules, newEditRule(this.rules)];
  },
  addCommand() {
    this.draft.rules = [...this.rules, newCommandRule(this.rules)];
  },
  remove(index) {
    this.draft.rules = this.rules.filter((_, i) => i !== index);
  },
  set(index, field, value) {
    this.draft.rules[index][field] = value;
    if (field === "at" && value !== "before_tool") delete this.draft.rules[index].tools;
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
  setTimeout(index, value) {
    const n = Number(value);
    if (value === "" || !Number.isFinite(n)) delete this.draft.rules[index].timeout_seconds;
    else this.draft.rules[index].timeout_seconds = n;
  },
});
