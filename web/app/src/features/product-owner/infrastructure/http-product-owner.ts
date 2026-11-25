// The product owner gateway and events over the harness HTTP API and the shared page stream.

import { ApiError, type ApiClient } from "../../../shared/infrastructure/api-client";
import { DtoError } from "../../../shared/infrastructure/dto/dto-error";
import type { Feed } from "../../../shared/kernel/feed";
import { ProductOwnerError, type ProductOwnerErrorCode, type ProductOwnerEvents, type ProductOwnerGateway } from "../application/ports";
import { ProductOwnerEventDto } from "./dto/event-dto";
import { DeleteMemoryEntryRequestDto, MemoryEntryDto, MemoryListDto, UpdateMemoryEntryRequestDto } from "./dto/memory-dto";
import { RenameThreadRequestDto, SendMessageRequestDto, SentDto, StartedDto, StartThreadRequestDto, ThreadRequestDto } from "./dto/request-dto";
import { SettingsDto, UpdateSettingsRequestDto } from "./dto/settings-dto";
import { ThreadListDto, ThreadPageDto, ThreadSummaryDto } from "./dto/thread-dto";
import { toEntry, toEvent, toPage, toSettings, toSummary } from "./thread-mapper";

/** The project's event feed on the page stream: the product owner's events are some of what it carries. */
export const eventsPath = (projectId: string): string => `/events?project_id=${encodeURIComponent(projectId)}`;

const KNOWN: readonly string[] = ["PRODUCT_OWNER_OFF", "THREAD_BUSY", "INVALID_INPUT", "PRODUCT_OWNER_UNAVAILABLE", "PROJECT_NOT_FOUND", "THREAD_NOT_FOUND", "MEMORY_ENTRY_NOT_FOUND", "NETWORK_ERROR", "BAD_RESPONSE"];

async function mapErrors<T>(call: () => Promise<T>): Promise<T> {
  try {
    return await call();
  } catch (err) {
    if (err instanceof DtoError) throw new ProductOwnerError("BAD_RESPONSE", err.message);
    if (err instanceof ApiError) throw new ProductOwnerError(KNOWN.includes(err.code) ? (err.code as ProductOwnerErrorCode) : "UNKNOWN", err.message, err.fixUrl);
    throw err;
  }
}

export function httpProductOwner(api: ApiClient, feed: Feed): ProductOwnerGateway & ProductOwnerEvents {
  return {
    settings: (projectId, signal) => mapErrors(async () => toSettings(SettingsDto.fromJSON(await api.get("/product_owner", { project_id: projectId }, signal)))),
    setEnabled: (projectId, enabled) => mapErrors(async () => toSettings(SettingsDto.fromJSON(await api.post("/update_product_owner", new UpdateSettingsRequestDto(projectId, enabled))))),
    threads: (projectId, signal) => mapErrors(async () => ThreadListDto.fromJSON(await api.get("/product_owner_threads", { project_id: projectId }, signal)).threads.map(toSummary)),
    thread: (threadId, before, signal) => mapErrors(async () => toPage(ThreadPageDto.fromJSON(await api.get("/product_owner_thread", { thread_id: threadId, ...(before === undefined ? {} : { before }) }, signal)))),
    startThread: (projectId, message) =>
      mapErrors(async () => {
        const started = StartedDto.fromJSON(await api.post("/start_product_owner_thread", new StartThreadRequestDto(projectId, message)));
        return { threadId: started.thread_id, itemId: started.item_id };
      }),
    sendMessage: (threadId, message) => mapErrors(async () => ({ itemId: SentDto.fromJSON(await api.post("/send_product_owner_message", new SendMessageRequestDto(threadId, message))).item_id })),
    stopTurn: (threadId) => mapErrors(async () => void (await api.post("/stop_product_owner_turn", new ThreadRequestDto(threadId)))),
    renameThread: (threadId, title) => mapErrors(async () => toSummary(ThreadSummaryDto.fromJSON(await api.post("/rename_product_owner_thread", new RenameThreadRequestDto(threadId, title))))),
    memory: (projectId, signal) => mapErrors(async () => MemoryListDto.fromJSON(await api.get("/product_owner_memory", { project_id: projectId }, signal)).entries.map(toEntry)),
    updateMemoryEntry: (entryId, text) => mapErrors(async () => toEntry(MemoryEntryDto.fromJSON(await api.post("/update_memory_entry", new UpdateMemoryEntryRequestDto(entryId, text))))),
    deleteMemoryEntry: (entryId) => mapErrors(async () => void (await api.post("/delete_memory_entry", new DeleteMemoryEntryRequestDto(entryId)))),

    follow: (projectId, onEvent, onStatus) =>
      feed.follow(eventsPath(projectId), {
        onMessage(data) {
          try {
            const dto = ProductOwnerEventDto.fromJSON(data);
            // The feed carries tasks and sessions too; and it is project-scoped, so another project's message is not for this follower.
            if (dto === null || dto.project_id !== projectId) return;
            const event = toEvent(dto);
            if (event) onEvent(event);
          } catch (err) {
            // A message that is not a valid event is dropped; the next resync corrects what is on screen. Anything else is a bug.
            if (!(err instanceof DtoError)) throw err;
          }
        },
        onStatus,
      }),
  };
}
