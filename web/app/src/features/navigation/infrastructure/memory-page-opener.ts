import type { PageOpener } from "../application/ports";

export interface MemoryPageOpener extends PageOpener {
  readonly opened: readonly string[];
}

export function memoryPageOpener(): MemoryPageOpener {
  const opened: string[] = [];
  return { opened, open: (url) => void opened.push(url) };
}
