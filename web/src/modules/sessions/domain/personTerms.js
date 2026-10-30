// The person's terminal dock: what it says and how tall it may be (pure; the
// component is presentation/components/personTerms.js).

export const DOCK_MIN_PX = 112; // the tab strip and four rows
const KEY_STEP = 40;
const PAGE_STEP = 200;

export const SESSION_ENDED_NOTE = "The session ended, and its resources folder with it.";
export const RESTART_NOTE = "The harness restarted; this terminal ended.";

/**
 * The strip over an ended terminal: the server's sentence, a word after it,
 * and whether a new terminal on the same folder is offered. Only the server's
 * own words are the sentence. A terminal whose root went away cannot be
 * opened again where it was.
 * @param {{ reason: string, message: string }} ended
 */
export function endedNote({ reason, message }) {
  return {
    strong: message,
    rest: "The screen below is its last.",
    tone: reason === "root_gone" ? "danger" : null,
    reopen: reason === "exited" || reason === "server_stopped" || reason === "restarted",
  };
}

const OFF = "Terminals are off";

const REFUSALS = {
  TERMINAL_SERVER_EXPOSED: [`${OFF}: the server is open beyond this computer.`, "The harness listens on a network address with no token, so anyone who can reach it could get a shell. Start it on loopback, or set a token."],
  TERMINAL_HOST_REFUSED: [`${OFF} for this address.`, "This page was reached through a host name that is not this computer (loopback). Open the harness at localhost to use terminals."],
  TERMINAL_ORIGIN_REFUSED: [`${OFF} for this page.`, "The request came from another origin than the harness's own. Reload the harness from its own address."],
  TERMINAL_READ_ONLY_UNAVAILABLE: ["A terminal in Resources cannot be made read-only here.", "This platform has no sandbox driver to deny writes, so the harness will not open a writable shell on the resources folder. A terminal in the Worktree still opens."],
  ROOT_GONE: ["That folder is no longer available.", "The root ended while you were opening the terminal."],
  PATH_NOT_FOUND: ["That folder is gone.", "It was deleted or moved. Refresh the tree to see what is there now."],
};

/** What the refusal strip says for a refused open's code; the server's words for one this page does not know. */
export function refusalCopy(code, serverMessage) {
  const [title, detail] = REFUSALS[code] ?? ["Cannot open a terminal here.", serverMessage];
  return { title, detail };
}

/** A height in px the dock may take, in a tab `max` px tall. */
export function clampDock(px, max) {
  const top = Math.max(1, max);
  if (!Number.isFinite(px)) return Math.min(DOCK_MIN_PX, top);
  return Math.min(Math.max(px, DOCK_MIN_PX), top);
}

/** The height a key on the handle asks for, or null when the key is not the handle's. Up grows the dock. */
export function nextDockHeight(key, current, max) {
  switch (key) {
    case "ArrowUp": return clampDock(current + KEY_STEP, max);
    case "ArrowDown": return clampDock(current - KEY_STEP, max);
    case "PageUp": return clampDock(current + PAGE_STEP, max);
    case "PageDown": return clampDock(current - PAGE_STEP, max);
    case "Home": return clampDock(0, max);
    case "End": return clampDock(max, max);
    default: return null;
  }
}
