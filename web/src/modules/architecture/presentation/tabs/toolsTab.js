// The Tools tab of the Architecture page: a component nested in
// architecturePage, so `this.draft`, `this.errorFor(path)` and `this.isInvalid(path)`
// reach the page's draft and its field errors. Rows are edited in a drawer (listEditing).

import { TOOL_KINDS, blankTool, kindLabel, noUsage, usageText } from "../../domain/descriptive.js";
import { agentPicker } from "./agentPicker.js";
import { listEditing, noDrawer } from "./listEditing.js";

/** @param { gateway: ReturnType<typeof import("../../infrastructure/architecture-gateway.js").architectureGateway> } deps */
export const toolsTab = ({ gateway }) => () => {
  const picker = agentPicker({ gateway });
  return {
    ...picker,
    kinds: TOOL_KINDS,
    kindLabel: (kind) => kindLabel("tool", kind),
    noUsage,
    usageText,
    /** The drawer: which row it is on, and its working copy. */
    editing: noDrawer(),
    ...listEditing(["tools"], blankTool, "editing", (row) => row.name ?? ""),

    async init() {
      this.$watch("errors", () => this.openForErrors());
      await picker.init.call(this);
    },
  };
};
