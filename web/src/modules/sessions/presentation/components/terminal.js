// A session's terminal: the PTY claude runs in on the server, drawn in the
// browser. The screen (xterm.js, injected as createScreen) draws; the gateway
// carries the snapshot, the output and the keys. Keyed on the session, so
// choosing another session mounts a fresh one and destroy() detaches. It takes
// the keyboard focus only when the person asks for it (a click on the screen,
// Reconnect, the page's terminal-focus-requested); never on what the server
// sends, so J/K in the sessions list walk past live terminals.
//
//   <div x-data="sessionsTerminal(sid)" x-on:terminal-focus-requested.window="focus" class="[ terminal ]">
//     <div class="[ screen ]" x-ref="screen"></div> …
//   </div>

/**
 * A screen draws a terminal and reports what is typed into it.
 * @typedef {object} Screen
 * @property {(snapshot: import("../../domain/ports.js").TerminalSnapshot) => void} draw  redraws from scratch
 * @property {(bytes: Uint8Array) => void} write
 * @property {() => ({ cols: number, rows: number } | null)} fit  fits the container; the new size, or null when unchanged or hidden
 * @property {() => void} focus
 * @property {() => void} dispose
 *
 * @typedef {(container: HTMLElement, input: {
 *   onKey: (event: KeyboardEvent) => boolean,
 *   onPaste: (text: string) => void,
 *   onCellSize?: () => void,
 * }) => Screen} CreateScreen
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
 * }} deps
 */
export const terminal = ({ terminals, createScreen, observeResize = resizeObserver, timers = globalThis }) => (sessionId = "") => {
  let screen = null;
  let connection = null;
  let stopObserving = () => {};
  let fitTimer = 0;

  return {
    sessionId,
    /** connecting → live → exited | closed */
    state: "connecting",
    exitCode: null,
    title: "",

    get stateWord() {
      switch (this.state) {
        case "live":
          return "live";
        case "exited":
          return `exited (code ${this.exitCode})`;
        case "closed":
          return "disconnected";
        default:
          return "connecting";
      }
    },
    get statusState() {
      return { live: "running", exited: "done", closed: "failed" }[this.state] ?? "idle";
    },
    get canReconnect() {
      return this.state === "closed";
    },

    init() {
      screen = createScreen(this.$refs.screen, {
        onKey: (event) => connection?.key(event) ?? false,
        onPaste: (text) => connection?.paste(text),
        // The font arrived after the screen measured it: the same pane holds other rows and columns.
        onCellSize: () => this.scheduleFit(),
      });
      this.connect();
      stopObserving = observeResize(this.$refs.screen, () => this.scheduleFit());
    },

    connect() {
      this.state = "connecting";
      connection = terminals.attach(this.sessionId, {
        onOpen: () => {
          this.state = "live";
        },
        onSnapshot: (snapshot) => {
          screen.draw(snapshot);
          if (snapshot.title) this.title = snapshot.title;
          // The snapshot is at the server's size; take this pane's, telling
          // the server only when they differ (a hidden pane has none). The
          // focus stays where the person left it: a terminal takes it on request.
          this.fit();
        },
        onOutput: (bytes) => screen.write(bytes),
        onTitle: (title) => {
          this.title = title;
        },
        onExit: (code) => {
          this.state = "exited";
          this.exitCode = code;
          this.$dispatch("terminal-exited", { id: this.sessionId, code });
        },
        onClosed: () => {
          if (this.state !== "exited") this.state = "closed";
        },
      });
    },

    /** The person pressed Reconnect: the terminal takes the focus the button had. */
    reconnect() {
      connection?.close();
      this.connect();
      screen?.focus();
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
