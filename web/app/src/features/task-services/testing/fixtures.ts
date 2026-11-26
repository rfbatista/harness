import type { ServiceRequest } from "../domain/request";
import type { Service } from "../domain/service";

export const TASK = "task-1";

export function service(over: Partial<Service> & { id?: string } = {}): Service {
  return {
    id: "svc-1",
    name: "api",
    status: "running",
    command: { line: "go run ./cmd/api", ports: [8080] },
    image: null,
    run: { startedAt: "2026-11-25T10:00:00Z", endedAt: "", exitCode: null, error: "", startedBy: { kind: "session", taskId: TASK, agent: "claude" } },
    createdBy: { kind: "session", taskId: TASK, agent: "claude" },
    credentials: [],
    ...over,
  };
}

export const exited = (code: number, over: Partial<Service> = {}): Service =>
  service({ status: "exited", run: { startedAt: "2026-11-25T10:00:00Z", endedAt: "2026-11-25T10:05:00Z", exitCode: code, error: "", startedBy: { kind: "session", taskId: TASK, agent: "claude" } }, ...over });

export const request = (over: Partial<ServiceRequest> = {}): ServiceRequest => ({
  id: "req-1", serviceId: "svc-1", serviceName: "api", action: "restart", taskId: TASK, agent: "claude", reason: "tests changed", summary: "Restart api", state: "pending", requestedAt: "2026-11-25T10:00:00Z", ...over,
});
