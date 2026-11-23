// The readers DTOs share: each checks one field's type and names the field's
// path in the DtoError it throws. Unknown extra fields are never looked at.

import { DtoError } from "./dto-error";

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
  if (!(allowed as readonly string[]).includes(value)) throw new DtoError(dto, join(at, key), `expected one of ${allowed.join(", ")}`);
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
