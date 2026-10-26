// The Sandboxes tab of the Architecture page: a component nested in
// architecturePage, so `this.draft`, `this.errorFor(path)` and `this.isInvalid(path)`
// reach the page's draft and its field errors.

import { blankSandbox, reaches } from "../../domain/descriptive.js";
import { agentPicker } from "./agentPicker.js";
import { listEditing } from "./listEditing.js";

/** @param { gateway: ReturnType<typeof import("../../infrastructure/architecture-gateway.js").architectureGateway> } deps */
export const sandboxesTab = ({ gateway }) => () => ({
  ...agentPicker({ gateway }),
  reaches,
  /** The position of the row whose editor is open. */
  editing: -1,
  ...listEditing(["sandboxes"], blankSandbox),
});
