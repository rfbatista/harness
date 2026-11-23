// What the board and the task page send each other as DOM events on the window
// (ADR 0106). The page is Alpine's, and reads the same fields from `detail`.

import { DtoError } from "../../../../shared/infrastructure/dto/dto-error";
import { object, optionalString, string, type Wire } from "../../../../shared/infrastructure/dto/fields";
import { AGENTS_VIEWS, type AgentsView } from "../../domain/view";

const view = (dto: string, from: Wire, key: string): AgentsView => {
  const value = string(dto, from, key);
  if (!(AGENTS_VIEWS as readonly string[]).includes(value)) throw new DtoError(dto, key, "expected list or whiteboard");
  return value as AgentsView;
};

/** The board tells the page which view to show, and which session to select in it ("" for none). */
export class ViewShownDto {
  constructor(
    readonly view: AgentsView,
    readonly session_id: string,
  ) {}
  static fromJSON(raw: unknown): ViewShownDto {
    const from = object("ViewShownDto", raw);
    return new ViewShownDto(view("ViewShownDto", from, "view"), optionalString("ViewShownDto", from, "session_id") ?? "");
  }
  toJSON(): Wire {
    return { view: this.view, session_id: this.session_id };
  }
}

/** The page tells the board which view the person chose with the toggle. */
export class ViewChosenDto {
  constructor(readonly view: AgentsView) {}
  static fromJSON(raw: unknown): ViewChosenDto {
    return new ViewChosenDto(view("ViewChosenDto", object("ViewChosenDto", raw), "view"));
  }
  toJSON(): Wire {
    return { view: this.view };
  }
}

/** The board asks the page to open a session's pane, on a tab when it names one. */
export class OpenSessionDto {
  constructor(
    readonly session_id: string,
    readonly tab: string,
  ) {}
  static fromJSON(raw: unknown): OpenSessionDto {
    const from = object("OpenSessionDto", raw);
    return new OpenSessionDto(string("OpenSessionDto", from, "session_id"), optionalString("OpenSessionDto", from, "tab") ?? "");
  }
  toJSON(): Wire {
    return { session_id: this.session_id, tab: this.tab };
  }
}

/** The page tells the board which session it has selected: "" for none. */
export class SessionSelectedDto {
  constructor(readonly session_id: string) {}
  static fromJSON(raw: unknown): SessionSelectedDto {
    return new SessionSelectedDto(optionalString("SessionSelectedDto", object("SessionSelectedDto", raw), "session_id") ?? "");
  }
  toJSON(): Wire {
    return { session_id: this.session_id };
  }
}

/** An event that carries nothing but its name. */
export class NoticeDto {
  static fromJSON(raw: unknown): NoticeDto {
    object("NoticeDto", raw);
    return new NoticeDto();
  }
  toJSON(): Wire {
    return {};
  }
}
