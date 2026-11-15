// The Proposals section of the Instructions & rules tab: the instruction files
// agents proposed (ADR 0092), listed open first, each decided and kept with its
// note. A proposal opens in a pane beside the list: each file with its op and a
// diff against the version the draft read, the stale marker, and the decision.
// It follows the project feed, so a proposal submitted or decided in another
// tab, or a file saved elsewhere, shows here at once, and it tells the page how
// many proposals are open for the tab's badge.
//
//   <section x-data="architectureInstructionProposals"> nested in the tab's scope,
//   which reads projectId and repositoryId from the page.

import { describeError } from "../../../../shared/presentation/errors.js";
import { drawLines } from "../../../code/presentation/render/lines.js";
import {
  agoText,
  approveLabel,
  approvedText,
  fileDiff,
  fileHead,
  opSummary,
  orderProposals,
  refusalTitle,
  refusedFiles,
  rowNote,
  sendBackNoteError,
  sessionText,
  staleCountText,
  stateLabel,
  stateTone,
} from "../../domain/instructionProposals.js";

/** What the section says on the window when it learns how many proposals are open: { detail: { open } }. */
export const PROPOSALS_COUNTED = "architecture-proposals-counted";

/** A pane opens its first files expanded; the rest start collapsed, as the code view's long diffs do. */
const EXPANDED_FILES = 3;

/** @param {{ gateway: ReturnType<typeof import("../../infrastructure/architecture-gateway.js").architectureGateway> }} deps */
export const instructionProposals = ({ gateway }) => () => {
  const proposals = gateway.instructionProposals;
  const stops = [];
  /** The open proposal's file diffs by path, prepared once per read of its files. */
  let diffs = new Map();
  return {
    /** The repository's proposals, open first, once listed. */
    list: [],
    openCount: 0,
    loaded: false,
    /** Why the list could not be read, "" when it could. */
    loadError: "",
    /** The time "proposed 25 min ago" counts from. */
    now: new Date(),

    /** The proposal in the pane: its row and its files, or null. */
    viewing: "",
    pane: null,
    paneBusy: false,
    paneError: "",
    /** The files the person collapsed (true) or expanded (false), by path. */
    folded: {},
    /** The proposal being decided: a second click, and the feed's echo of our own decision, wait for its answer. */
    deciding: "",
    /** After an INSTRUCTION_CHANGED refusal: the paths it named. */
    refusal: null,
    /** After an approval: the version each saved file got, by path. */
    approved: null,
    decideError: "",
    /** The ask under the decision row: "reject", "sendback" or "" for none; the note in it and why it cannot go. */
    asking: "",
    note: "",
    noteError: "",

    async init() {
      stops.push(
        proposals.follow(this.projectId, this.repositoryId, { onChanged: (change) => this.proposalChanged(change) }),
        gateway.instructionFiles.follow(this.projectId, this.repositoryId, { onChanged: (change) => this.fileChanged(change) }),
      );
      await this.load();
    },
    destroy() {
      for (const stop of stops) stop();
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

    /** The feed said a proposal was submitted or decided: the badge takes its count now, the list is read again, and so is the pane if it shows that proposal and the echo is not of our own decision. */
    proposalChanged(change) {
      this.counted(change.openCount);
      this.load();
      if (change.proposalId === this.viewing && change.proposalId !== this.deciding) this.readPane(change.proposalId);
    },
    /** A file was saved or deleted: an open proposal's stale marks may have moved. */
    fileChanged(change) {
      if (this.pane === null || this.deciding !== "" || this.pane.proposal.state !== "open") return;
      if (this.pane.files.some((f) => f.path === change.path)) this.readPane(this.viewing);
    },
    counted(open) {
      this.openCount = open;
      this.$dispatch(PROPOSALS_COUNTED, { open });
    },

    // ── the pane ───────────────────────────────────────────────────────────

    async open(id) {
      if (this.viewing === id) return;
      this.viewing = id;
      this.pane = null;
      this.folded = {};
      this.refusal = null;
      this.approved = null;
      this.decideError = "";
      this.cancelAsk();
      await this.readPane(id);
    },
    close() {
      this.viewing = "";
      this.pane = null;
      this.refusal = null;
      this.approved = null;
      this.cancelAsk();
    },
    /** Reads the proposal with its files; an answer for a proposal no longer in the pane is dropped. */
    async readPane(id) {
      this.paneBusy = this.pane === null;
      try {
        const out = await proposals.get(id);
        if (this.viewing !== id) return;
        diffs = new Map(out.files.map((f) => [f.path, fileDiff(f)]));
        this.pane = out;
        this.paneError = "";
      } catch (err) {
        if (this.viewing === id) this.paneError = describeError(err).message;
      } finally {
        if (this.viewing === id) this.paneBusy = false;
      }
    },

    async approve() {
      if (this.deciding !== "" || this.pane === null) return;
      const id = this.viewing;
      this.deciding = id;
      this.refusal = null;
      this.decideError = "";
      try {
        const out = await proposals.approve(id);
        this.approved = Object.fromEntries(out.saved.map((s) => [s.path, s.version]));
        if (this.viewing === id) this.pane = { ...this.pane, proposal: out.proposal };
      } catch (err) {
        await this.refused(id, err);
      } finally {
        this.deciding = "";
      }
      await this.load();
    },
    askToReject() {
      this.ask("reject");
    },
    askToSendBack() {
      this.ask("sendback");
    },
    ask(which) {
      if (this.deciding !== "") return;
      this.asking = which;
      this.note = "";
      this.noteError = "";
    },
    /** The note takes focus when its ask opens, so the person types at once. */
    focusNote(el) {
      this.$nextTick(() => el.focus());
    },
    cancelAsk() {
      this.asking = "";
      this.note = "";
      this.noteError = "";
    },
    /** The ask's submit: a send back without a note says so and sends nothing. */
    async submitAsk() {
      if (this.deciding !== "" || this.pane === null) return;
      const sendBack = this.asking === "sendback";
      const note = this.note.trim();
      this.noteError = sendBack ? sendBackNoteError(this.note) : "";
      if (this.noteError !== "") return;
      const id = this.viewing;
      this.deciding = id;
      this.decideError = "";
      try {
        const out = sendBack ? await proposals.sendBack(id, note) : await proposals.reject(id, note);
        if (this.viewing === id) this.pane = { ...this.pane, proposal: out.proposal };
        this.cancelAsk();
      } catch (err) {
        if (err.code === "INVALID_INPUT") this.noteError = describeError(err).message;
        else await this.refused(id, err);
      } finally {
        this.deciding = "";
      }
      await this.load();
    },
    /** What each refusal does: INSTRUCTION_CHANGED names the stale files (and the pane reads them again); PROPOSAL_DECIDED shows what was decided. */
    async refused(id, err) {
      if (err.code === "INSTRUCTION_CHANGED") {
        this.refusal = { paths: err.paths ?? [] };
        await this.readPane(id);
      } else if (err.code === "PROPOSAL_DECIDED") {
        this.cancelAsk();
        await this.readPane(id);
      } else {
        this.decideError = describeError(err).message;
      }
    },

    toggleFile(path) {
      this.folded = { ...this.folded, [path]: !this.isFolded(path) };
    },
    toggleAll() {
      const fold = !this.allFolded;
      this.folded = Object.fromEntries(this.pane.files.map((f) => [f.path, fold]));
    },
    isFolded(path) {
      if (path in this.folded) return this.folded[path];
      const i = this.pane.files.findIndex((f) => f.path === path);
      return i >= EXPANDED_FILES && !this.pane.files[i].stale;
    },
    /** Draws a file's diff rows into its .code-lines (x-init on it). */
    drawFile(el, path) {
      const d = diffs.get(path);
      if (d) drawLines(el, { rows: d.rows, virtual: false });
    },

    // ── what the templates read ────────────────────────────────────────────

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
        taskHref: this.taskHref(p.task),
        session: sessionText(p),
        hasDecisionNote: p.note !== "",
        decisionNote: p.note,
        viewing: p.id === this.viewing,
        openLabel: `Open the proposal: ${p.summary}`,
      }));
    },
    taskHref(task) {
      return task ? `/projects/${encodeURIComponent(this.projectId)}/tasks/${encodeURIComponent(task.id)}` : "";
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

    get paneOpen() {
      return this.viewing !== "";
    },
    get hasPane() {
      return this.pane !== null;
    },
    get paneLabel() {
      return this.pane ? `Proposal: ${this.pane.proposal.summary}` : "Proposal";
    },
    get paneTitle() {
      return this.pane?.proposal.summary ?? "";
    },
    get paneStateLabel() {
      return this.pane ? stateLabel(this.pane.proposal.state) : "";
    },
    get paneTone() {
      return this.pane ? stateTone(this.pane.proposal.state) : "";
    },
    get paneIsOpen() {
      return this.pane?.proposal.state === "open";
    },
    get paneSkills() {
      const skills = this.pane?.proposal.skills ?? [];
      return skills.map((name, i) => ({ name, sep: i < skills.length - 1 ? " · " : "" }));
    },
    /** "software-architect · architect on" before the task's link. */
    get paneSessionOn() {
      if (!this.pane) return "";
      const session = sessionText(this.pane.proposal);
      return this.pane.proposal.task ? `${session} on ` : session;
    },
    get paneTask() {
      return this.pane?.proposal.task ?? null;
    },
    get paneTaskHref() {
      return this.taskHref(this.paneTask);
    },
    get paneProposed() {
      return this.pane ? agoText(this.pane.proposal.createdAt, this.now) : "";
    },
    get paneDecided() {
      const p = this.pane?.proposal;
      if (!p || p.state === "open") return "";
      return `${agoText(p.decidedAt || p.createdAt, this.now)} · ${stateLabel(p.state).toLowerCase()} · ${p.note || "no note"}`;
    },
    get paneIsDecided() {
      return this.hasPane && !this.paneIsOpen;
    },
    get fileCount() {
      return this.pane?.files.length ?? 0;
    },
    get paneOps() {
      return this.pane ? opSummary(this.pane.files) : "";
    },
    get paneFiles() {
      if (!this.pane) return [];
      const state = this.pane.proposal.state;
      return this.pane.files.map((f, i) => {
        const head = fileHead(f, state);
        const d = diffs.get(f.path) ?? { added: 0, deleted: 0, digits: 2 };
        const folded = this.isFolded(f.path);
        const name = f.path.split("/").pop();
        const savedAt = this.approved?.[f.path];
        return {
          ...head,
          key: `${f.path}@${f.baseVersion}-${f.currentVersion}-${state}`,
          path: f.path,
          id: `proposal-file-${i}`,
          nameId: `proposal-file-${i}-path`,
          bodyId: `proposal-file-${i}-hunks`,
          savedText: this.approved ? (f.op === "remove" ? "removed" : `saved as v${savedAt}`) : "",
          againstText: this.approved ? "" : head.againstText,
          added: d.added,
          deleted: d.deleted,
          addedText: d.added > 0 ? `+${d.added}` : "",
          deletedText: d.deleted > 0 ? `−${d.deleted}` : "",
          digitsStyle: `--digits: ${d.digits}`,
          collapsed: folded,
          expanded: !folded,
          toggleLabel: `${folded ? "Expand" : "Collapse"} ${name}`,
        };
      });
    },
    get allFolded() {
      return this.pane ? this.pane.files.every((f) => this.isFolded(f.path)) : false;
    },
    get collapseAllLabel() {
      return this.allFolded ? "Expand all" : "Collapse all";
    },
    get staleCount() {
      return this.paneIsOpen ? this.pane.files.filter((f) => f.stale).length : 0;
    },
    get staleNotice() {
      return this.refusal === null && this.staleCount > 0;
    },
    get staleNoticeText() {
      return staleCountText(this.staleCount);
    },
    get hasRefusal() {
      return this.refusal !== null;
    },
    get refusalText() {
      return refusalTitle(this.refusal?.paths.length ?? 0);
    },
    get refusalPaths() {
      if (!this.refusal || !this.pane) return [];
      return refusedFiles(this.refusal.paths, this.pane.files).map((r) => ({ ...r, suffix: r.text ? ` · ${r.text}` : "" }));
    },
    get approveText() {
      return approveLabel(this.fileCount);
    },
    get cannotDecide() {
      return this.deciding !== "" || this.asking !== "";
    },
    get cannotSubmit() {
      return this.deciding !== "";
    },
    get isAsking() {
      return this.asking !== "";
    },
    get asksSendBack() {
      return this.asking === "sendback";
    },
    get askTone() {
      return this.asksSendBack ? "attention" : "danger";
    },
    get askTitle() {
      return this.asksSendBack ? "Send it back to the session." : "Reject the proposal.";
    },
    get askBody() {
      return this.asksSendBack
        ? "The session gets a turn with your note, keeps its draft and can submit again."
        : "Nothing is saved; the session is told and the proposal stays in the list as rejected.";
    },
    get askAria() {
      return this.asksSendBack ? "Send the proposal back" : "Reject the proposal";
    },
    get askRequirement() {
      return this.asksSendBack ? "(required)" : "(optional)";
    },
    get askPlaceholder() {
      return this.asksSendBack ? "What should change before you approve it?" : "Why, if the session should know";
    },
    get askButton() {
      return this.asksSendBack ? "Send back" : "Reject";
    },
    get askVariant() {
      return this.asksSendBack ? "primary" : "danger";
    },
    get askRejecting() {
      return this.asking === "reject";
    },
    get hasNoteError() {
      return this.noteError !== "";
    },
    get hasApproved() {
      return this.approved !== null && this.paneStateIsApproved;
    },
    get paneStateIsApproved() {
      return this.pane?.proposal.state === "approved";
    },
    get approvedNotice() {
      return this.pane ? approvedText(this.pane.files) : "";
    },
    get hasDecideError() {
      return this.decideError !== "";
    },
    get hasPaneError() {
      return this.paneError !== "";
    },
  };
};
