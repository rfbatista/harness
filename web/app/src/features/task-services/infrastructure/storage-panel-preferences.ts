// PanelPreferences over a key-value storage, under the `harness:` prefix. Storage
// can be missing or throw: the defaults then apply, and nothing here throws.

import type { PanelPreferences } from "../application/ports";

interface KeyValueStorage {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
}

export const panelKey = (taskId: string): string => `harness:task-services-panel:${taskId}`;

export function storagePanelPreferences(storage: KeyValueStorage | null): PanelPreferences {
  return {
    open(taskId) {
      try {
        const text = storage?.getItem(panelKey(taskId));
        return text === "open" ? true : text === "collapsed" ? false : null;
      } catch {
        return null;
      }
    },
    saveOpen(taskId, open) {
      try {
        storage?.setItem(panelKey(taskId), open ? "open" : "collapsed");
      } catch {
        // Not kept; the choice still applies to this page.
      }
    },
  };
}
