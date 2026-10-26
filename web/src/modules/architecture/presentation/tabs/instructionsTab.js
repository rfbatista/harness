// The Instructions & rules tab of the Architecture page: a component nested in
// architecturePage, so `this.draft`, `this.errorFor(path)` and `this.isInvalid(path)`
// reach the page's draft and its field errors.

import { INSTRUCTION_KINDS, LOADED, blankInstruction } from "../../domain/descriptive.js";
import { agentPicker } from "./agentPicker.js";
import { listEditing } from "./listEditing.js";

/** @param { gateway: ReturnType<typeof import("../../infrastructure/architecture-gateway.js").architectureGateway> } deps */
export const instructionsTab = ({ gateway }) => () => ({
  ...agentPicker({ gateway }),
  kinds: INSTRUCTION_KINDS,
  loadedModes: LOADED,
  ...listEditing(["instructions"], blankInstruction),
});
