import { OPTIONAL_GATE_KINDS, REQUIRED_GATE_KINDS, type Field, type Gate, type Guard, type SettingsView } from "../domain/settings";
import type { BoolFieldDto, GateDto, GuardDto, ListFieldDto, NumberFieldDto, SettingsViewDto, TextFieldDto } from "./dto/settings-view-dto";

const field = <T>(dto: { value: T; source: Field<T>["source"]; reason: string }): Field<T> => ({ value: dto.value, source: dto.source, reason: dto.reason });
const bool = (dto: BoolFieldDto): Field<boolean> => field(dto);
const num = (dto: NumberFieldDto): Field<number> => field(dto);
const text = (dto: TextFieldDto): Field<string> => field(dto);
const list = (dto: ListFieldDto): Field<readonly string[]> => field(dto);

const NOTHING: Field<string> = { value: "", source: "harness", reason: "" };
const NO_TIMEOUT: Field<number> = { value: 600, source: "harness", reason: "" };

/** Every gate kind in the Gates list's order: a required kind is always listed; an optional one the store left out is off. */
function gates(listed: readonly GateDto[]): readonly Gate[] {
  const by = new Map(listed.map((g) => [g.kind, g] as const));
  const one = (kind: string, required: boolean): Gate => {
    const g = by.get(kind);
    return g === undefined ? { kind, required, on: false, command: NOTHING, timeoutSeconds: NO_TIMEOUT } : { kind, required, on: true, command: text(g.command), timeoutSeconds: num(g.timeout_seconds) };
  };
  return [...REQUIRED_GATE_KINDS.map((k) => one(k, true)), ...OPTIONAL_GATE_KINDS.map((k) => one(k, false))];
}

function guard(dto: GuardDto): Guard {
  return { gates: gates(dto.gates), protectedPaths: list(dto.protected_paths), lockfileRuleEnabled: bool(dto.lockfile_rule_enabled), retryLimit: num(dto.retry_limit), testsFailFirstAvailable: bool(dto.tests_fail_first_available) };
}

export function toSettingsView(dto: SettingsViewDto): SettingsView {
  return {
    effective: {
      contextMapEnabled: bool(dto.context_map_enabled),
      taskMemoryRequireUpdate: bool(dto.task_memory_require_update),
      guard: guard(dto.guard),
      critic: { enabled: bool(dto.review.enabled), model: text(dto.review.model), standards: list(dto.review.standards) },
      visualQaUi: field(dto.review.visual_qa_ui),
      trajectoryRecordingDefault: bool(dto.trajectory_recording_default),
      missingRequiredGates: dto.missing_required_gates,
    },
    values: dto.values,
    updatedAt: dto.updated_at,
  };
}
