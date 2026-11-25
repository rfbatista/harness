// Digests for tests.

import type { Digest, DigestItem } from "../domain/digest";

export const TIME_ZONE = "America/Sao_Paulo";

export const digestItem = (over: Partial<DigestItem> = {}): DigestItem => ({ kind: "task_moved", title: "Child 1", url: "/tasks/t1", from: "review", to: "done", ...over });

/** A digest for `date` ("YYYY-MM-DD"), recorded at 09:00 UTC, covering the day before. */
export function digest(date: string, over: Partial<Digest> = {}): Digest {
  const end = new Date(`${date}T12:00:00Z`);
  return { digestId: `d-${date}`, date, periodStart: new Date(end.getTime() - 24 * 3600 * 1000), periodEnd: end, summary: `Summary of ${date}.`, items: [digestItem()], ...over };
}

/** `count` digests on consecutive days ending at `last`, oldest first. */
export function digestRun(last: string, count: number): Digest[] {
  const end = Date.parse(`${last}T00:00:00Z`);
  return Array.from({ length: count }, (_, i) => digest(new Date(end - (count - 1 - i) * 86400000).toISOString().slice(0, 10)));
}
