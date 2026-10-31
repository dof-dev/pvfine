import { computed, ref, watch } from "vue";
import { defineStore } from "pinia";
import { Events } from "@wailsio/runtime";
import { ExportService } from "../../bindings/pvfine/services";
import type { ExportPreview, ExportRequest } from "../../bindings/pvfine/services/models";
import type { Descriptor } from "../../bindings/pvfine/internal/mod/models";
import { exportFileKey } from "../utils/export-tree";

export type ExportSource = Pick<ExportRequest, "source" | "scopes" | "commitId" | "diffId" | "reverse">;

export function modNameError(name: string): string {
  if (!name || name !== name.trim() || /^[._]/.test(name)) return "名称不能为空、包含首尾空格或以 . / _ 开头";
  if (name.length > 255) return "名称过长";
  if (/[<>:"/\\|?*\x00-\x1f\x7f]/.test(name) || /[. ]$/.test(name)) return "名称包含非法字符";
  if (/^(CON|PRN|AUX|NUL|CONIN\$|CONOUT\$|COM[1-9¹²³]|LPT[1-9¹²³])(\.|$)/i.test(name)) return "不能使用 Windows 保留名称";
  return "";
}

export function modVersionError(version: string): string {
  if (version !== version.trim() || new TextEncoder().encode(version).length > 128) return "版本号不能包含首尾空格或超过 128 字节";
  if (/[\x00-\x1f\x7f]/.test(version)) return "版本号不能包含控制字符";
  return "";
}

export function modIDError(id: string): string {
  return /^[a-z0-9_-]{1,64}$/.test(id) ? "" : "ID 仅支持小写字母、数字、-、_，长度为 1–64";
}

export const useExportStore = defineStore("export", () => {
  const visible = ref(false);
  const title = ref("");
  const mode = ref<"direct" | "mod">("direct");
  const format = ref("110USextend");
  const name = ref("");
  const version = ref("1.0");
  const modId = ref("");
  const includeDependencies = ref(false);
  const formats = ref<Descriptor[]>([]);
  const preview = ref<ExportPreview | null>(null);
  const preparing = ref(false);
  const running = ref(false);
  const error = ref("");
  const source = ref<ExportSource | null>(null);
  const excludedPaths = ref<string[]>([]);
  const selectedFiles = computed(() => {
    const excluded = new Set(excludedPaths.value);
    return (preview.value?.files ?? []).filter((file) => !excluded.has(file.path));
  });
  const selectedCount = computed(() => selectedFiles.value.filter((file) => !file.required).length);
  const selectedDependencyCount = computed(() => selectedFiles.value.reduce((count, file) => count + file.dependencyCount, 0));
  const checkedKeys = computed(() => selectedFiles.value.map((file) => exportFileKey(file.path)));
  const nameError = computed(() => mode.value === "mod" ? modNameError(name.value) : "");
  const versionError = computed(() => mode.value === "mod" ? modVersionError(version.value) : "");
  const idError = computed(() => mode.value === "mod" && format.value === "115CN-mod" ? modIDError(modId.value) : "");
  const canExport = computed(() => visible.value && !!preview.value && selectedCount.value > 0 && !preparing.value && !running.value && !nameError.value && !versionError.value && !idError.value);
  let generation = 0;
  let activePreviewID = "";
  let queue = Promise.resolve();
  let complete: ((path: string) => void) | null = null;

  function releasePreview(): void {
    const id = activePreviewID || preview.value?.id;
    activePreviewID = "";
    preview.value = null;
    if (id) void ExportService.Release(id).catch(() => {});
  }

  function refresh(): Promise<void> {
    const token = ++generation;
    const session = complete;
    // Keep the backend snapshot until Prepare replaces it, allowing name or
    // version edits to reuse content without decoding files again.
    preview.value = null;
    error.value = "";
    preparing.value = visible.value && !!source.value && !nameError.value && !versionError.value && !idError.value;
    if (!preparing.value) return Promise.resolve();
    const request: ExportRequest = {
      ...source.value!, mode: mode.value, format: format.value,
      name: name.value, version: version.value, modId: modId.value, includeDependencies: mode.value === "mod" && includeDependencies.value,
    };
    // Serialize preparations so an older backend request cannot replace the
    // latest preview, even when users change options quickly.
    queue = queue.catch(() => {}).then(async () => {
      if (token !== generation || !visible.value) return;
      try {
        const next = await ExportService.Prepare(request);
        if (!visible.value || complete !== session) {
          if (next?.id) await ExportService.Release(next.id);
          return;
        }
        activePreviewID = next?.id ?? "";
        if (token !== generation) {
          if (!preparing.value) releasePreview();
          return;
        }
        preview.value = next;
      } catch (value: any) {
        if (token === generation) error.value = String(value?.message ?? value);
      } finally {
        if (token === generation) preparing.value = false;
      }
    });
    return queue;
  }

  function open(request: ExportSource, label: string): Promise<string> {
    if (visible.value) return Promise.resolve("");
    generation++;
    mode.value = "direct";
    format.value = "110USextend";
    name.value = "";
    version.value = "1.0";
    modId.value = `pvfine-${crypto.randomUUID()}`;
    includeDependencies.value = false;
    excludedPaths.value = [];
    source.value = { ...request, scopes: [...(request.scopes ?? [])] };
    title.value = label;
    error.value = "";
    formats.value = [];
    visible.value = true;
    const result = new Promise<string>((resolve) => { complete = resolve; });
    const session = complete;
    void ExportService.Formats().then((items) => {
      if (visible.value && complete === session) {
        formats.value = (items ?? []).filter((item) => item.canWrite);
      }
    }).catch((value: any) => {
      if (visible.value && complete === session) error.value = String(value?.message ?? value);
    });
    void refresh();
    return result;
  }

  function finish(path: string): void {
    generation++;
    releasePreview();
    visible.value = false;
    preparing.value = false;
    source.value = null;
    const resolve = complete;
    complete = null;
    resolve?.(path);
  }

  function close(): void {
    if (!running.value) finish("");
  }

  async function execute(): Promise<void> {
    if (!canExport.value || !preview.value) return;
    running.value = true;
    error.value = "";
    try {
      const current = preview.value;
      const paths = new Set((current.files ?? []).map((file) => file.path));
      const path = await ExportService.ExecuteSelectionDialog(current.id, excludedPaths.value.filter((p) => paths.has(p)));
      if (path) finish(path);
    } catch (value: any) {
      const text = String(value?.message ?? value);
      if (!text.toLowerCase().includes("cancel")) {
        error.value = text;
        if (text.includes("过期")) invalidate(true);
      }
    } finally {
      running.value = false;
    }
  }

  function invalidate(force = false): void {
    // Archive comparisons export a fixed snapshot, including captured drafts.
    if (!force && source.value?.source === "archive-diff") return;
    if (!visible.value || (running.value && !force)) return;
    generation++;
    releasePreview();
    preparing.value = false;
    error.value = "归档或版本已变化，请重新准备导出";
  }

  function selectFiles(keys: Array<string | number>): void {
    if (running.value || preparing.value || !preview.value) return;
    const files = preview.value.files ?? [];
    const checked = new Set(keys);
    const visiblePaths = new Set(files.map((file) => file.path));
    excludedPaths.value = [
      ...excludedPaths.value.filter((path) => !visiblePaths.has(path)),
      ...files.filter((file) => !file.required && !checked.has(exportFileKey(file.path))).map((file) => file.path),
    ];
  }

  watch([mode, format, name, version, includeDependencies], () => {
    if (visible.value && !running.value) void refresh();
  });
  watch(modId, () => {
    if (visible.value && !running.value && mode.value === "mod" && format.value === "115CN-mod") void refresh();
  });
  Events.On("archive:opened", close);
  Events.On("archive:closed", close);
  Events.On("archive:changed", () => invalidate());
  Events.On("archive:reloaded", () => invalidate(source.value?.source === "archive-diff"));
  Events.On("version:changed", () => invalidate());
  Events.On("version:committed", () => invalidate());
  return {
    visible, title, mode, format, name, version, includeDependencies, formats, preview,
    modId, idError, preparing, running, error, nameError, versionError, canExport, open, close, refresh, execute, invalidate,
    excludedPaths, selectedCount, selectedDependencyCount, checkedKeys, selectFiles,
  };
});
