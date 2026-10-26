// The Orchestration tab of the Architecture page: model routing and hand-offs.
// A component nested in architecturePage, so `this.draft`, `this.errorFor(path)`
// and `this.isInvalid(path)` reach the page's draft and its field errors.

import { blankHandoff, blankRoute } from "../../domain/descriptive.js";
import { agentPicker } from "./agentPicker.js";
import { listEditing } from "./listEditing.js";

const routing = listEditing(["orchestration", "routing"], blankRoute, "editingRoute");
const handoffs = listEditing(["orchestration", "handoffs"], blankHandoff, "editingHandoff");

/** @param { gateway: ReturnType<typeof import("../../infrastructure/architecture-gateway.js").architectureGateway> } deps */
export const orchestrationTab = ({ gateway }) => () => ({
  ...agentPicker({ gateway }),
  /** The positions of the route and the hand-off whose editors are open. */
  editingRoute: -1,
  editingHandoff: -1,
  routes: routing.rows,
  isRouteOpen: routing.isOpen,
  editRoute: routing.edit,
  closeRoute: routing.closeEditor,
  isHandoffOpen: handoffs.isOpen,
  editHandoff: handoffs.edit,
  closeHandoff: handoffs.closeEditor,
  editLabel: routing.editLabel,
  addRoute: routing.add,
  removeRoute: routing.remove,
  routeErr: routing.err,
  routeBad: routing.bad,
  routePtr: routing.ptr,
  handoffRows: handoffs.rows,
  addHandoff: handoffs.add,
  removeHandoff: handoffs.remove,
  handoffErr: handoffs.err,
  handoffBad: handoffs.bad,
  handoffPtr: handoffs.ptr,
  removeLabel: routing.removeLabel,
});
