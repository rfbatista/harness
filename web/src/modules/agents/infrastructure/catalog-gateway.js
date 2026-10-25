// CatalogGateway over the harness HTTP API (contract sections 3 and 4).

import {
  agentBody, bundleBody, mcpServerBody, serverFields, skillBody,
  toCatalog, toProbe, toServerNames, toSkillDetail, toWriteResult, writeFields,
} from "./dto.js";

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @returns {import("../domain/ports.js").CatalogGateway}
 */
export function catalogGateway(api) {
  const write = async (verb, body, options) => toWriteResult(await api.post(`/${verb}`, { ...body, ...writeFields(options) }));
  return {
    decodeCatalog: toCatalog,
    async getCatalog() {
      return toCatalog(await api.get("/get_agent_catalog"));
    },
    async getSkill(name) {
      return toSkillDetail(await api.get("/get_catalog_skill", { name }));
    },
    async checkEnvVars(names) {
      const body = await api.get("/check_env_vars", { names: names.join(",") });
      return Object.fromEntries(Object.entries(body ?? {}).map(([k, v]) => [k, v === true]));
    },
    saveAgent: (input, options) => write("save_catalog_agent", agentBody(input), options),
    deleteAgent: (name, options) => write("delete_catalog_agent", { name }, options),
    saveBundle: (input, options) => write("save_catalog_bundle", bundleBody(input), options),
    deleteBundle: (name, options) => write("delete_catalog_bundle", { name }, options),
    saveSkill: (input, options) => write("save_catalog_skill", skillBody(input), options),
    deleteSkill: (name, options) => write("delete_catalog_skill", { name }, options),
    saveMcpServer: (input, options) => write("save_catalog_mcp_server", mcpServerBody(input), options),
    deleteMcpServer: (name, options) => write("delete_catalog_mcp_server", { name }, options),
    async undoChange(changeId) {
      return toWriteResult(await api.post("/undo_catalog_change", { change_id: changeId }));
    },
    async testMcpServer(server) {
      return toProbe(await api.post("/test_catalog_mcp_server", { server: serverFields(server) }));
    },
    async getProjectMcpServers(projectId) {
      return toServerNames(await api.get("/get_project_mcp_servers", { project_id: projectId }));
    },
    async setProjectMcpServers(projectId, names) {
      return toServerNames(await api.post("/set_project_mcp_servers", { project_id: projectId, mcp_servers: names }));
    },
  };
}
