// SettingsFeed over the project feed of the shared page stream: the settings saves.

import { DtoError } from "../../../shared/infrastructure/dto/dto-error";
import type { Feed } from "../../../shared/kernel/feed";
import type { SettingsChange, SettingsFeed } from "../application/ports";
import { SettingsChangedDto } from "./dto/settings-view-dto";

const projectFeedPath = (projectId: string): string => `/events?project_id=${encodeURIComponent(projectId)}`;

/** One feed message as a settings change of the project, or null for any other kind or one that cannot be read. */
export function toSettingsChange(data: unknown, projectId: string): SettingsChange | null {
  if (typeof data !== "object" || data === null || !("agent_system_settings" in data)) return null;
  try {
    const dto = SettingsChangedDto.fromJSON(data);
    return dto.project_id === projectId ? { repositoryId: dto.repository_id } : null;
  } catch (err) {
    if (err instanceof DtoError) return null;
    throw err;
  }
}

export function httpSettingsFeed(feed: Feed): SettingsFeed {
  return {
    follow: (projectId, { onChange }) =>
      feed.follow(projectFeedPath(projectId), {
        onMessage(data) {
          const change = toSettingsChange(data, projectId);
          if (change) onChange(change);
        },
      }),
  };
}
