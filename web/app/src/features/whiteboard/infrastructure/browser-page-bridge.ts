// The board and the Alpine task page talk through DOM events on the window
// (ADR 0106: their details are DTOs). The names are the page's contract:
//   board → page   whiteboard-view {view, session_id}, whiteboard-open {session_id, tab},
//                  whiteboard-close, whiteboard-reviews, whiteboard-new-session,
//                  whiteboard-show-list, whiteboard-ready
//   page → board   whiteboard-set-view {view}, whiteboard-select {session_id}

import type { BoardHost } from "../application/host";
import type { PageSignals, ViewHost } from "../application/page-bridge";
import { NoticeDto, OpenSessionDto, SessionSelectedDto, ViewChosenDto, ViewShownDto } from "./dto/page-event-dto";
import { DtoError } from "../../../shared/infrastructure/dto/dto-error";

export function browserPageBridge(win: Window): { viewHost: ViewHost; boardHost: BoardHost; signals: PageSignals } {
  const send = (name: string, dto: { toJSON(): unknown }): void => {
    win.dispatchEvent(new CustomEvent(name, { detail: dto.toJSON() }));
  };
  const follow = <T>(name: string, read: (raw: unknown) => T, on: (value: T) => void): (() => void) => {
    const listener = (event: Event): void => {
      try {
        on(read((event as CustomEvent).detail));
      } catch (err) {
        // An event that does not read is not for the board.
        if (!(err instanceof DtoError)) throw err;
      }
    };
    win.addEventListener(name, listener);
    return () => win.removeEventListener(name, listener);
  };
  return {
    viewHost: { showView: (view, sessionId) => send("whiteboard-view", new ViewShownDto(view, sessionId)) },
    boardHost: {
      openSession: (sessionId, tab) => send("whiteboard-open", new OpenSessionDto(sessionId, tab ?? "")),
      closePane: () => send("whiteboard-close", new NoticeDto()),
      openReviews: () => send("whiteboard-reviews", new NoticeDto()),
      newSession: () => send("whiteboard-new-session", new NoticeDto()),
      showList: () => send("whiteboard-show-list", new NoticeDto()),
      boardReady: () => send("whiteboard-ready", new NoticeDto()),
    },
    signals: {
      followViewChoice: (on) => follow("whiteboard-set-view", ViewChosenDto.fromJSON, (dto) => on(dto.view)),
      followSelection: (on) => follow("whiteboard-select", SessionSelectedDto.fromJSON, (dto) => on(dto.session_id === "" ? null : dto.session_id)),
    },
  };
}
