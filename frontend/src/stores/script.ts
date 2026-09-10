import { computed, ref } from "vue";
import { defineStore } from "pinia";
import { Events } from "@wailsio/runtime";
import { ScriptService } from "../../bindings/pvfine/services";
import type {
  ScriptApplyResult,
  ScriptCompileResult,
  ScriptDiagnostic,
  ScriptFile,
  ScriptFilePreview,
  ScriptLog,
  ScriptPreviewPage,
  ScriptRunResult,
} from "../../bindings/pvfine/services/models";
import { useArchiveStore } from "./archive";

export const defaultScriptName = "untitled.pvf.js";
export const defaultScriptSource = `const minimumPrice = 10;
for (const file of pvf.glob("equipment/**/*.equ")) {
\tconst document = file.parse();
\tconst price = document.section("price");
\tif (!price) continue;
\tconst currentPrice = price.get();
\tif (typeof currentPrice !== "number" || currentPrice >= minimumPrice) continue;
\tprice.set(minimumPrice);
\tfile.write(document);
}
`;

/** 脚本工作区状态、运行日志和当前预览计划。 */
export const useScriptStore = defineStore("script", () => {
  const archive = useArchiveStore();
  const workspaceVisible = ref(false);
  const files = ref<ScriptFile[]>([]);
  const directory = ref("");
  const currentName = ref(defaultScriptName);
  const source = ref(defaultScriptSource);
  const savedSource = ref(defaultScriptSource);
  const loadingFiles = ref(false);
  const loadingScript = ref(false);
  const saving = ref(false);
  const running = ref(false);
  const stopping = ref(false);
  const applying = ref(false);
  const refreshing = ref(false);
  const error = ref("");
  const stale = ref(false);
  const compileResult = ref<ScriptCompileResult | null>(null);
  const runResult = ref<ScriptRunResult | null>(null);
  const logs = ref<ScriptLog[]>([]);
  const progress = ref({ done: 0, total: 0, message: "", currentPath: "" });
  const rows = ref<ScriptFilePreview[]>([]);
  const planId = ref("");
  const nextCursor = ref(-1);
  const scannedFiles = ref(0);
  const modifiedFiles = ref(0);
  const selectedIndexes = ref<Set<number>>(new Set());
  let selectionMode: "all" | "none" | "some" = "all";
  const excludedIndexes = new Set<number>();
  let runRequest = 0;

  const dirty = computed(() => source.value !== savedSource.value);
  const hasPreview = computed(() => !!planId.value && !stale.value);
  const selectedCount = computed(() => {
    // Read the reactive set in both modes so toggling one loaded row also
    // refreshes the total count while the all-pages mode uses exclusions.
    const loadedSelectedCount = selectedIndexes.value.size;
    if (selectionMode === "all") {
      return Math.max(0, modifiedFiles.value - excludedIndexes.size);
    }
    return loadedSelectedCount;
  });
  const canRun = computed(
    () => archive.open && !running.value && !saving.value && source.value.trim() !== "",
  );
  const canApply = computed(
    () => hasPreview.value && !running.value && !applying.value && selectedCount.value > 0,
  );
  const diagnostics = computed(() =>
    (compileResult.value?.diagnostics ?? []).filter(
      (item): item is ScriptDiagnostic => !!item,
    ),
  );

  function eventData(event: any): any {
    return event?.data ?? event;
  }

  function errorMessage(value: any): string {
    return String(value?.message ?? value ?? "脚本操作失败");
  }

  function resetPreview(): void {
    runRequest++;
    rows.value = [];
    planId.value = "";
    nextCursor.value = -1;
    scannedFiles.value = 0;
    modifiedFiles.value = 0;
    selectedIndexes.value = new Set();
    selectionMode = "all";
    excludedIndexes.clear();
    stale.value = false;
    runResult.value = null;
    progress.value = { done: 0, total: 0, message: "", currentPath: "" };
  }

  function resetForNewSource(): void {
    compileResult.value = null;
    error.value = "";
    resetPreview();
  }

  function showWorkspace(): void {
    workspaceVisible.value = true;
    void refreshFiles();
  }

  function hideWorkspace(): void {
    if (running.value) return;
    workspaceVisible.value = false;
  }

  // 外部文件入口（资源管理器、文件集、书签等）打开归档文件时，
  // 允许直接切回归档编辑，即使脚本仍在后台运行。
  function showArchiveEditor(): void {
    workspaceVisible.value = false;
  }

  async function refreshFiles(): Promise<void> {
    if (loadingFiles.value) return;
    loadingFiles.value = true;
    error.value = "";
    try {
      const [result, path] = await Promise.all([
        ScriptService.ListScripts(),
        ScriptService.ScriptDirectory(),
      ]);
      files.value = (result ?? []).filter((item): item is ScriptFile => !!item);
      directory.value = path;
    } catch (value: any) {
      error.value = errorMessage(value);
    } finally {
      loadingFiles.value = false;
    }
  }

  function newScript(): void {
    currentName.value = defaultScriptName;
    source.value = defaultScriptSource;
    savedSource.value = defaultScriptSource;
    resetForNewSource();
  }

  async function loadScript(name: string): Promise<void> {
    if (loadingScript.value || running.value) return;
    loadingScript.value = true;
    error.value = "";
    try {
      const text = await ScriptService.LoadScript(name);
      currentName.value = name;
      source.value = text;
      savedSource.value = text;
      resetForNewSource();
    } catch (value: any) {
      error.value = errorMessage(value);
    } finally {
      loadingScript.value = false;
    }
  }

  function updateSource(text: string): void {
    source.value = text;
    compileResult.value = null;
    if (planId.value) stale.value = true;
  }

  async function saveScript(name = currentName.value): Promise<void> {
    if (saving.value) return;
    saving.value = true;
    error.value = "";
    try {
      let nextName = name.trim() || defaultScriptName;
      if (!nextName.toLowerCase().endsWith(".pvf.js")) nextName += ".pvf.js";
      await ScriptService.SaveScript(nextName, source.value);
      currentName.value = nextName;
      savedSource.value = source.value;
      await refreshFiles();
    } catch (value: any) {
      error.value = errorMessage(value);
      throw value;
    } finally {
      saving.value = false;
    }
  }

  async function openDirectory(): Promise<void> {
    try {
      await ScriptService.OpenScriptDirectory();
    } catch (value: any) {
      error.value = errorMessage(value);
      throw value;
    }
  }

  async function compile(): Promise<ScriptCompileResult> {
    error.value = "";
    const result = await ScriptService.Compile(source.value);
    compileResult.value = result;
    return result;
  }

  function applyPage(page: ScriptPreviewPage, append: boolean): void {
    const pageRows = (page.rows ?? []).filter(
      (item): item is ScriptFilePreview => !!item,
    );
    if (append) rows.value.push(...pageRows);
    else rows.value = pageRows;
    const selected = new Set(selectedIndexes.value);
    for (const row of pageRows) {
      if (
        row.status === "changed" &&
        selectionMode === "all" &&
        !excludedIndexes.has(row.fileIndex)
      ) {
        selected.add(row.fileIndex);
      }
    }
    selectedIndexes.value = selected;
    planId.value = page.planId;
    nextCursor.value = page.nextCursor;
    scannedFiles.value = page.scannedFiles;
    modifiedFiles.value = page.modifiedFiles;
  }

  async function run(): Promise<ScriptRunResult> {
    if (!canRun.value) throw new Error("当前无法运行脚本");
    const request = ++runRequest;
    running.value = true;
    stopping.value = false;
    error.value = "";
    compileResult.value = null;
    rows.value = [];
    planId.value = "";
    nextCursor.value = -1;
    selectedIndexes.value = new Set();
    selectionMode = "all";
    excludedIndexes.clear();
    stale.value = false;
    runResult.value = null;
    logs.value = [];
    progress.value = { done: 0, total: 0, message: "准备运行…", currentPath: "" };
    try {
      const result = await ScriptService.Run({ name: currentName.value, source: source.value });
      if (request !== runRequest) return result;
      runResult.value = result;
      if (result.logs) logs.value = result.logs.filter((item): item is ScriptLog => !!item);
      if (result.status === "completed" && result.planId) {
        const page = await ScriptService.PreviewPage(result.planId, 0, 100);
        if (page) applyPage(page, false);
      } else if (result.error) {
        error.value = result.error.message;
      }
      return result;
    } catch (value: any) {
      if (request === runRequest) error.value = errorMessage(value);
      throw value;
    } finally {
      if (request === runRequest) {
        running.value = false;
        stopping.value = false;
      }
    }
  }

  async function cancel(): Promise<void> {
    if (!running.value) return;
    stopping.value = true;
    try {
      await ScriptService.Cancel();
    } catch (value: any) {
      stopping.value = false;
      error.value = errorMessage(value);
    }
  }

  async function loadMore(): Promise<void> {
    if (!hasPreview.value || nextCursor.value < 0 || !planId.value) return;
    try {
      const page = await ScriptService.PreviewPage(planId.value, nextCursor.value, 100);
      if (page) applyPage(page, true);
    } catch (value: any) {
      stale.value = true;
      error.value = errorMessage(value);
    }
  }

  async function loadAll(): Promise<void> {
    while (hasPreview.value && nextCursor.value >= 0) await loadMore();
  }

  function toggleSelected(fileIndex: number): void {
    const selected = new Set(selectedIndexes.value);
    if (selectionMode === "all") {
      if (selected.has(fileIndex)) {
        selected.delete(fileIndex);
        excludedIndexes.add(fileIndex);
      } else {
        selected.add(fileIndex);
        excludedIndexes.delete(fileIndex);
      }
    } else if (selected.has(fileIndex)) {
      selected.delete(fileIndex);
    } else {
      selected.add(fileIndex);
    }
    if (selectionMode !== "all") selectionMode = selected.size > 0 ? "some" : "none";
    selectedIndexes.value = selected;
  }

  function selectAll(): void {
    selectionMode = "all";
    excludedIndexes.clear();
    selectedIndexes.value = new Set(
      rows.value.filter((row) => row.status === "changed").map((row) => row.fileIndex),
    );
  }

  function clearSelection(): void {
    selectionMode = "none";
    excludedIndexes.clear();
    selectedIndexes.value = new Set();
  }

  async function apply(): Promise<ScriptApplyResult> {
    if (!canApply.value || !planId.value) throw new Error("没有选中的脚本变更");
    applying.value = true;
    error.value = "";
    try {
      await loadAll();
      if (!hasPreview.value) throw new Error("脚本预览已过期,请重新运行");
      const result = await ScriptService.Apply(planId.value, [...selectedIndexes.value]);
      runResult.value = runResult.value
        ? { ...runResult.value, modifiedFiles: result.modifiedCount }
        : runResult.value;
      resetPreview();
      return result;
    } catch (value: any) {
      error.value = errorMessage(value);
      if (error.value.includes("预览已过期")) stale.value = true;
      throw value;
    } finally {
      applying.value = false;
    }
  }

  async function discard(): Promise<void> {
    const id = planId.value;
    resetPreview();
    if (id) await ScriptService.Discard(id);
  }

  function markStale(): void {
    if (planId.value) stale.value = true;
  }

  function handleState(event: any): void {
    const data = eventData(event);
    if (!data?.runId || (runResult.value?.runId && data.runId !== runResult.value.runId)) return;
    if (data.status === "running") running.value = true;
    if (["completed", "failed", "cancelled"].includes(data.status)) running.value = false;
  }

  function handleLog(event: any): void {
    const data = eventData(event);
    if (!data?.runId) return;
    if (runResult.value?.runId && data.runId !== runResult.value.runId) return;
    if (!runResult.value) runResult.value = { runId: data.runId, status: "running", scannedFiles: 0, modifiedFiles: 0, durationMs: 0, logs: [] };
    logs.value.push({ level: String(data.level ?? "info"), message: String(data.message ?? "") });
  }

  function handleProgress(event: any): void {
    const data = eventData(event);
    if (!data?.runId) return;
    if (runResult.value?.runId && data.runId !== runResult.value.runId) return;
    progress.value = {
      done: Number(data.done ?? 0),
      total: Number(data.total ?? 0),
      message: String(data.message ?? ""),
      currentPath: String(data.currentPath ?? ""),
    };
  }

  Events.On("script:state", handleState);
  Events.On("script:log", handleLog);
  Events.On("script:progress", handleProgress);
  Events.On("archive:advanced-search-stale", markStale);
  Events.On("archive:batch-applied", markStale);
  Events.On("archive:script-applied", markStale);
  Events.On("archive:reloaded", markStale);
  Events.On("archive:opened", () => markStale());
  Events.On("archive:closed", () => markStale());

  return {
    workspaceVisible,
    files,
    directory,
    currentName,
    source,
    loadingFiles,
    loadingScript,
    saving,
    running,
    stopping,
    applying,
    refreshing,
    error,
    dirty,
    stale,
    compileResult,
    diagnostics,
    runResult,
    logs,
    progress,
    rows,
    planId,
    nextCursor,
    scannedFiles,
    modifiedFiles,
    selectedIndexes,
    selectedCount,
    hasPreview,
    canRun,
    canApply,
    showWorkspace,
    hideWorkspace,
    showArchiveEditor,
    refreshFiles,
    newScript,
    loadScript,
    updateSource,
    saveScript,
    openDirectory,
    compile,
    run,
    cancel,
    loadMore,
    loadAll,
    toggleSelected,
    selectAll,
    clearSelection,
    apply,
    discard,
  };
});
