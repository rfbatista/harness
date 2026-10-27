// The Sandboxes tab of the Architecture page: a component nested in
// architecturePage, so `this.draft`, `this.errorFor(path)` and `this.isInvalid(path)`
// reach the page's draft and its field errors. Rows are edited in a drawer (listEditing).

import { blankSandbox, reaches } from "../../domain/descriptive.js";
import { agentPicker } from "./agentPicker.js";
import { listEditing, noDrawer } from "./listEditing.js";

/** @param { gateway: ReturnType<typeof import("../../infrastructure/architecture-gateway.js").architectureGateway> } deps */
export const sandboxesTab = ({ gateway }) => () => {
  const picker = agentPicker({ gateway });
  return {
    ...picker,
    reaches,
    /** The drawer: which row it is on, and its working copy. */
    editing: noDrawer(),
    ...listEditing(["sandboxes"], blankSandbox, "editing", (row) => row.name ?? ""),

    async init() {
      this.$watch("errors", () => this.openForErrors());
      await picker.init.call(this);
    },
  };
};
