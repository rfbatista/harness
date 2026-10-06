// The terminal screen, on xterm.js: the one place the browser terminal
// library is used. It draws what the server's PTY prints and hands key
// presses and pastes back; it never interprets input itself (xterm's own
// onData is ignored), so the server's emulator stays the single source of
// truth for the terminal's modes. Its colors are the design tokens, taken at
// mount and again on every theme change (screen-theme.js), never xterm's own.
//
// The viewport is xterm's: wheel, trackpad and touch scroll it, and the
// scrollbar is its own (terminal.css keeps it visible and themed while there
// is history). Scrolling never sends keys. Typing, pasting and a snapshot
// redraw bring the view back to the live screen, as terminal emulators do.

import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";

import { followTheme, screenColors, screenFont } from "./screen-theme.js";

/**
 * Lines of history the screen keeps: the server's snapshot carries up to
 * 2000 (the attach contract's bound); the rest is room for live output.
 */
const HISTORY_LINES = 5000;

/**
 * The keyboard scrolling every terminal emulator has, taken by the viewport
 * only while there is history to scroll (never on the alternate screen, so a
 * full-screen program still gets its keys). Plain PageUp/PageDown/Home/End
 * and the arrows always go to the program.
 */
const SCROLL_KEYS = {
  PageUp: (term) => term.scrollPages(-1),
  PageDown: (term) => term.scrollPages(1),
  Home: (term) => term.scrollToTop(),
  End: (term) => term.scrollToBottom(),
};

const crlf = (text) => text.replace(/\r?\n/g, "\r\n");

/** @type {import("./modules/sessions/presentation/components/terminal.js").CreateScreen} */
export function createScreen(container, { onKey, onPaste }) {
  const term = new Terminal({
    cursorBlink: true,
    fontFamily: screenFont(),
    fontSize: 13,
    lineHeight: 1.15,
    scrollback: HISTORY_LINES,
    theme: screenColors(),
  });
  // A pinned theme restored after this screen mounted, a toggle click, the OS
  // flipping: each re-themes the screen in place.
  const stopFollowing = followTheme(() => {
    term.options.theme = screenColors();
  });
  const fitter = new FitAddon();
  term.loadAddon(fitter);
  term.open(container);

  // Whether the main screen has lines scrolled off above it: the hook the
  // stylesheet uses to keep the scrollbar on. Checked once each write is parsed.
  const hasHistory = () => term.buffer.active.type === "normal" && term.buffer.active.baseY > 0;
  const markHistory = () => {
    if (hasHistory()) container.dataset.history = "";
    else delete container.dataset.history;
  };
  term.onWriteParsed(markHistory);

  const scrollKey = (event) => {
    if (!event.shiftKey || event.ctrlKey || event.altKey || event.metaKey) return false;
    const scroll = SCROLL_KEYS[event.key];
    if (!scroll || !hasHistory()) return false;
    scroll(term);
    return true;
  };

  // Keys go to the server as KeyEvents, after the view comes back to the live
  // screen. A key it does not take (Cmd shortcuts, lone modifiers) is left to
  // the browser, so copy still works.
  term.attachCustomKeyEventHandler((event) => {
    if (event.type !== "keydown") return false;
    if (scrollKey(event)) {
      event.preventDefault();
      return false;
    }
    if (onKey(event)) {
      term.scrollToBottom();
      event.preventDefault();
      return false;
    }
    return !event.metaKey;
  });

  // Pastes too: caught before xterm turns them into input.
  const paste = (event) => {
    const text = event.clipboardData?.getData("text") ?? "";
    event.preventDefault();
    event.stopImmediatePropagation();
    if (!text) return;
    term.scrollToBottom();
    onPaste(text);
  };
  container.addEventListener("paste", paste, true);

  return {
    draw(snapshot) {
      term.reset();
      term.resize(Math.max(snapshot.cols, 2), Math.max(snapshot.rows, 1));
      if (snapshot.log) {
        // An application run's output so far: replayed as printed.
        term.write(snapshot.screen);
        return;
      }
      // History first, so it rolls off the top as the screen fills; then the
      // screen as the server has it; then the cursor, relative to the screen.
      if (snapshot.scrollback) term.write(`${crlf(snapshot.scrollback)}\r\n`);
      if (snapshot.altScreen) term.write("\x1b[?1049h");
      term.write(crlf(snapshot.screen));
      term.write(`\x1b[${snapshot.cursorY + 1};${snapshot.cursorX + 1}H`);
    },
    write(bytes) {
      term.write(bytes);
    },
    fit() {
      const before = `${term.cols}x${term.rows}`;
      try {
        fitter.fit();
      } catch {
        return null; // not laid out yet
      }
      return `${term.cols}x${term.rows}` === before ? null : { cols: term.cols, rows: term.rows };
    },
    size: () => ({ cols: term.cols, rows: term.rows }),
    focus: () => term.focus(),
    dispose() {
      stopFollowing();
      container.removeEventListener("paste", paste, true);
      term.dispose();
    },
  };
}
