// BoardPreferences over a key-value storage (the browser's localStorage), under
// the `harness:` prefix of the other screen preferences (ADR 0037). Storage can
// be missing or throw, and a stored value can be stale or hand-edited: the
// defaults then apply, and nothing here throws.

import type { BoardPreferences } from "../application/preferences";
import { DtoError } from "../../../shared/infrastructure/dto/dto-error";
import { CameraDto, PositionsDto } from "./dto/preferences-dto";

/** The part of Web Storage this adapter uses. */
export interface KeyValueStorage {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
}

const PREFIX = "harness:";
export const cameraKey = (ticketId: string): string => `${PREFIX}whiteboard-camera:${ticketId}`;
export const positionsKey = (ticketId: string): string => `${PREFIX}whiteboard-positions:${ticketId}`;

/** localStorage, or nothing when the site's data is blocked. */
export function browserStorage(): KeyValueStorage | null {
  try {
    return globalThis.localStorage ?? null;
  } catch {
    return null;
  }
}

export function storageBoardPreferences(storage: KeyValueStorage | null): BoardPreferences {
  const read = (key: string): string | null => {
    try {
      return storage?.getItem(key) ?? null;
    } catch {
      return null;
    }
  };
  const write = (key: string, value: string | null): void => {
    try {
      if (value === null) storage?.removeItem(key);
      else storage?.setItem(key, value);
    } catch {
      // Not kept; the choice still applies to this page.
    }
  };
  return {
    positions(ticketId) {
      const text = read(positionsKey(ticketId));
      if (text === null) return {};
      try {
        return { ...PositionsDto.fromStored(text).positions };
      } catch (err) {
        if (err instanceof DtoError) return {};
        throw err;
      }
    },
    savePositions(ticketId, positions) {
      write(positionsKey(ticketId), Object.keys(positions).length === 0 ? null : new PositionsDto(positions).toStored());
    },
    camera(ticketId) {
      const text = read(cameraKey(ticketId));
      if (text === null) return null;
      try {
        const { x, y, scale } = CameraDto.fromStored(text);
        return { x, y, scale };
      } catch (err) {
        if (err instanceof DtoError) return null;
        throw err;
      }
    },
    saveCamera(ticketId, camera) {
      write(cameraKey(ticketId), new CameraDto(camera.x, camera.y, camera.scale).toStored());
    },
  };
}
