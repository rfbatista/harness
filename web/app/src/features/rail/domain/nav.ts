// The project's pages, as the rail links them (shell.RailNav). A page the
// server does not serve has no href and is left out.

export interface ProjectNav {
  readonly newTaskHref: string;
  readonly boardHref: string;
  readonly codeHref: string;
  readonly servicesHref: string;
  readonly servicesProjectId: string;
  readonly reviewsHref: string;
  readonly documentsHref: string;
  readonly designHref: string;
  readonly designSystemHref: string;
  /** Design-system proposals waiting on the person: painted by the server, updated by the design-system page. */
  readonly designSystemProposals: number;
  readonly usageHref: string;
  readonly productOwnerHref: string;
}

/** The page keys of shell/frame.go: the project page open now, "" for any other. */
export type PageKey = "board" | "code" | "services" | "reviews" | "documents" | "design" | "design-system" | "usage" | "product-owner";

export type NavIcon = "board" | "code" | "services" | "review" | "document" | "design" | "design-system" | "usage" | "owner";

export interface NavItem {
  readonly key: PageKey;
  readonly href: string;
  readonly icon: NavIcon;
  readonly label: string;
}

/** The items the server serves, in the rail's order. */
export function navItems(nav: ProjectNav): NavItem[] {
  const all: NavItem[] = [
    { key: "board", href: nav.boardHref, icon: "board", label: "Board" },
    { key: "code", href: nav.codeHref, icon: "code", label: "Code" },
    { key: "services", href: nav.servicesHref, icon: "services", label: "Services" },
    { key: "reviews", href: nav.reviewsHref, icon: "review", label: "Review requests" },
    { key: "documents", href: nav.documentsHref, icon: "document", label: "Documents" },
    { key: "design", href: nav.designHref, icon: "design", label: "Design assets" },
    { key: "design-system", href: nav.designSystemHref, icon: "design-system", label: "Design systems" },
    { key: "usage", href: nav.usageHref, icon: "usage", label: "Usage" },
    { key: "product-owner", href: nav.productOwnerHref, icon: "owner", label: "Product owner" },
  ];
  return all.filter((i) => i.href !== "");
}

/** The Services badge: "" at 0, "99+" past 99. */
export const servicesBadge = (running: number): string => (running <= 0 ? "" : running > 99 ? "99+" : String(running));
