// The empty state's "start from what was found": discovery lists instruction
// files, tools and sandboxes to confirm; confirming adds them to the page's
// draft, and the person saves from the save bar like any other change.
//
// Nested in architecturePage, so `this.draft` and `this.repositoryId` are the page's.

import { describeError } from "../../../../shared/presentation/errors.js";
import { addCandidates, candidatesOf } from "../../domain/descriptive.js";

/** @param { gateway: ReturnType<typeof import("../../infrastructure/architecture-gateway.js").architectureGateway> } deps */
export const architectureSetup = ({ gateway }) => () => ({
  loading: false,
  failure: "",
  /** The candidates to confirm, once discovery answered; null before. */
  candidates: null,
  agents: [],
  /** How many the person added, once they confirmed. */
  added: -1,

  get listing() {
    return this.candidates !== null && this.added < 0;
  },
  get nothingFound() {
    return this.listing && this.candidates.length === 0;
  },
  get picked() {
    return (this.candidates ?? []).filter((c) => c.on).length;
  },
  get confirmText() {
    return `Add ${this.picked} to the architecture`;
  },
  get agentsText() {
    return this.agents.length === 0 ? "" : `Agents the harness knows: ${this.agents.join(", ")}.`;
  },
  get addedText() {
    return this.added === 1 ? "1 entry added. Review it in the tabs, then save." : `${this.added} entries added. Review them in the tabs, then save.`;
  },

  async discover() {
    this.loading = true;
    this.failure = "";
    try {
      const found = await gateway.discover(this.repositoryId);
      this.candidates = candidatesOf(found);
      this.agents = found.agents;
    } catch (err) {
      this.failure = describeError(err).message;
    } finally {
      this.loading = false;
    }
  },

  confirm() {
    this.added = addCandidates(this.draft, this.candidates);
  },
});
