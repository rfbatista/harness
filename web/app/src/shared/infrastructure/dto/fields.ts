// The readers DTOs share: each checks one field's type and names the field's
// path in the DtoError it throws. Unknown extra fields are never looked at.

import { DtoError, UnknownValueError } from "./dto-error";

export type Wire = Record<string, unknown>;

const isRecord = (value: unknown): value is Wire => typeof value === "object" && value !== null && !Array.isArray(value);

const join = (at: string, key: string): string => (at === "" ? key : `${at}.${key}`);

/** `raw` as an object, or a DtoError at `at`. */
export function object(dto: string, raw: unknown, at = ""): Wire {
  if (!isRecord(raw)) throw new DtoError(dto, at, "expected an object");
  return raw;
}

export function string(dto: string, from: Wire, key: string, at = ""): string {
  const value = from[key];
  if (typeof value !== "string") throw new DtoError(dto, join(at, key), "expected a string");
  return value;
}

export function optionalString(dto: string, from: Wire, key: string, at = ""): string | undefined {
  const value = from[key];
  if (value === undefined || value === null) return undefined;
  if (typeof value !== "string") throw new DtoError(dto, join(at, key), "expected a string");
  return value;
}

export function boolean(dto: string, from: Wire, key: string, at = ""): boolean {
  const value = from[key];
  if (typeof value !== "boolean") throw new DtoError(dto, join(at, key), "expected a boolean");
  return value;
}

export function count(dto: string, from: Wire, key: string, at = ""): number {
  const value = from[key];
  if (typeof value !== "number" || !Number.isInteger(value) || value < 0) throw new DtoError(dto, join(at, key), "expected a count");
  return value;
}

/** The elements of an array field, each read by `read` with its own path ("projects[1]"). */
export function array<T>(dto: string, from: Wire, key: string, read: (item: unknown, at: string) => T, at = ""): T[] {
  const value = from[key];
  if (!Array.isArray(value)) throw new DtoError(dto, join(at, key), "expected an array");
  return value.map((item, i) => read(item, `${join(at, key)}[${i}]`));
}

/** A field that is one of `allowed`. */
export function oneOf<T extends string>(dto: string, from: Wire, key: string, allowed: readonly T[], at = ""): T {
  const value = string(dto, from, key, at);
  if (!(allowed as readonly string[]).includes(value)) throw new UnknownValueError(dto, join(at, key), `expected one of ${allowed.join(", ")}`);
  return value as T;
}

/** A path on this web UI only: "//host" is another origin. */
export function localPath(dto: string, from: Wire, key: string, at = ""): string {
  const value = string(dto, from, key, at);
  if (!value.startsWith("/") || value.startsWith("//")) throw new DtoError(dto, join(at, key), "expected a path on this web UI");
  return value;
}

/** An RFC 3339 time. */
export function time(dto: string, from: Wire, key: string, at = ""): Date {
  const value = new Date(string(dto, from, key, at));
  if (Number.isNaN(value.getTime())) throw new DtoError(dto, join(at, key), "expected a time");
  return value;
}

export function optionalTime(dto: string, from: Wire, key: string, at = ""): Date | null {
  const value = from[key];
  if (value === undefined || value === null || value === "") return null;
  return time(dto, from, key, at);
}

/**
 * A part of a DTO that the client can do without: when it does not read, it reads
 * as absent, so one malformed extra never empties a list. Only a DtoError is
 * swallowed.
 */
export function lenient<T>(read: () => T): T | undefined {
  try {
    return read();
  } catch (err) {
    if (err instanceof DtoError) return undefined;
    throw err;
  }
}

/** The entries of an optional array that read, each with its own path; the ones that do not are dropped. A field that is present but not an array is a DtoError. */
export function lenientArray<T>(dto: string, from: Wire, key: string, read: (item: unknown, at: string) => T, at = ""): T[] {
  if (from[key] === undefined || from[key] === null) return [];
  const out: T[] = [];
  for (const entry of array(dto, from, key, (item, path) => lenient(() => read(item, path)), at)) if (entry !== undefined) out.push(entry);
  return out;
}

export function optionalCount(dto: string, from: Wire, key: string, at = ""): number | undefined {
  if (from[key] === undefined || from[key] === null) return undefined;
  return count(dto, from, key, at);
}

/**
 * The notifications of a list, each read by `read`: one that holds a value the
 * client does not know (an UnknownValueError) is dropped, and the rest are kept.
 * Anything else malformed is still a DtoError for the whole list.
 */
export function rowsKnown<T>(dto: string, from: Wire, key: string, read: (item: unknown, at: string) => T, at = ""): T[] {
  const out: T[] = [];
  const rows = array(dto, from, key, (item, path) => {
    try {
      return { row: read(item, path) };
    } catch (err) {
      if (err instanceof UnknownValueError) return null;
      throw err;
    }
  }, at);
  for (const entry of rows) if (entry) out.push(entry.row);
  return out;
}
