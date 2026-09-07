import { defineStore } from "pinia";
import { ref, computed } from "vue";
import { Events } from "@wailsio/runtime";
import { ArchiveService, EditorService } from "../../bindings/pvfine/services";
import type { ArchiveInfo } from "../../bindings/pvfine/services/models";

/** 归档全局状态:打开/关闭/统计/解包进度 */
export const useArchiveStore = defineStore("archive", () => {
  const info = ref<ArchiveInfo | null>(null);
  const loading = ref(false);
  const loadError = ref("");

  // 解包状态
  const unpacking = ref(false);
  const unpackProgress = ref({ done: 0, total: 0 });
  const unpackMessage = ref("");

  const open = computed(() => !!info.value && info.value.path !== "");
  const modifiedCount = computed(() => info.value?.modifiedCount ?? 0);

  async function openDialog() {
    loading.value = true;
    loadError.value = "";
    try {
      const res = await ArchiveService.OpenDialog();
      if (res) info.value = res;
    } catch (e: any) {
      loadError.value = String(e?.message ?? e);
      throw e;
    } finally {
      loading.value = false;
    }
  }

  async function close() {
    await ArchiveService.Close();
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
  Events.On("archive:opened", (data: any) => {
    info.value = data;
    loading.value = false;
  });
  Events.On("archive:closed", () => {
    info.value = null;
  });
  Events.On("archive:saved", (data: any) => {
    info.value = data;
  });
  Events.On("unpack:progress", (data: any) => {
    unpackProgress.value = { done: data.done ?? 0, total: data.total ?? 0 };
  });
  Events.On("unpack:done", (data: any) => {
    unpacking.value = false;
    unpackMessage.value = data?.message ?? "";
  });

  return {
    info,
    loading,
    loadError,
    open,
    modifiedCount,
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
