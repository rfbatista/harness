// How a project summary reads on the projects list. The Go BFF writes the
// same first paint (internal/adapter/in/web/projects/manage.go);
// web/testdata/views/project-summary.json pins both.

import { relativeTime } from "../../../shared/presentation/format.js";

/**
 * @param {import("../domain/project.js").ProjectSummary} s
 * @param {Date} now
 * @returns {{ state: string, word: string, meta: string }}
 */
export function summaryView(s, now) {
  const running = s.runningSessionCount > 0;
  let activity = "no activity";
  if (s.lastActivityAt) {
    const ago = relativeTime(s.lastActivityAt, now);
    activity = ago === "now" ? "active now" : `active ${ago} ago`;
  }
  return {
    state: running ? "running" : "idle",
    word: running ? `${s.runningSessionCount} running` : "idle",
    meta: [countOrNone(s.repositoryCount, "repo", "repos"), countOrNone(s.openTaskCount, "open task", "open tasks"), activity].join(" · "),
  };
}

/** The line under the list: "4 projects · 3 sessions running". */
export function totalLine(projects, running) {
  if (projects === 0) return "no projects";
  const line = countOrNone(projects, "project", "projects");
  return running > 0 ? `${line} · ${countOrNone(running, "session", "sessions")} running` : line;
}

function countOrNone(n, one, many) {
  if (n === 0) return `no ${many}`;
  return n === 1 ? `1 ${one}` : `${n} ${many}`;
}
