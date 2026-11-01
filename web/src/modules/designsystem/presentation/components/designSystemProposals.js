// The Proposals tab and the banner above the tabs: the proposals of the design
// system's design sessions, and the review of the one the person opens. It
// lives inside designSystemPage and reads its id, its tab and its version from
// it; it keeps the open count the page shows on the tab, the navigation badge
// and the banner in step with the project feed.
//
//   <div x-data="designSystemProposals" data-seed="design-system-seed">

import { describeError } from "../../../../shared/presentation/errors.js";
import { FeedStatus } from "../../../../shared/domain/feed.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { contrastFigures, dependencyFlags, itemView, previewPairs, rowView, sectionDiff, tokenRows } from "../../domain/proposals.js";

/** What an apply did, as the line shown above the review. */
function decisionResult({ version, state }) {
  const wrote = version > 0 ? `version ${version} was written` : "nothing was written";
  if (state === "rejected") return "Rejected: nothing changed.";
  if (state === "approved") return `Approved: ${wrote}.`;
  if (state === "partly_approved") return `Partly approved: ${wrote}.`;
  return `Applied: ${wrote}; the other changes wait for you.`;
}

/** @param {{ gateway: ReturnType<typeof import("../../infrastructure/designsystem-gateway.js").designSystemGateway>, clock: import("../../../../shared/infrastructure/clock.js").Clock, location?: Location }} deps */
export const designSystemProposals = ({ gateway, clock, location = globalThis.location }) => () => ({
  proposalRows: [],
  proposalsLoaded: false,
  proposalId: "",
  proposalView: null,
  /** The design system as it is now, for the "before" of a preview. */
  currentRecord: { tokens: [], principles: "", components: [] },
  /** The ids of the conflicting items whose overwrite the person confirmed. */
  overwriteIds: [],
  /** The person's marks on pending items, by item id: "approve" or "reject". Applied together. */
  marks: {},
  /** Why the server refused an item, by item key. */
  itemProblems: {},
  rejectingAll: false,
  deciding: false,
  proposalError: null,
  /** The line shown once a decision was made: "Version 9 written." or "Rejected.". */
  decisionResult: "",
  unfollowProposals: null,

  get proposalRowViews() {
    return this.proposalRows.map((r) => rowView(r, clock.now()));
  },
  get hasProposalRows() {
    return this.proposalRows.length > 0;
  },
  get noProposalRows() {
    return this.proposalsLoaded && this.proposalRows.length === 0;
  },
  get openProposalRows() {
    return this.proposalRows.filter((r) => r.state === "open");
  },
  get openProposalCount() {
    return this.openProposalRows.length;
  },
  get proposalsWaitingText() {
    return this.openProposalCount === 0 ? "none waiting" : `${this.openProposalCount} waiting`;
  },
  get hasProposalsWaiting() {
    return this.openProposalCount > 0;
  },
  /** The banner: a proposal waits and the person is not looking at the tab. */
  get showProposalBanner() {
    return this.openProposalCount > 0 && !this.isTab("proposals");
  },
  get proposalBannerText() {
    const first = this.openProposalRows[0];
    if (!first) return "";
    const who = first.agent === "" ? "An agent" : first.agent;
    const more = this.openProposalCount > 1 ? ` (and ${this.openProposalCount - 1} more)` : "";
    return `${who} proposed changes${more}: ${first.summary}`;
  },
  get reviewingProposal() {
    return this.proposalView !== null;
  },
  get viewState() {
    return this.proposalView ? rowView(this.proposalView, clock.now()) : null;
  },
  get proposalDecidable() {
    return this.proposalView !== null && this.proposalView.state === "open";
  },
  get dependencyFlagsNow() {
    return this.proposalView ? dependencyFlags(this.proposalView, this.marks) : {};
  },
  get markedItems() {
    return (this.proposalView?.items ?? []).filter((it) => it.decision === "pending" && this.marks[it.id] !== undefined);
  },
  get markedApprove() {
    return this.markedItems.filter((it) => this.marks[it.id] === "approve");
  },
  get hasMarks() {
    return this.markedItems.length > 0;
  },
  get pendingItems() {
    return (this.proposalView?.items ?? []).filter((it) => it.decision === "pending");
  },
  get hasFlags() {
    return Object.keys(this.dependencyFlagsNow).length > 0;
  },
  get canApplyMarks() {
    return this.proposalDecidable && !this.deciding && this.hasMarks && !this.hasFlags && this.markedApprove.every((it) => !it.conflicting || this.overwriteIds.includes(it.id));
  },
  get marksText() {
    const a = this.markedApprove.length;
    const r = this.markedItems.length - a;
    return `${a} approved, ${r} rejected`;
  },
  get proposalItemViews() {
    return this.proposalView ? this.proposalView.items.map((it) => ({ ...itemView(it), confirmed: this.overwriteIds.includes(it.id) })) : [];
  },
  /** Each item with what the person reads of it: its values, its diff, its contrast. */
  get proposalPanels() {
    if (!this.proposalView) return [];
    const view = this.proposalView;
    return view.items.map((it) => {
      const iv = itemView(it);
      const figures = contrastFigures(it, this.currentRecord.tokens, view.proposedRecord.tokens);
      return {
        ...iv,
        isToken: it.kind === "token",
        isPrinciple: it.kind === "principles_section",
        isComponent: it.kind === "component",
        rows: it.kind === "token" ? tokenRows(it) : [],
        diff: it.kind === "principles_section" ? sectionDiff(it) : [],
        hasContrast: figures !== null,
        contrast: figures ?? { against: "", before: "", after: "" },
        conflictNote: it.conflicting ? "You changed this since the proposal's base. Approving overwrites your version." : "",
        confirmed: this.overwriteIds.includes(it.id),
        canMark: this.proposalDecidable && it.decision === "pending",
        approveOn: this.marks[it.id] === "approve",
        rejectOn: this.marks[it.id] === "reject",
        decided: it.decision === "approved" || it.decision === "rejected",
        flag: this.dependencyFlagsNow[it.id] ?? "",
        hasFlag: this.dependencyFlagsNow[it.id] !== undefined,
        problem: this.itemProblems[it.key] ?? "",
        hasProblem: this.itemProblems[it.key] !== undefined,
        componentNote: it.kind === "component" ? (it.op === "remove" ? "Removed by the proposal." : it.op === "add" ? "New component, see its previews below." : "Changed, see its previews below.") : "",
      };
    });
  },
  get previewPairsOf() {
    return this.proposalView ? previewPairs(this.proposalView, this.currentRecord) : [];
  },
  get hasPreviewPairs() {
    return this.previewPairsOf.length > 0;
  },
  get proposalConflictCount() {
    return this.proposalView ? this.proposalView.items.filter((i) => i.conflicting).length : 0;
  },
  get proposalHasConflicts() {
    return this.proposalConflictCount > 0;
  },
  get proposalConflictText() {
    const n = this.proposalConflictCount;
    return `You saved version ${this.proposalView?.currentVersion} after this proposal was made (its base is version ${this.proposalView?.baseVersion}). ${n === 1 ? "One change touches" : `${n} changes touch`} what you saved: confirm each overwrite below before approving.`;
  },
  get everyOverwriteConfirmed() {
    return this.proposalView !== null && this.pendingItems.filter((i) => i.conflicting).every((i) => this.overwriteIds.includes(i.id));
  },
  get canApproveProposal() {
    return this.proposalDecidable && !this.deciding && this.everyOverwriteConfirmed;
  },
  get canRejectProposal() {
    return this.proposalDecidable && !this.deciding;
  },
  get proposalNextVersionText() {
    const v = this.proposalView;
    if (!v) return "";
    if (!this.hasMarks) return `Approving all writes version ${v.currentVersion + 1} · “${v.summary}”`;
    const writes = this.markedApprove.length > 0 ? `writes version ${v.currentVersion + 1}` : "writes no version";
    return `Applying ${this.marksText} ${writes} · ${this.pendingItems.length - this.markedItems.length} stay pending`;
  },
  get hasDecisionResult() {
    return this.decisionResult !== "";
  },
  get hasProposalError() {
    return this.proposalError !== null;
  },

  init() {
    const seed = readSeed(this.$el);
    this.unfollowProposals = gateway.followProposals(seed.project_id, seed.design_system_id, {
      onProposal: (e) => this.proposalFollowed(e),
      onStatus: (status) => {
        if (status === FeedStatus.RESYNCED) this.reloadProposals();
      },
    });
    this.reloadProposals().then(() => {
      const want = new URLSearchParams(location?.search ?? "").get("proposal");
      if (want && this.proposalRows.some((r) => r.id === want)) this.selectProposal(want);
    });
  },

  destroy() {
    this.unfollowProposals?.();
  },

  /** A proposal of this project reached a state: the list, the open one and the navigation badge follow it. */
  proposalFollowed(e) {
    this.syncProposalBadge();
    if (e.designSystemId !== this.designSystemId) return;
    this.reloadProposals();
    if (e.proposalId === this.proposalId) this.loadProposalView(e.proposalId);
  },

  async reloadProposals() {
    try {
      this.proposalRows = await gateway.proposals(this.designSystemId);
      this.proposalsLoaded = true;
      this.openProposals = this.openProposalCount;
      this.syncProposalBadge();
    } catch (err) {
      this.proposalError = describeError(err);
    }
  },

  /** The navigation item's badge: how many proposals wait across the project's design systems. */
  async syncProposalBadge() {
    const badge = document.querySelector("[data-nav-proposals]");
    if (!badge) return;
    try {
      const n = await gateway.openProposalCount(this.projectId);
      badge.textContent = String(n);
      badge.hidden = n === 0;
    } catch {
      // the badge keeps what it shows
    }
  },

  async selectProposal(id) {
    this.proposalId = id;
    this.overwriteIds = [];
    this.rejectingAll = false;
    this.decisionResult = "";
    this.proposalError = null;
    this.marks = {};
    this.itemProblems = {};
    await this.loadProposalView(id);
  },

  async loadProposalView(id) {
    try {
      const view = await gateway.proposal(id);
      // What waits is compared with the design system as it is; what was decided, with the version it started from.
      const record = view.state === "open" || view.state === "drafting" ? (await gateway.get(this.designSystemId)).record : await gateway.baseRecord(this.designSystemId, view.baseVersion);
      if (id !== this.proposalId) return;
      this.proposalView = view;
      this.currentRecord = record;
      // A mark on an item that is no longer pending is spent.
      this.marks = Object.fromEntries(Object.entries(this.marks).filter(([itemId]) => view.items.some((it) => it.id === itemId && it.decision === "pending")));
    } catch (err) {
      this.proposalError = describeError(err);
    }
  },

  /** The banner's Review: opens the tab on the first waiting proposal, even on a design system with no version yet. */
  async reviewProposal() {
    this.started = true;
    this.openTab("proposals");
    const first = this.openProposalRows[0];
    if (first) await this.selectProposal(first.id);
  },

  closeProposal() {
    this.proposalId = "";
    this.proposalView = null;
    this.decisionResult = "";
  },

  /** Marks a pending item to approve or reject at the next apply; marking it the same way again unmarks it. */
  markItem(id, decision) {
    const marks = { ...this.marks };
    if (marks[id] === decision) delete marks[id];
    else marks[id] = decision;
    this.marks = marks;
    this.itemProblems = {};
  },

  toggleOverwrite(id) {
    this.overwriteIds = this.overwriteIds.includes(id) ? this.overwriteIds.filter((x) => x !== id) : [...this.overwriteIds, id];
  },

  askRejectProposal() {
    this.rejectingAll = true;
  },

  cancelRejectProposal() {
    this.rejectingAll = false;
  },

  approveProposal() {
    return this.decideProposal("approve");
  },

  rejectProposal() {
    return this.decideProposal("reject");
  },

  async decideProposal(decision) {
    if (this.proposalView === null) return;
    return this.send(this.pendingItems.map((i) => ({ id: i.id, decision })), decision === "approve" ? this.pendingItems.filter((i) => i.conflicting).map((i) => i.id) : []);
  },

  /** Applies the marks: the approved items become one version, the rejected are dropped, the rest stay pending. */
  applyMarks() {
    return this.send(
      this.markedItems.map((it) => ({ id: it.id, decision: this.marks[it.id] })),
      this.markedApprove.filter((it) => it.conflicting).map((it) => it.id),
    );
  },

  async send(decisions, confirm) {
    if (this.proposalView === null || this.deciding) return;
    this.deciding = true;
    this.proposalError = null;
    this.itemProblems = {};
    const id = this.proposalView.id;
    try {
      const out = await gateway.decideItems(id, decisions, confirm);
      this.decisionResult = decisionResult(out);
      this.rejectingAll = false;
      this.marks = {};
      await this.reloadProposals();
      await this.loadProposalView(id);
    } catch (err) {
      this.proposalError = describeError(err);
      // The design system refused some approved items: say which, on the item.
      const keys = err?.details?.item_keys;
      if (Array.isArray(keys)) this.itemProblems = Object.fromEntries(keys.map((k) => [k, "The design system cannot take this item as it is now. Reject it, or leave it pending."]));
    } finally {
      this.deciding = false;
    }
  },

  async withdrawProposal(id) {
    this.proposalError = null;
    try {
      await gateway.withdraw(id);
      await this.reloadProposals();
    } catch (err) {
      this.proposalError = describeError(err);
    }
  },

  /** Draws a before or after preview in a sandboxed frame inside its host (the CSP build of Alpine refuses directives on an iframe). */
  paintPreview(host, doc, title) {
    if (doc === "") return;
    let frame = host.querySelector("iframe");
    if (!frame) {
      frame = document.createElement("iframe");
      frame.setAttribute("sandbox", "");
      frame.setAttribute("data-preview-frame", "");
      frame.setAttribute("title", title);
      frame.style.cssText = "display:block;inline-size:100%;block-size:7rem;border:0;background:var(--color-canvas)";
      host.append(frame);
    }
    if (frame.srcdoc !== doc) frame.srcdoc = doc;
  },
});
