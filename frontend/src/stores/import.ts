import { defineStore } from "pinia";
import { ref } from "vue";
import type { ImportPreview } from "../../bindings/pvfine/services/models";

export type ImportMode = "text" | "raw";

/** 文件导入弹窗状态，供工具栏和资源管理器共享。 */
export const useImportStore = defineStore("import", () => {
  const visible = ref(false);
  const targetDir = ref("");
  const mode = ref<ImportMode>("text");
  const sourcePaths = ref<string[]>([]);
  const preview = ref<ImportPreview | null>(null);
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
    running,
    error,
    open,
    close,
  };
});
