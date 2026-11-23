// What this browser keeps about a task's board, as the stored text reads and
// writes it (ADR 0037, ADR 0106). A value that does not read is a DtoError and
// the adapter falls back to the default.

import { DtoError } from "../../../../shared/infrastructure/dto/dto-error";
import { object, type Wire } from "../../../../shared/infrastructure/dto/fields";
import { MAX_SCALE, MIN_SCALE } from "../../domain/camera";
import { AGENTS_VIEWS, type AgentsView } from "../../domain/view";

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

/** The camera of a task's board: { x, y, scale }, the scale between the zoom limits. */
export class CameraDto {
  constructor(
    readonly x: number,
    readonly y: number,
    readonly scale: number,
  ) {}

  static fromJSON(raw: unknown): CameraDto {
    const from = object("CameraDto", raw);
    const scale = number("CameraDto", from, "scale");
    if (scale < MIN_SCALE || scale > MAX_SCALE) throw new DtoError("CameraDto", "scale", "expected a scale within the zoom limits");
    return new CameraDto(number("CameraDto", from, "x"), number("CameraDto", from, "y"), scale);
  }

  static fromStored(text: string): CameraDto {
    return CameraDto.fromJSON(parse("CameraDto", text));
  }

  toJSON(): Wire {
    return { x: this.x, y: this.y, scale: this.scale };
  }

  toStored(): string {
    return JSON.stringify(this);
  }
}

/** The view the person left a task on, stored as its plain name: "list" or "whiteboard". */
export class AgentsViewDto {
  constructor(readonly view: AgentsView) {}

  static fromJSON(raw: unknown): AgentsViewDto {
    if (typeof raw !== "string" || !(AGENTS_VIEWS as readonly string[]).includes(raw)) throw new DtoError("AgentsViewDto", "", "expected list or whiteboard");
    return new AgentsViewDto(raw as AgentsView);
  }

  static fromStored(text: string): AgentsViewDto {
    return AgentsViewDto.fromJSON(text);
  }

  toJSON(): AgentsView {
    return this.view;
  }

  toStored(): string {
    return this.view;
  }
}
