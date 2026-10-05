// Composition root for web/dev pages: the real modules over the in-memory
// gateway, so a page runs with no server. Mirrors src/main.js; only the
// infrastructure differs. Drive it from the console via `window.harness`.

import Alpine from "alpinejs";

import { FeedStatus } from "../src/shared/domain/feed.js";
import { systemClock } from "../src/shared/infrastructure/clock.js";
import { preferences } from "../src/shared/infrastructure/storage.js";
import { registerShared } from "../src/shared/presentation/register.js";

import { memoryGateway } from "../src/modules/sessions/infrastructure/memory-gateway.js";
import { registerSessions } from "../src/modules/sessions/presentation/register.js";

// The page's seed doubles as the in-memory world; the gateway owns the wire format.
const seed = memoryGateway().gateway.decodeSeed(JSON.parse(document.getElementById("sessions-seed").textContent));
const memory = memoryGateway({ projects: [seed.projectId], sessions: seed.sessions });

registerShared(Alpine, { prefs: preferences() });
registerSessions(Alpine, { gateway: memory.gateway, clock: systemClock });

window.harness = {
  memory,
  /** Simulates what the server's feed would push. */
  finish: (id) => memory.update(id, { status: "done" }),
  ask: (id) => memory.update(id, { status: "waiting_approval", pendingApprovals: 1 }),
  drop: () => memory.feedStatus(FeedStatus.PAUSED),
  reconnect: () => memory.feedStatus(FeedStatus.RESYNCED),
};

Alpine.start();
