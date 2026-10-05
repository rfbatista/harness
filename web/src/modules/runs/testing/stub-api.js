// A stand-in for the server's run routes, backed by the memory gateway and
// speaking the real wire format.

import { codeOf } from "../../../shared/domain/errors.js";
import { jsonResponse } from "../../../shared/testing/doubles.js";
import { memoryRuns } from "../infrastructure/memory-gateway.js";

const STATUS = { INVALID_NAME: 400, RUN_NOT_FOUND: 404, RUN_COMMAND_NOT_FOUND: 404, SESSION_NOT_FOUND: 404, INVALID_INPUT: 400 };
const runDTO = (r) => ({
  id: r.id, session_id: r.sessionId, name: r.name, command: r.command, status: r.status,
  exit_code: r.exitCode, started_at: r.startedAt.toISOString(),
});

export function stubRunsApi(world) {
  const memory = memoryRuns(world);
  const { gateway } = memory;
  async function fetch(input, init = {}) {
    const url = new URL(input, "http://harness.test");
    const body = init.body ? JSON.parse(init.body) : {};
    try {
      switch (`${init.method ?? "GET"} ${url.pathname}`) {
        case "GET /api/list_repository_run_commands":
          return jsonResponse(200, { run_commands: await gateway.listCommands(url.searchParams.get("repository_id")) });
        case "POST /api/save_repository_run_command":
          return jsonResponse(200, { run_command: await gateway.saveCommand(body.repository_id, body.name, body.command) });
        case "POST /api/delete_repository_run_command":
          await gateway.deleteCommand(body.repository_id, body.name);
          return new Response(null, { status: 204 });
        case "GET /api/list_runs":
          return jsonResponse(200, { runs: (await gateway.listRuns(url.searchParams.get("session_id"))).map(runDTO) });
        case "POST /api/start_run":
          return jsonResponse(200, { run: runDTO(await gateway.start(body.session_id, { name: body.name, command: body.command })) });
        case "POST /api/stop_run":
          return jsonResponse(200, { run: runDTO(await gateway.stop(body.run_id)) });
        default:
          return jsonResponse(404, { error: `no route ${url.pathname}` });
      }
    } catch (err) {
      return jsonResponse(STATUS[codeOf(err)] ?? 500, { error: err.message, code: codeOf(err) });
    }
  }
  return { fetch, memory };
}
