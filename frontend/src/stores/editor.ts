import { defineStore } from "pinia";
import { computed, reactive, ref } from "vue";
import { Events } from "@wailsio/runtime";
import { ArchiveService, EditorService } from "../../bindings/pvfine/services";
import type { EditorAnnotation, FileMeta, TreeTag } from "../../bindings/pvfine/services/models";
import { useArchiveStore } from "./archive";
import { useExplorerStore } from "./explorer";

export type EditorPaneId = string;
export type SplitOrientation = "columns" | "rows";

export interface EditorTab {
  index: number;
  path: string;
  title: string;
  dataType: number;
  size: number;
  tags: TreeTag[];
  editable: boolean;
  original: string; // 打开时的文本(脏判定基准)
  text: string; // 当前编辑器内容
  modified: boolean; // 后端 overlay 状态
  annotations: EditorAnnotation[];
}

export interface EditorPaneState {
  id: EditorPaneId;
  tabIndexes: number[];
  activeKey: number | null;
}

export interface DraggedEditorTab {
  paneId: EditorPaneId;
  index: number;
}

export interface EditorLayoutPane {
  kind: "pane";
  paneId: EditorPaneId;
}

export interface EditorLayoutSplit {
  kind: "split";
  id: string;
  orientation: SplitOrientation;
  ratio: number;
  first: EditorLayoutNode;
  second: EditorLayoutNode;
}

export type EditorLayoutNode = EditorLayoutPane | EditorLayoutSplit;

interface LayoutReplacement {
  node: EditorLayoutNode;
  found: boolean;
  destinationPaneId?: EditorPaneId;
}

/** 编辑器状态:全局文件内容 + 可递归分屏的多标签窗格 */
export const useEditorStore = defineStore("editor", () => {
  const initialPaneId = "pane-1";
  const tabs = ref<EditorTab[]>([]);
  const paneStates = reactive<Record<EditorPaneId, EditorPaneState>>({
    [initialPaneId]: {
      id: initialPaneId,
      tabIndexes: [],
      activeKey: null,
    },
  });
  const layout = ref<EditorLayoutNode>({ kind: "pane", paneId: initialPaneId });
  const activePaneId = ref<EditorPaneId>(initialPaneId);
  const draggingTab = ref<DraggedEditorTab | null>(null);
  const openingPaneId = ref<EditorPaneId | null>(null);
  const saving = ref(false);
  let paneSequence = 1;
  let splitSequence = 0;

  const panes = computed(() => {
    const ids: EditorPaneId[] = [];
    collectPaneIds(layout.value, ids);
    return ids.map((id) => paneStates[id]).filter((pane): pane is EditorPaneState => !!pane);
  });
  const isSplit = computed(() => panes.value.length > 1);
  const activePane = computed<EditorPaneState>(
    () => paneStates[activePaneId.value] ?? panes.value[0] ?? paneStates[initialPaneId]
  );
  const activeKey = computed<number | null>({
    get: () => activePane.value.activeKey,
    set: (value) => {
      activePane.value.activeKey = value;
    },
  });
  const activeTab = computed(
    () => tabs.value.find((tab) => tab.index === activePane.value.activeKey) ?? null
  );
  const dirtyCount = computed(() => tabs.value.filter((tab) => tab.text !== tab.original).length);

  let syncTimer: number | undefined;
  const pendingSync = new Set<number>();

  function collectPaneIds(node: EditorLayoutNode, result: EditorPaneId[]): void {
    if (node.kind === "pane") {
      result.push(node.paneId);
      return;
    }
    collectPaneIds(node.first, result);
    collectPaneIds(node.second, result);
  }

  function resolvePaneId(requested?: EditorPaneId): EditorPaneId {
    if (requested && paneStates[requested] && panes.value.some((pane) => pane.id === requested)) {
      return requested;
    }
    if (paneStates[activePaneId.value] && panes.value.some((pane) => pane.id === activePaneId.value)) {
      return activePaneId.value;
    }
    return initialPaneId;
  }

  function activatePane(paneId: EditorPaneId): void {
    if (!paneStates[paneId] || !panes.value.some((pane) => pane.id === paneId)) return;
    activePaneId.value = paneId;
  }

  function beginTabDrag(paneId: EditorPaneId, index: number): void {
    draggingTab.value = { paneId, index };
  }

  function endTabDrag(): void {
    draggingTab.value = null;
  }

  function activateTab(paneId: EditorPaneId, index: number): void {
    const pane = paneStates[paneId];
    if (!pane || !pane.tabIndexes.includes(index)) return;
    pane.activeKey = index;
    activatePane(paneId);
  }

  function addTabToPane(paneId: EditorPaneId, index: number): void {
    const pane = paneStates[paneId];
    if (!pane) return;
    if (!pane.tabIndexes.includes(index)) pane.tabIndexes.push(index);
    pane.activeKey = index;
    activatePane(paneId);
  }

  /** 打开文件(已在当前窗格则激活,已在其他窗格则复用内容并建立视图引用) */
  async function openFile(index: number, requestedPaneId: EditorPaneId = activePaneId.value) {
    const targetPaneId = resolvePaneId(requestedPaneId);
    const targetPane = paneStates[targetPaneId];
    if (targetPane.tabIndexes.includes(index)) {
      activateTab(targetPaneId, index);
      return;
    }

    const existing = tabs.value.find((tab) => tab.index === index);
    if (existing) {
      addTabToPane(targetPaneId, index);
      return;
    }

    openingPaneId.value = targetPaneId;
    try {
      const meta: FileMeta | null = await EditorService.GetFile(index);
      if (!meta) return;
      // 标签上限,防误开大量文件；同一文件在多个窗格中的引用不重复计数。
      if (tabs.value.length >= 20) {
        throw new Error("打开的标签过多,请先关闭一些(上限 20)");
      }
      tabs.value.push({
        index,
        path: meta.path,
        title: meta.path.split("/").pop() ?? meta.path,
        dataType: meta.dataType,
        size: meta.size,
        tags: cleanTreeTags(meta.tags),
        editable: meta.editable,
        original: meta.text,
        text: meta.text,
        modified: meta.modified,
        annotations: (meta.annotations ?? []).filter(
          (annotation): annotation is EditorAnnotation => !!annotation
        ),
      });
      addTabToPane(targetPaneId, index);
    } finally {
      if (openingPaneId.value === targetPaneId) openingPaneId.value = null;
    }
  }

  function closeTab(index: number, requestedPaneId: EditorPaneId = activePaneId.value): void {
    const paneId = resolvePaneId(requestedPaneId);
    const pane = paneStates[paneId];
    if (!pane) return;
    const tabIndex = pane.tabIndexes.indexOf(index);
    if (tabIndex < 0) return;

    pane.tabIndexes.splice(tabIndex, 1);
    if (pane.activeKey === index) {
      pane.activeKey = pane.tabIndexes[Math.min(tabIndex, pane.tabIndexes.length - 1)] ?? null;
    }

    const stillUsed = Object.values(paneStates).some((item) => item.tabIndexes.includes(index));
    if (!stillUsed) {
      const globalTabIndex = tabs.value.findIndex((tab) => tab.index === index);
      if (globalTabIndex >= 0) tabs.value.splice(globalTabIndex, 1);
      pendingSync.delete(index);
    }

    if (pane.tabIndexes.length === 0 && isSplit.value) {
      closeSplit(paneId);
    }
  }

  /** 把标签引用移动到另一个窗格,源窗格变空时默认自动收起。 */
  function moveTab(
    index: number,
    requestedSourcePaneId: EditorPaneId,
    requestedTargetPaneId: EditorPaneId,
    closeEmptySource = true
  ): void {
    const sourcePaneId = resolvePaneId(requestedSourcePaneId);
    const targetPaneId = resolvePaneId(requestedTargetPaneId);
    if (sourcePaneId === targetPaneId) {
      activateTab(targetPaneId, index);
      return;
    }

    const source = paneStates[sourcePaneId];
    const target = paneStates[targetPaneId];
    if (!source || !target) return;
    const sourceIndex = source.tabIndexes.indexOf(index);
    if (sourceIndex < 0) return;

    source.tabIndexes.splice(sourceIndex, 1);
    if (source.activeKey === index) {
      source.activeKey = source.tabIndexes[Math.min(sourceIndex, source.tabIndexes.length - 1)] ?? null;
    }
    if (!target.tabIndexes.includes(index)) target.tabIndexes.push(index);
    target.activeKey = index;
    activePaneId.value = targetPaneId;

    if (closeEmptySource && source.tabIndexes.length === 0 && isSplit.value) {
      closeSplit(sourcePaneId);
      if (paneStates[targetPaneId] && panes.value.some((pane) => pane.id === targetPaneId)) {
        activePaneId.value = targetPaneId;
      }
    }
  }

  function nextPaneId(): EditorPaneId {
    paneSequence += 1;
    return `pane-${paneSequence}`;
  }

  function nextSplitId(): string {
    splitSequence += 1;
    return `split-${splitSequence}`;
  }

  function replacePane(
    node: EditorLayoutNode,
    targetPaneId: EditorPaneId,
    replacement: EditorLayoutNode
  ): LayoutReplacement {
    if (node.kind === "pane") {
      return node.paneId === targetPaneId
        ? { node: replacement, found: true }
        : { node, found: false };
    }

    const first = replacePane(node.first, targetPaneId, replacement);
    if (first.found) return { node: { ...node, first: first.node }, found: true };
    const second = replacePane(node.second, targetPaneId, replacement);
    if (second.found) return { node: { ...node, second: second.node }, found: true };
    return { node, found: false };
  }

  function createSplit(
    targetPaneId: EditorPaneId,
    orientation: SplitOrientation,
    insertBefore: boolean,
    cloneActiveTab: boolean
  ): EditorPaneId | null {
    const targetPane = paneStates[targetPaneId];
    if (!targetPane) return null;

    const newPaneId = nextPaneId();
    const targetNode: EditorLayoutPane = { kind: "pane", paneId: targetPaneId };
    const newNode: EditorLayoutPane = { kind: "pane", paneId: newPaneId };
    const replacement: EditorLayoutSplit = {
      kind: "split",
      id: nextSplitId(),
      orientation,
      ratio: 0.5,
      first: insertBefore ? newNode : targetNode,
      second: insertBefore ? targetNode : newNode,
    };
    const result = replacePane(layout.value, targetPaneId, replacement);
    if (!result.found) return null;

    const clonedTab = cloneActiveTab ? targetPane.activeKey : null;
    paneStates[newPaneId] = {
      id: newPaneId,
      tabIndexes: clonedTab === null ? [] : [clonedTab],
      activeKey: clonedTab,
    };
    layout.value = result.node;
    return newPaneId;
  }

  function split(orientation: SplitOrientation, requestedPaneId: EditorPaneId = activePaneId.value): void {
    const targetPaneId = resolvePaneId(requestedPaneId);
    const newPaneId = createSplit(targetPaneId, orientation, false, true);
    if (!newPaneId) return;
    activePaneId.value = newPaneId;
  }

  function splitAndMoveTab(
    index: number,
    requestedSourcePaneId: EditorPaneId,
    requestedTargetPaneId: EditorPaneId,
    orientation: SplitOrientation,
    insertBefore: boolean
  ): void {
    const sourcePaneId = resolvePaneId(requestedSourcePaneId);
    const targetPaneId = resolvePaneId(requestedTargetPaneId);
    const source = paneStates[sourcePaneId];
    if (!source || !source.tabIndexes.includes(index)) return;

    const newPaneId = createSplit(targetPaneId, orientation, insertBefore, false);
    if (!newPaneId) return;
    moveTab(index, sourcePaneId, newPaneId, false);
    activePaneId.value = newPaneId;
  }

  function removePane(node: EditorLayoutNode, targetPaneId: EditorPaneId): LayoutReplacement {
    if (node.kind === "pane") return { node, found: false };
    if (node.first.kind === "pane" && node.first.paneId === targetPaneId) {
      return {
        node: node.second,
        found: true,
        destinationPaneId: firstPaneId(node.second),
      };
    }
    if (node.second.kind === "pane" && node.second.paneId === targetPaneId) {
      return {
        node: node.first,
        found: true,
        destinationPaneId: firstPaneId(node.first),
      };
    }

    const first = removePane(node.first, targetPaneId);
    if (first.found) {
      return {
        node: { ...node, first: first.node },
        found: true,
        destinationPaneId: first.destinationPaneId,
      };
    }
    const second = removePane(node.second, targetPaneId);
    if (second.found) {
      return {
        node: { ...node, second: second.node },
        found: true,
        destinationPaneId: second.destinationPaneId,
      };
    }
    return { node, found: false };
  }

  function firstPaneId(node: EditorLayoutNode): EditorPaneId {
    return node.kind === "pane" ? node.paneId : firstPaneId(node.first);
  }

  function mergePaneTabs(fromPaneId: EditorPaneId, toPaneId: EditorPaneId): void {
    const from = paneStates[fromPaneId];
    const to = paneStates[toPaneId];
    if (!from || !to) return;
    const fromActive = from.activeKey;
    for (const index of from.tabIndexes) {
      if (!to.tabIndexes.includes(index)) to.tabIndexes.push(index);
    }
    if (fromActive !== null && to.tabIndexes.includes(fromActive)) to.activeKey = fromActive;
  }

  /** 关闭指定窗格,将其标签并入剩余兄弟树的最近兄弟窗格。 */
  function closeSplit(requestedPaneId: EditorPaneId = activePaneId.value): void {
    const currentPaneId = resolvePaneId(requestedPaneId);
    const currentPane = paneStates[currentPaneId];
    const result = removePane(layout.value, currentPaneId);
    if (!currentPane || !result.found) return;

    const destinationPaneId = result.destinationPaneId ?? firstPaneId(result.node);
    mergePaneTabs(currentPaneId, destinationPaneId);
    layout.value = result.node;
    delete paneStates[currentPaneId];
    if (openingPaneId.value === currentPaneId) openingPaneId.value = null;
    activePaneId.value = destinationPaneId;
  }

  function findSplit(node: EditorLayoutNode, splitId: string): EditorLayoutSplit | null {
    if (node.kind === "pane") return null;
    if (node.id === splitId) return node;
    return findSplit(node.first, splitId) ?? findSplit(node.second, splitId);
  }

  function setSplitRatio(splitId: string, value: number): void {
    const splitNode = findSplit(layout.value, splitId);
    if (!splitNode || !Number.isFinite(value)) return;
    splitNode.ratio = Math.min(0.8, Math.max(0.2, value));
  }

  /** 编辑器内容变化:立即更新本地脏状态,短暂防抖后同步到后端 overlay */
  function updateContent(index: number, text: string) {
    const tab = tabs.value.find((item) => item.index === index);
    if (!tab || !tab.editable) return;
    tab.text = text;
    pendingSync.add(index);
    window.clearTimeout(syncTimer);
    syncTimer = window.setTimeout(async () => {
      const batch = [...pendingSync];
      pendingSync.clear();
      for (const itemIndex of batch) {
        const currentTab = tabs.value.find((item) => item.index === itemIndex);
        if (!currentTab) continue;
        try {
          await syncTab(currentTab);
        } catch (e) {
          console.error("sync failed", e);
        }
      }
      await useArchiveStore().refreshInfo();
    }, 200);
  }

  /** 保存到源文件 */
  async function save() {
    const archive = useArchiveStore();
    saving.value = true;
    try {
      await flushPending();
      const info = await EditorService.Save();
      archive.info = info;
      // 保存成功后,文本与基准重置(overlay 已清空)
      for (const tab of tabs.value) {
        tab.original = tab.text;
        tab.modified = false;
      }
      return info;
    } finally {
      saving.value = false;
    }
  }

  /** 另存为新 PVF,返回保存路径(取消返回 null) */
  async function saveAs() {
    const archive = useArchiveStore();
    saving.value = true;
    try {
      await flushPending();
      const path = await EditorService.SaveAsDialog();
      if (path) {
        for (const tab of tabs.value) {
          tab.original = tab.text;
          tab.modified = false;
        }
        await archive.refreshInfo();
      }
      return path || null;
    } finally {
      saving.value = false;
    }
  }

  async function flushPending() {
    window.clearTimeout(syncTimer);
    const batch = [...pendingSync];
    pendingSync.clear();
    for (const index of batch) {
      const tab = tabs.value.find((item) => item.index === index);
      if (tab) await syncTab(tab);
    }
  }

  function discardPendingSync(): void {
    window.clearTimeout(syncTimer);
    pendingSync.clear();
  }

  async function syncTab(tab: EditorTab) {
    const text = tab.text;
    await EditorService.SetText(tab.index, text);
    const annotations = (await EditorService.GetAnnotations(tab.index)) ?? [];
    const current = tabs.value.find((item) => item.index === tab.index);
    if (!current || current.text !== text) return;
    current.annotations = annotations.filter(
      (annotation): annotation is EditorAnnotation => !!annotation
    );
    await useExplorerStore().refreshTreeTags();
  }

  async function refreshAnnotations() {
    await flushPending();
    await Promise.all(
      tabs.value.map(async (tab) => {
        const text = tab.text;
        const annotations = (await EditorService.GetAnnotations(tab.index)) ?? [];
        const current = tabs.value.find((item) => item.index === tab.index);
        if (!current || current.text !== text) return;
        current.annotations = annotations.filter(
          (annotation): annotation is EditorAnnotation => !!annotation
        );
      })
    );
  }

  /** 批处理写入 overlay 后刷新已经打开的标签,但保留原始文本基准。 */
  async function refreshBatchFiles(indexes: number[]) {
    const uniqueIndexes = [...new Set(indexes)];
    await Promise.all(
      uniqueIndexes.map(async (index) => {
        const current = tabs.value.find((tab) => tab.index === index);
        if (!current) return;
        const meta = await EditorService.GetFile(index);
        if (!meta) return;
        current.path = meta.path;
        current.text = meta.text;
        current.modified = meta.modified;
        current.tags = cleanTreeTags(meta.tags);
        current.annotations = (meta.annotations ?? []).filter(
          (annotation): annotation is EditorAnnotation => !!annotation
        );
      })
    );
  }

  /** 文件表变化后按路径重新绑定标签，并刷新被自动修改的 lst 标签。 */
  async function refreshAfterArchiveChange(
    refreshPaths: string[] = [],
    resetOriginal = false
  ): Promise<void> {
    // 结构变更已经完成，旧索引上的待同步请求不能再发送；标签中的
    // 本地文本保留，后续编辑会按新的文件索引继续同步。
    discardPendingSync();
    if (tabs.value.length === 0) return;

    const currentTabs = [...tabs.value];
    const nodes = (await ArchiveService.ResolveFiles(currentTabs.map((tab) => tab.path))) ?? [];
    const indexByPath = new Map(
      nodes
        .filter((node): node is NonNullable<typeof node> => !!node && !node.isDir)
        .map((node) => [node.path, node])
    );
    const nextIndexByOld = new Map<number, number>();
    const remainingTabs: EditorTab[] = [];
    const pathsToRefresh = new Set(
      refreshPaths.map((path) => path.replaceAll("\\", "/").replace(/^\/+|\/+$/g, ""))
    );

    for (const tab of currentTabs) {
      const node = indexByPath.get(tab.path);
      if (!node) continue;
      nextIndexByOld.set(tab.index, node.fileIndex);
      tab.index = node.fileIndex;
      tab.path = node.path;
      tab.title = node.path.split("/").pop() ?? node.path;
      tab.size = node.size;
      tab.dataType = node.dataType;
      tab.tags = cleanTreeTags(node.tags);
      remainingTabs.push(tab);
    }

    for (const pane of Object.values(paneStates)) {
      const oldActive = pane.activeKey;
      const nextIndexes = pane.tabIndexes
        .map((index) => nextIndexByOld.get(index))
        .filter((index): index is number => index !== undefined);
      pane.tabIndexes.splice(0, pane.tabIndexes.length, ...nextIndexes);
      pane.activeKey =
        (oldActive === null ? null : nextIndexByOld.get(oldActive)) ??
        pane.tabIndexes[pane.tabIndexes.length - 1] ??
        null;
    }

    tabs.value = remainingTabs;
    pendingSync.clear();
    await Promise.all(
      remainingTabs
        .filter((tab) => pathsToRefresh.has(tab.path))
        .map(async (tab) => {
          const meta = await EditorService.GetFile(tab.index);
          if (!meta) return;
          tab.path = meta.path;
          tab.title = meta.path.split("/").pop() ?? meta.path;
          tab.dataType = meta.dataType;
          tab.size = meta.size;
          tab.tags = cleanTreeTags(meta.tags);
          tab.text = meta.text;
          if (resetOriginal) {
            tab.original = meta.text;
            tab.modified = false;
          } else {
            tab.modified = meta.modified;
          }
          tab.annotations = (meta.annotations ?? []).filter(
            (annotation): annotation is EditorAnnotation => !!annotation
          );
        })
    );
    while (isSplit.value) {
      const emptyPane = panes.value.find((pane) => pane.tabIndexes.length === 0);
      if (!emptyPane) break;
      closeSplit(emptyPane.id);
    }
  }

  async function refreshOpenTabTags(): Promise<void> {
    const currentTabs = [...tabs.value];
    await Promise.all(
      currentTabs.map(async (tab) => {
        const meta = await EditorService.GetFile(tab.index).catch(() => null);
        const current = tabs.value.find((item) => item.index === tab.index);
        if (!current || !meta) return;
        current.tags = cleanTreeTags(meta.tags);
      })
    );
  }

  Events.On("archive:reloaded", () => {
    void refreshAfterArchiveChange(tabs.value.map((tab) => tab.path), true);
  });
  Events.On("archive:index-ready", () => {
    void refreshOpenTabTags();
  });
  Events.On("archive:index-updated", () => {
    void refreshOpenTabTags();
  });

  return {
    tabs,
    panes,
    layout,
    activeKey,
    activeTab,
    dirtyCount,
    activePaneId,
    draggingTab,
    isSplit,
    opening: computed(() => openingPaneId.value !== null),
    openingPaneId,
    saving,
    openFile,
    activatePane,
    beginTabDrag,
    endTabDrag,
    activateTab,
    closeTab,
    moveTab,
    split,
    splitAndMoveTab,
    closeSplit,
    setSplitRatio,
    updateContent,
    save,
    saveAs,
    flushPending,
    refreshAnnotations,
    refreshBatchFiles,
    refreshAfterArchiveChange,
  };
});

function cleanTreeTags(tags: (TreeTag | null)[] | null | undefined): TreeTag[] {
  return (tags ?? []).filter((tag): tag is TreeTag => !!tag);
}
