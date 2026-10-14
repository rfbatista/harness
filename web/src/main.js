// Composition root: the one place infrastructure is constructed and wired
// into the modules. Bundled by `go run ./cmd/webbuild`, which resolves the
// bare imports (Alpine, xterm.js) to web/vendor and htmx to web/third_party.

import Alpine from "alpinejs";
import htmx from "htmx.org";

import { apiClient } from "./shared/infrastructure/api.js";
import { systemClock } from "./shared/infrastructure/clock.js";
import { feed } from "./shared/infrastructure/feed.js";
import { xhrUpload } from "./shared/infrastructure/upload.js";
import { preferences } from "./shared/infrastructure/storage.js";
import { registerShared } from "./shared/presentation/register.js";

import { desktopAlerts } from "./modules/notifications/infrastructure/desktop-alerts.js";
import { notificationsGateway } from "./modules/notifications/infrastructure/notifications-gateway.js";
import { registerNotifications } from "./modules/notifications/presentation/register.js";
import { codeGateway } from "./modules/code/infrastructure/code-gateway.js";
import { credentialsGateway } from "./modules/credentials/infrastructure/credentials-gateway.js";
import { registerCredentials } from "./modules/credentials/presentation/register.js";
import { hljsHighlighter } from "./modules/code/infrastructure/hljs-highlighter.js";
import { registerCode } from "./modules/code/presentation/register.js";
import { runsGateway } from "./modules/runs/infrastructure/runs-gateway.js";
import { reviewsGateway } from "./modules/reviews/infrastructure/reviews-gateway.js";
import { registerReviews } from "./modules/reviews/presentation/register.js";
import { registerRuns } from "./modules/runs/presentation/register.js";
import { projectsGateway } from "./modules/projects/infrastructure/projects-gateway.js";
import { pullRequestsGateway } from "./modules/pullrequests/infrastructure/pullrequests-gateway.js";
import { registerPullRequests } from "./modules/pullrequests/presentation/register.js";
import { registerProjects } from "./modules/projects/presentation/register.js";
import { artifactsGateway } from "./modules/sessions/infrastructure/artifacts-gateway.js";
import { channelGateway } from "./modules/sessions/infrastructure/channel-gateway.js";
import { sessionsGateway } from "./modules/sessions/infrastructure/sessions-gateway.js";
import { terminalGateway } from "./modules/sessions/infrastructure/terminal-gateway.js";
import { transcriptGateway } from "./modules/sessions/infrastructure/transcript-gateway.js";
import { registerSessions } from "./modules/sessions/presentation/register.js";
import { railGateway } from "./modules/tasks/infrastructure/rail-gateway.js";
import { cookieRailPrefs } from "./modules/tasks/infrastructure/rail-prefs.js";
import { resourcesGateway } from "./modules/tasks/infrastructure/resources-gateway.js";
import { tasksGateway } from "./modules/tasks/infrastructure/tasks-gateway.js";
import { registerTasks } from "./modules/tasks/presentation/register.js";
import { usageGateway } from "./modules/usage/infrastructure/usage-gateway.js";
import { registerUsage } from "./modules/usage/presentation/register.js";
import { createHljs } from "./highlight.js";
import { createScreen } from "./xterm-screen.js";
import { startSwapRegions } from "./swap-regions.js";

const api = apiClient({ base: "/api" });
const events = feed({ base: "/api" });

const prefs = preferences();
registerShared(Alpine, { prefs });
const navigate = (url) => window.location.assign(url);
const reload = () => window.location.reload();
startSwapRegions(htmx, { navigate });

registerProjects(Alpine, { gateway: projectsGateway(api, events), navigate, clock: systemClock });
registerTasks(Alpine, {
  gateway: tasksGateway(api),
  rail: railGateway(api, events),
  railPrefs: cookieRailPrefs(),
  resources: resourcesGateway(api, events, xhrUpload({ base: "/api" })),
  navigate,
  reload,
  clock: systemClock,
});
registerSessions(Alpine, {
  gateway: sessionsGateway(api, events),
  artifacts: artifactsGateway(api, events),
  channel: channelGateway(api, events),
  terminals: terminalGateway({ base: "/api" }),
  transcripts: transcriptGateway(api),
  runTerminals: terminalGateway({ base: "/api", path: (id) => `/runs/${encodeURIComponent(id)}/terminal` }),
  createScreen,
  clock: systemClock,
  prefs,
});

registerRuns(Alpine, { gateway: runsGateway(api) });
registerReviews(Alpine, { gateway: reviewsGateway(api, events), clock: systemClock });
registerPullRequests(Alpine, { gateway: pullRequestsGateway(api, events) });
registerCode(Alpine, { gateway: codeGateway(api, events), highlighter: hljsHighlighter(createHljs()), prefs, navigate });
registerNotifications(Alpine, { gateway: notificationsGateway(api, events), alerts: desktopAlerts(), prefs, clock: systemClock, navigate });
registerCredentials(Alpine, { gateway: credentialsGateway(api, events), clock: systemClock });
registerUsage(Alpine, { gateway: usageGateway(api, events) });

Alpine.start();
