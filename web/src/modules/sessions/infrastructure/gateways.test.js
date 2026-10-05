// Both SessionGateway implementations run the same contract.

import { apiClient } from "../../../shared/infrastructure/api.js";
import { feed } from "../../../shared/infrastructure/feed.js";
import { file } from "../../../shared/testing/test.js";
import { makeSession } from "../testing/fixtures.js";
import { sessionGatewayContract } from "../testing/gateway-contract.js";
import { stubApi } from "../testing/stub-api.js";
import { memoryGateway } from "./memory-gateway.js";
import { sessionsGateway } from "./sessions-gateway.js";

file("sessions/infrastructure/gateways");

sessionGatewayContract(
  "memory",
  (world) => {
    const memory = memoryGateway(world);
    return { gateway: memory.gateway, upsert: memory.upsert };
  },
  makeSession,
);

sessionGatewayContract(
  "http",
  (world) => {
    const stub = stubApi(world);
    const gateway = sessionsGateway(
      apiClient({ base: "/api", fetch: stub.fetch }),
      feed({ base: "/api", EventSource: stub.EventSource }),
    );
    return { gateway, upsert: stub.memory.upsert };
  },
  makeSession,
);
