// Composition root: the one place infrastructure is constructed and wired
// into the modules. Bundled by `go run ./cmd/webbuild`, which resolves the
// bare imports (Alpine, xterm.js) to web/vendor.

import Alpine from "alpinejs";

import { apiClient } from "./shared/infrastructure/api.js";
import { systemClock } from "./shared/infrastructure/clock.js";
import { feed } from "./shared/infrastructure/feed.js";
import { preferences } from "./shared/infrastructure/storage.js";
import { registerShared } from "./shared/presentation/register.js";

import { projectsGateway } from "./modules/projects/infrastructure/projects-gateway.js";
import { registerProjects } from "./modules/projects/presentation/register.js";
import { sessionsGateway } from "./modules/sessions/infrastructure/sessions-gateway.js";
import { terminalGateway } from "./modules/sessions/infrastructure/terminal-gateway.js";
import { registerSessions } from "./modules/sessions/presentation/register.js";
import { tasksGateway } from "./modules/tasks/infrastructure/tasks-gateway.js";
import { registerTasks } from "./modules/tasks/presentation/register.js";
import { createScreen } from "./xterm-screen.js";

const api = apiClient({ base: "/api" });
const events = feed({ base: "/api" });

registerShared(Alpine, { prefs: preferences() });
const navigate = (url) => window.location.assign(url);
const reload = () => window.location.reload();

registerProjects(Alpine, { gateway: projectsGateway(api), navigate });
registerTasks(Alpine, { gateway: tasksGateway(api), navigate, reload });
registerSessions(Alpine, {
  gateway: sessionsGateway(api, events),
  terminals: terminalGateway({ base: "/api" }),
  createScreen,
  clock: systemClock,
});

Alpine.start();
