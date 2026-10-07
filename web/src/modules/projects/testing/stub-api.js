// A stand-in for the server's project and repository routes and the project
// catalog feed, backed by the memory gateway and speaking the real wire
// format, so the real gateway runs the contract suite against it.

import { codeOf } from "../../../shared/domain/errors.js";
import { jsonResponse } from "../../../shared/testing/doubles.js";
import { memoryProjects } from "../infrastructure/memory-gateway.js";
import { projectDTO, repositoryDTO, summaryDTO } from "./fixtures.js";

const STATUS = {
  PROJECT_NOT_FOUND: 404,
  REPOSITORY_NOT_FOUND: 404,
  ENV_FILE_NOT_FOUND: 404,
  PROJECT_NAME_TAKEN: 409,
  PROJECT_ROOT_INVALID: 400,
  PROJECT_HAS_RUNNING_SESSIONS: 409,
  INVALID_INPUT: 400,
  INVALID_ROOT: 400,
  INVALID_URL: 400,
  INVALID_PATH: 400,
  ENV_FILE_TOO_LARGE: 400,
};
const envDTO = (f) => ({ path: f.path, content: f.content, updated_at: f.updatedAt?.toISOString() });

export function stubProjectsApi(world) {
  const memory = memoryProjects(world);
  const { gateway } = memory;
  const sources = new Set();

  async function fetch(input, init = {}) {
    const url = new URL(input, "http://harness.test");
    const body = init.body ? JSON.parse(init.body) : {};
    try {
      switch (`${init.method ?? "GET"} ${url.pathname}`) {
        case "GET /api/find_repositories": {
          const { root, found } = await gateway.findRepositories(url.searchParams.get("root_dir") ?? "");
          return jsonResponse(200, { root, repositories: found.map((f) => ({ path: f.path, name: f.name, remote: f.remote || undefined })) });
        }
        case "POST /api/create_project":
          return jsonResponse(200, { project: projectDTO(await gateway.createProject({ name: body.name, rootDir: body.root_dir })) });
        case "GET /api/list_project_summaries":
          return jsonResponse(200, { summaries: (await gateway.listProjectSummaries()).map(summaryDTO) });
        case "POST /api/update_project":
          return jsonResponse(200, {
            project: projectDTO(await gateway.updateProject({ projectId: body.project_id, name: body.name, rootDir: body.root_dir })),
          });
        case "POST /api/delete_project":
          await gateway.deleteProject(body.project_id);
          return jsonResponse(200, {});
        case "POST /api/add_ignored_path":
          return jsonResponse(200, { project: projectDTO(await gateway.addIgnoredPath(body.project_id, body.path)) });
        case "POST /api/remove_ignored_path":
          return jsonResponse(200, { project: projectDTO(await gateway.removeIgnoredPath(body.project_id, body.path)) });
        case "POST /api/create_repository": {
          const r = await gateway.addRepository({
            projectId: body.project_id,
            name: body.name,
            description: body.description,
            url: body.url,
            rootDir: body.root_dir,
          });
          return jsonResponse(200, { repository: repositoryDTO(r) });
        }
        case "POST /api/save_repository_env_file":
          return jsonResponse(200, { env_file: envDTO(await gateway.saveEnvFile(body.repository_id, body.path, body.content)) });
        case "POST /api/delete_repository_env_file":
          await gateway.deleteEnvFile(body.repository_id, body.path);
          return new Response(null, { status: 204 });
        case "POST /api/import_repository_env_file":
          return jsonResponse(200, { env_file: envDTO(await gateway.importEnvFile(body.repository_id, body.path)) });
        case "POST /api/delete_repository":
          await gateway.removeRepository(body.repository_id);
          return new Response(null, { status: 204 });
        default:
          return jsonResponse(404, { error: `no route ${url.pathname}` });
      }
    } catch (err) {
      return jsonResponse(STATUS[codeOf(err)] ?? 500, errorBody(err));
    }
  }

  // GET /api/project_events: one change per message, as the server sends it.
  class StubEventSource {
    constructor(input) {
      this.url = new URL(input, "http://harness.test");
      this.onopen = null;
      this.onmessage = null;
      this.onerror = null;
      sources.add(this);
      this.unfollow =
        this.url.pathname === "/api/project_events"
          ? gateway.followCatalog((c) => this.send(c.kind === "deleted" ? { project: { id: c.id }, deleted: true } : { project: projectDTO(c.project) }))
          : () => {};
      queueMicrotask(() => this.onopen?.({}));
    }
    send(dto) {
      this.onmessage?.({ data: typeof dto === "string" ? dto : JSON.stringify(dto) });
    }
    close() {
      sources.delete(this);
      this.unfollow();
    }
  }

  return {
    fetch,
    EventSource: StubEventSource,
    memory,
    pushRaw(dto) {
      for (const s of sources) s.send(dto);
    },
  };
}

/** The server's flat error envelope; details only on the codes that carry them. */
function errorBody(err) {
  const body = { error: err.message, code: codeOf(err) };
  if (codeOf(err) === "PROJECT_HAS_RUNNING_SESSIONS") {
    body.details = { sessions: err.details.sessions.map((s) => ({ id: s.id, ticket_id: s.ticketId, agent: s.agent })) };
  }
  return body;
}
