import type { Piece } from "../../domain/session-context";
import type { FullTextState, GroupHeading, InstructionRow, InstructionsGroup } from "./context-view-model";

export interface PieceState {
  readonly open: boolean;
  readonly fullText: FullTextState;
  readonly full: string;
}

export const closedPiece: PieceState = { open: false, fullText: "idle", full: "" };

export function instructionsGroup(heading: GroupHeading, pieces: readonly Piece[], states: ReadonlyMap<string, PieceState>): InstructionsGroup {
  return { ...heading, rows: pieces.map((piece) => row(piece, states.get(piece.id) ?? closedPiece)) };
}

function row(piece: Piece, state: PieceState): InstructionRow {
  const done = state.fullText === "done";
  return {
    id: piece.id,
    title: piece.title,
    source: piece.source,
    path: piece.path,
    version: piece.version,
    size: `${bytes(piece.bytes)} · ≈${piece.approxTokens} tokens`,
    open: state.open,
    text: done ? state.full : piece.text,
    canShowAll: piece.truncated && !done,
    fullText: state.fullText,
  };
}

export function bytes(n: number): string {
  if (n < 1024) return `${n} B`;
  const kb = n / 1024;
  return kb < 1024 ? `${kb.toFixed(1).replace(/\.0$/, "")} KB` : `${(kb / 1024).toFixed(1).replace(/\.0$/, "")} MB`;
}
