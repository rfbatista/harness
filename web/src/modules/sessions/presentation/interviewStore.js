// The interviews of the sessions this page shows, and how the panel is laid
// out for the one on screen: the Alpine store "sessionsInterviews". The page
// seeds it, the session's panel follows the session's interview feed into it,
// and the tab row's Questions toggle reads it. All the rules are here, so the
// markup only binds.
//
// A session's panel is *shown* when it has an open interview and either the
// panel is docked (the session column is wide enough) and the person has not
// hidden it in this session, or it is a sheet the person opened. A sheet never
// opens by itself. The person's choice to hide is kept in this browser.
//
// The store also holds what the person has answered in each open interview
// (interviewAnswering.js): kept in this browser as a draft until it is sent.
//
// Open interviews stack: the newest expanded, older ones folded with how far
// the person got, each folding and expanding on its own. What is no longer
// open (sent, dismissed, expired) moves under Earlier, read-only. A change the
// feed brings (another tab sent it, the session ended) does that here too and
// lets go of the unsent draft; it never moves what the person is doing.
// An interview the person sent or dismissed here is moved with `moveToEarlier`.
// A panel with only history is shown when the person asks for it.

import { toAnswers } from "../domain/answers.js";
import { applyEvent, arrived, closedNow, emptyList, historyOf, openOnes } from "../domain/interviews.js";
import { answering } from "./interviewAnswering.js";
import { sending } from "./interviewSending.js";
import { answersText, arrivalText, closedText, panelView } from "./interviewView.js";

const noDrafts = { read: () => null, write() {}, drop() {}, sweep() {} };

/** How long the toggle's signal ring shows after an arrival the panel did not show. */
export const SIGNAL_MS = 1_200;

/**
 * @param {{
 *   gateway?: import("../domain/ports.js").InterviewGateway | null,  without it nothing is followed or read again
 *   prefs?: { interviewHidden: (sessionId: string) => boolean, setInterviewHidden: (sessionId: string, hidden: boolean) => void },
 *   timers?: { setTimeout: typeof globalThis.setTimeout, clearTimeout: typeof globalThis.clearTimeout },
 *   drafts?: { read: Function, write: Function, drop: Function, sweep: Function },  the unsent answers kept in this browser
 *   visible?: () => boolean,  the tab is in front: a hidden tab applies changes without a ring or an announcement
 * }} deps
 */
export function interviewStore({ gateway = null, prefs = { interviewHidden: () => false, setInterviewHidden() {} }, timers = globalThis, drafts = noDrafts, visible = () => !globalThis.document?.hidden } = {}) {
  /** Sessions whose list was followed and then let go: events may have been missed. */
  const stale = new Set();
  const rings = new Map();
  /** The feeds being followed, by session: the page follows its selected session and the panel follows its own, over one feed. */
  const following = new Map();
  /** The interviews being sent or dismissed from this page: their echo on the feed is this tab's own doing. */
  const inflight = new Set();

  return {
    ...answering(drafts),
    ...sending({ gateway, inflight }),
    /** @type {Record<string, import("../domain/interviews.js").InterviewList>} */
    lists: {},
    /** The session column is wide enough to dock the panel (set by the panel from its width). */
    docked: true,
    /** The narrow layout's sheet is open. */
    sheet: false,
    /** Sessions whose toggle shows its ring. */
    rings: {},
    /** Bumped when a hide choice changes, so what reads it follows. */
    prefsRev: 0,
    /** The person's fold choice per open interview id (true: folded); an id not here follows the stack's order. */
    folds: {},
    /** The history rows the person opened, by interview id. */
    openedRows: {},
    /** Sessions whose panel the person opened while it holds only history. */
    viewing: {},

    /** @param {Record<string, import("../domain/interviews.js").InterviewList>} lists  the page seed's, by session id */
    seed(lists) {
      this.lists = { ...lists };
      drafts.sweep();
      this.hydrate(this.lists);
    },

    list(sessionId) {
      return this.lists[sessionId] ?? emptyList;
    },

    /** The person hid the panel in this session. */
    hidden(sessionId) {
      void this.prefsRev;
      return prefs.interviewHidden(sessionId);
    },

    /** The panel as the markup binds it. */
    view(sessionId) {
      return panelView(this.list(sessionId), {
        folded: (id) => this.folds[id],
        progress: (id) => this.progress(id),
        opened: (id) => this.openedRows[id] === true,
        typed: (interview) => this.typedAnswers(interview),
        sentHere: (id) => this.sentHere[id] === true,
      });
    },

    /** How many questions the session's open interviews hold: the Agent tab's badge and the title count. */
    openQuestions(sessionId) {
      return this.view(sessionId).questions;
    },

    hasOpen(sessionId) {
      return openOnes(this.list(sessionId)).length > 0;
    },

    /** What the person typed into an interview that expired, as answers; null when nothing was kept. */
    typedAnswers(interview) {
      const states = this.answers[interview.id];
      return states ? toAnswers(interview.questions, states) : null;
    },

    /** The text Copy answers puts on the clipboard for a closed interview, or "". */
    copyText(id) {
      for (const list of Object.values(this.lists)) {
        const interview = list.interviews.find((i) => i.id === id);
        if (interview) return answersText(interview, interview.answers ?? this.typedAnswers(interview));
      }
      return "";
    },

    /** Whether an open card is folded now: the person's choice, else only the newest is expanded. */
    cardFolded(id) {
      if (id in this.folds) return this.folds[id];
      for (const list of Object.values(this.lists)) {
        const open = openOnes(list);
        if (open.some((i) => i.id === id)) return open[open.length - 1].id !== id;
      }
      return false;
    },

    /** An open card folds or expands, on its own. */
    toggleCard(id) {
      this.folds = { ...this.folds, [id]: !this.cardFolded(id) };
    },

    /** A history row opens or folds. */
    toggleRow(id) {
      this.openedRows = { ...this.openedRows, [id]: !this.openedRows[id] };
    },

    /** The panel is on screen: docked beside the terminal, or as the sheet. */
    shown(sessionId) {
      if (!this.hasOpen(sessionId)) {
        if (historyOf(this.list(sessionId)).length === 0) return false;
        return this.docked ? this.viewing[sessionId] === true : this.sheet;
      }
      return this.docked ? !this.hidden(sessionId) : this.sheet;
    },

    /** The peek strip rises over the terminal's foot: narrow, an open interview, the sheet closed. */
    peeking(sessionId) {
      return !this.docked && !this.sheet && openOnes(this.list(sessionId)).length > 0;
    },

    /** The tab row's Questions toggle: with a badge while questions are open, bare while only history remains, null before anything was asked. */
    toggle(sessionId) {
      const v = this.view(sessionId);
      if (v.open) return { count: String(v.questions), label: v.toggleLabel, pressed: this.shown(sessionId), signal: this.rings[sessionId] === true };
      if (!v.hasHistory) return null;
      return { count: "", label: "Questions", pressed: this.shown(sessionId), signal: false };
    },

    /** Docks or undocks the panel with the column's width; a docked panel has no sheet. */
    setDocked(docked) {
      this.docked = docked;
      if (docked) this.sheet = false;
    },

    /** The Questions toggle: hides or shows the docked panel (and remembers it), or opens or closes the sheet. */
    flip(sessionId) {
      if (this.shown(sessionId)) this.dismissPanel(sessionId);
      else this.reveal(sessionId);
    },

    /** Makes the panel visible: shows the docked one, or opens the sheet. */
    reveal(sessionId) {
      if (!this.docked) this.sheet = true;
      else if (this.hasOpen(sessionId)) this.setHidden(sessionId, false);
      else this.viewing = { ...this.viewing, [sessionId]: true };
    },

    /** Hides the panel: the person's choice, kept (for open interviews), or closes the sheet. */
    dismissPanel(sessionId) {
      if (!this.docked) this.sheet = false;
      else if (this.hasOpen(sessionId)) this.setHidden(sessionId, true);
      else this.viewing = { ...this.viewing, [sessionId]: false };
    },

    setHidden(sessionId, hidden) {
      prefs.setInterviewHidden(sessionId, hidden);
      this.prefsRev++;
    },

    /**
     * Follows the session's interview feed into its list until the returned
     * function is called. `announce` says what arrived, for the page's polite
     * region. The page and the panel may follow the same session: one feed
     * serves both, and what it says goes to the latest follower.
     * @param {string} sessionId
     * @param {(text: string) => void} announce
     */
    follow(sessionId, announce) {
      if (!gateway) return () => {};
      let held = following.get(sessionId);
      if (held) held.say.push(announce);
      else {
        const say = [announce];
        const tell = (text) => say.at(-1)?.(text);
        if (!(sessionId in this.lists) || stale.delete(sessionId)) void this.reload(sessionId, tell);
        const stop = gateway.follow(
          sessionId,
          (event) => this.receive(sessionId, event, tell),
          (status) => {
            this.connection = status;
            if (status === "resynced") void this.reload(sessionId, tell);
          },
        );
        held = { say, stop };
        following.set(sessionId, held);
      }
      const mine = held;
      return () => {
        const at = mine.say.indexOf(announce);
        if (at < 0) return;
        mine.say.splice(at, 1);
        if (mine.say.length > 0) return;
        following.delete(sessionId);
        mine.stop();
        stale.add(sessionId);
      };
    },

    /** One event of the feed: applied in order, ignored when the seed holds it, a read when one was missed. */
    receive(sessionId, event, announce) {
      const result = applyEvent(this.list(sessionId), event);
      if (result.outcome === "gap") void this.reload(sessionId, announce);
      else if (result.outcome === "applied") this.replace(sessionId, result.list, announce);
    },

    /** Reads the session's list again; the newer of it and what is held stays. */
    async reload(sessionId, announce) {
      if (!gateway) return;
      let list;
      try {
        list = await gateway.list(sessionId);
      } catch {
        return; // the feed's next event or resync tries again
      }
      if (list.seq >= this.list(sessionId).seq) this.replace(sessionId, list, announce);
    },

    /**
     * Takes a list, and tells the person what arrived in it and what closed,
     * without moving anything they are doing: the cards stack, an interview the
     * person had begun stays open, one that closed moves to Earlier and loses
     * its unsent draft. A hidden tab hears and rings nothing.
     */
    replace(sessionId, list, announce) {
      const before = this.list(sessionId);
      const now = arrived(before, list);
      // What this page is sending closed because it sent it, not because another tab did.
      const closed = closedNow(before, list).filter((i) => !inflight.has(i.id));
      const wasShown = this.shown(sessionId);
      if (now.length > 0) this.keepTouched(sessionId);
      this.lists = { ...this.lists, [sessionId]: list };
      this.hydrate({ [sessionId]: list });
      if (wasShown && !this.hasOpen(sessionId) && historyOf(list).length > 0) this.viewing = { ...this.viewing, [sessionId]: true };
      const loud = visible();
      const others = openOnes(before).length > 0;
      if (loud) {
        now.forEach((interview, n) => announce?.(arrivalText(interview, others || n > 0)));
        closed.forEach((interview) => announce?.(closedText(interview)));
      }
      if (loud && now.length > 0 && this.docked && this.hidden(sessionId)) this.ring(sessionId);
    },

    /** A card the person had begun stays as it is when another arrives; an untouched one folds (the stack's order does it). */
    keepTouched(sessionId) {
      const kept = {};
      for (const card of this.view(sessionId).cards) if (!card.folded && !(card.id in this.folds) && this.touched(card.id)) kept[card.id] = false;
      this.folds = { ...this.folds, ...kept };
    },

    /**
     * An interview the person sent or dismissed here leaves the open ones at
     * once, before the feed says so: the server's copy replaces it by id when
     * the event comes, which then announces nothing. `open` opens its row.
     */
    moveToEarlier(sessionId, interview, { open = false } = {}) {
      const list = this.list(sessionId);
      this.replace(sessionId, Object.freeze({ seq: list.seq, interviews: Object.freeze(list.interviews.map((i) => (i.id === interview.id ? interview : i))) }));
      if (open) this.openedRows = { ...this.openedRows, [interview.id]: true };
    },

    /** One ring on the toggle: the person collapsed the panel, and a new count is what shows. */
    ring(sessionId) {
      timers.clearTimeout(rings.get(sessionId));
      this.rings = { ...this.rings, [sessionId]: false };
      // Off then on, so a second arrival plays the ring again.
      timers.setTimeout(() => (this.rings = { ...this.rings, [sessionId]: true }), 0);
      rings.set(
        sessionId,
        timers.setTimeout(() => (this.rings = { ...this.rings, [sessionId]: false }), SIGNAL_MS),
      );
    },
  };
}
