// The "Product owner" section of the project's Settings page: the switch, in words and in the
// knob, and what it means. The agent, prompt and digest controls come with later children.

import { Presenter } from "../../../../shared/kernel/presenter";
import { pageHref } from "../../application/links";
import type { OwnerSwitch, OwnerSwitchState } from "../../application/owner-switch";

export interface SettingsSectionViewModel {
  /** "loading" while the switch is not read; the switch is then disabled. */
  readonly state: "loading" | "on" | "off";
  readonly label: string;
  readonly checked: boolean;
  readonly disabled: boolean;
  readonly busy: boolean;
  readonly error: string;
  readonly pageHref: string;
  readonly note: string;
}

export const NOTE = "Off: nobody can message it and no digest is recorded. Threads and memory are kept.";

export class SettingsSectionPresenter extends Presenter<SettingsSectionViewModel> {
  readonly #switch: OwnerSwitch;
  readonly #projectId: string;
  readonly #stop: () => void;

  constructor(owner: OwnerSwitch, projectId: string) {
    super(build(owner.state, projectId));
    this.#switch = owner;
    this.#projectId = projectId;
    this.#stop = owner.subscribe((state) => this.set(build(state, this.#projectId)));
  }

  destroy(): void {
    this.#stop();
  }

  toggle = (): Promise<void> => this.#switch.toggle();
}

function build(s: OwnerSwitchState, projectId: string): SettingsSectionViewModel {
  const state = s.enabled === null ? "loading" : s.enabled ? "on" : "off";
  return {
    state,
    label: state === "loading" ? "Product owner" : state === "on" ? "Product owner is on" : "Product owner is off",
    checked: state === "on",
    disabled: state === "loading" || s.busy,
    busy: s.busy,
    error: s.error,
    pageHref: pageHref(projectId),
    note: NOTE,
  };
}
