// The tasks of the project on screen, as the page's rail seed holds them (it is the page's own
// copy, read when asked, so a task the person just opened in the rail shows its current status).

import type { TaskCatalog } from "../application/ports";
import { RailTasksDto } from "./dto/rail-tasks-dto";

type State = "running" | "waiting" | "done" | "idle";

const STATE: Readonly<Record<string, State>> = { in_progress: "running", review: "waiting", done: "done" };

export function browserTaskCatalog(doc: Document): TaskCatalog {
  return {
    count: () => RailTasksDto.fromText(doc.getElementById("rail-seed")?.textContent ?? "").tasks.length,
    find(taskId) {
      const seed = doc.getElementById("rail-seed");
      const task = RailTasksDto.fromText(seed?.textContent ?? "").tasks.find((t) => t.id === taskId);
      return task ? { state: STATE[task.status] ?? "idle" } : null;
    },
  };
}

export const noTasks: TaskCatalog = { count: () => 0, find: () => null };
