import { defineStore } from "pinia";
import { ref } from "vue";
import { ArchiveService } from "../../bindings/pvfine/services";
import type { TreeNode } from "../../bindings/pvfine/services/models";

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

/** 资源管理器状态:懒加载树 + 搜索 */
export const useExplorerStore = defineStore("explorer", () => {
  const roots = ref<TreeItem[]>([]);
  const expanded = ref<Set<string>>(new Set([""]));
  const itemsByKey = new Map<string, TreeItem>();

  // 搜索状态
  const query = ref("");
  const hits = ref<TreeItem[]>([]);
  const nextCursor = ref(-1);
  const searching = ref(false);
  const mode = ref<"tree" | "search">("tree");

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
    roots.value = [];
    itemsByKey.clear();
    expanded.value = new Set([""]);
    clearSearch();
    mode.value = "tree";
  }

  /** 执行新搜索(从第一页开始) */
  async function search(q: string) {
    query.value = q;
    hits.value = [];
    nextCursor.value = -1;
    if (!q.trim()) {
      mode.value = "tree";
      return;
    }
    mode.value = "search";
    searching.value = true;
    try {
      await loadMore();
    } finally {
      searching.value = false;
    }
  }

  /** 加载下一页搜索结果 */
  async function loadMore() {
    if (nextCursor.value === -1 && hits.value.length > 0) return;
    const res = await ArchiveService.Search(query.value, Math.max(nextCursor.value, 0), 200);
    const page = (res?.hits ?? []).filter((n): n is TreeNode => !!n).map(toTreeItem);
    hits.value.push(...page);
    nextCursor.value = res?.nextCursor ?? -1;
  }

  function clearSearch() {
    query.value = "";
    hits.value = [];
    nextCursor.value = -1;
    mode.value = "tree";
  }

  return {
    roots,
    expanded,
    query,
    hits,
    nextCursor,
    searching,
    mode,
    toTreeItem,
    getItem,
    loadRoots,
    loadChildren,
    reset,
    search,
    loadMore,
    clearSearch,
  };
});
