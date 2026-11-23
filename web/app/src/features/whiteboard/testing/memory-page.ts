import type { PageSignals, ViewHost } from "../application/page-bridge";
import type { AgentsView } from "../domain/view";

/** A page in memory: it records what the board asks, and the test plays the person and the page's own changes. */
export function memoryPage() {
  const shown: { view: AgentsView; session: string }[] = [];
  const choices = new Set<(view: AgentsView) => void>();
  const readies = new Set<() => void>();
  const selections = new Set<(id: string | null) => void>();
  const viewHost: ViewHost = { showView: (view, session) => void shown.push({ view, session }) };
  const signals: PageSignals = {
    followViewChoice: (cb) => (choices.add(cb), () => void choices.delete(cb)),
    followPageReady: (cb) => (readies.add(cb), () => void readies.delete(cb)),
    followSelection: (cb) => (selections.add(cb), () => void selections.delete(cb)),
  };
  return {
    shown,
    viewHost,
    signals,
    /** The person clicks the toggle. */
    choose: (view: AgentsView) => [...choices].forEach((cb) => cb(view)),
    /** The page starts and listens. */
    pageReady: () => [...readies].forEach((cb) => cb()),
    /** The page selects a session. */
    select: (id: string | null) => [...selections].forEach((cb) => cb(id)),
    followers: () => choices.size + selections.size + readies.size,
  };
}
