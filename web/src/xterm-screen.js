// The terminal screen, on xterm.js: the one place the browser terminal
// library is used. It draws what the server's PTY prints and hands key
// presses and pastes back; it never interprets input itself (xterm's own
// onData is ignored), so the server's emulator stays the single source of
// truth for the terminal's modes. Its colors are the design tokens, taken at
// mount and again on every theme change (screen-theme.js), never xterm's own.
//
// The viewport is xterm's: wheel, trackpad and touch scroll it, and the
// scrollbar is its own (terminal.css keeps it visible and themed while there
// is history). Typing, pasting and a snapshot redraw bring the view back to
// the live screen, as terminal emulators do. Scrolling sends no keys, except
// to a full-screen program that turned on mouse reporting (claude does): the
// alternate screen has no history to scroll, the program scrolls its own
// view, and the socket carries keys, not mouse reports, so the wheel and
// touch become PageUp/PageDown (wheel-pages.js).

import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";

import { followTheme, screenColors, screenFont } from "./screen-theme.js";
import { wheelPages } from "./wheel-pages.js";

/**
 * Lines of history the screen keeps: the server's snapshot carries up to
 * 10,000 (the agent session history contract, H1/H4); the rest is room for
 * live output.
 */
const HISTORY_LINES = 12_000;

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

const FONT_SIZE = 13;

/**
 * Calls onChange when text in the screen's font changes size: the font
 * arriving after xterm measured its cells with a fallback (a local font is
 * not ready on first use). xterm re-measures only on a resize or a font
 * option change, so without this it would fit with the fallback's narrower,
 * shorter cells and clip the last row and columns. A hidden sample of the
 * font, watched for size, is the signal.
 */
function watchFont(parent, font, onChange) {
  if (typeof ResizeObserver === "undefined") return () => {};
  const sample = document.createElement("span");
  sample.setAttribute("aria-hidden", "true");
  // Fixed and short, so it never makes the page or a pane scroll.
  sample.style.cssText = `position: fixed; inset: 0 auto auto 0; visibility: hidden; pointer-events: none; display: inline-block; white-space: pre; font: ${FONT_SIZE}px ${font};`;
  sample.textContent = "W".repeat(10);
  parent.append(sample);
  let last = "";
  // The first size counts too: the font may have arrived between xterm's
  // measure and this observer's first report.
  const observer = new ResizeObserver(() => {
    const size = `${sample.offsetWidth}x${sample.offsetHeight}`;
    if (size === "0x0" || size === last) return;
    last = size;
    onChange();
  });
  observer.observe(sample);
  return () => {
    observer.disconnect();
    sample.remove();
  };
}

/** @type {import("./modules/sessions/presentation/components/terminal.js").CreateScreen} */
export function createScreen(container, { onKey, onPaste, onPageKey, onCellSize, onScrollState, fontSize = FONT_SIZE }) {
  const font = screenFont();
  const term = new Terminal({
    cursorBlink: true,
    fontFamily: font,
    fontSize,
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
  // xterm opens in an unpadded host filling the screen's content box: the fit
  // addon measures its parent's box with the padding in (border-box), so a
  // padded parent would get a row and columns more than it can show.
  const host = document.createElement("div");
  host.className = "host";
  container.append(host);
  term.open(host);
  const stopWatchingFont = watchFont(container, font, () => {
    // Setting the option again (it must differ to count) makes xterm re-measure.
    term.options.fontFamily = `${font}, monospace`;
    term.options.fontFamily = font;
    onCellSize?.();
  });

  // Whether the main screen has lines scrolled off above it: the hook the
  // stylesheet uses to keep the scrollbar on. Checked once each write is parsed.
  const hasHistory = () => term.buffer.active.type === "normal" && term.buffer.active.baseY > 0;
  const markHistory = () => {
    if (hasHistory()) container.dataset.history = "";
    else delete container.dataset.history;
  };
  term.onWriteParsed(markHistory);

  // Scrolled up off the live screen, and how many lines arrived below since:
  // the tab row's Jump to latest. Reported only when it changes.
  let scrolledUp = false;
  let newLines = 0;
  const reportScroll = () => {
    const buffer = term.buffer.active;
    const up = buffer.viewportY < buffer.baseY;
    if (up === scrolledUp) return;
    scrolledUp = up;
    newLines = 0;
    onScrollState?.({ scrolledUp, newLines });
  };
  term.onRender(reportScroll);
  term.onLineFeed(() => {
    if (!scrolledUp) return;
    newLines++;
    onScrollState?.({ scrolledUp, newLines });
  });

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

  // A full-screen program that asked for the mouse scrolls its own view: the
  // wheel and touch reach it as page keys. Anywhere else xterm scrolls its
  // viewport as usual (a program on the alternate screen that did not ask
  // gets nothing it did not type).
  const pagesToProgram = () => term.buffer.active.type === "alternate" && term.modes.mouseTrackingMode !== "none";
  const pager = wheelPages({
    step: () => Math.max(host.clientHeight / 2, 40),
    line: () => (host.clientHeight || 1) / Math.max(term.rows, 1),
  });
  const page = (key) => {
    if (key) onPageKey?.(key);
  };
  term.attachCustomWheelEventHandler((event) => {
    if (!pagesToProgram()) {
      pager.reset();
      return true;
    }
    event.preventDefault();
    page(pager.add(event.deltaY, event.deltaMode));
    return false;
  });
  let touchY = null;
  const touchStart = (event) => {
    touchY = event.touches.length === 1 ? event.touches[0].clientY : null;
    pager.reset();
  };
  const touchMove = (event) => {
    if (touchY === null || event.touches.length !== 1 || !pagesToProgram()) return;
    const y = event.touches[0].clientY;
    // A finger moving up shows what is below: PageDown.
    page(pager.add(touchY - y));
    touchY = y;
  };
  container.addEventListener("touchstart", touchStart, { passive: true });
  container.addEventListener("touchmove", touchMove, { passive: true });

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
      // A hidden pane (another column on a narrow screen) has no size to send.
      if (host.clientWidth === 0 || host.clientHeight === 0) return null;
      const before = `${term.cols}x${term.rows}`;
      try {
        fitter.fit();
      } catch {
        return null; // not laid out yet
      }
      return `${term.cols}x${term.rows}` === before ? null : { cols: term.cols, rows: term.rows };
    },
    focus: () => term.focus(),
    setFontSize(px) {
      term.options.fontSize = px;
    },
    scrollToBottom: () => term.scrollToBottom(),
    // An ended terminal is read-only: no cursor inviting input. xterm's cursor
    // element differs by renderer, so the terminal is told, not the stylesheet.
    hideCursor: () => term.write("\x1b[?25l"),
    dispose() {
      stopFollowing();
      stopWatchingFont();
      container.removeEventListener("paste", paste, true);
      container.removeEventListener("touchstart", touchStart);
      container.removeEventListener("touchmove", touchMove);
      term.dispose();
      host.remove();
    },
  };
}
