// Composition root for web/dev pages: the real modules over the in-memory
// gateway, so a page runs with no server. Mirrors src/main.js; only the
// infrastructure differs. Drive it from the console via `window.harness`.

import Alpine from "alpinejs";

import { FeedStatus } from "../src/shared/domain/feed.js";
import { systemClock } from "../src/shared/infrastructure/clock.js";
import { preferences } from "../src/shared/infrastructure/storage.js";
import { registerShared } from "../src/shared/presentation/register.js";

import { memoryProjects } from "../src/modules/projects/infrastructure/memory-gateway.js";
import { registerProjects } from "../src/modules/projects/presentation/register.js";
import { memoryArtifacts } from "../src/modules/sessions/infrastructure/memory-artifacts.js";
import { memoryChannel } from "../src/modules/sessions/infrastructure/memory-channel.js";
import { memoryGateway } from "../src/modules/sessions/infrastructure/memory-gateway.js";
import { memoryTerminals } from "../src/modules/sessions/infrastructure/memory-terminals.js";
import { registerSessions } from "../src/modules/sessions/presentation/register.js";
import { createScreen } from "../src/xterm-screen.js";

registerShared(Alpine, { prefs: preferences() });

const seedOf = (id) => {
  const el = document.getElementById(id);
  return el ? JSON.parse(el.textContent) : null;
};

if (seedOf("sessions-seed")) startTaskPage(seedOf("sessions-seed"));
else startProjectPages();

/** web/dev/task.html: one task's sessions. */
function startTaskPage(rawSeed) {
  // The page's seed doubles as the in-memory world; the gateway owns the wire format.
  const seed = memoryGateway().gateway.decodeSeed(rawSeed);
  const memory = memoryGateway({ projects: [seed.projectId], sessions: seed.sessions });

  // A terminal that greets and echoes what you type, in place of the server's
  // PTY, with history above the screen as the server's snapshot would carry it.
  const terminals = memoryTerminals({
    greeting: "dev terminal: type, and it echoes back",
    history: Array.from({ length: 60 }, (_, i) => `\x1b[2m${String(i + 1).padStart(2)}\x1b[0m earlier output, line ${i + 1}`),
  });
  const artifacts = memoryArtifacts({ tickets: [{ id: seed.ticketId, projectId: seed.projectId }] });
  const channel = memoryChannel({
    projectId: seed.projectId,
    checks: seed.sessions.flatMap((s) => (s.statusCheck ? [s.statusCheck] : [])),
    onCheck: (check) => memory.update(check.delegateSessionId, { statusCheck: check }),
  });
  registerSessions(Alpine, { gateway: memory.gateway, artifacts: artifacts.gateway, channel: channel.gateway, terminals: terminals.gateway, createScreen, clock: systemClock });

  window.harness = {
    memory,
    terminals,
    /** Simulates what the server's feed would push. */
    finish: (id) => memory.update(id, { status: "done" }),
    ask: (id) => memory.update(id, { status: "waiting_approval", pendingApprovals: 1 }),
    drop: () => memory.feedStatus(FeedStatus.PAUSED),
    reconnect: () => memory.feedStatus(FeedStatus.RESYNCED),
    artifacts,
    channel,
    /** A delegate reports to the architect, as message_architect would: harness.report("s-port", "s-arch", "ready_for_review"). */
    report: (from, to, status = "working", body = "From the console.") =>
      channel.send({ taskId: seed.ticketId, fromSessionId: from, toSessionId: to, kind: "status_report", status, body }),
    /** The architect answers a delegate, as reply_to_session would. */
    reply: (from, to, body = "Go on.", verdict = "") => channel.send({ taskId: seed.ticketId, fromSessionId: from, toSessionId: to, kind: "reply", body, verdict }),
    /** A delegate's status check fires, as the server's scheduler would. */
    fireCheck: (id) => channel.fire(id),
    /** Publishes a page into a session, as the agent's publish_artifact tool would. */
    publish: (id, title = "Hero", path = "hero.html") =>
      artifacts.publish({ sessionId: id, kind: "page", title, note: "from the console", path, mime: "text/html", sizeBytes: 1 }),
    /** Moves an artifact to the project, as move_artifact_to_project would. */
    promote: (artifactId) => artifacts.gateway.setScope(artifactId, "project"),
    /** Attaches a project asset to a task, as attach_artifact_to_task would: harness.attach("art-1", "t2"). */
    attach: (artifactId, ticketId) => artifacts.gateway.attach(artifactId, ticketId),
    /** Detaches it, as detach_artifact_from_task would. */
    detach: (artifactId, ticketId) => artifacts.gateway.detach(artifactId, ticketId),
  };
}

/**
 * web/dev/projects.html and settings.html: the projects list and a project's
 * settings, over one in-memory catalog built from the page's seed.
 */
function startProjectPages() {
  const home = "/home/you";
  const list = seedOf("projects-seed");
  const settings = seedOf("settings-seed");
  const decoder = memoryProjects().gateway;
  const projects = list
    ? decoder.decodeProjectsSeed(list).map((s) => s.project)
    : [decoder.decodeSettingsSeed(settings).project, { id: "p1", name: "coding_pool", rootDir: `${home}/src/coding_pool`, ignoredPaths: [] }];
  const memory = memoryProjects({
    projects,
    home,
    dirs: ["/tmp", `${home}/src`, ...projects.map((p) => p.rootDir)],
    sessions: [
      { id: "s-port-1234", projectId: "p2", ticketId: "t1", agent: "go-developer", status: "running", lastActivityAt: new Date() },
      { id: "s-free-5678", projectId: "p2", ticketId: "", agent: "", status: "waiting_approval", lastActivityAt: new Date() },
    ],
  });
  registerProjects(Alpine, { gateway: memory.gateway, navigate: (url) => console.info("navigate", url), clock: systemClock });

  window.harness = {
    memory,
    /** What another tab, an agent or the TUI would do. */
    create: (name, rootDir = "/tmp") => memory.gateway.createProject({ name, rootDir }),
    rename: (id, name) => memory.gateway.updateProject({ projectId: id, name }),
    remove: (id) => memory.gateway.deleteProject(id),
    /** Sessions keep a project from being deleted until they end: harness.stop("s-port-1234"). */
    stop: (id) => memory.endSession(id),
    drop: () => memory.reportStatus(FeedStatus.PAUSED),
    reconnect: () => memory.reportStatus(FeedStatus.RESYNCED),
  };
}

Alpine.start();
