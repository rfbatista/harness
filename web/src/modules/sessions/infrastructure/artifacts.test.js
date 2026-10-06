// Both ArtifactGateway implementations run the same contract.

import { file } from "../../../shared/testing/test.js";
import { artifactGatewayContract } from "../testing/artifact-contract.js";
import { memoryArtifacts } from "./memory-artifacts.js";

file("sessions/infrastructure/artifacts");

artifactGatewayContract("memory", (world) => {
  const memory = memoryArtifacts(world);
  return { gateway: memory.gateway, publish: memory.publish, end: memory.end };
});
