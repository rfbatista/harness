// A task's trajectory recording over the harness HTTP API (a PUT of
// {"enabled": true|false|null}, which answers the resolved recording).

import { decodeRecording, enabledOf } from "../domain/recording.js";

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 */
export function recordingGateway(api) {
  return {
    decode: decodeRecording,
    /**
     * @param {string} projectId
     * @param {string} taskId
     * @param {import("../domain/recording.js").RecordingChoice} choice
     * @returns {Promise<import("../domain/recording.js").Recording>}
     */
    async set(projectId, taskId, choice) {
      const path = `/projects/${encodeURIComponent(projectId)}/tasks/${encodeURIComponent(taskId)}/trajectory-recording`;
      return decodeRecording(await api.put(path, { enabled: enabledOf(choice) }));
    },
  };
}
