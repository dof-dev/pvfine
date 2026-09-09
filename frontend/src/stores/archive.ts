import { defineStore } from "pinia";
import { ref, computed } from "vue";
import { Events } from "@wailsio/runtime";
import { ArchiveService, EditorService } from "../../bindings/pvfine/services";
import type { ArchiveInfo, IndexStatus } from "../../bindings/pvfine/services/models";

const recentArchivesKey = "pvfine.recentArchives";
const maxRecentArchives = 8;

/** 归档全局状态:打开/关闭/统计/解包进度 */
export const useArchiveStore = defineStore("archive", () => {
  const recentArchives = ref<string[]>(readRecentArchives());
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

  function readRecentArchives(): string[] {
    if (typeof window === "undefined") return [];
    try {
      const raw = window.localStorage.getItem(recentArchivesKey);
      if (!raw) return [];
      const parsed: unknown = JSON.parse(raw);
      if (!Array.isArray(parsed)) return [];
      const unique: string[] = [];
      for (const path of parsed) {
        if (typeof path !== "string" || path.trim() === "" || unique.includes(path)) continue;
        unique.push(path);
        if (unique.length >= maxRecentArchives) break;
      }
      return unique;
    } catch {
      return [];
    }
  }

  function persistRecentArchives(): void {
    try {
      window.localStorage.setItem(recentArchivesKey, JSON.stringify(recentArchives.value));
    } catch {
      // 本地存储不可用时仍保留本次运行内的记录。
    }
  }

  function rememberArchive(path: string): void {
    if (!path.trim()) return;
    recentArchives.value = [
      path,
      ...recentArchives.value.filter((item) => item !== path),
    ].slice(0, maxRecentArchives);
    persistRecentArchives();
  }

  function clearRecentArchives(): void {
    recentArchives.value = [];
    persistRecentArchives();
  }

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

  function applyOpenedInfo(res: ArchiveInfo, poll = true): ArchiveInfo {
    info.value = res;
    rememberArchive(res.path);
    indexStatus.value = readIndexStatus({ state: "building", stage: "preparing" });
    if (poll) startIndexPolling();
    return res;
  }

  async function openPath(path: string): Promise<ArchiveInfo | null> {
    if (!path.trim()) return null;
    loading.value = true;
    loadError.value = "";
    try {
      const res = await ArchiveService.Open(path);
      if (!res) return null;
      return applyOpenedInfo(res);
    } catch (e: any) {
      loadError.value = String(e?.message ?? e);
      throw e;
    } finally {
      loading.value = false;
    }
  }

  async function openDialog() {
    loading.value = true;
    loadError.value = "";
    try {
      const res = await ArchiveService.OpenDialog();
      if (res) {
        applyOpenedInfo(res, false);
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

  // 后端事件
  Events.On("archive:opened", (event: any) => {
    const data = eventData(event);
    applyOpenedInfo(data);
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
  Events.On("archive:changed", (event: any) => {
    const data = eventData(event);
    if (data?.path) info.value = data;
  });
  Events.On("archive:batch-applied", () => {
    void refreshInfo();
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
    recentArchives,
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
    openPath,
    clearRecentArchives,
    close,
    refreshInfo,
    unpackDialog,
    cancelUnpack,
  };
});
