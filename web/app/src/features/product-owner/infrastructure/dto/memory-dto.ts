// The project's memory (Contract: product owner, Memory).

import { array, object, oneOf, optionalString, optionalTime, string, time, type Wire } from "../../../../shared/infrastructure/dto/fields";

const ENTRY = "MemoryEntryDto";
const LIST = "MemoryListDto";
const UPDATE = "UpdateMemoryEntryRequestDto";
const DELETE = "DeleteMemoryEntryRequestDto";

export class MemoryEntryDto {
  constructor(
    readonly entry_id: string,
    readonly text: string,
    readonly saved_at: Date,
    readonly changed_at: Date | null,
    readonly changed_by: "person" | "product_owner" | null,
    readonly thread_id: string,
    readonly thread_title: string,
  ) {}

  static fromJSON(raw: unknown, at = ""): MemoryEntryDto {
    const from = object(ENTRY, raw, at);
    const by = from.changed_by === undefined || from.changed_by === null ? null : oneOf(ENTRY, from, "changed_by", ["person", "product_owner"] as const, at);
    return new MemoryEntryDto(
      string(ENTRY, from, "entry_id", at),
      string(ENTRY, from, "text", at),
      time(ENTRY, from, "saved_at", at),
      optionalTime(ENTRY, from, "changed_at", at),
      by,
      optionalString(ENTRY, from, "thread_id", at) ?? "",
      optionalString(ENTRY, from, "thread_title", at) ?? "",
    );
  }

  toJSON(): Wire {
    return {
      entry_id: this.entry_id,
      text: this.text,
      saved_at: this.saved_at.toISOString(),
      changed_at: this.changed_at === null ? null : this.changed_at.toISOString(),
      changed_by: this.changed_by,
      thread_id: this.thread_id,
      thread_title: this.thread_title,
    };
  }
}

/** GET /api/product_owner_memory. */
export class MemoryListDto {
  constructor(readonly entries: readonly MemoryEntryDto[]) {}

  static fromJSON(raw: unknown): MemoryListDto {
    return new MemoryListDto(array(LIST, object(LIST, raw), "entries", (item, at) => MemoryEntryDto.fromJSON(item, at)));
  }

  toJSON(): Wire {
    return { entries: this.entries.map((e) => e.toJSON()) };
  }
}

/** POST /api/update_memory_entry. */
export class UpdateMemoryEntryRequestDto {
  constructor(
    readonly entry_id: string,
    readonly text: string,
  ) {}

  static fromJSON(raw: unknown): UpdateMemoryEntryRequestDto {
    const from = object(UPDATE, raw);
    return new UpdateMemoryEntryRequestDto(string(UPDATE, from, "entry_id"), string(UPDATE, from, "text"));
  }

  toJSON(): Wire {
    return { entry_id: this.entry_id, text: this.text };
  }
}

/** POST /api/delete_memory_entry. */
export class DeleteMemoryEntryRequestDto {
  constructor(readonly entry_id: string) {}

  static fromJSON(raw: unknown): DeleteMemoryEntryRequestDto {
    return new DeleteMemoryEntryRequestDto(string(DELETE, object(DELETE, raw), "entry_id"));
  }

  toJSON(): Wire {
    return { entry_id: this.entry_id };
  }
}
