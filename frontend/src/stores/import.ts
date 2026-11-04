import { defineStore } from "pinia";
import { computed, ref, watch } from "vue";
import type { ImportPreview } from "../../bindings/pvfine/services/models";

export type ImportMode = "text" | "raw";

/** 文件导入弹窗状态，供工具栏和资源管理器共享。 */
export const useImportStore = defineStore("import", () => {
  const visible = ref(false);
  const targetDir = ref("");
  const mode = ref<ImportMode>("text");
  const sourcePaths = ref<string[]>([]);
  const preview = ref<ImportPreview | null>(null);
  const checkedKeys = ref<Array<string | number>>([]);
  const entries = computed(() => (preview.value?.entries ?? []).filter(entry => !!entry));
  const selectedEntries = computed(() => {
    const checked = new Set(checkedKeys.value);
    return entries.value.filter(entry => checked.has(entry.targetPath));
  });
  const allChecked = computed(() => entries.value.length > 0 && selectedEntries.value.length === entries.value.length);
  const partiallyChecked = computed(() => selectedEntries.value.length > 0 && !allChecked.value);
  const selectedAddedCount = computed(() => selectedEntries.value.filter(entry => !entry.overwrite).length);
  const selectedOverwriteCount = computed(() => selectedEntries.value.length - selectedAddedCount.value);
  const selectedConvertedCount = computed(() => selectedEntries.value.filter(entry => entry.sourceEncoding && entry.sourceEncoding !== "UTF-8").length);

  function selectAll(checked: boolean): void {
    checkedKeys.value = checked ? entries.value.map(entry => entry.targetPath) : [];
  }

  watch(preview, () => selectAll(true), { flush: "sync" });

  const running = ref(false);
  const error = ref("");

  function open(target = ""): void {
    if (running.value) return;
    targetDir.value = target.replaceAll("\\", "/").replace(/^\/+|\/+$/g, "");
    mode.value = "text";
    sourcePaths.value = [];
    preview.value = null;
    error.value = "";
    visible.value = true;
  }

  function close(): void {
    if (running.value) return;
    visible.value = false;
    sourcePaths.value = [];
    preview.value = null;
    error.value = "";
  }

  return {
    visible,
    targetDir,
    mode,
    sourcePaths,
    preview,
    checkedKeys,
    selectedEntries,
    allChecked,
    partiallyChecked,
    selectedAddedCount,
    selectedOverwriteCount,
    selectedConvertedCount,
    selectAll,
    running,
    error,
    open,
    close,
  };
});
