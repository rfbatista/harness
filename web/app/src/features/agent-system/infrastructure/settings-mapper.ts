import type { Field, SettingsView } from "../domain/settings";
import type { BoolFieldDto, SettingsViewDto } from "./dto/settings-view-dto";

const field = (dto: BoolFieldDto): Field<boolean> => ({ value: dto.value, source: dto.source, reason: dto.reason });

export function toSettingsView(dto: SettingsViewDto): SettingsView {
  return {
    effective: {
      contextMapEnabled: field(dto.context_map_enabled),
      taskMemoryRequireUpdate: field(dto.task_memory_require_update),
      trajectoryRecordingDefault: field(dto.trajectory_recording_default),
      missingRequiredGates: dto.missing_required_gates,
    },
    values: dto.values,
  };
}
