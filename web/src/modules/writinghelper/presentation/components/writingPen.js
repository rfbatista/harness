// The magic pen and its panel: one component attached to a field by markup.
// The server paints the wrapper (components.WritingPen) and this runs it.
//
//   <div x-data="writingPen" data-kind="task_description" data-noun="description"
//        data-label="Description" data-project-id=… data-project-name=…
//        data-siblings='[{"label":"Title","selector":"#task-title"}]'
//        data-helper-available="true">
//     <div class="pen-field"> <textarea …> <button class="pen"> </div>
//     <section class="helper"> … </section>
//   </div>
//
// The field stays the person's until Use this. Everything the conversation
// holds (answers, draft) lives in `panel` (domain/panel.js) and is gone when
// the panel closes.

import { describeError } from "../../../../shared/presentation/errors.js";
import { Codes } from "../../../../shared/domain/errors.js";
import { LIGHT_EDIT_CHIPS, REFINE_CHIPS, pickedMessage, startLead, startPlaceholder, startQuestion, usedMessage } from "../../domain/copy.js";
import { diffHtml, wordDiff } from "../../domain/diff.js";
import * as model from "../../domain/panel.js";
import { stepRequest } from "../../domain/steps.js";
import { isShown, isTextTarget, replaceField } from "../field.js";

/** How long the Undo toast stays. */
const TOAST_MS = 10_000;
/** How long the field's ring from the helper's write lasts (matches --duration-signal). */
const RING_MS = 900;
/** Under this width the panel is a bottom sheet. */
const SHEET_QUERY = "(max-width: 40rem)";

/**
 * @param {{
 *   gateway: import("../../domain/ports.js").StepGateway,
 *   coordinator: ReturnType<typeof import("../coordinator.js").createCoordinator>,
 *   setTimeout?: typeof globalThis.setTimeout,
 *   clearTimeout?: typeof globalThis.clearTimeout,
 *   matchMedia?: typeof globalThis.matchMedia,
 * }} deps
 */
export const writingPen = ({ gateway, coordinator, setTimeout: later = globalThis.setTimeout.bind(globalThis), clearTimeout: cancel = globalThis.clearTimeout.bind(globalThis), matchMedia = globalThis.matchMedia?.bind(globalThis) }) => () => {
  // Not reactive: Alpine would proxy them, and a proxied element or
  // controller throws when its methods run.
  /** @type {HTMLElement | null} */
  let root = null;
  /** @type {HTMLInputElement | HTMLTextAreaElement | null} */
  let field = null;
  /** @type {AbortController | null} */
  let controller = null;
  /** The text the field had before the last Use this, for Undo. */
  let previous = "";
  let toastTimer = 0;
  let ringTimer = 0;
  /** What Try again runs for a failed draft. */
  let lastDraft = { request: "", base: "" };
  let observer = null;

  return {
    kind: "task_description",
    noun: "description",
    label: "Description",
    projectId: "",
    projectName: "",
    panelId: "",
    /** @type {{ label: string, selector?: string, text?: string }[]} */
    siblingSpecs: [],
    available: true,
    unavailableReason: "",
    fixUrl: "",
    fieldDisabled: false,
    sheet: false,
    ringed: false,
    /** @type {model.PanelState} */
    panel: model.closed(),
    refineText: "",
    /** @type {{ message: string, undo: boolean, href: string } | null} */
    toast: null,
    /** The siblings that have text, as read when the panel opened (what the header names). */
    readLabels: [],
    /** The lowercased label of the first sibling with text ("title"), as read when the panel opened. */
    startSibling: "",

    init() {
      root = this.$el;
      const d = root.dataset;
      this.kind = /** @type {any} */ (d.kind ?? "task_description");
      this.noun = d.noun ?? "description";
      this.label = d.label ?? "Description";
      this.projectId = d.projectId ?? "";
      this.projectName = d.projectName ?? "";
      this.panelId = d.panelId ?? "";
      this.available = d.helperAvailable !== "false";
      this.unavailableReason = d.helperReason ?? "";
      this.fixUrl = d.helperFixUrl ?? "";
      try {
        this.siblingSpecs = JSON.parse(d.siblings || "[]");
      } catch {
        this.siblingSpecs = [];
      }

      field = /** @type {any} */ (root.querySelector(".pen-field > input, .pen-field > textarea"));
      if (field) {
        this.fieldDisabled = field.disabled;
        // Capture, so that the field's own Esc and ⌘↵ (cancel an edit, send
        // a comment) do not also fire while the helper is open.
        field.addEventListener("keydown", (e) => this.fieldKey(e), true);
        field.addEventListener("input", () => this.fieldEdited());
        if (typeof MutationObserver === "function") {
          observer = new MutationObserver(() => {
            this.fieldDisabled = Boolean(field?.disabled);
          });
          observer.observe(field, { attributes: true, attributeFilter: ["disabled"] });
        }
      }
      if (matchMedia) {
        const query = matchMedia(SHEET_QUERY);
        this.sheet = query.matches;
        query.addEventListener?.("change", (e) => {
          this.sheet = e.matches;
        });
      }
      coordinator.add(this);
    },

    destroy() {
      this.close(false);
      observer?.disconnect();
      cancel(toastTimer);
      cancel(ringTimer);
      coordinator.remove(this);
    },

    // --- availability -----------------------------------------------------

    /** @param {{ reason: string, fixUrl: string }} info */
    setUnavailable(info) {
      this.available = false;
      this.unavailableReason = info.reason;
      this.fixUrl = info.fixUrl;
      if (this.isOpen) this.close(false);
    },

    get penDisabled() {
      return !this.available || this.fieldDisabled;
    },
    get penLabel() {
      return this.available ? `Write the ${this.noun} with the helper` : "Writing helper unavailable";
    },
    get penTitle() {
      if (!this.available) return this.unavailableReason ? `Writing helper unavailable: ${this.unavailableReason}` : "Writing helper unavailable";
      return `${this.penLabel} ⌘J`;
    },

    // --- what the panel shows ----------------------------------------------

    get isOpen() {
      return this.panel.phase !== "closed";
    },
    get panelLabel() {
      return `Writing helper for ${this.label}`;
    },
    get showStart() {
      return this.panel.phase === "start";
    },
    get showReading() {
      return this.panel.phase === "reading";
    },
    get showQuestions() {
      return this.panel.phase === "questions";
    },
    get showGood() {
      return this.panel.phase === "good";
    },
    get showDrafting() {
      return this.panel.phase === "drafting";
    },
    get showProposal() {
      return this.panel.phase === "proposal" && !this.panel.stale;
    },
    get showStale() {
      return this.panel.phase === "proposal" && this.panel.stale;
    },
    get showTitles() {
      return this.panel.phase === "titles";
    },
    get showError() {
      return this.panel.phase === "error";
    },
    get showRefine() {
      return this.showProposal || this.showGood;
    },
    get isBusy() {
      return this.panel.phase === "reading" || this.panel.phase === "drafting";
    },
    get isSheetOpen() {
      return this.sheet && this.isOpen;
    },
    get readingText() {
      return this.panel.step === "titles" ? "Looking for titles…" : "Reading what you wrote…";
    },
    get refinePlaceholder() {
      return this.panel.phase === "good" ? "Or ask for something specific…" : "Ask for a change… e.g. mention the phone layout";
    },

    /** What the panel reads, for the header chips. */
    get reads() {
      const names = [this.label, ...this.readLabels];
      if (this.projectName) names.push(`project ${this.projectName}`);
      return names;
    },

    // Start
    get startLead() {
      return startLead(this.kind, this.noun, this.startSibling);
    },
    get startQuestion() {
      return startQuestion(this.kind);
    },
    get startPlaceholder() {
      return startPlaceholder(this.kind);
    },
    get startChip() {
      return this.startSibling ? `Draft from the ${this.startSibling}` : "";
    },
    get hasStartChip() {
      return this.startSibling !== "";
    },
    get cannotContinue() {
      return !model.canContinue(this.panel);
    },
    get intentValue() {
      return this.panel.intent;
    },
    intentInput(event) {
      this.panel = model.setIntent(this.panel, event.target.value);
    },

    // Questions
    get questionRows() {
      return this.panel.questions.map((q) => ({
        id: q.id,
        text: q.text,
        why: q.why,
        inputId: `${this.panelId}-${q.id}`,
        value: this.panel.answers[q.id] ?? "",
        answered: (this.panel.answers[q.id] ?? "").trim() !== "",
        chips: q.chips.slice(0, 4).map((text) => ({ text, pressed: model.chipPressed(this.panel, q.id, text) })),
      }));
    },
    get answered() {
      return model.answeredCount(this.panel);
    },
    get answeredText() {
      return `${this.answered} of ${this.panel.questions.length} answered · unanswered gaps stay open`;
    },
    get stepMarks() {
      return this.panel.questions.map((_, i) => ({ id: i, done: i < this.answered }));
    },

    // Draft, proposal, stale
    get proposalLead() {
      return `Proposed ${this.noun}`;
    },
    get changesPressed() {
      return this.panel.view === "changes";
    },
    get cleanPressed() {
      return this.panel.view === "clean";
    },
    get staleMessage() {
      return `You changed the ${this.noun} after this draft. Using it would drop your new words.`;
    },
    get refineChips() {
      return REFINE_CHIPS;
    },
    get lightEditChips() {
      return LIGHT_EDIT_CHIPS;
    },
    get gapCount() {
      return model.openGaps(this.panel.draft).length;
    },
    get hasGaps() {
      return this.gapCount > 0;
    },
    get gapText() {
      return this.gapCount === 1 ? "1 open placeholder: fill it in the field after you use the draft." : `${this.gapCount} open placeholders: fill them in the field after you use the draft.`;
    },

    // Titles
    get titleRows() {
      return this.panel.titles.map((text, i) => ({ key: `${i}-${text}`, text, index: i, number: i + 1 }));
    },
    get titlesLead() {
      return this.startSibling ? `From your ${this.startSibling}. Pick one; the title field takes it.` : "Pick one; the title field takes it.";
    },

    // Error
    get errorMessage() {
      return this.panel.error ? `${this.panel.error.message} Your ${this.noun} is unchanged.` : "";
    },
    get errorCode() {
      return this.panel.error?.code ?? "";
    },
    get errorNext() {
      return this.panel.error?.next ?? "";
    },

    /** Paints the draft: the word diff against the field's text, or the clean text. Called from x-effect. */
    paintDraft(el) {
      const s = this.panel;
      const view = s.stale ? "clean" : s.view;
      el.dataset.view = view;
      el.innerHTML = diffHtml(wordDiff(s.baseline, s.draft));
    },

    // --- opening and closing ----------------------------------------------

    toggle() {
      if (this.isOpen) this.close(true);
      else this.open();
    },

    open() {
      if (!field || this.penDisabled) return;
      coordinator.claim(this);
      this.refineText = "";
      this.panel = model.opened({ kind: this.kind, text: field.value });
      const read = this.readSiblings().filter((s) => s.text.trim() !== "");
      this.readLabels = read.map((s) => s.label);
      this.startSibling = read.length ? read[0].label.toLowerCase() : "";
      if (this.panel.phase === "reading") this.runRead();
      this.settleFocus();
    },

    /** @param {boolean} [toField] give the focus back to the field */
    close(toField = true) {
      controller?.abort();
      controller = null;
      this.panel = model.closed();
      coordinator.release(this);
      if (toField) field?.focus();
    },

    // --- steps ---------------------------------------------------------------

    readSiblings() {
      return this.siblingSpecs.map((spec) => {
        const el = spec.selector ? document.querySelector(spec.selector) : null;
        const text = el && "value" in el ? /** @type {HTMLInputElement} */ (el).value : (spec.text ?? "");
        return { label: spec.label, text };
      });
    },

    /** Runs a step: ignores the answer if the panel moved on (stopped, closed, another run began). */
    async run(step, extra, apply) {
      controller?.abort();
      const mine = new AbortController();
      controller = mine;
      const current = () => controller === mine;
      try {
        const request = stepRequest(step, {
          projectId: this.projectId,
          kind: this.kind,
          text: field?.value ?? "",
          siblings: this.readSiblings(),
          ...extra,
        });
        const result = await gateway.runStep(request, {
          signal: mine.signal,
          onDelta: (text) => {
            if (current()) this.panel = model.delta(this.panel, text);
          },
        });
        if (current()) apply(result);
      } catch (err) {
        if (!current() || /** @type {any} */ (err)?.name === "AbortError") return;
        this.fail(err);
      } finally {
        if (current()) controller = null;
      }
      this.settleFocus();
    },

    fail(err) {
      if (/** @type {any} */ (err)?.code === Codes.WRITING_HELPER_UNAVAILABLE) {
        const reason = /** @type {any} */ (err).message ?? "";
        const fixUrl = /** @type {any} */ (err).details?.fix_url ?? "";
        this.close(false);
        coordinator.markUnavailable({ reason, fixUrl });
        this.showToast("Writing helper unavailable: " + reason, false, fixUrl);
        return;
      }
      this.panel = model.failed(this.panel, describeError(err));
    },

    /** Analyze, or titles for a title field. */
    runRead() {
      const titles = this.panel.step === "titles";
      return this.run(
        titles ? "titles" : "analyze",
        titles ? { exclude: this.panel.offered } : {},
        titles ? (r) => (this.panel = model.titled(this.panel, r)) : (r) => (this.panel = model.analyzed(this.panel, r)),
      );
    },

    /** "Continue" on the start question. */
    continueStart() {
      if (this.cannotContinue) return;
      return this.draft();
    },

    /** "Draft from the title": no answer, the siblings carry it. */
    draftFromSibling() {
      this.panel = model.setIntent(this.panel, "");
      return this.draft();
    },

    /**
     * Draft, from the answers (or none: Skip questions). A refine passes the
     * proposal it changes as `base`.
     */
    draft({ request = "", base = "" } = {}) {
      const baseline = field?.value ?? "";
      const answers = model.answersFor(this.panel);
      lastDraft = { request, base };
      this.panel = model.drafting(this.panel, { baseline, request });
      return this.run("draft", { answers, draft: base, request }, (r) => {
        this.panel = model.drafted(this.panel, r.text ?? "");
        this.panel = model.fieldChanged(this.panel, field?.value ?? "");
      });
    },

    skipQuestions() {
      this.panel = { ...this.panel, answers: {} };
      return this.draft();
    },

    /** A chip under the proposal, the light edits, or the refine line. */
    refine(request) {
      const text = (request ?? "").trim();
      if (!text) return;
      this.refineText = "";
      return this.draft({ request: text, base: this.panel.phase === "proposal" ? this.panel.draft : "" });
    },

    refineChip(chip) {
      return this.refine(chip);
    },

    refineSubmit(event) {
      if (event.metaKey || event.ctrlKey) return;
      event.preventDefault();
      return this.refine(this.refineText);
    },

    /** Use anyway: the stale draft replaces what the person typed since. */
    useAnyway() {
      this.use();
    },

    /** Redraft with my edits: a fresh draft from the field as it is now. */
    redraft() {
      return this.draft();
    },

    stop() {
      controller?.abort();
      controller = null;
      this.panel = model.stopped(this.panel);
      if (this.panel.phase === "closed") {
        coordinator.release(this);
        field?.focus();
      } else {
        this.settleFocus();
      }
    },

    retry() {
      const step = this.panel.retry;
      if (step === "draft") return this.draft(lastDraft);
      this.panel = model.reading(this.panel, step);
      return this.runRead();
    },

    threeMore() {
      this.panel = model.reading(this.panel, "titles");
      return this.runRead();
    },

    // --- answers -----------------------------------------------------------

    answerInput(id, event) {
      this.panel = model.setAnswer(this.panel, id, event.target.value);
    },
    pickChip(id, chip) {
      this.panel = model.pickChip(this.panel, id, chip);
    },
    setView(view) {
      this.panel = model.setView(this.panel, view);
    },
    showChanges() {
      this.setView("changes");
    },
    showClean() {
      this.setView("clean");
    },

    // --- accepting ---------------------------------------------------------

    /** Use this: the field takes the draft. */
    use() {
      if (!field || this.panel.phase !== "proposal") return;
      this.accept(this.panel.draft, usedMessage(this.noun));
    },

    /** Pick a title option by its position, 0-based. */
    pick(index) {
      const text = this.panel.titles[index];
      if (text === undefined) return;
      this.accept(text, pickedMessage());
    },

    accept(text, message) {
      if (!field) return;
      previous = field.value;
      this.close(false);
      replaceField(field, text);
      this.ringed = true;
      cancel(ringTimer);
      ringTimer = later(() => (this.ringed = false), RING_MS);
      this.showToast(message, true);
    },

    undo() {
      if (!field) return;
      replaceField(field, previous);
      this.dismissToast();
    },

    showToast(message, undo, href = "") {
      cancel(toastTimer);
      this.toast = { message, undo, href };
      toastTimer = later(() => this.dismissToast(), TOAST_MS);
    },
    dismissToast() {
      cancel(toastTimer);
      this.toast = null;
    },
    get hasToast() {
      return this.toast !== null;
    },
    get toastMessage() {
      return this.toast?.message ?? "";
    },
    get toastCanUndo() {
      return Boolean(this.toast?.undo);
    },
    get toastHref() {
      return this.toast?.href ?? "";
    },
    get toastHasLink() {
      return Boolean(this.toast?.href);
    },

    // --- the field and the keys ----------------------------------------------

    /** The person typed in the field: a draft made from older text goes stale. */
    fieldEdited() {
      if (this.isOpen && field) this.panel = model.fieldChanged(this.panel, field.value);
    },

    /** Capture-phase keydown on the field. */
    fieldKey(event) {
      const mod = event.metaKey || event.ctrlKey;
      if (mod && !event.altKey && !event.shiftKey && event.key.toLowerCase() === "j") {
        event.preventDefault();
        event.stopPropagation();
        this.toggle();
        return;
      }
      if (this.isOpen) this.panelKey(event);
    },

    /** Esc closes, ⌘↵ runs the primary action, Enter in the panel's inputs answers them; from the field or from inside the panel. */
    panelKey(event) {
      if (event.key === "Escape") {
        event.preventDefault();
        event.stopPropagation();
        this.close(true);
      } else if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
        event.preventDefault();
        event.stopPropagation();
        this.primary();
      } else if (event.key === "Enter" && event.target instanceof HTMLInputElement) {
        // The panel sits inside the page's form: Enter must not submit it.
        event.preventDefault();
        event.stopPropagation();
        if (event.target.hasAttribute("data-refine")) this.refine(this.refineText);
        else if (this.panel.phase === "start") this.continueStart();
      }
    },

    /** The key 1-3 picks a title option when no text input has the focus. */
    windowKey(event) {
      if (!this.showTitles || event.metaKey || event.ctrlKey || event.altKey) return;
      if (!["1", "2", "3"].includes(event.key) || isTextTarget(event.target)) return;
      event.preventDefault();
      this.pick(Number(event.key) - 1);
    },

    /** ⌘↵: what the footer's primary button does. */
    primary() {
      switch (this.panel.phase) {
        case "start":
          return this.continueStart();
        case "questions":
          return this.draft();
        case "proposal":
          return this.panel.stale ? this.redraft() : this.use();
        case "error":
          return this.retry();
        default:
      }
    },

    // --- focus -------------------------------------------------------------

    /** After a phase change, keep the focus somewhere that is still shown. */
    settleFocus() {
      this.$nextTick(() => {
        if (!root || !this.isOpen) return;
        const active = document.activeElement;
        const panel = root.querySelector(".helper");
        if (!panel) return;
        const first = [".ask input", ".options button", "footer [data-primary]", "header button"]
          .map((s) => Array.from(panel.querySelectorAll(s)).find((el) => isShown(/** @type {HTMLElement} */ (el))))
          .find(Boolean);
        const inPanelBody = active && panel.contains(active) && isShown(/** @type {HTMLElement} */ (active));
        // The panel's first input or option wins on arrival; elsewhere keep what the person was on.
        if ((this.panel.phase === "questions" || this.panel.phase === "start" || this.panel.phase === "titles") && !(active instanceof HTMLInputElement && inPanelBody)) {
          /** @type {HTMLElement | undefined} */ (first)?.focus();
        } else if (!inPanelBody && active !== field) {
          /** @type {HTMLElement | undefined} */ (first)?.focus();
        }
      });
    },
  };
};
