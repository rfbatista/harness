// The wire format of token saver activations (internal/domain/tokensaver.go).

import { Codes, StructuredError } from "../../../shared/domain/errors.js";

const VALUES = ["inherit", "on", "off"];

/** @returns {import("../domain/ports.js").Activations} */
export function toActivations(body) {
  if (!body || typeof body.level !== "string" || typeof body.owner_id !== "string" || !Array.isArray(body.activations)) {
    bad("expected {level, owner_id, activations: [...]}");
  }
  return { level: body.level, ownerId: body.owner_id, activations: body.activations.map(toActivation) };
}

function toActivation(dto) {
  if (!dto || typeof dto.saver !== "string" || dto.saver === "") bad("activation without a saver");
  if (!VALUES.includes(dto.value)) bad(`${dto.saver} has unknown value "${dto.value}"`);
  return Object.freeze({ saver: dto.saver, value: dto.value, summary: dto.summary ?? "" });
}

function bad(detail) {
  throw new StructuredError(Codes.BAD_RESPONSE, `Unexpected token saver data: ${detail}.`);
}
