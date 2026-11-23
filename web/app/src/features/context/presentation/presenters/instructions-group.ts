import type { Piece } from "../../domain/session-context";
import { countLabel, matches } from "./search";
import type { FullTextState, HeadingBase, InstructionRow, InstructionsGroup } from "./context-view-model";

export interface PieceState {
  readonly open: boolean;
  readonly fullText: FullTextState;
  readonly full: string;
}

export const closedPiece: PieceState = { open: false, fullText: "idle", full: "" };

export function instructionsGroup(heading: HeadingBase, pieces: readonly Piece[], states: ReadonlyMap<string, PieceState>, query: string): InstructionsGroup {
  const rows = pieces.map((piece) => row(piece, states.get(piece.id) ?? closedPiece, query)).filter((r) => r !== null);
  return { ...heading, count: countLabel(query, rows.length, pieces.length), noMatch: query !== "" && rows.length === 0, rows };
}

function row(piece: Piece, state: PieceState, query: string): InstructionRow | null {
  const done = state.fullText === "done";
  const text = done ? state.full : piece.text;
  const inHeading = matches(query, piece.title, piece.source, piece.path, piece.version);
  const inText = matches(query, text);
  if (!inHeading && !inText) return null;
  return {
    id: piece.id,
    title: piece.title,
    source: piece.source,
    path: piece.path,
    version: piece.version,
    size: `${bytes(piece.bytes)} · ≈${piece.approxTokens} tokens`,
    open: state.open,
    changed: piece.changed ? `changed in this entry · ${piece.changed.from} → ${piece.changed.to}` : "",
    matchedInText: query !== "" && !inHeading,
    text,
    canShowAll: piece.truncated && !done,
    fullText: state.fullText,
  };
}

export function bytes(n: number): string {
  if (n < 1024) return `${n} B`;
  const kb = n / 1024;
  return kb < 1024 ? `${kb.toFixed(1).replace(/\.0$/, "")} KB` : `${(kb / 1024).toFixed(1).replace(/\.0$/, "")} MB`;
}
