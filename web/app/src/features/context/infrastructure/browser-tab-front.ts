// TabFront read from the element around the island's mount: the session pane sets data-front on it.

import type { TabFront } from "../application/ports";

export function browserTabFront(mount: Element): TabFront {
  const holder = mount.closest("[data-front]") ?? mount;
  const read = () => holder.getAttribute("data-front") === "true";
  return {
    subscribe(listener) {
      listener(read());
      const Observer = mount.ownerDocument.defaultView?.MutationObserver;
      if (!Observer) return () => {};
      const observer = new Observer(() => listener(read()));
      observer.observe(holder, { attributes: true, attributeFilter: ["data-front"] });
      return () => observer.disconnect();
    },
  };
}
