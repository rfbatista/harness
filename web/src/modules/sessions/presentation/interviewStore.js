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

import { applyEvent, arrived, emptyList, openOnes } from "../domain/interviews.js";
import { arrivalText, panelView } from "./interviewView.js";

/** How long the toggle's signal ring shows after an arrival the panel did not show. */
export const SIGNAL_MS = 1_200;

/**
 * @param {{
 *   gateway?: import("../domain/ports.js").InterviewGateway | null,  without it nothing is followed or read again
 *   prefs?: { interviewHidden: (sessionId: string) => boolean, setInterviewHidden: (sessionId: string, hidden: boolean) => void },
 *   timers?: { setTimeout: typeof globalThis.setTimeout, clearTimeout: typeof globalThis.clearTimeout },
 * }} deps
 */
export function interviewStore({ gateway = null, prefs = { interviewHidden: () => false, setInterviewHidden() {} }, timers = globalThis } = {}) {
  /** Sessions whose list was followed and then let go: events may have been missed. */
  const stale = new Set();
  const rings = new Map();

  return {
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

    /** @param {Record<string, import("../domain/interviews.js").InterviewList>} lists  the page seed's, by session id */
    seed(lists) {
      this.lists = { ...lists };
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
      return panelView(this.list(sessionId));
    },

    /** The panel is on screen: docked beside the terminal, or as the sheet. */
    shown(sessionId) {
      if (openOnes(this.list(sessionId)).length === 0) return false;
      return this.docked ? !this.hidden(sessionId) : this.sheet;
    },

    /** The peek strip rises over the terminal's foot: narrow, an open interview, the sheet closed. */
    peeking(sessionId) {
      return !this.docked && !this.sheet && openOnes(this.list(sessionId)).length > 0;
    },

    /** The tab row's Questions toggle; null while the session has no open interview. */
    toggle(sessionId) {
      const v = this.view(sessionId);
      if (!v.open) return null;
      return { count: String(v.questions), label: v.toggleLabel, pressed: this.shown(sessionId), signal: this.rings[sessionId] === true };
    },

    /** Docks or undocks the panel with the column's width; a docked panel has no sheet. */
    setDocked(docked) {
      this.docked = docked;
      if (docked) this.sheet = false;
    },

    /** The Questions toggle: hides or shows the docked panel (and remembers it), or opens or closes the sheet. */
    flip(sessionId) {
      if (this.docked) this.setHidden(sessionId, this.shown(sessionId));
      else this.sheet = !this.sheet;
    },

    /** Makes the panel visible: shows the docked one, or opens the sheet. */
    reveal(sessionId) {
      if (this.docked) this.setHidden(sessionId, false);
      else this.sheet = true;
    },

    /** Hides the panel: the person's choice, kept, or closes the sheet. */
    dismissPanel(sessionId) {
      if (this.docked) this.setHidden(sessionId, true);
      else this.sheet = false;
    },

    setHidden(sessionId, hidden) {
      prefs.setInterviewHidden(sessionId, hidden);
      this.prefsRev++;
    },

    /**
     * Follows the session's interview feed into its list until the returned
     * function is called. `announce` says what arrived, for the page's polite region.
     * @param {string} sessionId
     * @param {(text: string) => void} announce
     */
    follow(sessionId, announce) {
      if (!gateway) return () => {};
      if (!(sessionId in this.lists) || stale.delete(sessionId)) void this.reload(sessionId, announce);
      const stop = gateway.follow(
        sessionId,
        (event) => this.receive(sessionId, event, announce),
        (status) => {
          if (status === "resynced") void this.reload(sessionId, announce);
        },
      );
      return () => {
        stop();
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

    /** Takes a list, and tells the person what arrived in it, without moving anything they are doing. */
    replace(sessionId, list, announce) {
      const before = this.list(sessionId);
      const now = arrived(before, list);
      this.lists = { ...this.lists, [sessionId]: list };
      const others = openOnes(before).length > 0;
      now.forEach((interview, n) => announce?.(arrivalText(interview, others || n > 0)));
      if (now.length > 0 && this.docked && this.hidden(sessionId)) this.ring(sessionId);
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
