import { apiClient } from "../../../shared/infrastructure/api-client";
import { fakeStore } from "../testing/fake-store";
import { settingsSourceContract } from "../testing/settings-source-contract";
import { httpSettingsSource } from "./http-settings-source";

settingsSourceContract("HTTP adapter over the fake store", () => {
  const store = fakeStore();
  return { source: httpSettingsSource(apiClient(store.fetch)), store };
});
