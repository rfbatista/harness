// The Proposals section of the Instructions & rules tab: the instruction files
// agents proposed (ADR 0092), listed open first, each decided and kept with its
// note. It follows the project feed, so a proposal submitted or decided in
// another tab shows here at once, and tells the page how many are open for the
// tab's badge.
//
//   <section x-data="architectureInstructionProposals"> nested in the tab's scope,
//   which reads projectId and repositoryId from the page.

import { describeError } from "../../../../shared/presentation/errors.js";
import { orderProposals, rowNote, sessionText, stateLabel, stateTone } from "../../domain/instructionProposals.js";

/** What the section says on the window when it learns how many proposals are open: { detail: { open } }. */
export const PROPOSALS_COUNTED = "architecture-proposals-counted";

/** @param {{ gateway: ReturnType<typeof import("../../infrastructure/architecture-gateway.js").architectureGateway> }} deps */
export const instructionProposals = ({ gateway }) => () => {
  const proposals = gateway.instructionProposals;
  let unfollow = null;
  return {
    /** The repository's proposals, open first, once listed. */
    list: [],
    openCount: 0,
    loaded: false,
    /** Why the list could not be read, "" when it could. */
    loadError: "",
    /** The time "proposed 25 min ago" counts from. */
    now: new Date(),

    async init() {
      unfollow = proposals.follow(this.projectId, this.repositoryId, {
        onChanged: (change) => this.proposalChanged(change),
      });
      await this.load();
    },
    destroy() {
      unfollow?.();
    },

    async load() {
      try {
        const out = await proposals.list(this.repositoryId);
        this.list = orderProposals(out.proposals);
        this.now = new Date();
        this.loadError = "";
        this.counted(out.openCount);
      } catch (err) {
        this.loadError = describeError(err).message;
      }
      this.loaded = true;
    },

    /** The feed said a proposal was submitted or decided: the badge takes its count now and the list is read again. */
    proposalChanged(change) {
      this.counted(change.openCount);
      this.load();
    },
    counted(open) {
      this.openCount = open;
      this.$dispatch(PROPOSALS_COUNTED, { open });
    },

    get rows() {
      return this.list.map((p) => ({
        id: p.id,
        state: p.state,
        label: stateLabel(p.state),
        tone: stateTone(p.state),
        summary: p.summary,
        skills: p.skills,
        note: rowNote(p, this.now),
        task: p.task,
        taskHref: p.task ? `/projects/${encodeURIComponent(this.projectId)}/tasks/${encodeURIComponent(p.task.id)}` : "",
        session: sessionText(p),
        hasDecisionNote: p.note !== "",
        decisionNote: p.note,
      }));
    },
    get isEmpty() {
      return this.loaded && this.list.length === 0 && this.loadError === "";
    },
    get hasList() {
      return this.list.length > 0;
    },
    get hasOpen() {
      return this.openCount > 0;
    },
    get openText() {
      return `${this.openCount} open`;
    },
    get hasLoadError() {
      return this.loadError !== "";
    },
  };
};
