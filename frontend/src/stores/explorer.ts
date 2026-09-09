import { defineStore } from "pinia";
import { ref } from "vue";
import { Events } from "@wailsio/runtime";
import { ArchiveService } from "../../bindings/pvfine/services";
import type {
  SearchHit,
  TreeAnnotation,
  TreeNode,
  TreeTag,
} from "../../bindings/pvfine/services/models";
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
  tags: TreeTag[];
  annotations: TreeAnnotation[];
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
  annotations: TreeAnnotation[];
  pathAnnotations: Record<string, TreeAnnotation[]>;
}

/** 资源管理器状态:懒加载树 + 搜索 */
export const useExplorerStore = defineStore("explorer", () => {
  const archive = useArchiveStore();
  const roots = ref<TreeItem[]>([]);
  const expanded = ref<Set<string>>(new Set([""]));
  const selectedKey = ref<string | null>(null);
  const itemsByKey = new Map<string, TreeItem>();

  // 搜索状态
  const query = ref("");
  const hits = ref<SearchItem[]>([]);
  const nextCursor = ref(-1);
  const searching = ref(false);
  const mode = ref<"tree" | "search">("tree");
  const exactMatch = ref(false);
  const revision = ref(0);
  let searchRequest = 0;
  let refreshTimer: number | undefined;
  let treeRefreshRequest = 0;

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
      tags: (n.tags ?? []).filter((tag): tag is TreeTag => !!tag),
      annotations: cleanAnnotations(n.annotations),
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
      annotations: cleanAnnotations(n.annotations),
      pathAnnotations: Object.fromEntries(
        Object.entries(n.pathAnnotations ?? {}).map(([path, annotations]) => [
          path,
          cleanAnnotations(annotations),
        ])
      ),
    };
  }

  /** 加载根节点(归档打开后调用) */
  async function loadRoots() {
    const nodes = (await ArchiveService.ListChildren("")) ?? [];
    const nextRoots = nodes.filter((n): n is TreeNode => !!n).map(toTreeItem);
    roots.value = nextRoots;
    itemsByKey.clear();
    registerItems(roots.value);
    revision.value++;
  }

  /** 重新加载资源树；搜索模式会等待新的语义索引完成后自动恢复。 */
  async function reload(): Promise<void> {
    const restoreSearch = mode.value === "search" && query.value.trim() !== "";
    const currentQuery = query.value;
    selectedKey.value = null;
    await loadRoots();
    if (!restoreSearch) {
      clearSearch();
      return;
    }

    const request = ++searchRequest;
    hits.value = [];
    nextCursor.value = -1;
    mode.value = "search";
    searching.value = true;
    try {
      const status = await ArchiveService.IndexStatus();
      if (request !== searchRequest || !archive.open) return;
      if (status?.state === "ready") {
        await search(currentQuery);
      }
    } finally {
      if (request === searchRequest && mode.value === "search" && hits.value.length === 0) {
        const status = await ArchiveService.IndexStatus().catch(() => null);
        if (status?.state !== "building") searching.value = false;
      }
    }
  }

  /** n-tree onLoad:展开目录时加载其子节点 */
  async function loadChildren(node: TreeItem): Promise<void> {
    if (!node.isDir || node.children) return;
    const nodes = (await ArchiveService.ListChildren(node.key)) ?? [];
    const children = nodes.filter((n): n is TreeNode => !!n).map(toTreeItem);
    node.children = children;
    registerItems(node.children);
  }

  /** 加载目标文件的父目录，并将其设为资源树当前选中项。 */
  async function revealPath(path: string): Promise<boolean> {
    if (!archive.open) return false;
    const parts = normalizePath(path).split("/").filter(Boolean);
    if (parts.length === 0) return false;

    if (roots.value.length === 0) await loadRoots();
    let items = roots.value;
    let node: TreeItem | undefined;
    for (let index = 0; index < parts.length; index++) {
      const key = parts.slice(0, index + 1).join("/");
      node = items.find((item) => item.key === key);
      if (!node) return false;
      if (index === parts.length - 1) break;
      if (!node.isDir) return false;
      await loadChildren(node);
      if (!node.children) return false;
      items = node.children;
    }

    if (!node || node.isDir) return false;
    selectedKey.value = node.key;
    return true;
  }

  function getItem(path: string): TreeItem | undefined {
    return itemsByKey.get(path);
  }

  function reset() {
    searchRequest++;
    treeRefreshRequest++;
    window.clearTimeout(refreshTimer);
    roots.value = [];
    itemsByKey.clear();
    selectedKey.value = null;
    expanded.value = new Set([""]);
    clearSearch();
    mode.value = "tree";
  }

  /** 刷新已加载节点的路径标注和可用的索引标签。 */
  async function refreshTreeTags() {
    if (!archive.open) return;
    const request = ++treeRefreshRequest;
    const loadedDirectories = Array.from(itemsByKey.values())
      .filter((item) => item.isDir && item.children !== null)
      .map((item) => item.key);
    const paths = ["", ...loadedDirectories];
    const nodeLists = await Promise.all(
      paths.map(async (path) => (await ArchiveService.ListChildren(path)) ?? [])
    );
    if (request !== treeRefreshRequest || !archive.open) return;

    const tagsByFile = new Map<number, TreeTag[]>();
    const annotationsByPath = new Map<string, TreeAnnotation[]>();
    for (const nodes of nodeLists) {
      for (const node of nodes) {
        if (!node) continue;
        annotationsByPath.set(node.path, cleanAnnotations(node.annotations));
        if (archive.indexReady && !node.isDir && node.fileIndex >= 0) {
          tagsByFile.set(
            node.fileIndex,
            (node.tags ?? []).filter((tag): tag is TreeTag => !!tag)
          );
        }
      }
    }
    for (const item of itemsByKey.values()) {
      item.annotations = annotationsByPath.get(item.key) ?? item.annotations;
      if (!item.isDir) {
        const tags = tagsByFile.get(item.fileIndex);
        if (tags) item.tags = tags;
      }
    }
  }

  async function refreshAnnotations() {
    await refreshTreeTags();
    if (mode.value === "search" && query.value.trim() && archive.indexReady) {
      await search(query.value);
    }
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

  /** 切换路径、名称和 id 的精确匹配模式。 */
  async function setExactMatch(value: boolean): Promise<void> {
    if (exactMatch.value === value) return;
    exactMatch.value = value;
    if (query.value.trim()) await search(query.value);
  }

  /** 加载下一页搜索结果 */
  async function loadMore(request = searchRequest) {
    if (!archive.indexReady || request !== searchRequest) return;
    if (nextCursor.value === -1 && hits.value.length > 0) return;
    searching.value = true;
    try {
      const res = exactMatch.value
        ? await ArchiveService.SearchExact(query.value, Math.max(nextCursor.value, 0), 200)
        : await ArchiveService.Search(query.value, Math.max(nextCursor.value, 0), 200);
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
    void refreshTreeTags();
    if (mode.value !== "search" || !query.value.trim() || !archive.indexReady) return;
    window.clearTimeout(refreshTimer);
    refreshTimer = window.setTimeout(() => void search(query.value), 0);
  });
  Events.On("archive:index-ready", () => {
    void refreshTreeTags();
    if (mode.value === "search" && query.value.trim()) {
      void search(query.value);
    }
  });
  Events.On("archive:reloaded", () => {
    reset();
    void loadRoots();
  });

  return {
    roots,
    expanded,
    selectedKey,
    query,
    hits,
    nextCursor,
    searching,
    exactMatch,
    mode,
    revision,
    toTreeItem,
    toSearchItem,
    getItem,
    loadRoots,
    reload,
    loadChildren,
    revealPath,
    refreshTreeTags,
    refreshAnnotations,
    reset,
    search,
    setExactMatch,
    loadMore,
    clearSearch,
  };
});

function cleanAnnotations(
  annotations: (TreeAnnotation | null)[] | null | undefined
): TreeAnnotation[] {
  return (annotations ?? []).filter(
    (annotation): annotation is TreeAnnotation => !!annotation
  );
}

function normalizePath(path: string): string {
  return path.replaceAll("\\", "/").replace(/^\/+|\/+$/g, "");
}
