// Both TaskGateway implementations run the same contract.

import { apiClient } from "../../../shared/infrastructure/api.js";
import { file } from "../../../shared/testing/test.js";
import { taskGatewayContract } from "../testing/gateway-contract.js";
import { stubTasksApi } from "../testing/stub-api.js";
import { memoryTasks } from "./memory-gateway.js";
import { tasksGateway } from "./tasks-gateway.js";

file("tasks/infrastructure/gateways");

taskGatewayContract("memory", (world) => memoryTasks(world));

taskGatewayContract("http", (world) => {
  const stub = stubTasksApi(world);
  return { gateway: tasksGateway(apiClient({ base: "/api", fetch: stub.fetch })), tasks: stub.memory.tasks };
});
