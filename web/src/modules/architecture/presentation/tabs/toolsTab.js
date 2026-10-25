// The Tools tab of the Architecture page: a component nested in
// architecturePage, so `this.draft`, `this.errorFor(path)` and `this.isInvalid(path)`
// reach the page's draft and its field errors.

import { TOOL_KINDS, blankTool } from "../../domain/descriptive.js";
import { listEditing } from "./listEditing.js";

/** @param { gateway: ReturnType<typeof import("../../infrastructure/architecture-gateway.js").architectureGateway> } deps */
export const toolsTab = ({ gateway }) => () => ({
  kinds: TOOL_KINDS,
  ...listEditing(["tools"], blankTool),
});
