import type { Navigator } from "../application/ports";

export interface MemoryNavigator extends Navigator {
  readonly opened: readonly string[];
}

export function memoryNavigator(): MemoryNavigator {
  const opened: string[] = [];
  return { opened, open: (url) => void opened.push(url) };
}
