// The session history page's pull request control, in the toolbar:
//
//   <div x-data="pullRequestPanel" data-seed="pull-request-seed">
//
// It offers to open a PR for the session's branch (or says why it cannot),
// takes the title, body, base and draft in a short form, and once one exists
// shows it with its state, linked to the host.
//
// GET never waits on the host: what the host says (a PR merged, one opened
// outside the harness and adopted) arrives later on the project feed, and the
// panel follows it. The person's own create echoes on the feed too, possibly
// before its POST answers; while a create is in flight, a PR for this
// workspace is held, not announced as someone else's.

import { Codes, codeOf } from "../../../../shared/domain/errors.js";
import { FeedStatus } from "../../../../shared/domain/feed.js";
import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { baseOptions, canCreate, formFromDraft, formProblems, hasProblems, isOpen, newerOf } from "../../domain/pullRequest.js";
import { blockerLink, blockerText, consequenceLine, fetchedText, refusalOf, summaryLine, toChip, uncommittedLine } from "../view.js";

/** Codes after which what the draft says has likely changed. */
const STALE_DRAFT = [Codes.PR_NO_COMMITS, Codes.PR_NO_REMOTE, Codes.PR_BASE_NOT_FOUND];

const NO_PROBLEMS = Object.freeze({ title: "", baseBranch: "" });

/** @param {{ gateway: import("../../domain/ports.js").PullRequestGateway }} deps */
export const pullRequestPanel = ({ gateway, now = () => new Date() }) => () => {
  let unfollow = () => {};
  /** A PR for this workspace that arrived while the person's own create was in flight. */
  let held = null;

  return {
    projectId: "",
    workspaceId: "",
    /** @type {import("../../domain/pullRequest.js").PullRequestDraft | null} */
    draft: null,
    /** @type {import("../../domain/pullRequest.js").PullRequest | null} */
    pullRequest: null,
    /** The workspace is gone (WORKSPACE_NOT_FOUND): no PR action here. */
    gone: false,
    /** @type {{ message: string, code: string, next: string } | null} a read that failed */
    error: null,
    formOpen: false,
    /** @type {import("../../domain/pullRequest.js").PullRequestForm} */
    form: { title: "", body: "", baseBranch: "", draft: false },
    /** The form was filled from a draft; reopening keeps what the person typed. */
    formReady: false,
    problems: NO_PROBLEMS,
    creating: false,
    /** @type {import("../../domain/pullRequest.js").PullRequestBases | null} origin's branches, as last fetched */
    bases: null,
    /** Origin is being fetched. */
    refreshing: false,
    /** Why the last fetch failed, in one line; the list is the one from before. */
    refreshError: "",
    /** Said beside the chip after the person's own create: what it did not touch. */
    createdNote: "",
    /** @type {{ lead: string, reason: string, code: string } | null} the last create's refusal */
    refusal: null,
    /** What happened that the person did not do here (or that needs saying), above the form. */
    notice: "",
    announcement: "",

    // ── what the markup binds ────────────────────────────────────────────
    get hidden() {
      return this.gone || (!this.draft && !this.pullRequest);
    },
    get visible() {
      return !this.hidden;
    },
    get chip() {
      return this.pullRequest ? toChip(this.pullRequest) : null;
    },
    get hasChip() {
      return !!this.pullRequest;
    },
    // The chip's parts, flat for the markup ("" / null with no PR).
    get chipHref() {
      return this.chip?.href ?? "";
    },
    get chipLabel() {
      return this.chip?.label ?? "";
    },
    get chipTitle() {
      return this.chip?.title ?? "";
    },
    get chipState() {
      return this.chip?.state ?? "";
    },
    get chipTone() {
      return this.chip?.tone ?? null;
    },
    get chipDescribe() {
      return this.chip?.describe ?? "";
    },
    /** The create button shows unless a PR is open; it is enabled when nothing is in the way. */
    get offersCreate() {
      return !!this.draft && !isOpen(this.pullRequest);
    },
    /** The toolbar's button; the form has its own while it is open. */
    get showsCreateButton() {
      return this.offersCreate && !this.showsForm;
    },
    get cannotCreate() {
      return this.creating || !canCreate(this.draft, this.pullRequest);
    },
    get disabledAttr() {
      return this.cannotCreate ? "true" : null;
    },
    get busyAttr() {
      return this.creating ? "true" : null;
    },
    get expandedAttr() {
      return this.formOpen ? "true" : "false";
    },
    get buttonLabel() {
      return this.creating ? "Creating…" : "Create pull request";
    },
    /** The form's own button: it says what it opens. */
    get submitLabel() {
      if (this.creating) return "Creating…";
      return this.form.draft ? "Open draft PR" : "Create pull request";
    },
    /** What pressing it does: the push and the base. */
    get consequence() {
      return this.draft ? consequenceLine(this.draft, this.form) : "";
    },
    /** The Base picker's branches: the default first, then origin's. */
    get baseChoices() {
      return baseOptions(this.bases, this.draft?.baseBranch ?? "");
    },
    get fetchedLine() {
      return fetchedText(this.bases?.fetchedAt ?? null, now());
    },
    get refreshLabel() {
      return this.refreshing ? "Fetching branches…" : "Fetch branches from origin";
    },
    get refreshBusyAttr() {
      return this.refreshing ? "true" : null;
    },
    get hasRefreshError() {
      return this.refreshError !== "";
    },
    get hasCreatedNote() {
      return this.createdNote !== "";
    },
    get blockers() {
      if (!this.draft || isOpen(this.pullRequest)) return [];
      return this.draft.blockers.map((code) => {
        const link = blockerLink(code, this.draft);
        return { code, text: blockerText(code, this.draft), href: link?.href ?? "", linkText: link?.text ?? "" };
      });
    },
    get blocked() {
      return this.blockers.length > 0;
    },
    get showsForm() {
      return this.formOpen && !!this.draft;
    },
    /** The strip under the toolbar shows when it has something to say. */
    get showsStrip() {
      return !this.gone && (this.blocked || this.hasNotice || this.hasCreatedNote || this.showsForm || !!this.error);
    },
    get hasNotice() {
      return this.notice !== "";
    },
    get hasUncommitted() {
      return this.uncommitted !== "";
    },
    get hasRefusal() {
      return !!this.refusal;
    },
    get refusalLead() {
      return this.refusal?.lead ?? "";
    },
    get refusalReason() {
      return this.refusal?.reason ?? "";
    },
    get refusalCode() {
      return this.refusal?.code ?? "";
    },
    get refusalHref() {
      return this.refusal?.href ?? "";
    },
    get refusalLinkText() {
      return this.refusal?.linkText ?? "";
    },
    get errorLine() {
      return this.error ? `${this.error.message} ${this.error.code} · ${this.error.next}` : "";
    },
    get summary() {
      return this.draft ? summaryLine(this.draft, this.form.baseBranch.trim()) : "";
    },
    get uncommitted() {
      return this.draft ? uncommittedLine(this.draft) : "";
    },
    get progress() {
      return this.draft ? `Pushing ${this.draft.headBranch} and opening the pull request on GitHub…` : "";
    },
    get titleInvalid() {
      return this.problems.title ? "true" : null;
    },
    get baseInvalid() {
      return this.problems.baseBranch ? "true" : null;
    },

    // ── lifecycle ────────────────────────────────────────────────────────
    init() {
      // The server's first paint of what Alpine now renders from data (the reasons list).
      for (const node of this.$el.querySelectorAll?.("[data-ssr]") ?? []) node.remove();
      let seed;
      try {
        seed = gateway.decodePanelSeed(readSeed(this.$el));
      } catch (err) {
        this.error = describeError(err);
        return;
      }
      this.projectId = seed.projectId;
      this.workspaceId = seed.workspaceId;
      if (seed.view) this.apply(seed.view);
      else this.load();
      unfollow = gateway.follow(
        this.projectId,
        (pr) => this.arrived(pr),
        (status) => status === FeedStatus.RESYNCED && this.load(),
      );
    },

    destroy() {
      unfollow();
    },

    /** Reads the draft and the PR again; the form keeps what the person typed. */
    async load() {
      try {
        this.apply(await gateway.get(this.workspaceId));
        this.error = null;
      } catch (err) {
        if (codeOf(err) === Codes.WORKSPACE_NOT_FOUND) this.gone = true;
        else this.error = describeError(err);
      }
    },

    apply({ draft, pullRequest }) {
      this.draft = draft;
      if (pullRequest) this.pullRequest = newerOf(this.pullRequest, pullRequest);
      if (isOpen(this.pullRequest)) this.formOpen = false;
    },

    /** A PR opened, adopted or changed, on the feed. */
    arrived(pr) {
      if (pr.workspaceId !== this.workspaceId) return;
      if (this.creating) {
        held = pr; // the person's own, most likely: it lands when the POST answers
        return;
      }
      const before = this.pullRequest;
      this.pullRequest = newerOf(before, pr);
      if (this.pullRequest !== pr) return;
      if (isOpen(pr) && this.formOpen) {
        this.formOpen = false;
        this.notice = `Pull request #${pr.number} was opened from elsewhere.`;
      }
      if (!before || before.number !== pr.number) this.say(`Pull request #${pr.number} is ${toChip(pr).state}.`);
      else if (before.state !== pr.state || before.draft !== pr.draft) this.say(`Pull request #${pr.number} is now ${toChip(pr).state}.`);
    },

    // ── the form ─────────────────────────────────────────────────────────
    openForm() {
      if (this.cannotCreate) return;
      if (!this.formReady) {
        this.form = formFromDraft(this.draft);
        this.formReady = true;
      }
      this.formOpen = true;
      this.notice = "";
      this.$nextTick(() => this.$refs.title?.focus?.());
      this.refreshDraft();
      this.loadBases();
    },

    /** Origin's branches as git has them now; never fetches. */
    async loadBases() {
      try {
        this.bases = await gateway.bases(this.workspaceId);
      } catch (err) {
        if (codeOf(err) === Codes.WORKSPACE_NOT_FOUND) this.gone = true;
        // Otherwise the picker still offers the draft's base.
      }
    },

    /** Fetches origin, then shows its branches; on a failure says why and keeps the list. */
    async refreshBases() {
      if (this.refreshing) return;
      this.refreshing = true;
      this.refreshError = "";
      try {
        this.bases = await gateway.refreshBases(this.workspaceId);
        if (this.form.baseBranch && !this.baseChoices.includes(this.form.baseBranch)) this.form.baseBranch = this.bases.default;
        this.say(`Fetched origin. ${this.bases.branches.length} branches.`);
      } catch (err) {
        if (codeOf(err) === Codes.WORKSPACE_NOT_FOUND) {
          this.gone = true;
        } else {
          const r = refusalOf(err);
          this.refreshError = `${r.lead} ${r.reason}`.trim();
          this.say(this.refreshError);
        }
      } finally {
        this.refreshing = false;
      }
    },

    /** What git says now (commits ahead, uncommitted files, blockers), without touching the form. */
    async refreshDraft() {
      try {
        this.apply(await gateway.get(this.workspaceId));
      } catch (err) {
        if (codeOf(err) === Codes.WORKSPACE_NOT_FOUND) this.gone = true;
      }
    },

    cancel() {
      if (this.creating) return;
      this.formOpen = false;
      this.refusal = null;
      this.problems = NO_PROBLEMS;
      this.$nextTick(() => this.$refs.create?.focus?.());
    },

    async submit() {
      if (this.creating || !this.draft) return;
      this.problems = formProblems(this.form);
      if (hasProblems(this.problems)) {
        this.$nextTick(() => this.$refs.title?.focus?.());
        return;
      }
      if (!canCreate(this.draft, this.pullRequest)) return;
      this.creating = true;
      this.refusal = null;
      this.notice = "";
      held = null;
      try {
        const pr = await gateway.create({
          workspaceId: this.workspaceId,
          title: this.form.title.trim(),
          body: this.form.body,
          baseBranch: this.form.baseBranch.trim(),
          draft: this.form.draft,
        });
        this.opened(pr);
        this.createdNote = "The session, its worktree and the task status are unchanged.";
        this.say(`Opened pull request #${pr.number}.`);
      } catch (err) {
        this.refused(err);
      } finally {
        this.creating = false;
        held = null;
      }
    },

    opened(pr) {
      const next = newerOf(this.pullRequest, pr);
      this.pullRequest = held ? newerOf(next, held) : next;
      this.formOpen = false;
      this.formReady = false;
      this.problems = NO_PROBLEMS;
    },

    refused(err) {
      const code = codeOf(err);
      if (code === Codes.PR_ALREADY_EXISTS && err.details?.pullRequest) {
        const pr = err.details.pullRequest;
        this.opened(pr);
        this.notice = `#${pr.number} was already open on GitHub; it is linked here now.`;
        this.say(this.notice);
        return;
      }
      // The feed says a PR is open for this workspace: it wins over the error.
      if (isOpen(held)) {
        this.opened(held);
        return;
      }
      if (code === Codes.WORKSPACE_NOT_FOUND) {
        this.gone = true;
        return;
      }
      if (code === Codes.INVALID_INPUT) {
        this.problems = { ...NO_PROBLEMS, title: "A pull request needs a title." };
      } else if (code === Codes.PR_BASE_NOT_FOUND) {
        this.problems = { ...NO_PROBLEMS, baseBranch: `${this.form.baseBranch.trim() || this.draft.baseBranch} is not a branch on origin.` };
      } else {
        this.refusal = refusalOf(err);
        this.$nextTick(() => this.$refs.refusal?.scrollIntoView?.({ block: "nearest" }));
      }
      if (STALE_DRAFT.includes(code)) this.refreshDraft();
      this.say(this.problems.title || this.problems.baseBranch || this.refusal?.lead || "");
    },

    dismissNotice() {
      this.notice = "";
    },

    /** Says it in the page's polite live region. */
    say(text) {
      if (!text) return;
      this.announcement = text;
      this.$dispatch("announce", { text });
    },
  };
};
