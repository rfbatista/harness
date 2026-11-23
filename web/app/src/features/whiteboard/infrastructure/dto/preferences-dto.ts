// What this browser keeps about a task's board, as the stored text reads and
// writes it (ADR 0037, ADR 0106). A value that does not read is a DtoError and
// the adapter falls back to the default.

import { DtoError } from "../../../../shared/infrastructure/dto/dto-error";
import { object, type Wire } from "../../../../shared/infrastructure/dto/fields";

const number = (dto: string, from: Wire, key: string, at = ""): number => {
  const value = from[key];
  if (typeof value !== "number" || !Number.isFinite(value)) throw new DtoError(dto, at === "" ? key : `${at}.${key}`, "expected a number");
  return value;
};

function parse(dto: string, text: string): unknown {
  try {
    return JSON.parse(text) as unknown;
  } catch {
    throw new DtoError(dto, "", "stored text is not JSON");
  }
}

/** The positions the person gave cards: { positions: { <session id>: { x, y } } }. */
export class PositionsDto {
  constructor(readonly positions: Record<string, { x: number; y: number }>) {}

  static fromJSON(raw: unknown): PositionsDto {
    const from = object("PositionsDto", raw);
    const given = object("PositionsDto", from.positions, "positions");
    const positions: Record<string, { x: number; y: number }> = {};
    for (const [id, value] of Object.entries(given)) {
      const at = `positions.${id}`;
      const point = object("PositionsDto", value, at);
      positions[id] = { x: number("PositionsDto", point, "x", at), y: number("PositionsDto", point, "y", at) };
    }
    return new PositionsDto(positions);
  }

  static fromStored(text: string): PositionsDto {
    return PositionsDto.fromJSON(parse("PositionsDto", text));
  }

  toJSON(): Wire {
    return { positions: this.positions };
  }

  toStored(): string {
    return JSON.stringify(this);
  }
}
