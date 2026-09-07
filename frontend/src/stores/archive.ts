import { defineStore } from "pinia";
import { ref, computed } from "vue";
import { Events } from "@wailsio/runtime";
import { ArchiveService, EditorService } from "../../bindings/pvfine/services";
import type { ArchiveInfo, IndexStatus } from "../../bindings/pvfine/services/models";

/** 归档全局状态:打开/关闭/统计/解包进度 */
export const useArchiveStore = defineStore("archive", () => {
  const info = ref<ArchiveInfo | null>(null);
  const loading = ref(false);
  const loadError = ref("");
  const indexStatus = ref<IndexStatus>({
    state: "idle",
    stage: "",
    done: 0,
    total: 0,
    skipped: 0,
    error: "",
  });

  // 解包状态
  const unpacking = ref(false);
  const unpackProgress = ref({ done: 0, total: 0 });
  const unpackMessage = ref("");

  const open = computed(() => !!info.value && info.value.path !== "");
  const modifiedCount = computed(() => info.value?.modifiedCount ?? 0);
  const indexing = computed(() => open.value && indexStatus.value.state === "building");
  const indexReady = computed(() => open.value && indexStatus.value.state === "ready");
  let indexPollTimer: number | undefined;
  let indexPollBusy = false;

  function readIndexStatus(data: any): IndexStatus {
    return {
      state: String(data?.state ?? "idle"),
      stage: String(data?.stage ?? ""),
      done: Number(data?.done ?? 0),
      total: Number(data?.total ?? 0),
      skipped: Number(data?.skipped ?? 0),
      error: String(data?.error ?? ""),
    };
  }

  function eventData(event: any): any {
    return event?.data ?? event;
  }

  function stopIndexPolling() {
    window.clearInterval(indexPollTimer);
    indexPollTimer = undefined;
  }

  async function refreshIndexStatus() {
    if (!open.value || indexPollBusy) return;
    indexPollBusy = true;
    try {
      const status = readIndexStatus(await ArchiveService.IndexStatus());
      indexStatus.value = status;
      if (status.state === "ready" || status.state === "error") stopIndexPolling();
    } catch {
      // Event delivery remains the primary path; a transient poll failure is harmless.
    } finally {
      indexPollBusy = false;
    }
  }

  function startIndexPolling() {
    stopIndexPolling();
    void refreshIndexStatus();
    indexPollTimer = window.setInterval(() => void refreshIndexStatus(), 250);
  }

  async function openDialog() {
    loading.value = true;
    loadError.value = "";
    try {
      const res = await ArchiveService.OpenDialog();
      if (res) {
        info.value = res;
        indexStatus.value = readIndexStatus(await ArchiveService.IndexStatus());
        startIndexPolling();
      }
    } catch (e: any) {
      loadError.value = String(e?.message ?? e);
      throw e;
    } finally {
      loading.value = false;
    }
  }

  async function close() {
    await ArchiveService.Close();
    stopIndexPolling();
    info.value = null;
  }

  async function refreshInfo() {
    info.value = await ArchiveService.Info();
  }

  async function unpackDialog(): Promise<boolean> {
    unpackMessage.value = "";
    const started = await EditorService.UnpackDialog();
    unpacking.value = started;
    return started;
  }

  function cancelUnpack() {
    EditorService.CancelUnpack();
  }

  async function exportFile(index: number) {
    return EditorService.ExportFileDialog(index);
  }

  // 后端事件
  Events.On("archive:opened", (event: any) => {
    const data = eventData(event);
    info.value = data;
    indexStatus.value = readIndexStatus({ state: "building", stage: "preparing" });
    startIndexPolling();
    loading.value = false;
  });
  Events.On("archive:closed", () => {
    stopIndexPolling();
    info.value = null;
    indexStatus.value = readIndexStatus(null);
  });
  Events.On("archive:index-progress", (event: any) => {
    indexStatus.value = readIndexStatus(eventData(event));
  });
  Events.On("archive:index-ready", (event: any) => {
    stopIndexPolling();
    indexStatus.value = readIndexStatus(eventData(event));
  });
  Events.On("archive:index-error", (event: any) => {
    stopIndexPolling();
    const data = eventData(event);
    indexStatus.value = readIndexStatus({ ...data, state: "error" });
  });
  Events.On("archive:saved", (event: any) => {
    info.value = eventData(event);
  });
  Events.On("unpack:progress", (event: any) => {
    const data = eventData(event);
    unpackProgress.value = { done: data.done ?? 0, total: data.total ?? 0 };
  });
  Events.On("unpack:done", (event: any) => {
    const data = eventData(event);
    unpacking.value = false;
    unpackMessage.value = data?.message ?? "";
  });

  return {
    info,
    loading,
    loadError,
    open,
    modifiedCount,
    indexStatus,
    indexing,
    indexReady,
    unpacking,
    unpackProgress,
    unpackMessage,
    openDialog,
    close,
    refreshInfo,
    unpackDialog,
    cancelUnpack,
    exportFile,
  };
});
