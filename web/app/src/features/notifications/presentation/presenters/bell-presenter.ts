// The bell: the unread count on every page, as a badge and in its accessible
// name, and whether the live stream is up. Absent when the inbox is not served.

import { Presenter } from "../../../../shared/kernel/presenter";
import type { Inbox, InboxState } from "../../application/inbox";
import { badgeText, bellLabel } from "../../domain/bell";
import type { FeedStatus } from "../../domain/feed-status";

export interface BellViewModel {
  readonly visible: boolean;
  /** The inbox page: without JavaScript the bell is a link to it. */
  readonly href: string;
  readonly label: string;
  readonly badge: string;
  readonly hasUnread: boolean;
  /** The stream's state: "paused" means the count may be stale. */
  readonly feed: FeedStatus;
  /** The count could not be read, so none is shown. */
  readonly loadFailed: boolean;
}

export function bellViewModel(inboxHref: string | null, state: InboxState): BellViewModel {
  return {
    visible: inboxHref !== null,
    href: inboxHref ?? "",
    label: bellLabel(state.unreadCount),
    badge: badgeText(state.unreadCount),
    hasUnread: !!state.unreadCount,
    feed: state.feed,
    loadFailed: state.loadFailed,
  };
}

export class BellPresenter extends Presenter<BellViewModel> {
  /** `inboxHref` is null when the server does not serve the inbox: there is no bell. */
  constructor(inbox: Inbox, inboxHref: string | null) {
    super(bellViewModel(inboxHref, inbox.state));
    inbox.subscribe((state) => this.set(bellViewModel(inboxHref, state)));
  }
}
