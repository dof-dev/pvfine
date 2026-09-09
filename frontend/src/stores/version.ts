import { computed, ref } from "vue";
import { defineStore } from "pinia";
import { Events } from "@wailsio/runtime";
import { VersionService } from "../../bindings/pvfine/services";
import type {
  VersionChange,
  VersionCommit,
  VersionStatus,
} from "../../bindings/pvfine/services/models";

const emptyStatus = (): VersionStatus => ({
  enabled: false,
  repositoryPath: "",
  branch: "main",
  headId: "",
  headMessage: "",
  changedFiles: 0,
  pendingChangeSets: 0,
  needsSave: false,
  viewCommitId: "",
  error: "",
});

/** 本地逻辑文件版本库状态与版本历史。 */
export const useVersionStore = defineStore("version", () => {
  const visible = ref(false);
  const status = ref<VersionStatus>(emptyStatus());
  const changes = ref<VersionChange[]>([]);
  const history = ref<VersionCommit[]>([]);
  const expandedCommitID = ref("");
  const commitChanges = ref<VersionChange[]>([]);
  const commitMessage = ref("");
  const loading = ref(false);
  const committing = ref(false);
  const exporting = ref(false);
  const exportingCommitID = ref("");
  const error = ref("");
  let refreshToken = 0;

  const enabled = computed(() => status.value.enabled);
  const canCommit = computed(
    () => enabled.value && status.value.changedFiles > 0 && !!commitMessage.value.trim() && !committing.value
  );

  function errorMessage(value: any): string {
    return String(value?.message ?? value ?? "版本操作失败");
  }

  function applyStatus(value: VersionStatus | null | undefined): void {
    if (!value) return;
    status.value = { ...emptyStatus(), ...value };
  }

  async function refreshLists(includeHistory = true): Promise<void> {
    const request = ++refreshToken;
    try {
      const changePage = await VersionService.ListChanges(0, 500);
      if (request !== refreshToken) return;
      changes.value = (changePage?.changes ?? []).filter(
        (item): item is VersionChange => !!item
      );
      if (!includeHistory) return;
      const historyPage = await VersionService.History(0, 100);
      if (request !== refreshToken) return;
      history.value = (historyPage?.commits ?? []).filter(
        (item): item is VersionCommit => !!item
      );
      if (!history.value.some((item) => item.id === expandedCommitID.value)) {
        expandedCommitID.value = "";
        commitChanges.value = [];
      }
    } catch (value: any) {
      if (request === refreshToken) error.value = errorMessage(value);
    }
  }

  async function refreshStatusOnly(): Promise<void> {
    const request = ++refreshToken;
    try {
      const next = await VersionService.Status();
      if (request === refreshToken) applyStatus(next);
    } catch (value: any) {
      if (request === refreshToken) error.value = errorMessage(value);
    }
  }

  async function refresh(): Promise<void> {
    const request = ++refreshToken;
    loading.value = true;
    try {
      const next = await VersionService.Status();
      if (request !== refreshToken) return;
      applyStatus(next);
      if (!status.value.enabled) {
        changes.value = [];
        history.value = [];
        expandedCommitID.value = "";
        commitChanges.value = [];
        return;
      }
      const [changePage, historyPage] = await Promise.all([
        VersionService.ListChanges(0, 500),
        VersionService.History(0, 100),
      ]);
      if (request !== refreshToken) return;
      changes.value = (changePage?.changes ?? []).filter(
        (item): item is VersionChange => !!item
      );
      history.value = (historyPage?.commits ?? []).filter(
        (item): item is VersionCommit => !!item
      );
      if (!history.value.some((item) => item.id === expandedCommitID.value)) {
        expandedCommitID.value = "";
        commitChanges.value = [];
      }
    } catch (value: any) {
      if (request === refreshToken) error.value = errorMessage(value);
    } finally {
      if (request === refreshToken) loading.value = false;
    }
  }

  function open(): void {
    visible.value = true;
    error.value = "";
    void refresh();
  }

  function close(): void {
    if (loading.value || committing.value || exporting.value) return;
    visible.value = false;
  }

  async function initialize(): Promise<void> {
    loading.value = true;
    error.value = "";
    try {
      await VersionService.Initialize();
      await refresh();
    } catch (value: any) {
      error.value = errorMessage(value);
      throw value;
    } finally {
      loading.value = false;
    }
  }

  async function commit(): Promise<VersionCommit | null> {
    if (!canCommit.value) throw new Error("请输入提交说明并确认存在工作区变更");
    committing.value = true;
    error.value = "";
    try {
      const result = await VersionService.Commit(commitMessage.value.trim());
      commitMessage.value = "";
      await refresh();
      return result;
    } catch (value: any) {
      error.value = errorMessage(value);
      throw value;
    } finally {
      committing.value = false;
    }
  }

  async function undo(): Promise<void> {
    loading.value = true;
    error.value = "";
    try {
      const next = await VersionService.Undo();
      applyStatus(next);
      await refreshLists(false);
    } catch (value: any) {
      error.value = errorMessage(value);
      throw value;
    } finally {
      loading.value = false;
    }
  }

  async function discard(): Promise<void> {
    loading.value = true;
    error.value = "";
    try {
      const next = await VersionService.Discard();
      applyStatus(next);
      await refreshLists(false);
    } catch (value: any) {
      error.value = errorMessage(value);
      throw value;
    } finally {
      loading.value = false;
    }
  }

  async function checkout(commit: VersionCommit): Promise<void> {
    loading.value = true;
    error.value = "";
    try {
      const next = await VersionService.Checkout(commit.id);
      applyStatus(next);
      await refreshLists(false);
    } catch (value: any) {
      error.value = errorMessage(value);
      throw value;
    } finally {
      loading.value = false;
    }
  }

  async function remove(): Promise<void> {
    loading.value = true;
    error.value = "";
    try {
      const next = await VersionService.Remove();
      applyStatus(next);
      changes.value = [];
      history.value = [];
      expandedCommitID.value = "";
      commitChanges.value = [];
      commitMessage.value = "";
    } catch (value: any) {
      error.value = errorMessage(value);
      throw value;
    } finally {
      loading.value = false;
    }
  }

  async function exportCommit(commit: VersionCommit): Promise<string> {
    if (exporting.value) return "";
    exporting.value = true;
    exportingCommitID.value = commit.id;
    error.value = "";
    try {
      return (await VersionService.ExportCommitFilesDialog(commit.id)) ?? "";
    } catch (value: any) {
      error.value = errorMessage(value);
      throw value;
    } finally {
      exporting.value = false;
      exportingCommitID.value = "";
    }
  }

  async function toggleCommitChanges(commit: VersionCommit): Promise<void> {
    if (expandedCommitID.value === commit.id) {
      expandedCommitID.value = "";
      commitChanges.value = [];
      return;
    }
    loading.value = true;
    error.value = "";
    try {
      const page = await VersionService.ListCommitChanges(commit.id, 0, 500);
      expandedCommitID.value = commit.id;
      commitChanges.value = (page?.changes ?? []).filter(
        (item): item is VersionChange => !!item
      );
    } catch (value: any) {
      error.value = errorMessage(value);
      throw value;
    } finally {
      loading.value = false;
    }
  }

  function eventData(event: any): any {
    return event?.data ?? event;
  }

  Events.On("version:changed", (event: any) => {
    const data = eventData(event);
    if (data?.status) applyStatus(data.status);
    if (data?.status && !data.status.enabled) {
      changes.value = [];
      history.value = [];
      expandedCommitID.value = "";
      commitChanges.value = [];
      commitMessage.value = "";
      return;
    }
    if (Array.isArray(data?.changes)) {
      changes.value = data.changes.filter(
        (item: VersionChange | null | undefined): item is VersionChange => !!item
      );
    } else {
      void refreshLists(false);
    }
  });
  Events.On("archive:opened", () => void refresh());
  Events.On("archive:closed", () => {
    refreshToken++;
    status.value = emptyStatus();
    changes.value = [];
    history.value = [];
    expandedCommitID.value = "";
    commitChanges.value = [];
    error.value = "";
  });
  Events.On("archive:saved", () => void refreshStatusOnly());

  return {
    visible,
    status,
    changes,
    history,
    expandedCommitID,
    commitChanges,
    commitMessage,
    loading,
    committing,
    exporting,
    exportingCommitID,
    error,
    enabled,
    canCommit,
    open,
    close,
    refresh,
    initialize,
    commit,
    undo,
    discard,
    checkout,
    remove,
    exportCommit,
    toggleCommitChanges,
  };
});
