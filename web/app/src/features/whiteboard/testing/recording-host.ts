import type { BoardHost } from "../application/host";

/** A host that writes down what it was asked. */
export function recordingHost(): BoardHost & { readonly calls: string[] } {
  const calls: string[] = [];
  return {
    calls,
    openSession: (id, tab) => void calls.push(tab ? `open ${id} ${tab}` : `open ${id}`),
    closePane: () => void calls.push("close"),
    openReviews: () => void calls.push("reviews"),
    newSession: () => void calls.push("new"),
    showList: () => void calls.push("list"),
    boardReady: () => void calls.push("ready"),
  };
}
