import { defineStore } from "pinia";
import { ref } from "vue";
import { Events } from "@wailsio/runtime";
import { ArchiveService } from "../../bindings/pvfine/services";
import type { SearchHit, TreeNode } from "../../bindings/pvfine/services/models";
import { useArchiveStore } from "./archive";

export interface TreeItem {
  key: string; // 归档内路径
  label: string; // 显示名(目录/文件最后一段)
  isDir: boolean;
  isLeaf: boolean;
  children: TreeItem[] | null; // null = 未加载
  fileIndex: number; // 目录为 -1
  size: number;
  dataType: number;
  childCount: number;
}

export interface SearchItem {
  key: string;
  label: string;
  path: string;
  id: string;
  category: string;
  fileIndex: number;
  size: number;
  dataType: number;
}

/** 资源管理器状态:懒加载树 + 搜索 */
export const useExplorerStore = defineStore("explorer", () => {
  const archive = useArchiveStore();
  const roots = ref<TreeItem[]>([]);
  const expanded = ref<Set<string>>(new Set([""]));
  const itemsByKey = new Map<string, TreeItem>();

  // 搜索状态
  const query = ref("");
  const hits = ref<SearchItem[]>([]);
  const nextCursor = ref(-1);
  const searching = ref(false);
  const mode = ref<"tree" | "search">("tree");
  let searchRequest = 0;
  let refreshTimer: number | undefined;

  function toTreeItem(n: TreeNode): TreeItem {
    return {
      key: n.path,
      label: n.name,
      isDir: n.isDir,
      isLeaf: !n.isDir,
      children: n.isDir ? null : undefined!,
      fileIndex: n.fileIndex,
      size: n.size,
      dataType: n.dataType,
      childCount: n.childCount,
    };
  }

  function registerItems(items: TreeItem[]) {
    for (const item of items) itemsByKey.set(item.key, item);
  }

  function toSearchItem(n: SearchHit): SearchItem {
    const fallback = n.path.slice(n.path.lastIndexOf("/") + 1);
    return {
      key: `${n.fileIndex}:${n.category}:${n.id}:${n.path}`,
      label: n.name || fallback,
      path: n.path,
      id: n.id,
      category: n.category,
      fileIndex: n.fileIndex,
      size: n.size,
      dataType: n.dataType,
    };
  }

  /** 加载根节点(归档打开后调用) */
  async function loadRoots() {
    const nodes = (await ArchiveService.ListChildren("")) ?? [];
    const nextRoots = nodes.filter((n): n is TreeNode => !!n).map(toTreeItem);
    roots.value = nextRoots;
    itemsByKey.clear();
    registerItems(roots.value);
  }

  /** n-tree onLoad:展开目录时加载其子节点 */
  async function loadChildren(node: TreeItem): Promise<void> {
    if (!node.isDir || node.children) return;
    const nodes = (await ArchiveService.ListChildren(node.key)) ?? [];
    const children = nodes.filter((n): n is TreeNode => !!n).map(toTreeItem);
    node.children = children;
    registerItems(node.children);
  }

  function getItem(path: string): TreeItem | undefined {
    return itemsByKey.get(path);
  }

  function reset() {
    searchRequest++;
    window.clearTimeout(refreshTimer);
    roots.value = [];
    itemsByKey.clear();
    expanded.value = new Set([""]);
    clearSearch();
    mode.value = "tree";
  }

  /** 执行新搜索(从第一页开始) */
  async function search(q: string) {
    const request = ++searchRequest;
    query.value = q;
    hits.value = [];
    nextCursor.value = -1;
    if (!q.trim()) {
      searching.value = false;
      mode.value = "tree";
      return;
    }
    mode.value = "search";
    if (!archive.indexReady) return;
    await loadMore(request);
  }

  /** 加载下一页搜索结果 */
  async function loadMore(request = searchRequest) {
    if (!archive.indexReady || request !== searchRequest) return;
    if (nextCursor.value === -1 && hits.value.length > 0) return;
    searching.value = true;
    try {
      const res = await ArchiveService.Search(query.value, Math.max(nextCursor.value, 0), 200);
      if (request !== searchRequest) return;
      const page = (res?.hits ?? [])
        .filter((n): n is SearchHit => !!n)
        .map(toSearchItem);
      hits.value.push(...page);
      nextCursor.value = res?.nextCursor ?? -1;
    } finally {
      if (request === searchRequest) searching.value = false;
    }
  }

  function clearSearch() {
    searchRequest++;
    window.clearTimeout(refreshTimer);
    query.value = "";
    hits.value = [];
    nextCursor.value = -1;
    mode.value = "tree";
  }

  Events.On("archive:index-updated", () => {
    if (mode.value !== "search" || !query.value.trim() || !archive.indexReady) return;
    window.clearTimeout(refreshTimer);
    refreshTimer = window.setTimeout(() => void search(query.value), 0);
  });

  return {
    roots,
    expanded,
    query,
    hits,
    nextCursor,
    searching,
    mode,
    toTreeItem,
    toSearchItem,
    getItem,
    loadRoots,
    loadChildren,
    reset,
    search,
    loadMore,
    clearSearch,
  };
});
