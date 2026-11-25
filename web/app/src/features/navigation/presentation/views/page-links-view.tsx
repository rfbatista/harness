import { Icon } from "../../../../shared/presentation/icon";
import { usePresenter } from "../../../../shared/presentation/use-presenter";
import type { LinkViewModel, PageLinksPresenter } from "../presenters/page-links-presenter";

const NOTE_ID = "agents-link-note";

export function PageLinksView({ presenter }: { presenter: PageLinksPresenter }) {
  const { links } = usePresenter(presenter);
  return (
    <>
      {links.map((link) => (
        <LinkView key={link.key} link={link} />
      ))}
    </>
  );
}

function LinkView({ link }: { link: LinkViewModel }) {
  const marks = {
    "aria-current": link.current ? ("page" as const) : undefined,
    "data-blocking": link.attention ? "true" : undefined,
  };
  if (link.icon) {
    return (
      <a className="[ button ]" data-variant="ghost" data-icon="" href={link.href} aria-label={link.label} title={link.label} {...marks}>
        <Icon name={link.icon} />
      </a>
    );
  }
  if (link.note) {
    return (
      <>
        <a className="[ button ]" data-variant="ghost" data-size="sm" href={link.href} title={link.note} aria-describedby={NOTE_ID} {...marks}>
          {link.label}
        </a>
        <span id={NOTE_ID} className="[ visually-hidden ]">
          {link.note}
        </span>
      </>
    );
  }
  return (
    <a className="[ button ]" data-variant="ghost" data-size="sm" href={link.href} {...marks}>
      {link.label}
    </a>
  );
}
