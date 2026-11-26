import type { ScopeAddress } from "../application/scope-address";

/** The scope in the page's address: ?repository=<id>; the project defaults leave it out. */
export function browserScopeAddress(win: Window): ScopeAddress {
  return {
    read: () => new URL(win.location.href).searchParams.get("repository") ?? "",
    write(id) {
      const url = new URL(win.location.href);
      if (id === "") url.searchParams.delete("repository");
      else url.searchParams.set("repository", id);
      win.history.pushState(win.history.state, "", url);
    },
    follow(listener) {
      const onPop = () => listener(new URL(win.location.href).searchParams.get("repository") ?? "");
      win.addEventListener("popstate", onPop);
      return () => win.removeEventListener("popstate", onPop);
    },
  };
}
