// The Orchestration tab of the Architecture page: model routing and hand-offs.
// A component nested in architecturePage, so `this.draft`, `this.errorFor(path)`
// and `this.isInvalid(path)` reach the page's draft and its field errors. Each
// list has its own drawer (listEditing), its methods named with Route or Handoff.

import { blankHandoff, blankRoute, handoffChains } from "../../domain/descriptive.js";
import { agentPicker } from "./agentPicker.js";
import { listEditing, noDrawer, suffixed } from "./listEditing.js";

const routing = listEditing(["orchestration", "routing"], blankRoute, "editingRoute", (row) => row.agent ?? "");
const handoffs = listEditing(["orchestration", "handoffs"], blankHandoff, "editingHandoff", (row) => `${row.from ?? ""}>${row.to ?? ""}`);

const asRoute = suffixed(routing, "Route");
const asHandoff = suffixed(handoffs, "Handoff");

/** @param { gateway: ReturnType<typeof import("../../infrastructure/architecture-gateway.js").architectureGateway> } deps */
export const orchestrationTab = ({ gateway }) => () => {
  const picker = agentPicker({ gateway });
  return {
    ...picker,
    /** The drawers: which row each is on, and its working copy. */
    editingRoute: noDrawer(),
    editingHandoff: noDrawer(),
    ...asRoute,
    ...asHandoff,
    editLabel: routing.editLabel,
    removeLabel: routing.removeLabel,

    async init() {
      this.$watch("errors", () => {
        this.openForErrorsRoute();
        this.openForErrorsHandoff();
      });
      await picker.init.call(this);
    },

    // One drawer at a time: opening one on a list closes the other.
    addRoute() {
      this.cancelHandoff();
      asRoute.addRoute.call(this);
    },
    editRoute(index) {
      this.cancelHandoff();
      asRoute.editRoute.call(this, index);
    },
    addHandoff() {
      this.cancelRoute();
      asHandoff.addHandoff.call(this);
    },
    editHandoff(index) {
      this.cancelRoute();
      asHandoff.editHandoff.call(this, index);
    },
    /** Either drawer is open, so the tab makes room for it. */
    anyDrawerOpen() {
      return this.drawerOpenRoute() || this.drawerOpenHandoff();
    },
    closeDrawers() {
      this.closeDrawerRoute();
      this.closeDrawerHandoff();
    },
    /** The hand-offs as chains of agents, each agent with whether an arrow follows it. */
    chains() {
      return handoffChains(this.rowsHandoff()).map((chain) => chain.map((name, i) => ({ name, arrow: i < chain.length - 1 })));
    },
    hasChains() {
      return this.chains().length > 0;
    },
  };
};
