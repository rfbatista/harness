import { apiClient } from "../../../shared/infrastructure/api-client";
import { NotificationsSourceError } from "../application/ports";
import { fakeNotificationsServer } from "../testing/fake-server";
import { notificationsSourceContract } from "../testing/notifications-source-contract";
import { httpNotificationsSource } from "./http-notifications-source";
import { memoryNotificationsSource } from "./memory-notifications-source";

notificationsSourceContract("HTTP adapter against a fake server on the wire", () => {
  const data = memoryNotificationsSource();
  const server = fakeNotificationsServer(data);
  return { source: httpNotificationsSource(apiClient(server.fetch)), seed: data.seed, breakNetwork: server.breakNetwork };
});

notificationsSourceContract("in-memory adapter", () => {
  const source = memoryNotificationsSource();
  return { source, seed: source.seed, breakNetwork: () => source.failWith(new NotificationsSourceError("NETWORK_ERROR", "offline")) };
});
