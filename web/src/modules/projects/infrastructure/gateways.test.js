// Both ProjectGateway implementations run the same contract.

import { apiClient } from "../../../shared/infrastructure/api.js";
import { file } from "../../../shared/testing/test.js";
import { projectGatewayContract } from "../testing/gateway-contract.js";
import { stubProjectsApi } from "../testing/stub-api.js";
import { memoryProjects } from "./memory-gateway.js";
import { projectsGateway } from "./projects-gateway.js";

file("projects/infrastructure/gateways");

projectGatewayContract("memory", (world) => memoryProjects(world));

projectGatewayContract("http", (world) => {
  const stub = stubProjectsApi(world);
  return { gateway: projectsGateway(apiClient({ base: "/api", fetch: stub.fetch })), repositories: stub.memory.repositories };
});
