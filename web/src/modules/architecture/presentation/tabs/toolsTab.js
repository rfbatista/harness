// The Tools tab of the Architecture page: a component nested in
// architecturePage, so `this.draft`, `this.errorFor(path)` and `this.isInvalid(path)`
// reach the page's draft and its field errors.

import { TOOL_KINDS, blankTool, kindLabel, noUsage, usageText } from "../../domain/descriptive.js";
import { agentPicker } from "./agentPicker.js";
import { listEditing } from "./listEditing.js";

/** @param { gateway: ReturnType<typeof import("../../infrastructure/architecture-gateway.js").architectureGateway> } deps */
export const toolsTab = ({ gateway }) => () => ({
  ...agentPicker({ gateway }),
  kinds: TOOL_KINDS,
  kindLabel: (kind) => kindLabel("tool", kind),
  noUsage,
  usageText,
  /** The position of the row whose editor is open. */
  editing: -1,
  ...listEditing(["tools"], blankTool),
});
