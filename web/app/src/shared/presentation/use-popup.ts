// A floating .popup beside its trigger: the top layer, placement, the phone
// sheet, one open at a time and light dismiss. This is the Alpine client's
// Alpine-free popup module, reused so the React panel behaves like the
// dropdowns beside it; it moves here when the Alpine client goes.
//
// The one import that leaves web/app/src, and the one exception the layer lint
// names (eslint-rules/layers.js; ADR 0101).

import { useEffect, useRef, type RefObject } from "react";
import { defaultEnv, floating } from "../../../../src/shared/presentation/components/popup.js";

interface Popup {
  open(mode?: { sheet?: boolean; tall?: boolean }): void;
  close(): void;
  isPhone(): boolean;
}

/** Shows `panel` as a popup anchored to `anchor` while `open`; `onDismiss` is called by a light dismiss (a click or scroll outside). */
export function usePopup(open: boolean, panel: RefObject<HTMLElement | null>, anchor: RefObject<HTMLElement | null>, onDismiss: () => void): void {
  const popup = useRef<Popup | null>(null);
  const dismiss = useRef(onDismiss);
  dismiss.current = onDismiss;

  useEffect(() => {
    if (!panel.current || !anchor.current) return;
    const created: Popup = floating(panel.current, { anchor: anchor.current, env: defaultEnv(), onDismiss: () => dismiss.current(), align: () => "end" });
    popup.current = created;
    return () => {
      created.close();
      popup.current = null;
    };
  }, [panel, anchor]);

  useEffect(() => {
    const current = popup.current;
    if (!current) return;
    if (open) current.open({ sheet: current.isPhone(), tall: true });
    else current.close();
  }, [open]);
}
