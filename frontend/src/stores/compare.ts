import { computed, ref } from "vue";
import { defineStore } from "pinia";
import { CompareService } from "../../bindings/pvfine/services";
import type {
  CompareSummary,
  CompareFileItem,
  CompareFileDiff,
  MergeResult,
} from "../../bindings/pvfine/services/models";
import { useArchiveStore } from "./archive";

export type CompareFilterType = "all" | "modify" | "add" | "delete" | "same";
export type CompareViewMode = "tree" | "flat";

export const useCompareStore = defineStore("compare", () => {
  const archive = useArchiveStore();

  const visible = ref(false);
  const targetPath = ref("");

  const summary = ref<CompareSummary | null>(null);
  const activeFilePath = ref<string>("");
  const activeDiff = ref<CompareFileDiff | null>(null);

  const loading = ref(false);
  const diffLoading = ref(false);
  const merging = ref(false);
  const error = ref("");
  const diffError = ref("");

  const filter = ref<CompareFilterType>("all");
  const searchQuery = ref("");
  const viewMode = ref<CompareViewMode>("tree");
  const sameFiles = ref<CompareFileItem[]>([]);
  const loadingSame = ref(false);

  // 勾选与合并状态
  const selectedPaths = ref<Set<string>>(new Set());
  const mergedPaths = ref<Set<string>>(new Set());

  // 树节点展开状态
  const expandedKeys = ref<Set<string>>(new Set());

  const hasSummary = computed(() => summary.value !== null);

  // 本地当前归档路径
  const localArchivePath = computed(() => {
    return archive.info?.path || "(当前已打开归档)";
  });

  const filteredFiles = computed(() => {
    if (!summary.value) return [];
    if (filter.value === "same") {
      const q = searchQuery.value.trim().toLowerCase();
      if (!q) return sameFiles.value;
      return sameFiles.value.filter((f) => f.path.toLowerCase().includes(q));
    }
    if (!summary.value.files) return [];
    let list = summary.value.files.filter((f): f is CompareFileItem => f !== null);

    if (filter.value !== "all") {
      list = list.filter((f) => f.operation === filter.value);
    }

    const q = searchQuery.value.trim().toLowerCase();
    if (q) {
      list = list.filter((f) => f.path.toLowerCase().includes(q));
    }

    return list;
  });

  const fileMap = computed<Map<string, CompareFileItem>>(() => {
    const map = new Map<string, CompareFileItem>();
    if (summary.value?.files) {
      for (const f of summary.value.files) {
        if (f) map.set(f.path, f);
      }
    }
    for (const f of sameFiles.value) {
      if (f) map.set(f.path, f);
    }
    return map;
  });

  async function setFilter(newFilter: CompareFilterType): Promise<void> {
    filter.value = newFilter;
    if (newFilter === "same" && sameFiles.value.length === 0) {
      await loadSameFiles();
    }
  }

  async function loadSameFiles(): Promise<void> {
    if (!summary.value) return;
    loadingSame.value = true;
    try {
      const res = await CompareService.GetSameFiles(searchQuery.value, 0, 500);
      const items = res?.[0] ?? [];
      sameFiles.value = items.filter((f): f is CompareFileItem => f !== null);
    } catch {
      sameFiles.value = [];
    } finally {
      loadingSame.value = false;
    }
  }

  function open(): boolean {
    if (!archive.open) {
      return false;
    }
    visible.value = true;
    return true;
  }

  function close(): void {
    visible.value = false;
    activeFilePath.value = "";
    activeDiff.value = null;
    sameFiles.value = [];
    selectedPaths.value.clear();
    mergedPaths.value.clear();
    void CompareService.CloseCompare();
  }

  async function selectTargetPath(): Promise<void> {
    try {
      const path = await CompareService.SelectPVFDialog("选择对比的目标 PVF 文件");
      if (path) {
        targetPath.value = path;
      }
    } catch (e: any) {
      if (!String(e?.message ?? e).includes("cancel")) {
        error.value = `选择文件失败: ${e?.message ?? e}`;
      }
    }
  }

  async function startCompare(): Promise<void> {
    error.value = "";
    if (!archive.open) {
      error.value = "当前未打开 PVF 归档，请先在主界面打开归档";
      return;
    }
    if (!targetPath.value.trim()) {
      error.value = "请选择要对比的目标 PVF 文件";
      return;
    }

    loading.value = true;
    activeFilePath.value = "";
    activeDiff.value = null;
    diffError.value = "";
    sameFiles.value = [];
    selectedPaths.value.clear();
    mergedPaths.value.clear();

    try {
      const result = await CompareService.Compare(
        "",
        targetPath.value.trim(),
        true
      );
      summary.value = result;

      // 默认展开顶层目录
      const defaultExpanded = new Set<string>();
      if (result?.files) {
        for (const f of result.files) {
          if (!f?.path) continue;
          const slash = f.path.indexOf("/");
          if (slash > 0) {
            defaultExpanded.add(f.path.substring(0, slash));
          }
        }
      }
      expandedKeys.value = defaultExpanded;

      // 默认选中第一个修改或有差异的文件
      if (result && result.files && result.files.length > 0) {
        const nonNullFiles = result.files.filter((f): f is CompareFileItem => f !== null);
        const firstDiff = nonNullFiles.find((f) => f.operation !== "same") || nonNullFiles[0];
        if (firstDiff) {
          void selectFile(firstDiff.path);
        }
      }
    } catch (e: any) {
      error.value = `比对失败: ${e?.message ?? e}`;
      summary.value = null;
    } finally {
      loading.value = false;
    }
  }

  async function selectFile(path: string): Promise<void> {
    if (!path) return;
    activeFilePath.value = path;
    diffLoading.value = true;
    diffError.value = "";
    try {
      const diff = await CompareService.GetDiff(path);
      if (activeFilePath.value === path) {
        activeDiff.value = diff;
      }
    } catch (e: any) {
      if (activeFilePath.value === path) {
        diffError.value = `获取 Diff 失败: ${e?.message ?? e}`;
        activeDiff.value = null;
      }
    } finally {
      if (activeFilePath.value === path) {
        diffLoading.value = false;
      }
    }
  }

  // 勾选管理
  function toggleSelect(path: string): void {
    const next = new Set(selectedPaths.value);
    if (next.has(path)) {
      next.delete(path);
    } else {
      next.add(path);
    }
    selectedPaths.value = next;
  }

  function selectAll(paths: string[]): void {
    const next = new Set(selectedPaths.value);
    for (const p of paths) {
      next.add(p);
    }
    selectedPaths.value = next;
  }

  function deselectAll(paths: string[]): void {
    const next = new Set(selectedPaths.value);
    for (const p of paths) {
      next.delete(p);
    }
    selectedPaths.value = next;
  }

  function clearSelection(): void {
    selectedPaths.value = new Set();
  }

  function isSelected(path: string): boolean {
    return selectedPaths.value.has(path);
  }

  function isMerged(path: string): boolean {
    return mergedPaths.value.has(path);
  }

  // 合并选中的文件
  async function mergeSelected(): Promise<MergeResult | null> {
    const paths = Array.from(selectedPaths.value);
    if (paths.length === 0) return null;
    merging.value = true;
    try {
      const res = await CompareService.MergeFiles(paths);
      if (res && res.mergedPaths) {
        const nextMerged = new Set(mergedPaths.value);
        for (const p of res.mergedPaths) {
          nextMerged.add(p);
        }
        mergedPaths.value = nextMerged;
        // 清空已合并项的勾选
        const nextSelected = new Set(selectedPaths.value);
        for (const p of res.mergedPaths) {
          nextSelected.delete(p);
        }
        selectedPaths.value = nextSelected;

        // 若当前选中的文件已合并，刷新其 Diff
        if (activeFilePath.value && res.mergedPaths.includes(activeFilePath.value)) {
          void selectFile(activeFilePath.value);
        }
      }
      return res;
    } finally {
      merging.value = false;
    }
  }

  // 合并单文件
  async function mergeSingleFile(path: string): Promise<boolean> {
    if (!path) return false;
    merging.value = true;
    try {
      const res = await CompareService.MergeFiles([path]);
      if (res && res.successCount > 0) {
        const nextMerged = new Set(mergedPaths.value);
        nextMerged.add(path);
        mergedPaths.value = nextMerged;
        const nextSelected = new Set(selectedPaths.value);
        nextSelected.delete(path);
        selectedPaths.value = nextSelected;
        if (activeFilePath.value === path) {
          void selectFile(path);
        }
        return true;
      }
      return false;
    } finally {
      merging.value = false;
    }
  }

  // 目录展开/折叠
  function toggleExpand(key: string): void {
    const next = new Set(expandedKeys.value);
    if (next.has(key)) {
      next.delete(key);
    } else {
      next.add(key);
    }
    expandedKeys.value = next;
  }

  function expandAll(allKeys: string[]): void {
    expandedKeys.value = new Set(allKeys);
  }

  function isExpanded(key: string): boolean {
    return expandedKeys.value.has(key);
  }

  function collapseAll(): void {
    expandedKeys.value = new Set();
  }

  return {
    visible,
    targetPath,
    summary,
    activeFilePath,
    activeDiff,
    loading,
    diffLoading,
    merging,
    error,
    diffError,
    filter,
    searchQuery,
    viewMode,
    sameFiles,
    loadingSame,
    selectedPaths,
    mergedPaths,
    expandedKeys,
    hasSummary,
    localArchivePath,
    filteredFiles,
    fileMap,
    open,
    close,
    selectTargetPath,
    startCompare,
    selectFile,
    setFilter,
    loadSameFiles,
    toggleSelect,
    selectAll,
    deselectAll,
    clearSelection,
    isSelected,
    isMerged,
    mergeSelected,
    mergeSingleFile,
    toggleExpand,
    isExpanded,
    expandAll,
    collapseAll,
  };
});
