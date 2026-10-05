// Both RunGateway implementations run the same contract.

import { apiClient } from "../../../shared/infrastructure/api.js";
import { file } from "../../../shared/testing/test.js";
import { runGatewayContract } from "../testing/gateway-contract.js";
import { stubRunsApi } from "../testing/stub-api.js";
import { memoryRuns } from "./memory-gateway.js";
import { runsGateway } from "./runs-gateway.js";

file("runs/infrastructure/gateways");

runGatewayContract("memory", (world) => memoryRuns(world));

runGatewayContract("http", (world) => {
  const stub = stubRunsApi(world);
  return { gateway: runsGateway(apiClient({ base: "/api", fetch: stub.fetch })), exit: stub.memory.exit };
});
