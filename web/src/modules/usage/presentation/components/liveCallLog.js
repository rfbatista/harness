// The Live view of a project's Usage page: the live call log. It opens
// stopped and holds no stream. Start opens the project's live call stream as
// an EventSource of its own (the gateway's openLive), Stop, leaving the page
// and destroying the component close it. Each call is a collapsed row that
// expands into Request, Response, Message and Usage tabs, each a JSON tree.
//
//   <section x-data="liveCallLog" data-project-id="…" data-testid="live-call-log">

import { FeedStatus } from "../../../../shared/domain/feed.js";
import { describeError } from "../../../../shared/presentation/errors.js";
import { applyEvent, emptyLog, filterItems } from "../../domain/liveCalls.js";
import { callRow, callTabs } from "../callRow.js";
import { jsonText, jsonTree, setFolds } from "../json-tree.js";

const NOT_FINISHED = "The call has not finished yet.";
const FINISH_MISSED = "No response: the call's finish was missed.";

/**
 * @param {{
 *   gateway: import("../../domain/ports.js").UsageGateway,
 *   clipboard?: { writeText: (text: string) => Promise<void> },
 * }} deps
 */
export const liveCallLog = ({ gateway, clipboard = globalThis.navigator?.clipboard }) => () => {
  let close = null;

  return {
    projectId: "",
    running: false,
    /** @type {import("../../../../shared/domain/feed.js").FeedStatus} */
    status: FeedStatus.CONNECTING,
    /** @type {import("../../domain/liveCalls.js").LiveLog} */
    log: emptyLog(),
    /** The open tab of each expanded row, by call id. */
    openTabs: {},
    /** What was last copied: "<id>:<tab>", or "". */
    copied: "",
    error: null,

    // ── what the markup binds ────────────────────────────────────────────
    get toggleLabel() {
      return this.running ? "Stop" : "Start";
    },
    get liveText() {
      if (!this.running) return "Stopped";
      return { live: "Live", paused: "Reconnecting…" }[this.status] ?? "Connecting…";
    },
    get liveTone() {
      return this.running && this.status === FeedStatus.LIVE ? "success" : "attention";
    },
    get rows() {
      return filterItems(this.log, {}).map((item) => this.rowOf(item));
    },
    get count() {
      const n = this.log.items.filter((i) => i.type === "call").length;
      return n === 1 ? "1 call" : `${n} calls`;
    },
    get empty() {
      return this.log.items.length === 0;
    },
    get emptyText() {
      return this.running ? "Waiting for the next call." : "Press Start to watch this project's LLM calls as they happen.";
    },

    rowOf(item) {
      if (item.type === "missed") {
        return { id: item.id, marker: true, call: false, cls: "call-missed", testid: "live-missed", text: item.count === 1 ? "1 call missed" : `${item.count} calls missed` };
      }
      const row = { ...callRow(item, this.projectId), marker: false, call: true, cls: "call", testid: "live-call", open: false, tab: "", tabs: [], value: null, emptyText: "", copyLabel: "Copy" };
      const tab = this.openTabs[item.id];
      if (!tab) return row;
      row.open = true;
      row.tab = tab;
      const tabs = callTabs(item);
      row.tabs = tabs.map((t) => ({ id: t.id, label: t.label, testid: `live-tab-${t.id}`, selected: t.id === tab ? "true" : "false" }));
      row.value = tabs.find((t) => t.id === tab).value;
      if (row.value === null) row.emptyText = item.noLongerInFlight ? FINISH_MISSED : NOT_FINISHED;
      row.copyLabel = this.copied === `${item.id}:${tab}` ? "Copied" : "Copy";
      return row;
    },

    // ── lifecycle ────────────────────────────────────────────────────────
    init() {
      this.projectId = this.$el.dataset.projectId ?? "";
    },

    destroy() {
      this.stop();
    },

    /** The Start/Stop button. */
    toggle() {
      if (this.running) this.stop();
      else this.start();
    },

    start() {
      if (this.running) return;
      this.running = true;
      this.status = FeedStatus.CONNECTING;
      // A stream's own handlers ignore it once it is stopped: a late event never lands.
      let mine = true;
      const release = gateway.openLive(this.projectId, {
        onEvent: (type, data) => mine && this.receive(type, data),
        onStatus: (status) => mine && (this.status = status),
      });
      close = () => {
        mine = false;
        release();
      };
    },

    stop() {
      close?.();
      close = null;
      this.running = false;
    },

    receive(type, data) {
      this.log = applyEvent(this.log, type, data);
    },

    // ── rows ─────────────────────────────────────────────────────────────
    /** A row's summary: open it to its request, or close it. */
    expand(event) {
      const id = event.currentTarget.dataset.id;
      const { [id]: was, ...rest } = this.openTabs;
      this.openTabs = was ? rest : { ...rest, [id]: "request" };
    },

    pickTab(event) {
      const { id, tab } = event.currentTarget.dataset;
      this.openTabs = { ...this.openTabs, [id]: tab };
    },

    /** The row an action button sits in. */
    treeOf(event) {
      return event.currentTarget.closest("[data-call]");
    },

    expandAll(event) {
      setFolds(this.treeOf(event), true);
    },

    collapseAll(event) {
      setFolds(this.treeOf(event), false);
    },

    async copy(event) {
      const { id } = event.currentTarget.dataset;
      const tab = this.openTabs[id];
      const row = this.rows.find((r) => r.id === id);
      try {
        await clipboard.writeText(jsonText(row.value));
        this.copied = `${id}:${tab}`;
      } catch (err) {
        this.error = describeError(err);
      }
    },

    dismissError() {
      this.error = null;
    },

    /**
     * Draws a row's value as a tree into el (x-effect). A value already drawn
     * is left alone, so a redraw triggered by another call arriving does not
     * unfold what the person folded.
     */
    renderTree(el, row) {
      if (!row.open) return;
      const drawn = el.__drawn;
      if (drawn && drawn.value === row.value && drawn.tab === row.tab) return;
      el.__drawn = { value: row.value, tab: row.tab };
      el.replaceChildren(row.value === null ? "" : jsonTree(el.ownerDocument, row.value));
    },
  };
};
