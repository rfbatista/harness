// Composition root: the one place infrastructure is constructed and wired
// into the modules. Bundled by `go run ./cmd/webbuild`, which aliases
// "alpinejs" to web/vendor/alpine-csp.esm.js.

import Alpine from "alpinejs";

import { apiClient } from "./shared/infrastructure/api.js";
import { systemClock } from "./shared/infrastructure/clock.js";
import { feed } from "./shared/infrastructure/feed.js";
import { preferences } from "./shared/infrastructure/storage.js";
import { registerShared } from "./shared/presentation/register.js";

import { sessionsGateway } from "./modules/sessions/infrastructure/sessions-gateway.js";
import { registerSessions } from "./modules/sessions/presentation/register.js";

const api = apiClient({ base: "/api" });
const events = feed({ base: "/api" });

registerShared(Alpine, { prefs: preferences() });
registerSessions(Alpine, { gateway: sessionsGateway(api, events), clock: systemClock });

Alpine.start();
