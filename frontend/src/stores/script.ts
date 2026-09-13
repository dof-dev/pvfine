import { computed, ref, watch } from "vue";
import { defineStore } from "pinia";
import { Events } from "@wailsio/runtime";
import { ScriptService, ScriptWindowService } from "../../bindings/pvfine/services";
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
  // 工作区已分离到独立窗口时为 true：主窗口不再内嵌脚本界面，工具栏按钮
  // 改为聚焦那个窗口。
  const workspaceDetached = ref(false);
  // 独立窗口里的未保存状态。主窗口有自己的一份 Pinia store，拿不到对方的
  // dirty，只能由脚本窗口主动上报。
  const detachedDirty = ref(false);
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
  // Path filter applied by the backend so paging and the matched total stay
  // consistent; never filter rows.value locally or later pages get lost.
  const filter = ref("");
  const matchedFiles = ref(0);
  const selectedKeys = ref<Set<string>>(new Set());
  let selectionMode: "all" | "none" | "some" = "all";
  const excludedKeys = new Set<string>();
  let runRequest = 0;
  let filterTimer: ReturnType<typeof setTimeout> | null = null;

  const dirty = computed(() => source.value !== savedSource.value);
  const hasPreview = computed(() => !!planId.value && !stale.value);
  const filtered = computed(() => filter.value.trim() !== "");
  const selectedCount = computed(() => {
    // Read the reactive set in both modes so toggling one loaded row also
    // refreshes the total count while the all-pages mode uses exclusions.
    const loadedSelectedCount = selectedKeys.value.size;
    if (selectionMode === "all") {
      return Math.max(0, matchedFiles.value - excludedKeys.size);
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
    if (filterTimer) {
      clearTimeout(filterTimer);
      filterTimer = null;
    }
    rows.value = [];
    planId.value = "";
    nextCursor.value = -1;
    scannedFiles.value = 0;
    modifiedFiles.value = 0;
    filter.value = "";
    matchedFiles.value = 0;
    selectedKeys.value = new Set();
    selectionMode = "all";
    excludedKeys.clear();
    stale.value = false;
    runResult.value = null;
    progress.value = { done: 0, total: 0, message: "", currentPath: "" };
  }

  /** 重新从第一页拉取预览；筛选变化后必须重取，否则只筛已加载的行。 */
  async function reloadPreview(): Promise<void> {
    if (!planId.value || !hasPreview.value) return;
    const request = runRequest;
    try {
      const page = await ScriptService.PreviewPage(planId.value, filter.value, 0, 100);
      if (request !== runRequest) return;
      if (page) applyPage(page, false);
    } catch (value: any) {
      stale.value = true;
      error.value = errorMessage(value);
    }
  }

  /**
   * 修改文件路径筛选。筛选由后端执行，因此这里要重取第一页，
   * 并把选择重置为“当前筛选结果全选”，避免隐藏的旧排除项继续生效。
   */
  function setFilter(value: string): void {
    if (filter.value === value) return;
    filter.value = value;
    selectionMode = "all";
    excludedKeys.clear();
    selectedKeys.value = new Set();
    if (filterTimer) clearTimeout(filterTimer);
    filterTimer = setTimeout(() => {
      filterTimer = null;
      void reloadPreview();
    }, 200);
  }

  function clearFilter(): void {
    if (filterTimer) {
      clearTimeout(filterTimer);
      filterTimer = null;
    }
    setFilter("");
  }

  function resetForNewSource(): void {
    compileResult.value = null;
    error.value = "";
    resetPreview();
  }

  /** 把当前脚本内容交付给独立窗口，并让主窗口回到归档编辑。 */
  async function detachWorkspace(): Promise<void> {
    if (!archive.open || running.value) return;
    error.value = "";
    try {
      await ScriptWindowService.OpenScriptWindow(sessionSnapshot());
      workspaceDetached.value = true;
      workspaceVisible.value = false;
    } catch (value: any) {
      error.value = errorMessage(value);
      throw value;
    }
  }

  /** 聚焦已分离的脚本窗口；窗口已不在时退回内嵌模式。 */
  async function focusScriptWindow(): Promise<void> {
    try {
      const focused = await ScriptWindowService.FocusScriptWindow();
      if (!focused) {
        workspaceDetached.value = false;
        showWorkspace();
      }
    } catch (value: any) {
      error.value = errorMessage(value);
    }
  }

  function sessionSnapshot(): { name: string; source: string; savedSource: string } {
    return {
      name: currentName.value,
      source: source.value,
      savedSource: savedSource.value,
    };
  }

  /** 独立窗口关闭后接收交回的内容，供主窗口下次进入工作区时恢复。 */
  function adoptHandedBackSession(session: { name?: string; source?: string; savedSource?: string } | null): void {
    workspaceDetached.value = false;
    detachedDirty.value = false;
    if (!session) return;
    if (typeof session.name === "string" && session.name !== "") currentName.value = session.name;
    if (typeof session.source !== "string") return;
    source.value = session.source;
    // savedSource 决定"未保存"标记，必须一起带回来。
    savedSource.value =
      typeof session.savedSource === "string" ? session.savedSource : session.source;
    resetForNewSource();
  }

  // 脚本窗口的 Pinia store 是独立的，运行日志、预览计划、源码都在那边，
  // 主窗口这份不再有意义；同步拉取暂存内容即可。
  async function syncFromDetachedWindow(): Promise<void> {
    try {
      const session = await ScriptWindowService.LoadScriptSession();
      adoptHandedBackSession(session);
    } catch (value: any) {
      error.value = errorMessage(value);
    }
  }

  function showWorkspace(): void {
    if (!archive.open) return;
    // 已分离时按钮语义是聚焦独立窗口，不在主窗口内嵌打开。
    if (workspaceDetached.value) {
      void focusScriptWindow();
      return;
    }
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

  function clearLogs(): void {
    logs.value = [];
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
    const selected = new Set(selectedKeys.value);
    for (const row of pageRows) {
      if (
        isSelectable(row) &&
        selectionMode === "all" &&
        !excludedKeys.has(row.changeKey)
      ) {
        selected.add(row.changeKey);
      }
    }
    selectedKeys.value = selected;
    planId.value = page.planId;
    nextCursor.value = page.nextCursor;
    scannedFiles.value = page.scannedFiles;
    modifiedFiles.value = page.modifiedFiles;
    matchedFiles.value = page.matchedFiles ?? page.modifiedFiles ?? 0;
  }

  /** 只有有实际变更的行可以勾选：新建、删除和已修改。 */
  function isSelectable(row: ScriptFilePreview): boolean {
    return (
      row.status === "changed" || row.status === "added" || row.status === "deleted"
    );
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
    filter.value = "";
    matchedFiles.value = 0;
    selectedKeys.value = new Set();
    selectionMode = "all";
    excludedKeys.clear();
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
        const page = await ScriptService.PreviewPage(result.planId, "", 0, 100);
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
      const page = await ScriptService.PreviewPage(planId.value, filter.value, nextCursor.value, 100);
      if (page) applyPage(page, true);
    } catch (value: any) {
      stale.value = true;
      error.value = errorMessage(value);
    }
  }

  function selectAll(): void {
    selectionMode = "all";
    excludedKeys.clear();
    selectedKeys.value = new Set(
      rows.value.filter((row) => isSelectable(row)).map((row) => row.changeKey),
    );
  }

  function clearSelection(): void {
    selectionMode = "none";
    excludedKeys.clear();
    selectedKeys.value = new Set();
  }

  /** 切换单行；只影响当前筛选结果内的这一行。 */
  function toggleSelected(changeKey: string): void {
    const selected = new Set(selectedKeys.value);
    if (selectionMode === "all") {
      if (selected.has(changeKey)) {
        selected.delete(changeKey);
        excludedKeys.add(changeKey);
      } else {
        selected.add(changeKey);
        excludedKeys.delete(changeKey);
      }
    } else if (selected.has(changeKey)) {
      selected.delete(changeKey);
    } else {
      selected.add(changeKey);
    }
    if (selectionMode !== "all") selectionMode = selected.size > 0 ? "some" : "none";
    selectedKeys.value = selected;
  }

  async function apply(): Promise<ScriptApplyResult> {
    if (!canApply.value || !planId.value) throw new Error("没有选中的脚本变更");
    applying.value = true;
    error.value = "";
    try {
      const planID = planId.value;
      // In "all" mode the intent is every row matching the current filter,
      // including pages never fetched. Ask the backend for that exact set so a
      // filter cannot silently shrink what gets applied.
      let keys: string[];
      if (selectionMode === "all") {
        const all = await ScriptService.SelectableChangeKeys(planID, filter.value);
        keys = (all ?? []).filter((key) => !excludedKeys.has(key));
      } else {
        keys = [...selectedKeys.value];
      }
      if (keys.length === 0) throw new Error("没有选中的脚本变更");
      if (!hasPreview.value) throw new Error("脚本预览已过期,请重新运行");
      const result = await ScriptService.Apply(planID, keys);
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
  Events.On("archive:closed", () => {
    markStale();
    workspaceVisible.value = false;
    // 归档关掉后独立脚本窗口会自行收起（它没有打开归档的入口），复位的
    // 责任在这里：否则工具栏按钮会一直指向一个正在关闭的窗口。
    workspaceDetached.value = false;
    detachedDirty.value = false;
  });
  // 独立窗口关闭：拉回它交出的脚本内容，主窗口停在归档编辑。
  Events.On("script-window:closed", () => {
    void syncFromDetachedWindow();
  });
  // 独立窗口上报未保存状态，主窗口的退出确认需要知道它。
  Events.On("script-window:dirty", (event: any) => {
    const data = eventData(event);
    detachedDirty.value = Boolean(data?.dirty);
  });

  watch(
    () => archive.open,
    (isOpen) => {
      if (!isOpen) {
        workspaceVisible.value = false;
      }
    },
    { immediate: true },
  );

  return {
    workspaceVisible,
    workspaceDetached,
    detachedDirty,
    files,
    directory,
    currentName,
    source,
    savedSource,
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
    filter,
    matchedFiles,
    filtered,
    selectedKeys,
    isSelectable,
    selectedCount,
    hasPreview,
    canRun,
    canApply,
    showWorkspace,
    hideWorkspace,
    showArchiveEditor,
    detachWorkspace,
    focusScriptWindow,
    adoptHandedBackSession,
    syncFromDetachedWindow,
    refreshFiles,
    newScript,
    loadScript,
    updateSource,
    clearLogs,
    saveScript,
    openDirectory,
    compile,
    run,
    cancel,
    loadMore,
    setFilter,
    clearFilter,
    toggleSelected,
    selectAll,
    clearSelection,
    apply,
    discard,
  };
});
