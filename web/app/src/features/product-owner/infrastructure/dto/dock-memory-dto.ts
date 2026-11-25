// What the browser remembers of the panel: one JSON text in local storage.
// { open, width (rem), threads: { <project id>: <thread id> } }. Anything that does not read is forgotten.

import { boolean, object, type Wire } from "../../../../shared/infrastructure/dto/fields";

const NAME = "DockMemoryDto";

export class DockMemoryDto {
  constructor(
    readonly open: boolean,
    readonly width: number | null,
    readonly threads: Readonly<Record<string, string>>,
  ) {}

  /** `raw` is the parsed text; anything else reads as nothing remembered. */
  static fromJSON(raw: unknown): DockMemoryDto {
    try {
      const from = object(NAME, raw);
      const width = typeof from.width === "number" && Number.isFinite(from.width) ? from.width : null;
      const threads: Record<string, string> = {};
      if (typeof from.threads === "object" && from.threads !== null) {
        for (const [project, id] of Object.entries(from.threads as Wire)) if (typeof id === "string") threads[project] = id;
      }
      return new DockMemoryDto(boolean(NAME, from, "open"), width, threads);
    } catch {
      return new DockMemoryDto(false, null, {});
    }
  }

  toJSON(): Wire {
    return { open: this.open, width: this.width, threads: this.threads };
  }
}
