// A stand-in for the harness server's token saver routes, backed by the
// memory gateway: a fetch that speaks the server's wire format. The real
// tokenSaversGateway runs the contract suite against it. It records every
// request so tests can check what was sent.

import { codeOf } from "../../../shared/domain/errors.js";
import { jsonResponse } from "../../../shared/testing/doubles.js";
import { memoryTokenSavers } from "../infrastructure/memory-gateway.js";
import { activationsDTO, indexDTO, saverDTO, saverListDTO } from "./fixtures.js";

const INTENSITY_KEY = "token_savers.caveman.intensity";

export function stubTokenSaversApi(world) {
  const memory = memoryTokenSavers(world);
  const { gateway } = memory;
  /** Every request: [method, path, body]. */
  const requests = [];

  const routes = {
    "POST /api/set_token_saver_activation": async (b) => activationsDTO(await gateway.setActivation(b.level, b.owner_id, b.saver, b.value)),
    "GET /api/list_token_savers": async () => saverListDTO(await gateway.listSavers()),
    "POST /api/install_token_saver": async (b) => ({ token_saver: saverDTO(await gateway.installSaver(b.saver)) }),
    "POST /api/remove_token_saver": async (b) => ({ token_saver: saverDTO(await gateway.removeSaver(b.saver)) }),
    "GET /api/list_repository_indexes": async () => ({ repository_indexes: (await gateway.listIndexes()).map(indexDTO) }),
    "POST /api/rebuild_repository_index": async (b) => ({ repository_index: indexDTO(await gateway.rebuildIndex(b.repository_id)) }),
    "POST /api/update_settings": async (b) => ({ settings: { [INTENSITY_KEY]: await gateway.setCavemanIntensity(b.settings?.[INTENSITY_KEY]) } }),
  };

  async function fetch(input, init = {}) {
    const url = new URL(input, "http://harness.test");
    const method = init.method ?? "GET";
    const body = init.body ? JSON.parse(init.body) : {};
    requests.push([method, url.pathname, body]);
    const route = routes[`${method} ${url.pathname}`];
    if (!route) return jsonResponse(404, { error: `no route ${method} ${url.pathname}` });
    try {
      return jsonResponse(200, await route(body));
    } catch (err) {
      return jsonResponse(err.status ?? 500, { error: err.message, code: codeOf(err) });
    }
  }

  return { fetch, requests, memory };
}
