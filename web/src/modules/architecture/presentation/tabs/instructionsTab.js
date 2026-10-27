// The Instructions & rules tab of the Architecture page: a component nested in
// architecturePage, so `this.draft`, `this.errorFor(path)` and `this.isInvalid(path)`
// reach the page's draft and its field errors. Rows are edited in a drawer
// (listEditing); a path that discovery did not find is tagged missing.

import { INSTRUCTION_KINDS, LOADED, blankInstruction, isMissingPath, kindLabel, loadedLabel } from "../../domain/descriptive.js";
import { agentPicker } from "./agentPicker.js";
import { listEditing, noDrawer } from "./listEditing.js";

/** @param { gateway: ReturnType<typeof import("../../infrastructure/architecture-gateway.js").architectureGateway> } deps */
export const instructionsTab = ({ gateway }) => () => {
  const picker = agentPicker({ gateway });
  return {
    ...picker,
    kinds: INSTRUCTION_KINDS,
    loadedModes: LOADED,
    kindLabel: (kind) => kindLabel("instruction", kind),
    loadedLabel,
    /** The drawer: which row it is on, and its working copy. */
    editing: noDrawer(),
    /** The paths discovery found in the checkout, once asked; null until then, and for good if it failed. */
    found: null,
    asked: false,
    ...listEditing(["instructions"], blankInstruction, "editing", (row) => row.path ?? ""),

    async init() {
      this.$watch("errors", () => this.openForErrors());
      this.$watch("tab", (tab) => {
        if (tab === "instructions") this.lookUp();
      });
      if (this.isTab("instructions")) this.lookUp();
      await picker.init.call(this);
    },

    /** Reads which instruction files discovery found, once, when the tab is first shown (the page's one discovery, shared with the agent pickers). A failure leaves every row unmarked. */
    async lookUp() {
      if (this.asked) return;
      this.asked = true;
      try {
        const found = await gateway.discovered(this.repositoryId);
        this.found = found.instructions.map((i) => i.path);
      } catch {
        this.found = null;
      }
    },
    isMissing(row) {
      return isMissingPath(row.path, this.found === null ? null : new Set(this.found));
    },
  };
};
