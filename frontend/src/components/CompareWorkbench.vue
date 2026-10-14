<script setup lang="ts">
/**
 * PVF 归档差异对比与合并工作台
 * 支持 IntelliJ IDEA 风格的 Live 双栏编辑器：
 * - 左侧为本地文件，可直接打字、换行、选区、复制粘贴，实时同步主编辑器；
 * - 右侧为对比目标，只读显示；
 * - 左右未修改行水平严格对齐，两栏中间提供段落快捷合入按钮（«）；
 * - 鲜明醒目的差异高亮标记与对齐 spacer；
 * - 顶部差异导航栏支持纯图标上一个/下一个差异快速定位。
 *
 * @author flobit
 */
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import {
  NAlert,
  NButton,
  NCheckbox,
  NEmpty,
  NIcon,
  NInput,
  NSpin,
  NTag,
  NTooltip,
  useMessage,
} from "naive-ui";
import {
  ArrowCollapseAll20Regular,
  ArrowDown20Regular,
  ArrowDownload20Regular,
  ArrowSyncCircle20Regular,
  ArrowUp20Regular,
  CheckmarkCircle20Regular,
  ChevronDown20Regular,
  ChevronRight20Regular,
  ChevronUp20Regular,
  Copy20Regular,
  Document20Regular,
  DocumentArrowLeft20Regular,
  Folder20Regular,
  FolderOpen20Regular,
  Open20Regular,
  Save20Regular,
  Search20Regular,
  TextBulletListSquare20Regular,
} from "@vicons/fluent";
import {
  EditorView,
  drawSelection,
  highlightActiveLine,
  highlightActiveLineGutter,
  keymap,
  lineNumbers,
} from "@codemirror/view";
import { EditorState } from "@codemirror/state";
import { defaultKeymap, history, historyKeymap } from "@codemirror/commands";
import { MergeView } from "@codemirror/merge";
import { ArchiveService, CompareService } from "../../bindings/pvfine/services";
import { useCompareStore } from "../stores/compare";
import { useArchiveStore } from "./../stores/archive";
import { useEditorStore } from "./../stores/editor";
import type { CompareFileItem } from "../../bindings/pvfine/services/models";
import { rarityTextStyle } from "../rarity";
import ImageThumbnail from "./ImageThumbnail.vue";
import type { ResolvedThemeId } from "../theme";
import { pvfHighlighting, pvfLanguage } from "../pvfLanguage";

const props = defineProps<{
  themeId?: ResolvedThemeId;
}>();

const compare = useCompareStore();
const archive = useArchiveStore();
const editor = useEditorStore();
const message = useMessage();

// 顶部归档配置栏折叠状态：比对成功后默认折叠收起，释放最大代码浏览高度
const isHeaderCollapsed = ref(false);

watch(
  () => compare.hasSummary,
  (has) => {
    if (has) {
      isHeaderCollapsed.value = true;
    }
  }
);

function getFileName(path: string): string {
  if (!path) return "";
  return path.split(/[\\/]/).pop() || path;
}

const activeFileItem = computed<CompareFileItem | null>(() => {
  if (!compare.activeFilePath) return null;
  return compare.fileMap.get(compare.activeFilePath) ?? null;
});

function formatSize(bytes: number): string {
  if (bytes <= 0) return "0 B";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(2)} MB`;
}

function copyPath(path: string): void {
  void navigator.clipboard.writeText(path);
  message.success("已复制文件路径");
}

async function openInEditor(path: string): Promise<void> {
  if (!archive.open) return;
  try {
    const nodes = await ArchiveService.ResolveFiles([path]);
    const target = nodes?.[0];
    if (!target || target.fileIndex < 0) {
      message.warning("当前归档中未找到该文件");
      return;
    }
    await editor.openFile(target.fileIndex);
    message.success("已在主编辑器中打开");
  } catch (err: any) {
    message.error(err?.message || "无法在主编辑器中打开");
  }
}

const canOpenInEditor = computed(() => {
  return archive.open && !!compare.activeFilePath;
});

function getOperationTagType(op: string): "default" | "error" | "primary" | "info" | "success" | "warning" {
  switch (op) {
    case "add":
      return "success";
    case "delete":
      return "error";
    case "modify":
      return "warning";
    default:
      return "default";
  }
}

function getOperationLabel(op: string): string {
  switch (op) {
    case "add":
      return "新增";
    case "delete":
      return "删除";
    case "modify":
      return "修改";
    default:
      return "相同";
  }
}

// ---------------- 树形数据构建 ----------------
export interface CompareTreeNode {
  key: string;
  name: string;
  isDir: boolean;
  operation?: string;
  item?: CompareFileItem;
  children: CompareTreeNode[];
  allFilePaths: string[];
}

export interface FlatTreeRow {
  key: string;
  name: string;
  isDir: boolean;
  depth: number;
  operation?: string;
  item?: CompareFileItem;
  allFilePaths: string[];
  expanded?: boolean;
}

const compareTree = computed<CompareTreeNode[]>(() => {
  const items = compare.filteredFiles;
  if (!items || items.length === 0) return [];

  const rootMap = new Map<string, CompareTreeNode>();

  for (const item of items) {
    const parts = item.path.split("/");
    let currentPath = "";
    let parentChildren: CompareTreeNode[] | null = null;

    for (let i = 0; i < parts.length; i++) {
      const part = parts[i];
      const isFile = i === parts.length - 1;
      currentPath = currentPath ? `${currentPath}/${part}` : part;

      if (i === 0) {
        let node = rootMap.get(currentPath);
        if (!node) {
          node = {
            key: currentPath,
            name: part,
            isDir: !isFile,
            operation: isFile ? item.operation : undefined,
            item: isFile ? item : undefined,
            children: [],
            allFilePaths: [],
          };
          rootMap.set(currentPath, node);
        }
        node.allFilePaths.push(item.path);
        parentChildren = node.children;
      } else {
        if (!parentChildren) break;
        let node = parentChildren.find((c) => c.key === currentPath);
        if (!node) {
          node = {
            key: currentPath,
            name: part,
            isDir: !isFile,
            operation: isFile ? item.operation : undefined,
            item: isFile ? item : undefined,
            children: [],
            allFilePaths: [],
          };
          parentChildren.push(node);
        }
        node.allFilePaths.push(item.path);
        parentChildren = node.children;
      }
    }
  }

  function sortNodes(nodes: CompareTreeNode[]): void {
    nodes.sort((a, b) => {
      if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
      return a.name.localeCompare(b.name);
    });
    for (const node of nodes) {
      if (node.children.length > 0) {
        sortNodes(node.children);
      }
    }
  }

  const result = Array.from(rootMap.values());
  sortNodes(result);
  return result;
});

const allDirectoryKeys = computed<string[]>(() => {
  const keys: string[] = [];
  function traverse(nodes: CompareTreeNode[]): void {
    for (const n of nodes) {
      if (n.isDir) {
        keys.push(n.key);
        traverse(n.children);
      }
    }
  }
  traverse(compareTree.value);
  return keys;
});

const visibleTreeRows = computed<FlatTreeRow[]>(() => {
  const rows: FlatTreeRow[] = [];
  function traverse(nodes: CompareTreeNode[], depth: number): void {
    for (const node of nodes) {
      const expanded = compare.isExpanded(node.key);
      rows.push({
        key: node.key,
        name: node.name,
        isDir: node.isDir,
        depth,
        operation: node.operation,
        item: node.item,
        allFilePaths: node.allFilePaths,
        expanded,
      });
      if (node.isDir && expanded && node.children.length > 0) {
        traverse(node.children, depth + 1);
      }
    }
  }
  traverse(compareTree.value, 0);
  return rows;
});

function getDirCheckState(allPaths: string[]): { checked: boolean; indeterminate: boolean } {
  if (allPaths.length === 0) return { checked: false, indeterminate: false };
  let selectedCount = 0;
  for (const p of allPaths) {
    if (compare.isSelected(p)) selectedCount++;
  }
  if (selectedCount === 0) return { checked: false, indeterminate: false };
  if (selectedCount === allPaths.length) return { checked: true, indeterminate: false };
  return { checked: false, indeterminate: true };
}

function onDirCheckChange(allPaths: string[]): void {
  const state = getDirCheckState(allPaths);
  if (state.checked || state.indeterminate) {
    compare.deselectAll(allPaths);
  } else {
    compare.selectAll(allPaths);
  }
}

// ---------------- 文本换行符归一化 ----------------
function normalizeText(text: string): string {
  if (!text) return "";
  return text.replace(/\r\n/g, "\n").replace(/\r/g, "\n");
}

// ---------------- IntelliJ IDEA 风格 Live 双栏 MergeView 编辑架构 ----------------
const mergeHostRef = ref<HTMLDivElement | null>(null);
let mergeViewInstance: MergeView | null = null;

// 差异块数量与当前选中的差异索引
const mergeChunksCount = ref(0);
const activeChunkIdx = ref(0);

// 本地草稿文本与未保存基准
const localDraftText = ref("");
const initialDraftText = ref("");
const isSavingLocal = ref(false);

const activeEditorTab = computed(() => {
  if (!compare.activeFilePath) return null;
  return editor.getFileDraft(compare.activeFilePath) ?? null;
});

const isLocalTextDirty = computed(() => {
  return localDraftText.value !== initialDraftText.value;
});

/**
 * 局部编辑器基础主题配置
 * 注意：不要设置 "&": { height: "100%" }，以便 MergeView 的 baseTheme 自然接管高度与滚动
 */
function createEditorTheme(themeId: ResolvedThemeId) {
  return EditorView.theme(
    {
      "&": {
        fontSize: "13px",
        color: "var(--pvf-text-primary)",
        backgroundColor: "transparent",
      },
      ".cm-scroller": {
        fontFamily: "'SF Mono', Menlo, Consolas, 'Courier New', monospace",
        lineHeight: "1.55",
        userSelect: "text",
      },
      ".cm-gutters": {
        backgroundColor: "transparent",
        border: "none",
        color: "var(--pvf-editor-gutter-text)",
      },
      ".cm-content": {
        padding: "8px 0",
      },
      ".cm-activeLine": {
        backgroundColor: "var(--pvf-editor-active-line)",
      },
      ".cm-activeLineGutter": {
        backgroundColor: "var(--pvf-editor-active-line)",
      },
      ".cm-selectionMatch": {
        backgroundColor: "var(--pvf-editor-selection-match)",
      },
    },
    { dark: themeId === "dark" }
  );
}

function destroyMergeView(): void {
  if (mergeViewInstance) {
    mergeViewInstance.destroy();
    mergeViewInstance = null;
    mergeChunksCount.value = 0;
    activeChunkIdx.value = 0;
  }
}

function updateActiveChunkFromHead(head: number): void {
  if (!mergeViewInstance || mergeViewInstance.chunks.length === 0) return;
  const idx = mergeViewInstance.chunks.findIndex(
    (c) => head >= c.fromA && head <= c.toA
  );
  if (idx !== -1) {
    activeChunkIdx.value = idx;
  }
}

function handleEditorUpdate(update: any): void {
  if (update.docChanged) {
    const newDoc = update.state.doc.toString();
    localDraftText.value = newDoc;
    if (activeEditorTab.value) {
      editor.updateContent(activeEditorTab.value.index, newDoc);
    }

    // 检查是否是一键合入段落触发的事务
    const isRevert = update.transactions.some((tr: any) => {
      try {
        return tr.isUserEvent && tr.isUserEvent("revert");
      } catch {
        return false;
      }
    });
    if (isRevert) {
      message.success("已合入该差异段落到本地 (Current)");
    }
  }

  if (mergeViewInstance) {
    mergeChunksCount.value = mergeViewInstance.chunks.length;
    if (update.selectionSet) {
      const head = update.state.selection.main.head;
      updateActiveChunkFromHead(head);
    }
  }
}

/**
 * 自定义渲染两栏中间的快捷合入按钮
 * 渲染经典 IDEA 风格的左箭头 «
 */
function renderRevertControlElement(): HTMLElement {
  const btn = document.createElement("button");
  btn.className = "cm-revert-action-btn";
  btn.title = "合入此段落到本地 (Current)";
  btn.setAttribute("aria-label", "合入此段落到本地");
  btn.textContent = "«";
  return btn;
}

function initMergeView(): void {
  destroyMergeView();
  if (!mergeHostRef.value || !compare.activeDiff || !compare.activeDiff.textAvailable) {
    return;
  }

  const baseDoc = normalizeText(localDraftText.value);
  const targetDoc = normalizeText(compare.activeDiff.targetText || "");
  const themeExt = createEditorTheme(props.themeId || "dark");

  const commonExtensions = [
    lineNumbers(),
    highlightActiveLineGutter(),
    highlightActiveLine(),
    drawSelection(),
    history(),
    pvfLanguage.extension,
    pvfHighlighting,
    themeExt,
    keymap.of([...defaultKeymap, ...historyKeymap]),
  ];

  mergeViewInstance = new MergeView({
    a: {
      doc: baseDoc,
      extensions: [
        ...commonExtensions,
        keymap.of([
          {
            key: "Mod-s",
            run: () => {
              void saveLocalDraft();
              return true;
            },
          },
        ]),
        EditorView.updateListener.of(handleEditorUpdate),
      ],
    },
    b: {
      doc: targetDoc,
      extensions: [
        ...commonExtensions,
        EditorState.readOnly.of(true),
        EditorView.editable.of(false),
      ],
    },
    parent: mergeHostRef.value,
    revertControls: "b-to-a",
    renderRevertControl: renderRevertControlElement,
    gutter: true,
    highlightChanges: true,
    orientation: "a-b",
  });

  // 绑定原生滚动监听，确保两栏滚动时绝对顺畅且合入按钮位置始终同步
  mergeViewInstance.dom.addEventListener(
    "scroll",
    () => {
      (mergeViewInstance as any)?.scheduleMeasure?.();
    },
    { passive: true }
  );

  mergeChunksCount.value = mergeViewInstance.chunks.length;
  activeChunkIdx.value = 0;
}

async function setupMergeView(): Promise<void> {
  await nextTick();
  initMergeView();
}

// 监听当前比对文件切换
watch(
  () => [compare.activeFilePath, compare.activeDiff?.baseText, compare.activeDiff?.targetText],
  () => {
    const rawTxt = activeEditorTab.value?.text ?? compare.activeDiff?.baseText ?? "";
    const txt = normalizeText(rawTxt);
    localDraftText.value = txt;
    initialDraftText.value = txt;
    void setupMergeView();
  },
  { immediate: true }
);

// 监听主编辑器外部文本变更时（若在主编辑器修改了该文件，对比器自动同步）
watch(
  () => activeEditorTab.value?.text,
  (newText) => {
    if (newText !== undefined) {
      const normalized = normalizeText(newText);
      if (normalized !== localDraftText.value) {
        localDraftText.value = normalized;
        initialDraftText.value = normalized;
        void setupMergeView();
      }
    }
  }
);

// 监听挂载容器 DOM 变化
watch(
  () => mergeHostRef.value,
  (el) => {
    if (el && !mergeViewInstance) {
      void setupMergeView();
    }
  }
);

// 保存本地编辑内容到本地 PVF 归档
async function saveLocalDraft(): Promise<void> {
  if (!compare.activeFilePath) return;
  isSavingLocal.value = true;
  try {
    await CompareService.MergeSingleFileText(compare.activeFilePath, localDraftText.value);
    initialDraftText.value = localDraftText.value;
    if (activeEditorTab.value) {
      activeEditorTab.value.original = localDraftText.value;
    }
    message.success("已保存修改并同步到本地归档与主编辑器");
    await compare.selectFile(compare.activeFilePath);
  } catch (err: any) {
    message.error(`保存失败: ${err?.message ?? err}`);
  } finally {
    isSavingLocal.value = false;
  }
}

// ---------------- 纯图标差异段落导航 ----------------
function scrollToChunk(targetIndex: number): void {
  if (!mergeViewInstance || mergeViewInstance.chunks.length === 0) return;
  const total = mergeViewInstance.chunks.length;
  const clampedIndex = ((targetIndex % total) + total) % total;
  activeChunkIdx.value = clampedIndex;

  const targetChunk = mergeViewInstance.chunks[clampedIndex];
  if (!targetChunk) return;

  const scroller = mergeViewInstance.dom;
  const clientHeight = scroller.clientHeight || 500;
  const scrollHeight = scroller.scrollHeight || clientHeight;

  // 1. 优先从已经挂载的段落合入按钮获取绝对物理坐标
  const chunkBtn = scroller.querySelector(
    `button[data-chunk="${clampedIndex}"]`
  ) as HTMLElement | null;

  let targetScrollTop = 0;

  if (chunkBtn && chunkBtn.offsetTop > 0) {
    targetScrollTop = Math.max(0, chunkBtn.offsetTop - clientHeight / 2 + 15);
  } else {
    // 2. 若按钮尚未进入视口，根据差异所在行号与总行数比例精确预估滚动位置
    const lineA = mergeViewInstance.a.state.doc.lineAt(
      Math.min(targetChunk.fromA, mergeViewInstance.a.state.doc.length)
    ).number;
    const lineB = mergeViewInstance.b.state.doc.lineAt(
      Math.min(targetChunk.fromB, mergeViewInstance.b.state.doc.length)
    ).number;
    const totalLinesA = mergeViewInstance.a.state.doc.lines;
    const totalLinesB = mergeViewInstance.b.state.doc.lines;
    const maxLines = Math.max(totalLinesA, totalLinesB, 1);
    const targetLine = Math.max(lineA, lineB, 1);

    targetScrollTop = Math.max(
      0,
      ((targetLine - 1) / maxLines) * scrollHeight - clientHeight / 2 + 20
    );
  }

  // 执行平滑居中滚动
  scroller.scrollTo({
    top: targetScrollTop,
    behavior: "smooth",
  });

  if (mergeHostRef.value && mergeHostRef.value.scrollHeight > mergeHostRef.value.clientHeight) {
    mergeHostRef.value.scrollTo({
      top: targetScrollTop,
      behavior: "smooth",
    });
  }

  // 3. 调度左侧与右侧编辑器的光标选区并聚焦
  const anchorA = Math.min(targetChunk.fromA, mergeViewInstance.a.state.doc.length);
  const anchorB = Math.min(targetChunk.fromB, mergeViewInstance.b.state.doc.length);

  mergeViewInstance.a.dispatch({
    selection: { anchor: anchorA },
    effects: EditorView.scrollIntoView(anchorA, { y: "center" }),
  });

  mergeViewInstance.b.dispatch({
    selection: { anchor: anchorB },
  });

  // 确保编辑器获得焦点，光标闪烁
  mergeViewInstance.a.focus();

  // 4. 等待视口渲染出实际 DOM 节点后做二次对齐校准
  requestAnimationFrame(() => {
    setTimeout(() => {
      if (!mergeViewInstance) return;
      const activeBtn = mergeViewInstance.dom.querySelector(
        `button[data-chunk="${clampedIndex}"]`
      ) as HTMLElement | null;
      if (activeBtn && activeBtn.offsetTop > 0) {
        const preciseTop = Math.max(0, activeBtn.offsetTop - scroller.clientHeight / 2 + 15);
        if (Math.abs(scroller.scrollTop - preciseTop) > 30) {
          scroller.scrollTo({
            top: preciseTop,
            behavior: "smooth",
          });
        }
      }
    }, 100);
  });
}

function goToPrevHunk(): void {
  scrollToChunk(activeChunkIdx.value - 1);
}

function goToNextHunk(): void {
  scrollToChunk(activeChunkIdx.value + 1);
}

// 快捷键全局监听 (Ctrl+S / Cmd+S)
function handleKeydown(e: KeyboardEvent): void {
  if ((e.ctrlKey || e.metaKey) && (e.key === "s" || e.key === "S")) {
    if (isLocalTextDirty.value) {
      e.preventDefault();
      void saveLocalDraft();
    }
  }
}

onMounted(() => {
  window.addEventListener("keydown", handleKeydown);
});

onBeforeUnmount(() => {
  destroyMergeView();
  window.removeEventListener("keydown", handleKeydown);
});

// ---------------- 批量与单文件合并操作 ----------------
async function handleMergeCurrentFile(): Promise<void> {
  if (!compare.activeFilePath) return;
  const ok = await compare.mergeSingleFile(compare.activeFilePath);
  if (ok) {
    message.success(`已成功合并 ${compare.activeFilePath} 到本地`);
    const newText = normalizeText(compare.activeDiff?.baseText ?? "");
    localDraftText.value = newText;
    initialDraftText.value = newText;
    void setupMergeView();
  } else {
    message.error("合并文件失败");
  }
}

async function handleMergeSelected(): Promise<void> {
  const count = compare.selectedPaths.size;
  if (count === 0) return;
  const res = await compare.mergeSelected();
  if (res) {
    if (res.successCount > 0) {
      message.success(`成功合并 ${res.successCount} 个文件到本地工作区`);
    }
    if (res.failedCount > 0) {
      message.error(`有 ${res.failedCount} 个文件合并失败: ${res.errors?.join("; ") || ""}`);
    }
  }
}

const allFilteredFilePaths = computed(() => {
  return compare.filteredFiles.map((f) => f.path);
});

const isAllFilteredSelected = computed(() => {
  if (allFilteredFilePaths.value.length === 0) return false;
  return allFilteredFilePaths.value.every((p) => compare.isSelected(p));
});

const isPartiallyFilteredSelected = computed(() => {
  if (isAllFilteredSelected.value) return false;
  return allFilteredFilePaths.value.some((p) => compare.isSelected(p));
});

function toggleSelectAllFiltered(): void {
  if (isAllFilteredSelected.value || isPartiallyFilteredSelected.value) {
    compare.deselectAll(allFilteredFilePaths.value);
  } else {
    compare.selectAll(allFilteredFilePaths.value);
  }
}
</script>

<template>
  <div class="compare-workbench-root">
    <!-- 顶部配置栏 (支持折叠收起) -->
    <div v-show="!isHeaderCollapsed" class="compare-header-bar">
      <!-- 左侧：本地归档展示 -->
      <div class="header-archive-side header-archive-side--local" :title="compare.localArchivePath">
        <span class="role-pill role-pill--local">本地</span>
        <NIcon size="15" class="pill-icon"><Document20Regular /></NIcon>
        <span class="pill-filename">{{ getFileName(compare.localArchivePath) }}</span>
        <span class="pill-path-subtle">{{ compare.localArchivePath }}</span>
      </div>

      <!-- 中间：对比连接标识 -->
      <div class="header-vs-connector" title="归档差异对比">
        <NIcon size="15" class="vs-icon"><ArrowSyncCircle20Regular /></NIcon>
        <span class="vs-label">VS</span>
      </div>

      <!-- 右侧：对比目标输入与操作 -->
      <div class="header-archive-side header-archive-side--target">
        <span class="role-pill role-pill--target">目标</span>
        <div class="target-input-box">
          <NInput
            v-model:value="compare.targetPath"
            placeholder="请选择或输入对比目标 .pvf 路径..."
            size="small"
            clearable
          >
            <template #prefix>
              <NIcon size="14"><FolderOpen20Regular /></NIcon>
            </template>
          </NInput>
        </div>
        <NButton size="small" secondary @click="compare.selectTargetPath">
          <template #icon><NIcon><FolderOpen20Regular /></NIcon></template>
          浏览
        </NButton>
        <NButton
          type="primary"
          size="small"
          :loading="compare.loading"
          :disabled="!compare.targetPath"
          @click="compare.startCompare"
        >
          开始对比
        </NButton>

        <!-- 有比对结果时允许手动收起 -->
        <NTooltip v-if="compare.hasSummary" trigger="hover">
          <template #trigger>
            <button class="collapse-header-btn" title="收起配置栏" @click="isHeaderCollapsed = true">
              <NIcon size="14"><ChevronUp20Regular /></NIcon>
              <span>收起</span>
            </button>
          </template>
          收起顶部配置栏，释放代码可视区域
        </NTooltip>
      </div>
    </div>

    <!-- 当配置栏收起时呈现的极简状态条 (高仅 28px) -->
    <div v-if="compare.hasSummary && isHeaderCollapsed" class="compare-mini-summary-bar">
      <div class="mini-bar-left">
        <span class="mini-role-pill mini-role-pill--local">本地</span>
        <span class="mini-filename">{{ getFileName(compare.localArchivePath) }}</span>
        <span class="mini-vs">⇄</span>
        <span class="mini-role-pill mini-role-pill--target">目标</span>
        <span class="mini-filename">{{ getFileName(compare.targetPath) }}</span>
      </div>
      <button class="mini-expand-btn" @click="isHeaderCollapsed = false">
        <NIcon size="13"><ChevronDown20Regular /></NIcon>
        <span>更换对比目标 / 展开配置</span>
      </button>
    </div>

    <NAlert v-if="compare.error" type="error" closable class="compare-error-alert">
      {{ compare.error }}
    </NAlert>

    <!-- 比对结果区域 -->
    <div v-if="compare.hasSummary" class="compare-body">
      <!-- 统计与过滤条 -->
      <div class="compare-toolbar">
        <div class="filter-tags-row">
          <button
            class="filter-tag-btn"
            :class="{ 'filter-tag-btn--active': compare.filter === 'all' }"
            @click="compare.setFilter('all')"
          >
            全部差异 ({{ compare.summary?.files?.length ?? 0 }})
          </button>
          <button
            class="filter-tag-btn filter-tag-btn--modify"
            :class="{ 'filter-tag-btn--active': compare.filter === 'modify' }"
            @click="compare.setFilter('modify')"
          >
            修改 ({{ compare.summary?.modifyCount ?? 0 }})
          </button>
          <button
            class="filter-tag-btn filter-tag-btn--add"
            :class="{ 'filter-tag-btn--active': compare.filter === 'add' }"
            @click="compare.setFilter('add')"
          >
            新增 ({{ compare.summary?.addCount ?? 0 }})
          </button>
          <button
            class="filter-tag-btn filter-tag-btn--delete"
            :class="{ 'filter-tag-btn--active': compare.filter === 'delete' }"
            @click="compare.setFilter('delete')"
          >
            删除 ({{ compare.summary?.deleteCount ?? 0 }})
          </button>
          <button
            class="filter-tag-btn filter-tag-btn--same"
            :class="{ 'filter-tag-btn--active': compare.filter === 'same' }"
            @click="compare.setFilter('same')"
          >
            相同 ({{ compare.summary?.sameCount ?? 0 }})
          </button>
        </div>

        <div class="search-input-col">
          <NInput
            v-model:value="compare.searchQuery"
            placeholder="快速过滤路径..."
            size="small"
            clearable
          >
            <template #prefix><NIcon size="14"><Search20Regular /></NIcon></template>
          </NInput>
        </div>
      </div>

      <!-- 主面板：左侧文件差异树 + 右侧双栏对比 -->
      <div class="compare-dual-pane">
        <!-- 左栏：文件差异树/列表 -->
        <div class="compare-file-list-pane">
          <!-- 树操作工具条 -->
          <div class="pane-action-bar">
            <div class="pane-action-left">
              <NCheckbox
                :checked="isAllFilteredSelected"
                :indeterminate="isPartiallyFilteredSelected"
                size="small"
                @update:checked="toggleSelectAllFiltered"
              >
                <span class="select-label">
                  全选
                  <span v-if="compare.selectedPaths.size > 0">
                    ({{ compare.selectedPaths.size }})
                  </span>
                </span>
              </NCheckbox>
            </div>

            <div class="pane-action-right">
              <NButton
                type="success"
                size="tiny"
                :disabled="compare.selectedPaths.size === 0"
                :loading="compare.merging"
                @click="handleMergeSelected"
              >
                <template #icon><NIcon><ArrowDownload20Regular /></NIcon></template>
                合并选中 ({{ compare.selectedPaths.size }})
              </NButton>

              <NTooltip trigger="hover">
                <template #trigger>
                  <button
                    class="icon-action-btn"
                    :class="{ 'icon-action-btn--active': compare.viewMode === 'tree' }"
                    @click="compare.viewMode = compare.viewMode === 'tree' ? 'flat' : 'tree'"
                  >
                    <NIcon size="15"><TextBulletListSquare20Regular /></NIcon>
                  </button>
                </template>
                {{ compare.viewMode === 'tree' ? '切换为平铺列表' : '切换为树形视图' }}
              </NTooltip>

              <template v-if="compare.viewMode === 'tree'">
                <NTooltip trigger="hover">
                  <template #trigger>
                    <button class="icon-action-btn" @click="compare.expandAll(allDirectoryKeys)">
                      <NIcon size="15"><ChevronDown20Regular /></NIcon>
                    </button>
                  </template>
                  全部展开
                </NTooltip>
                <NTooltip trigger="hover">
                  <template #trigger>
                    <button class="icon-action-btn" @click="compare.collapseAll">
                      <NIcon size="15"><ArrowCollapseAll20Regular /></NIcon>
                    </button>
                  </template>
                  全部折叠
                </NTooltip>
              </template>
            </div>
          </div>

          <!-- 加载态 / 空状态 -->
          <div v-if="compare.loadingSame" class="pane-empty-box">
            <NSpin size="medium" description="正在按需加载未变更文件..." />
          </div>
          <div v-else-if="compare.filteredFiles.length === 0" class="pane-empty-box">
            <NEmpty
              :description="compare.filter === 'same' ? '未变更文件默认不加载以节省内存，可点击上方“相同”加载' : '无匹配差异文件'"
              size="small"
            />
          </div>

          <!-- 1. 树形视图渲染 -->
          <div v-else-if="compare.viewMode === 'tree'" class="file-items-scroll">
            <div
              v-for="row in visibleTreeRows"
              :key="row.key"
              class="tree-node-row"
              :class="{
                'tree-node-row--active': !row.isDir && compare.activeFilePath === row.key,
                'tree-node-row--dir': row.isDir,
              }"
              :style="{ paddingLeft: `${row.depth * 14 + 6}px` }"
              @click="row.isDir ? compare.toggleExpand(row.key) : compare.selectFile(row.key)"
            >
              <span v-if="row.isDir" class="expand-icon-box" @click.stop="compare.toggleExpand(row.key)">
                <NIcon size="13">
                  <ChevronDown20Regular v-if="row.expanded" />
                  <ChevronRight20Regular v-else />
                </NIcon>
              </span>
              <span v-else class="expand-placeholder" />

              <span class="checkbox-box" @click.stop>
                <NCheckbox
                  v-if="row.isDir"
                  :checked="getDirCheckState(row.allFilePaths).checked"
                  :indeterminate="getDirCheckState(row.allFilePaths).indeterminate"
                  size="small"
                  @update:checked="onDirCheckChange(row.allFilePaths)"
                />
                <NCheckbox
                  v-else
                  :checked="compare.isSelected(row.key)"
                  size="small"
                  @update:checked="compare.toggleSelect(row.key)"
                />
              </span>

              <ImageThumbnail
                v-if="!row.isDir && row.item?.icon"
                :reference="row.item.icon"
                :size="16"
                class="node-thumb"
              />
              <NIcon v-else class="node-icon" size="15">
                <Folder20Regular v-if="row.isDir" />
                <Document20Regular v-else />
              </NIcon>
              <span class="node-name" :title="row.key">{{ row.name }}</span>

              <div v-if="!row.isDir && row.item" class="node-tags-inline">
                <NTag
                  v-for="(ann, aIdx) in row.item.annotations"
                  :key="`ann-${aIdx}`"
                  size="tiny"
                  :bordered="false"
                  :type="ann.type === 'reference' ? 'success' : ann.type === 'enum' ? 'warning' : 'info'"
                  class="tree-tag-annotation"
                  :title="ann.content"
                >
                  {{ ann.title }}
                </NTag>

                <template v-for="(tag, tIdx) in row.item.tags" :key="`tag-${tIdx}`">
                  <NTag
                    v-if="tag.id"
                    size="tiny"
                    :bordered="false"
                    type="info"
                    class="tree-tag-id"
                    :title="`id: ${tag.id}`"
                  >
                    {{ tag.id }}
                  </NTag>
                  <NTag
                    v-if="tag.name"
                    size="tiny"
                    :bordered="false"
                    type="success"
                    class="tree-tag-name"
                    :title="tag.name"
                    :style="rarityTextStyle(tag.rarity)"
                  >
                    {{ tag.name }}
                  </NTag>
                </template>
              </div>

              <span v-if="!row.isDir && compare.isMerged(row.key)" class="merged-tag">
                <NIcon size="12"><CheckmarkCircle20Regular /></NIcon> 已合并
              </span>
              <NTag
                v-else-if="!row.isDir && row.operation"
                :type="getOperationTagType(row.operation)"
                size="tiny"
                round
                class="node-op-tag"
              >
                {{ getOperationLabel(row.operation) }}
              </NTag>
            </div>
          </div>

          <!-- 2. 平铺列表视图渲染 -->
          <div v-else class="file-items-scroll">
            <div
              v-for="item in compare.filteredFiles"
              :key="item.path"
              class="file-item-row"
              :class="{ 'file-item-row--active': compare.activeFilePath === item.path }"
              @click="compare.selectFile(item.path)"
            >
              <span class="checkbox-box" @click.stop>
                <NCheckbox
                  :checked="compare.isSelected(item.path)"
                  size="small"
                  @update:checked="compare.toggleSelect(item.path)"
                />
              </span>
              <div class="file-item-left">
                <ImageThumbnail
                  v-if="item.icon"
                  :reference="item.icon"
                  :size="16"
                  class="node-thumb"
                />
                <span v-if="compare.isMerged(item.path)" class="merged-tag">
                  <NIcon size="12"><CheckmarkCircle20Regular /></NIcon> 已合并
                </span>
                <NTag v-else :type="getOperationTagType(item.operation)" size="tiny" round>
                  {{ getOperationLabel(item.operation) }}
                </NTag>
                <span class="file-item-path" :title="item.path">{{ item.path }}</span>

                <div class="node-tags-inline">
                  <NTag
                    v-for="(ann, aIdx) in item.annotations"
                    :key="`ann-${aIdx}`"
                    size="tiny"
                    :bordered="false"
                    :type="ann.type === 'reference' ? 'success' : ann.type === 'enum' ? 'warning' : 'info'"
                    class="tree-tag-annotation"
                    :title="ann.content"
                  >
                    {{ ann.title }}
                  </NTag>

                  <template v-for="(tag, tIdx) in item.tags" :key="`tag-${tIdx}`">
                    <NTag
                      v-if="tag.id"
                      size="tiny"
                      :bordered="false"
                      type="info"
                      class="tree-tag-id"
                      :title="`id: ${tag.id}`"
                    >
                      {{ tag.id }}
                    </NTag>
                    <NTag
                      v-if="tag.name"
                      size="tiny"
                      :bordered="false"
                      type="success"
                      class="tree-tag-name"
                      :title="tag.name"
                      :style="rarityTextStyle(tag.rarity)"
                    >
                      {{ tag.name }}
                    </NTag>
                  </template>
                </div>
              </div>
              <div class="file-item-right">
                <span v-if="item.operation === 'modify'" class="size-diff-tag">
                  {{ formatSize(item.baseSize) }} → {{ formatSize(item.targetSize) }}
                </span>
                <span v-else-if="item.operation === 'add'" class="size-diff-tag">
                  +{{ formatSize(item.targetSize) }}
                </span>
                <span v-else-if="item.operation === 'delete'" class="size-diff-tag">
                  -{{ formatSize(item.baseSize) }}
                </span>
              </div>
            </div>
          </div>
        </div>

        <!-- 右栏：IDEA 风格双栏代码对比与段落合入面板 -->
        <div class="compare-diff-pane">
          <div v-if="compare.activeFilePath" class="diff-header-bar">
            <div class="diff-header-left">
              <span class="diff-file-path" :title="compare.activeFilePath">
                {{ compare.activeFilePath }}
              </span>
              <NTag
                v-if="compare.activeDiff"
                :type="getOperationTagType(compare.activeDiff.operation)"
                size="small"
              >
                {{ getOperationLabel(compare.activeDiff.operation) }}
              </NTag>
              <span v-if="compare.isMerged(compare.activeFilePath)" class="merged-tag merged-tag--header">
                <NIcon size="14"><CheckmarkCircle20Regular /></NIcon> 已合并至本地
              </span>
            </div>

            <!-- 中间：上一个差异 / 下一个差异 纯图标导航器 -->
            <div class="diff-header-center">
              <div class="diff-nav-box">
                <NTooltip trigger="hover">
                  <template #trigger>
                    <button
                      class="nav-diff-btn nav-diff-btn--icon-only"
                      :disabled="mergeChunksCount === 0"
                      @click="goToPrevHunk"
                    >
                      <NIcon size="15"><ArrowUp20Regular /></NIcon>
                    </button>
                  </template>
                  上一个差异
                </NTooltip>

                <span class="nav-diff-counter">
                  {{ mergeChunksCount > 0 ? `${activeChunkIdx + 1} / ${mergeChunksCount}` : '无差异' }}
                </span>

                <NTooltip trigger="hover">
                  <template #trigger>
                    <button
                      class="nav-diff-btn nav-diff-btn--icon-only"
                      :disabled="mergeChunksCount === 0"
                      @click="goToNextHunk"
                    >
                      <NIcon size="15"><ArrowDown20Regular /></NIcon>
                    </button>
                  </template>
                  下一个差异
                </NTooltip>
              </div>
            </div>

            <div class="diff-actions-row">
              <NButton
                type="success"
                size="tiny"
                :loading="compare.merging"
                @click="handleMergeCurrentFile"
              >
                <template #icon><NIcon><DocumentArrowLeft20Regular /></NIcon></template>
                « 将此文件合并到本地
              </NButton>

              <NButton size="tiny" :disabled="!canOpenInEditor" @click="openInEditor(compare.activeFilePath)">
                <template #icon><NIcon><Open20Regular /></NIcon></template>
                主编辑器打开
              </NButton>
              <NButton size="tiny" @click="copyPath(compare.activeFilePath)">
                <template #icon><NIcon><Copy20Regular /></NIcon></template>
                复制路径
              </NButton>
            </div>
          </div>

          <!-- Diff 视口内容 -->
          <div class="diff-viewport">
            <div v-if="compare.diffLoading" class="diff-msg-box">
              <NSpin size="medium" description="正在生成代码级双栏对比..." />
            </div>
            <div v-else-if="compare.diffError" class="diff-msg-box">
              <NAlert type="error">{{ compare.diffError }}</NAlert>
            </div>
            <div v-else-if="!compare.activeFilePath" class="diff-msg-box">
              <NEmpty description="请在左侧文件树选择文件以查看双栏差异" size="small" />
            </div>

            <!-- 二进制文件展示 -->
            <div v-else-if="compare.activeDiff && !compare.activeDiff.textAvailable" class="diff-binary-box">
              <NIcon size="44" color="var(--pvf-text-muted)"><Document20Regular /></NIcon>
              <div class="diff-binary-title">二进制或复合资源文件</div>
              <div class="diff-binary-desc">
                该文件非纯文本脚本，无法提供代码级文本对比。
                <div class="binary-sizes">
                  本地大小: {{ formatSize(compare.activeDiff.baseSize) }} | 目标大小: {{ formatSize(compare.activeDiff.targetSize) }}
                </div>
              </div>
              <NButton
                type="success"
                size="small"
                style="margin-top: 14px;"
                :loading="compare.merging"
                @click="handleMergeCurrentFile"
              >
                <template #icon><NIcon><DocumentArrowLeft20Regular /></NIcon></template>
                « 覆盖合并该二进制文件到本地
              </NButton>
            </div>

            <!-- 文本文件对比与直接编辑容器 (IDEA 风格双栏 MergeView) -->
            <div v-else-if="compare.activeDiff" class="side-by-side-wrapper">
              <!-- 双栏置顶表头 -->
              <div class="side-diff-header">
                <div class="side-column-title side-column-title--left">
                  <span class="side-title-badge side-title-badge--local">本地 (Current - 可直接编辑)</span>

                  <!-- 未保存状态标与保存按钮 -->
                  <div v-if="isLocalTextDirty" class="save-status-group">
                    <span class="dirty-badge" title="本地草稿已修改，尚未写盘">● 未保存</span>
                    <button
                      class="save-pill-btn"
                      :disabled="isSavingLocal"
                      title="保存修改到本地归档与主编辑器 (Ctrl+S)"
                      @click="saveLocalDraft"
                    >
                      <NIcon size="13"><Save20Regular /></NIcon>
                      <span>保存</span>
                    </button>
                  </div>

                  <span class="side-title-path">
                    {{ compare.activeDiff.operation === 'add' ? '(本地不存在此文件)' : compare.activeFilePath }}
                  </span>
                  <span class="side-title-size">
                    {{ formatSize(compare.activeDiff.baseSize) }}
                  </span>
                </div>

                <div class="side-column-title side-column-title--right">
                  <span class="side-title-badge side-title-badge--target">目标 (Target - 只读)</span>
                  <span class="side-title-path">
                    {{ compare.activeDiff.operation === 'delete' ? '(目标不存在此文件)' : compare.activeFilePath }}
                  </span>
                  <span class="side-title-size">
                    {{ formatSize(compare.activeDiff.targetSize) }}
                  </span>
                </div>
              </div>

              <!-- CodeMirror 原生双栏合并编辑器挂载宿主容器 -->
              <div ref="mergeHostRef" class="live-merge-editor-host" />
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.compare-workbench-root {
  display: flex;
  flex-direction: column;
  width: 100%;
  height: 100%;
  min-height: 0;
  min-width: 0;
  overflow: hidden;
  box-sizing: border-box;
  padding: 10px 14px 12px 14px;
  gap: 8px;
  background: var(--pvf-window-background-solid);
  color: var(--pvf-text-primary, #333);
}

/* 顶部配置栏 (单行流线型现代控制条) */
.compare-header-bar {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  background: var(--pvf-surface-subtle, rgba(0, 0, 0, 0.03));
  border: 1px solid var(--pvf-border-subtle, rgba(0, 0, 0, 0.1));
  border-radius: 6px;
  padding: 6px 12px;
  min-height: 40px;
  box-sizing: border-box;
}

.header-archive-side {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.header-archive-side--local {
  flex: 1 1 0%;
  overflow: hidden;
}

.header-archive-side--target {
  flex: 1.2 1 0%;
  justify-content: flex-end;
}

.role-pill {
  font-size: 11px;
  font-weight: 600;
  padding: 2px 7px;
  border-radius: 4px;
  letter-spacing: 0.3px;
  flex-shrink: 0;
}

.role-pill--local {
  background: rgba(33, 150, 243, 0.16);
  color: #2196f3;
  border: 1px solid rgba(33, 150, 243, 0.3);
}

.role-pill--target {
  background: rgba(156, 39, 176, 0.16);
  color: #ab47bc;
  border: 1px solid rgba(156, 39, 176, 0.3);
}

.pill-icon {
  color: var(--pvf-text-muted, #888);
  flex-shrink: 0;
}

.pill-filename {
  font-size: 12px;
  font-weight: 600;
  color: var(--pvf-text-primary, #e6edf3);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  white-space: nowrap;
  flex-shrink: 0;
}

.pill-path-subtle {
  font-size: 11px;
  color: var(--pvf-text-muted, #777);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

.header-vs-connector {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
  padding: 2px 8px;
  background: var(--pvf-surface-panel, rgba(0, 0, 0, 0.05));
  border: 1px solid var(--pvf-border-subtle, rgba(255, 255, 255, 0.08));
  border-radius: 12px;
  color: var(--pvf-text-secondary, #999);
  user-select: none;
}

.vs-icon {
  color: var(--pvf-primary, #18a058);
}

.vs-label {
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.5px;
}

.target-input-box {
  flex: 1;
  min-width: 180px;
  max-width: 380px;
}

.collapse-header-btn {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  background: transparent;
  border: 1px solid var(--pvf-border-subtle, rgba(255, 255, 255, 0.12));
  color: var(--pvf-text-secondary, #aaa);
  font-size: 11px;
  padding: 3px 8px;
  border-radius: 4px;
  cursor: pointer;
  transition: all 0.15s ease;
  flex-shrink: 0;
  height: 28px;
}

.collapse-header-btn:hover {
  background: var(--pvf-surface-hover, rgba(255, 255, 255, 0.08));
  color: #fff;
  border-color: rgba(255, 255, 255, 0.25);
}

/* 当配置栏折叠收起时呈现的极简状态条 (高度仅约 26px) */
.compare-mini-summary-bar {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 2px 10px;
  height: 28px;
  min-height: 28px;
  border-radius: 4px;
  background: var(--pvf-surface-subtle, rgba(0, 0, 0, 0.04));
  border: 1px solid var(--pvf-border-subtle, rgba(255, 255, 255, 0.06));
  font-size: 11px;
  box-sizing: border-box;
  user-select: none;
}

.mini-bar-left {
  display: flex;
  align-items: center;
  gap: 6px;
  overflow: hidden;
}

.mini-role-pill {
  font-size: 10px;
  font-weight: 600;
  padding: 1px 5px;
  border-radius: 3px;
}

.mini-role-pill--local {
  background: rgba(33, 150, 243, 0.16);
  color: #2196f3;
}

.mini-role-pill--target {
  background: rgba(156, 39, 176, 0.16);
  color: #ab47bc;
}

.mini-filename {
  font-family: monospace;
  color: var(--pvf-text-primary, #e6edf3);
  font-weight: 600;
}

.mini-vs {
  color: var(--pvf-text-muted, #777);
}

.mini-expand-btn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  background: transparent;
  border: none;
  color: var(--pvf-primary, #18a058);
  font-size: 11px;
  cursor: pointer;
  padding: 2px 6px;
  border-radius: 3px;
  transition: all 0.15s ease;
}

.mini-expand-btn:hover {
  background: rgba(24, 160, 88, 0.12);
}

.compare-error-alert {
  margin-top: 4px;
}

/* 主体与工具条 */
.compare-body {
  display: flex;
  flex-direction: column;
  flex: 1 1 0%;
  min-height: 0;
  overflow: hidden;
  gap: 6px;
}

.compare-toolbar {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.filter-tags-row {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}

.filter-tag-btn {
  background: var(--pvf-surface-subtle, rgba(0, 0, 0, 0.04));
  border: 1px solid var(--pvf-border-subtle, rgba(0, 0, 0, 0.12));
  color: var(--pvf-text-secondary, #666);
  padding: 3px 10px;
  border-radius: 4px;
  font-size: 12px;
  cursor: pointer;
  transition: all 0.15s ease;
}

.filter-tag-btn:hover {
  background: var(--pvf-surface-hover, rgba(0, 0, 0, 0.08));
  color: var(--pvf-text-primary, #333);
}

.filter-tag-btn--active {
  background: var(--pvf-primary, #18a058);
  border-color: var(--pvf-primary, #18a058);
  color: #fff !important;
  font-weight: 600;
}

.filter-tag-btn--modify {
  border-left: 3px solid #e6a23c;
}

.filter-tag-btn--add {
  border-left: 3px solid #18a058;
}

.filter-tag-btn--delete {
  border-left: 3px solid #e74c3c;
}

.filter-tag-btn--same {
  border-left: 3px solid var(--pvf-text-muted, #888);
}

.search-input-col {
  width: 200px;
}

/* 双栏主工作区 */
.compare-dual-pane {
  display: grid;
  grid-template-columns: 280px 1fr;
  flex: 1 1 0%;
  min-height: 0;
  overflow: hidden;
  gap: 8px;
}

/* 左侧文件列表/树面板 */
.compare-file-list-pane {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  overflow: hidden;
  background: var(--pvf-surface-panel, #252528);
  border: 1px solid var(--pvf-border-subtle, rgba(255, 255, 255, 0.08));
  border-radius: 6px;
}

.pane-action-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 6px 8px;
  background: var(--pvf-surface-subtle, rgba(0, 0, 0, 0.05));
  border-bottom: 1px solid var(--pvf-border-subtle, rgba(255, 255, 255, 0.06));
  flex-shrink: 0;
  min-height: 32px;
}

.pane-action-left {
  display: flex;
  align-items: center;
}

.select-label {
  font-size: 11px;
  color: var(--pvf-text-secondary, #aaa);
}

.pane-action-right {
  display: flex;
  align-items: center;
  gap: 4px;
}

.icon-action-btn {
  background: transparent;
  border: none;
  color: var(--pvf-text-secondary, #aaa);
  padding: 3px 5px;
  border-radius: 3px;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.15s ease;
}

.icon-action-btn:hover {
  background: var(--pvf-surface-hover, rgba(255, 255, 255, 0.08));
  color: var(--pvf-text-primary, #fff);
}

.icon-action-btn--active {
  background: rgba(24, 160, 88, 0.18);
  color: var(--pvf-primary, #18a058);
}

.pane-empty-box {
  display: flex;
  align-items: center;
  justify-content: center;
  flex: 1;
  padding: 20px;
}

.file-items-scroll {
  flex: 1 1 0%;
  min-height: 0;
  overflow-y: auto;
  overflow-x: hidden;
}

/* 树节点渲染行 */
.tree-node-row {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 4px 6px;
  cursor: pointer;
  user-select: none;
  font-size: 12px;
  min-height: 24px;
  border-bottom: 1px solid var(--pvf-border-faint, rgba(255, 255, 255, 0.03));
}

.tree-node-row:hover {
  background: var(--pvf-surface-hover, rgba(255, 255, 255, 0.04));
}

.tree-node-row--active {
  background: rgba(24, 160, 88, 0.18) !important;
  color: var(--pvf-primary, #18a058);
}

.tree-node-row--dir {
  font-weight: 600;
  color: var(--pvf-text-secondary, #ccc);
}

.expand-icon-box {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 14px;
  height: 14px;
  cursor: pointer;
  color: var(--pvf-text-muted, #777);
}

.expand-icon-box:hover {
  color: var(--pvf-text-primary, #fff);
}

.expand-placeholder {
  width: 14px;
  height: 14px;
  display: inline-block;
  flex-shrink: 0;
}

.checkbox-box {
  display: inline-flex;
  align-items: center;
  flex-shrink: 0;
}

.node-thumb {
  flex-shrink: 0;
  border-radius: 2px;
}

.node-icon {
  flex-shrink: 0;
  color: var(--pvf-text-muted, #888);
}

.node-name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

.node-tags-inline {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  flex-shrink: 0;
}

.node-op-tag {
  flex-shrink: 0;
}

.merged-tag {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  font-size: 10px;
  color: #2ecc71;
  background: rgba(46, 204, 113, 0.15);
  padding: 1px 5px;
  border-radius: 3px;
  flex-shrink: 0;
}

.merged-tag--header {
  font-size: 11px;
  padding: 2px 8px;
}

/* 平铺列表渲染行 */
.file-item-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 5px 8px;
  cursor: pointer;
  border-bottom: 1px solid var(--pvf-border-faint, rgba(255, 255, 255, 0.03));
  gap: 6px;
  font-size: 12px;
}

.file-item-row:hover {
  background: var(--pvf-surface-hover, rgba(255, 255, 255, 0.04));
}

.file-item-row--active {
  background: rgba(24, 160, 88, 0.18) !important;
  color: var(--pvf-primary, #18a058);
}

.file-item-left {
  display: flex;
  align-items: center;
  gap: 6px;
  overflow: hidden;
  flex: 1;
}

.file-item-path {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

.file-item-right {
  flex-shrink: 0;
}

.size-diff-tag {
  font-size: 10px;
  color: var(--pvf-text-muted, #777);
  font-family: monospace;
}

/* 右侧 Diff 主区域 */
.compare-diff-pane {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  overflow: hidden;
  background: var(--pvf-surface-panel, #252528);
  border: 1px solid var(--pvf-border-subtle, rgba(255, 255, 255, 0.08));
  border-radius: 6px;
}

.diff-header-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 6px 12px;
  background: var(--pvf-surface-subtle, rgba(0, 0, 0, 0.05));
  border-bottom: 1px solid var(--pvf-border-subtle, rgba(255, 255, 255, 0.06));
  flex-shrink: 0;
  min-height: 34px;
}

.diff-header-left {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 1;
  min-width: 0;
}

.diff-file-path {
  font-size: 12px;
  font-weight: 600;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--pvf-text-primary, #fff);
}

.diff-header-center {
  display: flex;
  align-items: center;
  justify-content: center;
  flex: 1;
}

.diff-nav-box {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  background: var(--pvf-surface-subtle, rgba(0, 0, 0, 0.08));
  padding: 2px 6px;
  border-radius: 4px;
  border: 1px solid var(--pvf-border-subtle, rgba(255, 255, 255, 0.08));
}

.nav-diff-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: transparent;
  border: none;
  color: var(--pvf-text-secondary, #ccc);
  font-size: 11px;
  border-radius: 3px;
  cursor: pointer;
  transition: all 0.15s ease;
}

.nav-diff-btn--icon-only {
  width: 22px;
  height: 22px;
  padding: 0;
}

.nav-diff-btn:hover:not(:disabled) {
  background: rgba(255, 255, 255, 0.12);
  color: #fff;
}

.nav-diff-btn:disabled {
  opacity: 0.35;
  cursor: not-allowed;
}

.nav-diff-counter {
  font-size: 11px;
  font-family: monospace;
  font-weight: 600;
  color: #18a058;
  padding: 0 4px;
  user-select: none;
}

.diff-actions-row {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
}

.diff-viewport {
  flex: 1 1 0%;
  min-height: 0;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.diff-msg-box {
  display: flex;
  align-items: center;
  justify-content: center;
  flex: 1;
  padding: 40px;
}

.diff-binary-box {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  flex: 1;
  gap: 8px;
  padding: 40px;
  color: var(--pvf-text-secondary, #aaa);
}

.diff-binary-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--pvf-text-primary, #fff);
}

.diff-binary-desc {
  font-size: 12px;
  text-align: center;
}

.binary-sizes {
  margin-top: 6px;
  color: var(--pvf-text-muted, #888);
  font-family: monospace;
}

/* 双栏对齐容器 */
.side-by-side-wrapper {
  display: flex;
  flex-direction: column;
  flex: 1 1 0%;
  min-height: 0;
  overflow: hidden;
}

.side-diff-header {
  display: grid;
  grid-template-columns: 1fr 1fr;
  background: var(--pvf-surface-subtle, rgba(0, 0, 0, 0.08));
  border-bottom: 1px solid var(--pvf-border-subtle, rgba(255, 255, 255, 0.06));
  flex-shrink: 0;
  min-height: 28px;
  box-sizing: border-box;
}

.side-column-title {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 10px;
  font-size: 11px;
  overflow: hidden;
}

.side-column-title--left {
  border-right: 1px solid var(--pvf-border-subtle, rgba(255, 255, 255, 0.08));
}

.side-title-badge {
  font-size: 10px;
  font-weight: 600;
  padding: 1px 6px;
  border-radius: 3px;
  flex-shrink: 0;
}

.side-title-badge--local {
  background: rgba(33, 150, 243, 0.16);
  color: #2196f3;
}

.side-title-badge--target {
  background: rgba(156, 39, 176, 0.16);
  color: #ab47bc;
}

.side-title-path {
  font-family: monospace;
  color: var(--pvf-text-secondary, #aaa);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
}

.side-title-size {
  font-family: monospace;
  color: var(--pvf-text-muted, #777);
  flex-shrink: 0;
}

/* 本地修改保存与未保存指示 */
.save-status-group {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  margin-right: 8px;
}

.dirty-badge {
  font-size: 11px;
  color: #e6a23c;
  font-weight: 600;
  animation: pulse 1.8s infinite;
}

@keyframes pulse {
  0% { opacity: 0.6; }
  50% { opacity: 1; }
  100% { opacity: 0.6; }
}

.save-pill-btn {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  background: rgba(38, 166, 91, 0.25);
  border: 1px solid rgba(38, 166, 91, 0.5);
  color: #2ecc71;
  font-size: 11px;
  padding: 1px 7px;
  border-radius: 3px;
  cursor: pointer;
  transition: all 0.15s ease;
}

.save-pill-btn:hover:not(:disabled) {
  background: #2ecc71;
  color: #fff;
}

/* Live 双栏 MergeView 宿主容器：100% 高度铺满，交给内部 .cm-mergeView 滚动 */
.live-merge-editor-host {
  flex: 1 1 0%;
  min-height: 0;
  min-width: 0;
  width: 100%;
  height: 100%;
  position: relative;
  overflow: hidden;
  background: var(--pvf-surface-code, #18181c);
}

/* 穿透样式：定制 CodeMirror MergeView 主容器 */
:deep(.cm-mergeView) {
  height: 100% !important;
  max-height: 100% !important;
  width: 100%;
  overflow-y: auto !important;
  overflow-x: auto !important;
  box-sizing: border-box;
}

:deep(.cm-mergeViewEditors) {
  min-height: 100%;
  display: flex;
  align-items: stretch;
}

:deep(.cm-mergeViewEditor) {
  flex: 1 1 0%;
  min-width: 0;
  overflow: hidden;
}

:deep(.cm-mergeViewEditor:first-child) {
  border-right: 1px solid var(--pvf-border-subtle, rgba(255, 255, 255, 0.08));
}

/* 内部 editor 保持自然高度，由外层 .cm-mergeView 滚动并由 CodeMirror 进行视口裁剪 */
:deep(.cm-editor) {
  box-sizing: border-box;
}

:deep(.cm-scroller) {
  font-family: 'SF Mono', Menlo, Consolas, 'Courier New', monospace;
  line-height: 1.55;
  user-select: text;
}

/* 中间合入按钮控制栏：position 必须为 relative，供子 button 进行 absolute 物理定位 */
:deep(.cm-merge-revert) {
  width: 28px !important;
  min-width: 28px !important;
  position: relative !important;
  flex-grow: 0 !important;
  flex-shrink: 0 !important;
  background: var(--pvf-surface-subtle, rgba(0, 0, 0, 0.08)) !important;
  border-left: 1px solid var(--pvf-border-subtle, rgba(255, 255, 255, 0.08)) !important;
  border-right: 1px solid var(--pvf-border-subtle, rgba(255, 255, 255, 0.08)) !important;
  user-select: none;
}

:deep(.cm-merge-revert button),
:deep(.cm-revert-action-btn) {
  position: absolute !important;
  left: 3px !important;
  width: 22px !important;
  height: 20px !important;
  line-height: 18px !important;
  background: rgba(38, 166, 91, 0.28) !important;
  border: 1px solid rgba(38, 166, 91, 0.6) !important;
  color: #2ecc71 !important;
  border-radius: 4px !important;
  font-size: 13px !important;
  font-weight: 700 !important;
  cursor: pointer !important;
  display: flex !important;
  align-items: center !important;
  justify-content: center !important;
  padding: 0 !important;
  transition: all 0.15s ease !important;
  z-index: 10 !important;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.25) !important;
}

:deep(.cm-merge-revert button:hover),
:deep(.cm-revert-action-btn:hover) {
  background: #2ecc71 !important;
  color: #ffffff !important;
  transform: scale(1.15) !important;
  box-shadow: 0 0 8px rgba(46, 204, 113, 0.7) !important;
}

/* 变更差异高亮样式：CodeMirror 6 标准类名 */
:deep(.cm-merge-a .cm-changedLine) {
  background-color: rgba(230, 162, 60, 0.16) !important;
}

:deep(.cm-merge-a .cm-changedText) {
  background-color: rgba(230, 162, 60, 0.42) !important;
  color: #ffffff !important;
  border-radius: 2px;
  padding: 0 1px;
  font-weight: 600;
}

:deep(.cm-merge-a .cm-changedLineGutter) {
  background-color: #e6a23c !important;
  width: 4px !important;
}

:deep(.cm-merge-b .cm-changedLine) {
  background-color: rgba(38, 166, 91, 0.16) !important;
}

:deep(.cm-merge-b .cm-changedText) {
  background-color: rgba(38, 166, 91, 0.42) !important;
  color: #ffffff !important;
  border-radius: 2px;
  padding: 0 1px;
  font-weight: 600;
}

:deep(.cm-merge-b .cm-changedLineGutter) {
  background-color: #2ecc71 !important;
  width: 4px !important;
}

:deep(.cm-changeGutter) {
  width: 4px !important;
  min-width: 4px !important;
}

/* 对齐 Spacer 斜纹占位层 */
:deep(.cm-merge-spacer) {
  background: repeating-linear-gradient(
    -45deg,
    rgba(0, 0, 0, 0.16),
    rgba(0, 0, 0, 0.16) 6px,
    rgba(255, 255, 255, 0.03) 6px,
    rgba(255, 255, 255, 0.03) 12px
  ) !important;
  border-top: 1px dashed rgba(255, 255, 255, 0.08) !important;
  border-bottom: 1px dashed rgba(255, 255, 255, 0.08) !important;
}

/* 滚动条美化 */
:deep(.cm-mergeView::-webkit-scrollbar),
.file-items-scroll::-webkit-scrollbar {
  width: 10px;
  height: 10px;
}

:deep(.cm-mergeView::-webkit-scrollbar-track),
.file-items-scroll::-webkit-scrollbar-track {
  background: rgba(0, 0, 0, 0.16);
  border-radius: 4px;
}

:deep(.cm-mergeView::-webkit-scrollbar-thumb),
.file-items-scroll::-webkit-scrollbar-thumb {
  background: rgba(255, 255, 255, 0.32);
  border-radius: 5px;
  border: 2px solid transparent;
  background-clip: padding-box;
}

:deep(.cm-mergeView::-webkit-scrollbar-thumb:hover),
.file-items-scroll::-webkit-scrollbar-thumb:hover {
  background: var(--pvf-primary, #18a058);
}
</style>
