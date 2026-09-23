import { defineStore } from "pinia";
import { computed, markRaw, nextTick, reactive, ref } from "vue";
import { Events } from "@wailsio/runtime";
import { ArchiveService, EditorService, FileGUIService } from "../../bindings/pvfine/services";
import type { ShopEditRequest, ShopEditResult } from "../../bindings/pvfine/services/models";
import { useFileGUIStore } from "./fileGUI";
import type { EditorAnnotation, FileMeta, TreeTag, ImageReference } from "../../bindings/pvfine/services/models";
import { useArchiveStore } from "./archive";
import { useExplorerStore } from "./explorer";
import { useScriptStore } from "./script";

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
  annotationsHidden: boolean; // 当前打开期间临时隐藏此文件的标注
  icon: ImageReference | null;
  fieldImage: ImageReference | null;
  loading: boolean;
  loadError: string | null;
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

/** 待确认的关闭动作:目标标签仍含未保存的本地草稿。 */
export type PendingTabClose =
  | { kind: "tab"; index: number; paneId: EditorPaneId }
  | { kind: "others"; keepIndex: number }
  | { kind: "all" };

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
  const openingPaneId = computed<EditorPaneId | null>(() =>
    Object.values(paneStates).find((pane) =>
      tabs.value.some((tab) => tab.loading && pane.activeKey === tab.index)
    )?.id ?? null
  );
  const saving = ref(false);
  const guiApplying = ref(false);
  const guiRefreshWarning = ref("");
  const script = useScriptStore();
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
  const pendingClose = ref<PendingTabClose | null>(null);

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
      script.showArchiveEditor();
      activateTab(targetPaneId, index);
      return;
    }

    const existing = tabs.value.find((tab) => tab.index === index);
    if (existing) {
      script.showArchiveEditor();
      addTabToPane(targetPaneId, index);
      return;
    }

    // 先占用标签名额并显示加载态；重复打开复用同一请求。
    if (tabs.value.length >= 20) {
      throw new Error("打开的标签过多,请先关闭一些(上限 20)");
    }
    const knownPath = useExplorerStore().getFilePath(index) ?? "";
    const tab = reactive<EditorTab>({
      index,
      path: knownPath,
      title: knownPath.split("/").pop() || `文件 ${index}`,
      dataType: 0,
      size: 0,
      tags: [],
      editable: false,
      original: "",
      text: "",
      modified: false,
      annotations: [],
      annotationsHidden: false,
      icon: null,
      fieldImage: null,
      loading: true,
      loadError: null,
    });
    tabs.value.push(tab);
    script.showArchiveEditor();
    addTabToPane(targetPaneId, index);
    await loadTab(tab);
  }

  async function loadTab(tab: EditorTab): Promise<void> {
    try {
      // 让 Vue 先提交加载态，再开始后端调用。
      await nextTick();
      if (!tabs.value.includes(tab)) return;
      const meta: FileMeta | null = await EditorService.GetFile(tab.index);
      // 关闭、重新打开或切换归档后，旧请求不能写入新标签。
      if (!tabs.value.includes(tab)) return;
      if (!meta) throw new Error("文件不存在或无法读取");
      Object.assign(tab, {
        path: meta.path,
        title: meta.path.split("/").pop() ?? meta.path,
        dataType: meta.dataType,
        size: meta.size,
        tags: cleanTreeTags(meta.tags),
        editable: meta.editable,
        original: meta.text,
        text: meta.text,
        modified: meta.modified,
        annotations: cleanEditorAnnotations(meta.annotations),
        icon: meta.icon ?? null,
        fieldImage: meta.fieldImage ?? null,
      });
    } catch (error) {
      if (tabs.value.includes(tab)) {
        tab.loadError = error instanceof Error ? error.message : String(error);
      }
    } finally {
      tab.loading = false;
    }
  }

  async function retryOpenFile(index: number): Promise<void> {
    const tab = tabs.value.find((item) => item.index === index);
    if (!tab || tab.loading || !tab.loadError) return;
    tab.loading = true;
    tab.loadError = null;
    await loadTab(tab);
  }

  function closeTab(index: number, requestedPaneId: EditorPaneId = activePaneId.value): void {
    const paneId = resolvePaneId(requestedPaneId);
    const pane = paneStates[paneId];
    if (!pane) return;
    const tab = tabs.value.find((item) => item.index === index);
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
      clearExplorerSelection(tab?.path);
    }

    if (pane.tabIndexes.length === 0 && isSplit.value) {
      closeSplit(paneId);
    }
  }

  /** 关闭所有标签,并清理分屏中对这些标签的引用。 */
  function closeAllTabs(): void {
    closeTabsExcept(null);
  }

  /** 关闭除指定标签外的所有标签,保留该标签在已有窗格中的引用。 */
  function closeOtherTabs(keepIndex: number): void {
    closeTabsExcept(keepIndex);
  }

  function closeTabsExcept(keepIndex: number | null): void {
    const removedIndexes = new Set(
      tabs.value
        .map((tab) => tab.index)
        .filter((index) => keepIndex === null || index !== keepIndex)
    );
    if (removedIndexes.size === 0) return;
    const removedPaths = tabs.value
      .filter((tab) => removedIndexes.has(tab.index))
      .map((tab) => tab.path);

    for (const pane of Object.values(paneStates)) {
      const oldActive = pane.activeKey;
      pane.tabIndexes = pane.tabIndexes.filter((index) => !removedIndexes.has(index));
      if (oldActive !== null && !removedIndexes.has(oldActive)) {
        pane.activeKey = oldActive;
      } else {
        pane.activeKey = pane.tabIndexes[pane.tabIndexes.length - 1] ?? null;
      }
    }

    tabs.value = tabs.value.filter((tab) => !removedIndexes.has(tab.index));
    clearExplorerSelection(removedPaths);

    while (isSplit.value) {
      const emptyPane = panes.value.find((pane) => pane.tabIndexes.length === 0);
      if (!emptyPane) break;
      closeSplit(emptyPane.id);
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

  /** 编辑器内容变化:只更新本地文本,写入 overlay 由保存动作显式触发。 */
  function updateContent(index: number, text: string) {
    if (guiApplying.value) return;
    const tab = tabs.value.find((item) => item.index === index);
    if (!tab || !tab.editable) return;
    tab.text = text;
  }

  function toggleAnnotationsHidden(index: number): void {
    const tab = tabs.value.find((item) => item.index === index);
    if (tab) tab.annotationsHidden = !tab.annotationsHidden;
  }

  function isDirty(tab: EditorTab): boolean {
    return tab.editable && tab.text !== tab.original;
  }

  /** 提交商店 GUI 编辑:校验草稿一致性与归档代次,成功后同步受影响标签的文本。 */
  async function applyShopEdit(request: ShopEditRequest): Promise<ShopEditResult> {
    if (saving.value) throw new Error("正在保存，请稍后再试");
    const source = tabs.value.find((tab) => tab.index === request.fileIndex && tab.path === request.path);
    if (!source || source.text !== request.text) throw new Error("商店草稿已变化，请关闭表单并重新打开");
    const gui = useFileGUIStore();
    const epoch = gui.epoch;
    request = { ...request, drafts: tabs.value.filter(isDirty).map((tab) => ({ fileIndex: tab.index, path: tab.path, text: tab.text })) };
    saving.value = true;
    guiApplying.value = true;
    guiRefreshWarning.value = "";
    try {
      await nextTick();
      const result = await FileGUIService.ApplyShopEdit(request);
      if (!result) throw new Error("未收到商店编辑结果");
      if (gui.epoch !== epoch) throw new Error("归档已切换，已忽略旧界面的编辑结果");
      for (const file of result.files ?? []) {
        const tab = tabs.value.find((item) => item.index === file.fileIndex && item.path === file.path);
        if (!tab) continue;
        if (tab.text === file.beforeText || tab.text === file.text) {
          tab.text = file.text;
          tab.original = file.text;
          tab.modified = true;
        }
      }
      const refreshes = await Promise.allSettled([refreshBatchFiles((result.files ?? []).map((file) => file.fileIndex)), useArchiveStore().refreshInfo()]);
      if (refreshes.some((refresh) => refresh.status === "rejected")) {
        guiRefreshWarning.value = "修改已应用，部分标签信息刷新失败，可重新打开相关文件";
      }
      return result;
    } finally {
      saving.value = false;
      guiApplying.value = false;
    }
  }

  /** 把单个标签的本地文本写入后端 overlay,成功后重置脏基线。 */
  async function saveTab(index: number): Promise<boolean> {
    const tab = tabs.value.find((item) => item.index === index);
    if (!tab || !isDirty(tab)) return false;
    const text = tab.text;
    await EditorService.SetText(tab.index, text);
    const annotations = (await EditorService.GetAnnotations(tab.index)) ?? [];
    const current = tabs.value.find((item) => item.index === tab.index);
    if (!current) return true;
    // 等待期间用户继续输入时保留其草稿,下一次保存再写入。
    if (current.text === text) {
      current.original = text;
      current.annotations = cleanEditorAnnotations(annotations);
    }
    await useExplorerStore().refreshTreeTags();
    return true;
  }

  /** 保存指定窗格的活动标签(默认当前窗格)。无修改时为空操作。 */
  async function saveActiveTab(requestedPaneId?: EditorPaneId): Promise<boolean> {
    const paneId = resolvePaneId(requestedPaneId);
    const index = paneStates[paneId]?.activeKey ?? null;
    if (index === null || saving.value) return false;
    saving.value = true;
    try {
      const saved = await saveTab(index);
      if (saved) await useArchiveStore().refreshInfo();
      return saved;
    } finally {
      saving.value = false;
    }
  }

  /** 把所有本地有修改的标签写入 overlay(PVF 写盘前调用)。 */
  async function saveAllDirty(): Promise<number> {
    const indexes = tabs.value.filter((tab) => isDirty(tab)).map((tab) => tab.index);
    let saved = 0;
    for (const index of indexes) {
      if (await saveTab(index)) saved += 1;
    }
    if (saved > 0) await useArchiveStore().refreshInfo();
    return saved;
  }

  /**
   * 改写某个 `<表号::键名>` 占位符背后的显示文本。
   * 只改字符串表(.str)的对应条目,脚本里的占位符保持不变。
   */
  async function setPlaceholderText(
    index: number,
    tableIndex: number,
    key: string,
    text: string
  ): Promise<void> {
    await EditorService.SetPlaceholderText(index, tableIndex, key, text);
    const annotations = (await EditorService.GetAnnotations(index)) ?? [];
    const current = tabs.value.find((item) => item.index === index);
    if (current) {
      current.annotations = cleanEditorAnnotations(annotations);
      current.modified = true;
    }
    await useExplorerStore().refreshTreeTags();
    await useArchiveStore().refreshInfo();
  }

  /** 保存到源文件 */
  async function save() {
    const archive = useArchiveStore();
    saving.value = true;
    try {
      await saveAllDirty();
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
      await saveAllDirty();
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

  function clearExplorerSelection(paths: string | string[] | undefined): void {
    if (!paths) return;
    const explorer = useExplorerStore();
    const selection = explorer.selectedKey;
    if (!selection) return;
    const closedPaths = new Set(Array.isArray(paths) ? paths : [paths]);
    if (closedPaths.has(selection)) explorer.selectedKey = null;
  }

  /** 关闭标签:存在未保存本地编辑时先请求确认。 */
  function requestCloseTab(index: number, requestedPaneId: EditorPaneId = activePaneId.value): void {
    if (pendingClose.value) return;
    const tab = tabs.value.find((item) => item.index === index);
    if (!tab) return;
    if (isDirty(tab)) {
      pendingClose.value = { kind: "tab", index, paneId: resolvePaneId(requestedPaneId) };
      return;
    }
    closeTab(index, requestedPaneId);
  }

  /** 关闭其它标签:有待确认的脏标签时先请求确认。 */
  function requestCloseOthers(keepIndex: number): void {
    if (pendingClose.value) return;
    const hasDirty = tabs.value.some((tab) => tab.index !== keepIndex && isDirty(tab));
    if (hasDirty) {
      pendingClose.value = { kind: "others", keepIndex };
      return;
    }
    closeOtherTabs(keepIndex);
  }

  /** 关闭所有标签:有待确认的脏标签时先请求确认。 */
  function requestCloseAll(): void {
    if (pendingClose.value) return;
    if (tabs.value.some((tab) => isDirty(tab))) {
      pendingClose.value = { kind: "all" };
      return;
    }
    closeAllTabs();
  }

  /** 确认丢弃未保存编辑并执行挂起的关闭动作。 */
  function confirmPendingClose(): void {
    const action = pendingClose.value;
    if (!action) return;
    pendingClose.value = null;
    if (action.kind === "tab") closeTab(action.index, action.paneId);
    else if (action.kind === "others") closeOtherTabs(action.keepIndex);
    else closeAllTabs();
  }

  function cancelPendingClose(): void {
    pendingClose.value = null;
  }

  async function refreshAnnotations() {
    await Promise.all(
      tabs.value.filter((tab) => !tab.loading && !tab.loadError).map(async (tab) => {
        const text = tab.text;
        const annotations = (await EditorService.GetAnnotations(tab.index)) ?? [];
        const current = tabs.value.find((item) => item.index === tab.index);
        if (!current || current.text !== text) return;
        current.annotations = cleanEditorAnnotations(annotations);
      })
    );
  }

  /** 重新读取当前 renderer 生成的文本,但保留已修改标签的脏基线。 */
  async function refreshRenderedText() {
    await Promise.all(
      tabs.value.filter((tab) => !tab.loading && !tab.loadError).map(async (tab) => {
        const meta = await EditorService.GetFile(tab.index);
        const current = tabs.value.find((item) => item.index === tab.index);
        if (!current || !meta) return;
        current.path = meta.path;
        current.title = meta.path.split("/").pop() ?? meta.path;
        current.dataType = meta.dataType;
        current.size = meta.size;
        current.editable = meta.editable;
        current.tags = cleanTreeTags(meta.tags);
        current.icon = meta.icon ?? null;
        current.fieldImage = meta.fieldImage ?? null;
        current.modified = meta.modified;
        // 本地有未保存编辑时保留编辑器内容,避免重载规则覆盖用户草稿。
        if (!isDirty(current)) {
          current.text = meta.text;
          if (!meta.modified) current.original = meta.text;
        }
        current.annotations = cleanEditorAnnotations(meta.annotations);
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
        current.modified = meta.modified;
        current.tags = cleanTreeTags(meta.tags);
        current.annotations = cleanEditorAnnotations(meta.annotations);
        current.icon = meta.icon ?? null;
        current.fieldImage = meta.fieldImage ?? null;
        // 本地有未保存编辑时保留编辑器内容,让保存动作以用户文本为准。
        if (!isDirty(current)) current.text = meta.text;
      })
    );
  }

  /** 文件表变化后按路径重新绑定标签，并刷新被自动修改的 lst 标签。 */
  async function refreshAfterArchiveChange(
    refreshPaths: string[] = [],
    resetOriginal = false
  ): Promise<void> {
    // 结构变更后旧索引已失效；标签中的本地文本保留,后续保存会按新索引写入。
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
      tab.icon = node.icon ?? null;
      tab.fieldImage = node.fieldImage ?? null;
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
          tab.icon = meta.icon ?? null;
          tab.fieldImage = meta.fieldImage ?? null;
          if (resetOriginal) {
            // 归档整体重载:索引已失效,直接采用新内容。
            tab.text = meta.text;
            tab.original = meta.text;
            tab.modified = false;
          } else {
            tab.modified = meta.modified;
            // 本地有未保存编辑时保留编辑器内容,不被后端结果覆盖。
            if (!isDirty(tab)) tab.text = meta.text;
          }
          tab.annotations = cleanEditorAnnotations(meta.annotations);
        })
    );
    while (isSplit.value) {
      const emptyPane = panes.value.find((pane) => pane.tabIndexes.length === 0);
      if (!emptyPane) break;
      closeSplit(emptyPane.id);
    }
  }

  async function refreshOpenTabTags(): Promise<void> {
    const currentTabs = tabs.value.filter((tab) => !tab.loading && !tab.loadError);
    await Promise.all(
      currentTabs.map(async (tab) => {
        const meta = await EditorService.GetFile(tab.index).catch(() => null);
        const current = tabs.value.find((item) => item.index === tab.index);
        if (!current || !meta) return;
        current.tags = cleanTreeTags(meta.tags);
        current.icon = meta.icon ?? null;
        current.fieldImage = meta.fieldImage ?? null;
        current.annotations = cleanEditorAnnotations(meta.annotations);
      })
    );
  }

  Events.On("archive:reloaded", () => {
    void refreshAfterArchiveChange(tabs.value.map((tab) => tab.path), true);
  });
  // 脚本或批处理在别的窗口应用了变更时，本窗口的标签不会自己更新。事件是
  // 广播的，所以这里同时覆盖同窗口（发起方已自行刷新，重复刷新是幂等的）
  // 和独立脚本窗口发起的情况。
  Events.On("archive:batch-applied", (event: any) => {
    const data = event?.data ?? event;
    // 结构变更会让后续条目重新编号，必须按路径重新解析树与标签。
    if (data?.structural) {
      void refreshAfterArchiveChange([], false);
      return;
    }
    const indexes = (data?.fileIndexes ?? []).filter(
      (value: unknown): value is number => typeof value === "number",
    );
    if (indexes.length > 0) void refreshBatchFiles(indexes);
  });
  Events.On("archive:registrations-changed", (event: any) => {
    const data = event?.data ?? event;
    const indexes = (data?.fileIndexes ?? []).filter(
      (value: unknown): value is number => typeof value === "number",
    );
    if (indexes.length > 0) void refreshBatchFiles(indexes);
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
    opening: computed(() => tabs.value.some((tab) => tab.loading)),
    openingPaneId,
    saving,
    guiApplying,
    guiRefreshWarning,
    applyShopEdit,
    pendingClose,
    openFile,
    retryOpenFile,
    activatePane,
    beginTabDrag,
    endTabDrag,
    activateTab,
    closeTab,
    closeAllTabs,
    closeOtherTabs,
    requestCloseTab,
    requestCloseOthers,
    requestCloseAll,
    confirmPendingClose,
    cancelPendingClose,
    moveTab,
    split,
    splitAndMoveTab,
    closeSplit,
    setSplitRatio,
    updateContent,
    toggleAnnotationsHidden,
    setPlaceholderText,
    saveTab,
    saveActiveTab,
    saveAllDirty,
    save,
    saveAs,
    refreshAnnotations,
    refreshRenderedText,
    refreshBatchFiles,
    refreshAfterArchiveChange,
  };
});

function cleanTreeTags(tags: (TreeTag | null)[] | null | undefined): TreeTag[] {
  return (tags ?? []).filter((tag): tag is TreeTag => !!tag);
}

/** 标注作为不可变快照使用，避免 Vue 为大文件建立深层代理。 */
function cleanEditorAnnotations(annotations: (EditorAnnotation | null)[] | null | undefined): EditorAnnotation[] {
  return markRaw((annotations ?? []).filter((annotation): annotation is EditorAnnotation => !!annotation));
}
