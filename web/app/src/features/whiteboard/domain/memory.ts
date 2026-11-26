// A session's memory as the board reads it (Contract: memória da sessão). Same shape as the shared kernel's values,
// which the presenters give these to: a domain imports nothing outside its feature.

export interface BoardMemory {
  sessionId: string;
  taskId: string;
  /** null before the first sample. */
  sampledAt: Date | null;
  rssBytes: number;
  budgetBytes: number;
  processes: number;
  heaviest: { pid: number; command: string; rssBytes: number } | null;
}

/** An encerramento por memória. */
export interface BoardMemoryKill {
  sessionId: string;
  taskId: string;
  killedAt: Date;
  pid: number;
  command: string;
  rssBytes: number;
  treeRssBytes: number;
  budgetBytes: number;
  signal: "SIGTERM" | "SIGKILL";
}
