// Composition root for web/dev pages: the real modules over the in-memory
// gateway, so a page runs with no server. Mirrors src/main.js; only the
// infrastructure differs. Drive it from the console via `window.harness`.

import Alpine from "alpinejs";

import { FeedStatus } from "../src/shared/domain/feed.js";
import { systemClock } from "../src/shared/infrastructure/clock.js";
import { preferences } from "../src/shared/infrastructure/storage.js";
import { registerShared } from "../src/shared/presentation/register.js";

import { memoryGateway } from "../src/modules/sessions/infrastructure/memory-gateway.js";
import { memoryTerminals } from "../src/modules/sessions/infrastructure/memory-terminals.js";
import { registerSessions } from "../src/modules/sessions/presentation/register.js";
import { createScreen } from "../src/xterm-screen.js";

// The page's seed doubles as the in-memory world; the gateway owns the wire format.
const seed = memoryGateway().gateway.decodeSeed(JSON.parse(document.getElementById("sessions-seed").textContent));
const memory = memoryGateway({ projects: [seed.projectId], sessions: seed.sessions });

registerShared(Alpine, { prefs: preferences() });
// A terminal that greets and echoes what you type, in place of the server's PTY.
const terminals = memoryTerminals({ greeting: "dev terminal: type, and it echoes back" });
registerSessions(Alpine, { gateway: memory.gateway, terminals: terminals.gateway, createScreen, clock: systemClock });

window.harness = {
  memory,
  terminals,
  /** Simulates what the server's feed would push. */
  finish: (id) => memory.update(id, { status: "done" }),
  ask: (id) => memory.update(id, { status: "waiting_approval", pendingApprovals: 1 }),
  drop: () => memory.feedStatus(FeedStatus.PAUSED),
  reconnect: () => memory.feedStatus(FeedStatus.RESYNCED),
};

Alpine.start();
