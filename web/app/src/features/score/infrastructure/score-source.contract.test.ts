import { apiClient } from "../../../shared/infrastructure/api-client";
import { fakeBench } from "../testing/fake-bench";
import { scoreSourceContract } from "../testing/score-source-contract";
import { httpScoreSource } from "./http-score-source";

scoreSourceContract("HTTP adapter over the fake bench", () => {
  const bench = fakeBench();
  return { source: httpScoreSource(apiClient(bench.fetch)), bench };
});
