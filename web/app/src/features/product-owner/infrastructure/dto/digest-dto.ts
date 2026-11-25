// A project's digests (Contract: product owner, Digests).

import { DtoError } from "../../../../shared/infrastructure/dto/dto-error";
import { array, boolean, object, oneOf, optionalString, string, time, type Wire } from "../../../../shared/infrastructure/dto/fields";
import type { DigestItemKind } from "../../domain/digest";

const ITEM = "DigestItemDto";
const DIGEST = "DigestDto";
const LIST = "DigestListDto";

/** A digest's date, "YYYY-MM-DD": the pages and the address compare dates as text, so a malformed one would break the order. */
export function isoDate(dto: string, from: Wire, key: string, at = ""): string {
  const value = string(dto, from, key, at);
  // Date.parse accepts 2026-02-30 and reads it as March 2: the date must survive the round trip.
  const parsed = /^\d{4}-\d{2}-\d{2}$/.test(value) ? new Date(`${value}T00:00:00Z`) : null;
  if (parsed === null || Number.isNaN(parsed.getTime()) || parsed.toISOString().slice(0, 10) !== value) throw new DtoError(dto, at === "" ? key : `${at}.${key}`, "expected a date, YYYY-MM-DD");
  return value;
}

export const ITEM_KINDS: readonly DigestItemKind[] = ["task_moved", "session_ended", "session_failed", "waiting_on_person"];

export class DigestItemDto {
  constructor(
    readonly kind: DigestItemKind,
    readonly title: string,
    readonly url: string,
    readonly from: string | undefined,
    readonly to: string | undefined,
  ) {}

  static fromJSON(raw: unknown, at: string): DigestItemDto {
    const from = object(ITEM, raw, at);
    return new DigestItemDto(oneOf(ITEM, from, "kind", ITEM_KINDS, at), string(ITEM, from, "title", at), string(ITEM, from, "url", at), optionalString(ITEM, from, "from", at), optionalString(ITEM, from, "to", at));
  }

  toJSON(): Wire {
    return { kind: this.kind, title: this.title, url: this.url, ...(this.from === undefined ? {} : { from: this.from }), ...(this.to === undefined ? {} : { to: this.to }) };
  }
}

export class DigestDto {
  constructor(
    readonly digest_id: string,
    readonly date: string,
    readonly period_start: Date,
    readonly period_end: Date,
    readonly summary: string,
    readonly items: DigestItemDto[],
  ) {}

  static fromJSON(raw: unknown, at: string): DigestDto {
    const from = object(DIGEST, raw, at);
    return new DigestDto(
      string(DIGEST, from, "digest_id", at),
      isoDate(DIGEST, from, "date", at),
      time(DIGEST, from, "period_start", at),
      time(DIGEST, from, "period_end", at),
      string(DIGEST, from, "summary", at),
      array(DIGEST, from, "items", DigestItemDto.fromJSON, at),
    );
  }

  toJSON(): Wire {
    return { digest_id: this.digest_id, date: this.date, period_start: this.period_start.toISOString(), period_end: this.period_end.toISOString(), summary: this.summary, items: this.items.map((i) => i.toJSON()) };
  }
}

/** The answer of GET /api/product_owner_digests. */
export class DigestListDto {
  constructor(
    readonly digests: DigestDto[],
    readonly has_earlier: boolean,
  ) {}

  static fromJSON(raw: unknown): DigestListDto {
    const from = object(LIST, raw);
    return new DigestListDto(array(LIST, from, "digests", DigestDto.fromJSON), boolean(LIST, from, "has_earlier"));
  }

  toJSON(): Wire {
    return { digests: this.digests.map((d) => d.toJSON()), has_earlier: this.has_earlier };
  }
}
