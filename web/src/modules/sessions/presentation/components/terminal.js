// A session's terminal: the PTY claude runs in on the server, drawn in the
// browser. The screen (xterm.js, injected as createScreen) draws; the gateway
// carries the snapshot, the output and the keys. Keyed on the session, so
// choosing another session mounts a fresh one and destroy() detaches. It takes
// the keyboard focus only when the person asks for it (a click on the screen,
// Reconnect, the page's terminal-focus-requested); never on what the server
// sends, so J/K in the sessions list walk past live terminals.
//
// The page owns the tab row its tools sit in, so the terminal tells it how it
// is (terminal-status, bubbling) and takes its commands from window events
// naming it: Reconnect, Jump to latest, a new text size.
//
//   <div class="[ terminal ]" x-data="sessionsTerminal(sid)" x-bind:data-state="state"
//        x-on:terminal-focus-requested.window="focus" x-on:terminal-reconnect-requested.window="reconnectRequested"
//        x-on:terminal-jump-requested.window="jumpRequested" x-on:terminal-text-size.window="textSizeRequested">
//     <div class="[ screen ]" x-ref="screen"></div> …
//   </div>

/**
 * A screen draws a terminal and reports what is typed into it.
 * @typedef {object} Screen
 * @property {(snapshot: import("../../domain/ports.js").TerminalSnapshot) => void} draw  redraws from scratch
 * @property {(bytes: Uint8Array) => void} write
 * @property {() => ({ cols: number, rows: number } | null)} fit  fits the container; the new size, or null when unchanged or hidden
 * @property {() => void} focus
 * @property {(px: number) => void} setFontSize
 * @property {() => void} scrollToBottom  back to the live screen, without the focus
 * @property {() => void} hideCursor  an ended terminal is read-only
 * @property {() => void} dispose
 *
 * @typedef {(container: HTMLElement, input: {
 *   onKey: (event: KeyboardEvent) => boolean,
 *   onPaste: (text: string) => void,
 *   onCellSize?: () => void,
 *   onScrollState?: (state: { scrolledUp: boolean, newLines: number }) => void,
 *   fontSize?: number,
 * }) => Screen} CreateScreen
 *
 * What a terminal tells the page, for its tools in the tab row.
 * @typedef {{ id: string, kind: "agent" | "run", state: "connecting" | "live" | "reconnecting" | "offline" | "ended",
 *   title: string, exitCode: number | null, lostAt: number | null, scrolledUp: boolean, newLines: number }} TerminalStatus
 */

/**
 * How long the pane's size must hold before the PTY hears of it: a window
 * drag or a header growing is one resize, not one per frame.
 */
const FIT_SETTLE_MS = 100;

/**
 * @param {{
 *   terminals: import("../../domain/ports.js").TerminalGateway,
 *   createScreen: CreateScreen,
 *   observeResize?: (el: HTMLElement, onResize: () => void) => () => void,
 *   timers?: { setTimeout: typeof globalThis.setTimeout, clearTimeout: typeof globalThis.clearTimeout },
 *   kind?: "agent" | "run",  whose terminal: a session's agent, or an application run from its worktree
 *   textSize?: () => number,  the text size this browser chose, in px
 *   now?: () => number,
 * }} deps
 */
export const terminal = ({ terminals, createScreen, observeResize = resizeObserver, timers = globalThis, kind = "agent", textSize = () => undefined, now = () => Date.now() }) => (sessionId = "") => {
  let screen = null;
  let connection = null;
  let stopObserving = () => {};
  let fitTimer = 0;
  let wasLive = false;

  return {
    sessionId,
    /** connecting (first attach) → live → offline (dropped) → reconnecting (Reconnect) → live; ended on exit */
    state: "connecting",
    exitCode: null,
    title: "",
    lostAt: null,
    scrolledUp: false,
    newLines: 0,

    init() {
      screen = createScreen(this.$refs.screen, {
        onKey: (event) => connection?.key(event) ?? false,
        onPaste: (text) => connection?.paste(text),
        // The font arrived after the screen measured it: the same pane holds other rows and columns.
        onCellSize: () => this.scheduleFit(),
        onScrollState: ({ scrolledUp, newLines }) => {
          this.scrolledUp = scrolledUp;
          this.newLines = newLines;
          this.report();
        },
        fontSize: textSize(),
      });
      this.connect();
      stopObserving = observeResize(this.$refs.screen, () => this.scheduleFit());
    },

    /** Tells the page how this terminal is. */
    report() {
      /** @type {TerminalStatus} */
      const status = {
        id: this.sessionId,
        kind,
        state: this.state,
        title: this.title,
        exitCode: this.exitCode,
        lostAt: this.lostAt,
        scrolledUp: this.scrolledUp,
        newLines: this.newLines,
      };
      this.$dispatch("terminal-status", status);
    },

    connect() {
      this.state = wasLive ? "reconnecting" : "connecting";
      this.report();
      connection = terminals.attach(this.sessionId, {
        onOpen: () => {
          wasLive = true;
          this.state = "live";
          this.lostAt = null;
          this.report();
        },
        onSnapshot: (snapshot) => {
          screen.draw(snapshot);
          if (snapshot.title) this.title = snapshot.title;
          // The snapshot is at the server's size; take this pane's, telling
          // the server only when they differ (a hidden pane has none). The
          // focus stays where the person left it: a terminal takes it on request.
          this.fit();
          this.report();
        },
        onOutput: (bytes) => screen.write(bytes),
        onTitle: (title) => {
          this.title = title;
          this.report();
        },
        onExit: (code) => {
          this.state = "ended";
          this.exitCode = code;
          screen.hideCursor();
          this.report();
          this.$dispatch("terminal-exited", { id: this.sessionId, code });
        },
        onClosed: () => {
          if (this.state === "ended") return;
          this.state = "offline";
          this.lostAt = now();
          this.report();
        },
      });
    },

    /** The person pressed Reconnect: the terminal takes the focus the button had. */
    reconnect() {
      connection?.close();
      this.connect();
      screen?.focus();
    },

    /** @param {CustomEvent<{ id: string }>} event  Reconnect, pressed in the tab row */
    reconnectRequested(event) {
      if (event.detail?.id === this.sessionId) this.reconnect();
    },

    /** @param {CustomEvent<{ id: string }>} event  Jump to latest: scrolls, and leaves the focus where it is */
    jumpRequested(event) {
      if (event.detail?.id === this.sessionId) screen?.scrollToBottom();
    },

    /** @param {CustomEvent<{ px: number }>} event  a new text size: the same pane, other rows and columns, at once */
    textSizeRequested(event) {
      if (!screen || !event.detail?.px) return;
      screen.setFontSize(event.detail.px);
      this.fit();
    },
    /** Fits the screen to the pane once its size has held for a moment. */
    scheduleFit() {
      timers.clearTimeout(fitTimer);
      fitTimer = timers.setTimeout(() => this.fit(), FIT_SETTLE_MS);
    },

    /** Fits the screen to the pane and tells the server the new size, if there is one. */
    fit() {
      if (!screen) return;
      const size = screen.fit();
      if (size) connection?.resize(size);
    },

    focus() {
      screen?.focus();
    },

    destroy() {
      timers.clearTimeout(fitTimer);
      stopObserving();
      connection?.close();
      screen?.dispose();
    },
  };
};

function resizeObserver(el, onResize) {
  if (typeof ResizeObserver === "undefined") return () => {};
  const observer = new ResizeObserver(() => onResize());
  observer.observe(el);
  return () => observer.disconnect();
}
