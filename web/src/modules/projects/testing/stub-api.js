// A stand-in for the server's project and repository routes, backed by the
// memory gateway and speaking the real wire format, so the real gateway runs
// the contract suite against it.

import { codeOf } from "../../../shared/domain/errors.js";
import { jsonResponse } from "../../../shared/testing/doubles.js";
import { memoryProjects } from "../infrastructure/memory-gateway.js";
import { repositoryDTO } from "./fixtures.js";

const STATUS = {
  PROJECT_NOT_FOUND: 404,
  REPOSITORY_NOT_FOUND: 404,
  ENV_FILE_NOT_FOUND: 404,
  INVALID_ROOT: 400,
  INVALID_URL: 400,
  INVALID_PATH: 400,
  ENV_FILE_TOO_LARGE: 400,
};
const envDTO = (f) => ({ path: f.path, content: f.content, updated_at: f.updatedAt?.toISOString() });

export function stubProjectsApi(world) {
  const memory = memoryProjects(world);
  const { gateway } = memory;

  async function fetch(input, init = {}) {
    const url = new URL(input, "http://harness.test");
    const body = init.body ? JSON.parse(init.body) : {};
    try {
      switch (`${init.method ?? "GET"} ${url.pathname}`) {
        case "GET /api/find_repositories": {
          const { root, found } = await gateway.findRepositories(url.searchParams.get("root_dir") ?? "");
          return jsonResponse(200, { root, repositories: found.map((f) => ({ path: f.path, name: f.name, remote: f.remote || undefined })) });
        }
        case "POST /api/create_project": {
          const p = await gateway.createProject({ name: body.name, rootDir: body.root_dir });
          return jsonResponse(200, { project: { id: p.id, name: p.name, root_dir: p.rootDir } });
        }
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
      return jsonResponse(STATUS[codeOf(err)] ?? 500, { error: err.message, code: codeOf(err) });
    }
  }

  return { fetch, memory };
}
