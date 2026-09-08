<script setup lang="ts">
import { computed, ref, watch } from "vue";
import {
  NButton,
  NDropdown,
  NEmpty,
  NIcon,
  NInput,
  NSpin,
  NTag,
  NTooltip,
  useMessage,
} from "naive-ui";
import { Target20Regular } from "@vicons/fluent";
import { ArchiveService, EditorService } from "../../bindings/pvfine/services";
import type { TreeNode } from "../../bindings/pvfine/services/models";
import { useArchiveStore } from "../stores/archive";
import { useExplorerStore, type SearchItem, type TreeItem } from "../stores/explorer";
import { useEditorStore } from "../stores/editor";
import { useFileSetStore, type FileSetEntry } from "../stores/fileSets";
import { useBatchStore } from "../stores/batch";
import { useSettingsStore } from "../stores/settings";
import FileTree from "./FileTree.vue";

const archive = useArchiveStore();
const explorer = useExplorerStore();
const editor = useEditorStore();
const fileSets = useFileSetStore();
const batch = useBatchStore();
const settings = useSettingsStore();
const message = useMessage();

const searchInput = ref("");
const adding = ref(false);
const exporting = ref(false);
const copying = ref(false);
const batching = ref(false);
const contextMenu = ref({
  show: false,
  x: 0,
  y: 0,
  items: [] as TreeItem[],
});
const searchTreeItems = computed(() => buildSearchTree(explorer.hits));
const visibleTreeItems = computed(() =>
  explorer.mode === "search" ? searchTreeItems.value : explorer.roots
);
const treeKey = computed(() => `${explorer.mode}:${explorer.query}`);
const contextMenuOptions = computed(() => [
  {
    label: "导出文件",
    key: "export",
    disabled:
      adding.value ||
      exporting.value ||
      copying.value ||
      batching.value ||
      !archive.open ||
      contextMenu.value.items.length === 0,
  },
  {
    label: "复制文件路径",
    key: "copy-paths",
    disabled:
      adding.value ||
      exporting.value ||
      copying.value ||
      batching.value ||
      !archive.open ||
      contextMenu.value.items.length === 0,
  },
  {
    label: `加入“${fileSets.activeSet?.name ?? "当前文件集"}”`,
    key: "add",
    disabled:
      adding.value ||
      exporting.value ||
      copying.value ||
      batching.value ||
      !archive.open ||
      contextMenu.value.items.length === 0,
  },
  {
    label: "批量处理…",
    key: "batch",
    disabled:
      adding.value ||
      exporting.value ||
      copying.value ||
      batching.value ||
      !archive.open ||
      contextMenu.value.items.length === 0,
  },
]);

watch(
  () => archive.info?.path ?? "",
  async (archivePath) => {
    if (archivePath) {
      explorer.reset();
      searchInput.value = "";
      await explorer.loadRoots();
    } else {
      explorer.reset();
    }
  }
);

function submitSearch(event: KeyboardEvent): void {
  if (event.isComposing || !archive.indexReady) return;
  void explorer.search(searchInput.value);
}

function clearSearch(): void {
  searchInput.value = "";
  explorer.clearSearch();
}

function toggleExactMatch(): void {
  void explorer.setExactMatch(!explorer.exactMatch);
}

async function onTreeLoad(item: TreeItem): Promise<void> {
  await explorer.loadChildren(item);
}

function onTreeOpen(item: TreeItem): void {
  if (item && !item.isDir) {
    explorer.selectedKey = item.key;
    void editor.openFile(item.fileIndex);
  }
}

function hideContextMenu(): void {
  contextMenu.value.show = false;
  contextMenu.value.items = [];
}

function onTreeContextMenu(
  event: MouseEvent,
  item: TreeItem | null,
  items: TreeItem[]
): void {
  if (!item || items.length === 0) {
    hideContextMenu();
    return;
  }
  contextMenu.value = {
    show: true,
    x: event.clientX,
    y: event.clientY,
    items,
  };
}

function serviceEntry(node: TreeNode): FileSetEntry {
  const names = [
    ...new Set((node.tags ?? []).map((tag) => tag.name.trim()).filter(Boolean)),
  ];
  return {
    fileIndex: node.fileIndex,
    path: node.path,
    name: names.join(" / ") || node.name || node.path.slice(node.path.lastIndexOf("/") + 1),
    ids: [...new Set((node.tags ?? []).map((tag) => tag.id).filter(Boolean))],
    size: node.size,
    dataType: node.dataType,
  };
}

function appendSearchPaths(item: TreeItem, paths: string[], seen: Set<string>): void {
  if (!item.isDir) {
    if (item.fileIndex >= 0 && !seen.has(item.key)) {
      seen.add(item.key);
      paths.push(item.key);
    }
    return;
  }
  for (const child of item.children ?? []) appendSearchPaths(child, paths, seen);
}

async function collectFilePaths(items: TreeItem[]): Promise<string[]> {
  const paths: string[] = [];
  const seen = new Set<string>();
  const appendPath = (path: string) => {
    if (!seen.has(path)) {
      seen.add(path);
      paths.push(path);
    }
  };
  const directoryRequests = new Map<string, Promise<TreeNode[]>>();

  for (const item of items) {
    if (!item.isDir) {
      if (item.fileIndex >= 0) appendPath(item.key);
      continue;
    }
    if (explorer.mode === "search") {
      appendSearchPaths(item, paths, seen);
      continue;
    }
    let request = directoryRequests.get(item.key);
    if (!request) {
      request = ArchiveService.ListDescendantFiles(item.key).then(
        (nodes) =>
          (nodes ?? []).filter(
            (node): node is TreeNode => !!node && !node.isDir && node.fileIndex >= 0
          )
      );
      directoryRequests.set(item.key, request);
    }
    for (const node of await request) appendPath(node.path);
  }
  return paths;
}

async function collectExportScopes(items: TreeItem[]): Promise<string[]> {
  if (explorer.mode === "search") return collectFilePaths(items);
  return [...new Set(items.map((item) => item.key).filter(Boolean))];
}

async function collectFiles(items: TreeItem[]): Promise<FileSetEntry[]> {
  const paths = await collectFilePaths(items);
  const nodes = await ArchiveService.ResolveFiles(paths);
  return (nodes ?? [])
    .filter((node): node is TreeNode => !!node && !node.isDir && node.fileIndex >= 0)
    .map(serviceEntry);
}

async function writeClipboardText(text: string): Promise<void> {
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return;
    } catch {
      // 某些桌面 WebView 不允许直接访问 Clipboard API，继续使用兼容方案。
    }
  }

  const textarea = document.createElement("textarea");
  textarea.value = text;
  textarea.setAttribute("readonly", "");
  textarea.style.position = "fixed";
  textarea.style.opacity = "0";
  document.body.appendChild(textarea);
  textarea.focus();
  textarea.select();
  const copied = document.execCommand("copy");
  textarea.remove();
  if (!copied) throw new Error("系统剪贴板不可用");
}

async function onCopyPaths(items: TreeItem[]): Promise<void> {
  if (copying.value) return;
  const archivePath = archive.info?.path ?? "";
  const session = fileSets.sessionId;
  hideContextMenu();
  copying.value = true;
  try {
    const paths = await collectFilePaths(items);
    if (session !== fileSets.sessionId || archive.info?.path !== archivePath) return;
    if (paths.length === 0) {
      message.info("选中的目录中没有文件");
      return;
    }
    await writeClipboardText(paths.join("\n"));
    message.success(`已复制 ${paths.length} 个文件路径`);
  } catch (error: any) {
    if (session === fileSets.sessionId && archive.info?.path === archivePath) {
      message.error(`复制文件路径失败: ${error?.message ?? error}`);
    }
  } finally {
    copying.value = false;
  }
}

async function onContextMenuSelect(key: string | number): Promise<void> {
  if (key === "export") {
    await onExportSelected(contextMenu.value.items);
    return;
  }
  if (key === "copy-paths") {
    await onCopyPaths(contextMenu.value.items);
    return;
  }
  if (key === "batch") {
    await onBatchSelected(contextMenu.value.items);
    return;
  }
  if (key !== "add" || adding.value) return;
  const selectedItems = contextMenu.value.items;
  const archivePath = archive.info?.path ?? "";
  const session = fileSets.sessionId;
  hideContextMenu();
  adding.value = true;
  try {
    const entries = await collectFiles(selectedItems);
    if (session !== fileSets.sessionId || archive.info?.path !== archivePath) return;
    const result = fileSets.addEntries(entries);
    if (result.added === 0 && result.skipped === 0) {
      message.info("选中的目录中没有文件");
      return;
    }
    const duplicateText = result.skipped > 0 ? `，跳过 ${result.skipped} 个重复项` : "";
    message.success(`已加入 ${result.added} 个文件${duplicateText}`);
  } catch (error: any) {
    if (session === fileSets.sessionId && archive.info?.path === archivePath) {
      message.error(`加入文件集失败: ${error?.message ?? error}`);
    }
  } finally {
    adding.value = false;
  }
}

async function onBatchSelected(items: TreeItem[]): Promise<void> {
  if (batching.value) return;
  const selectedItems = [...items];
  const archivePath = archive.info?.path ?? "";
  const session = fileSets.sessionId;
  hideContextMenu();
  batching.value = true;
  try {
    await editor.flushPending();
    const paths = await collectFilePaths(selectedItems);
    if (session !== fileSets.sessionId || archive.info?.path !== archivePath) return;
    if (paths.length === 0) {
      message.info("选中的目录中没有文件");
      return;
    }
    batch.open(paths, `资源管理器选择（${paths.length} 个文件）`);
  } catch (error: any) {
    if (session === fileSets.sessionId && archive.info?.path === archivePath) {
      message.error(`打开批处理失败: ${error?.message ?? error}`);
    }
  } finally {
    batching.value = false;
  }
}

async function onExportSelected(items: TreeItem[]): Promise<void> {
  if (exporting.value) return;
  const archivePath = archive.info?.path ?? "";
  const session = fileSets.sessionId;
  hideContextMenu();
  exporting.value = true;
  try {
    await editor.flushPending();
    const scopes = await collectExportScopes(items);
    if (session !== fileSets.sessionId || archive.info?.path !== archivePath) return;
    if (scopes.length === 0) {
      message.info("选中的目录中没有文件");
      return;
    }
    const path = await EditorService.ExportFilesDialog(scopes);
    if (path) message.success(`已导出选中文件到 ${path}`);
  } catch (error: any) {
    if (session === fileSets.sessionId && archive.info?.path === archivePath) {
      if (!isCancel(error)) message.error(`导出失败: ${error?.message ?? error}`);
    }
  } finally {
    exporting.value = false;
  }
}

function isCancel(error: any): boolean {
  return String(error?.message ?? error).toLowerCase().includes("cancel");
}

function buildSearchTree(items: SearchItem[]): TreeItem[] {
  const roots: TreeItem[] = [];
  const nodesByPath = new Map<string, TreeItem>();

  for (const item of items) {
    const parts = item.path
      .replaceAll("\\", "/")
      .split("/")
      .filter(Boolean);
    if (parts.length === 0) continue;

    let children = roots;
    let parentPath = "";
    for (const part of parts.slice(0, -1)) {
      const path = parentPath ? `${parentPath}/${part}` : part;
      let directory = nodesByPath.get(path);
      if (!directory) {
        directory = {
          key: path,
          label: part,
          isDir: true,
          isLeaf: false,
          children: [],
          fileIndex: -1,
          size: 0,
          dataType: 0,
          childCount: 0,
          tags: [],
          annotations: item.pathAnnotations[path] ?? [],
        };
        nodesByPath.set(path, directory);
        children.push(directory);
      }
      children = directory.children ?? [];
      parentPath = path;
    }

    const filePath = parts.join("/");
    let file = nodesByPath.get(filePath);
    if (!file) {
      file = {
        key: filePath,
        label: parts[parts.length - 1],
        isDir: false,
        isLeaf: true,
        children: [],
        fileIndex: item.fileIndex,
        size: item.size,
        dataType: item.dataType,
        childCount: 0,
        tags: [],
        annotations: item.annotations,
      };
      nodesByPath.set(filePath, file);
      children.push(file);
    }

    if (file.annotations.length === 0 && item.annotations.length > 0) {
      file.annotations = item.annotations;
    }

    if (item.category !== "file") {
      file.tags.push({
        id: item.id,
        name: item.label,
        category: item.category,
      });
    }
  }

  sortTree(roots);
  return roots;
}

function sortTree(items: TreeItem[]): void {
  items.sort((a, b) => {
    if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
    return a.label < b.label ? -1 : a.label > b.label ? 1 : 0;
  });
  for (const item of items) {
    if (item.children) sortTree(item.children);
  }
}
</script>

<template>
  <div class="explorer">
    <div class="exp-search">
      <NInput
        v-model:value="searchInput"
        :placeholder="archive.indexReady ? '搜索路径、名称或 id…' : '索引完成后可搜索路径、名称或 id…'"
        clearable
        size="small"
        :disabled="!archive.open"
        :readonly="!archive.indexReady"
        @clear="clearSearch"
        @keydown.enter.prevent="submitSearch"
      >
        <template #suffix>
          <NTooltip trigger="hover">
            <template #trigger>
              <NButton
                text
                circle
                size="tiny"
                :type="explorer.exactMatch ? 'primary' : 'default'"
                class="exact-toggle"
                :class="{ 'exact-toggle--active': explorer.exactMatch }"
                :disabled="!archive.open"
                aria-label="切换精确匹配"
                :aria-pressed="explorer.exactMatch"
                @click.stop="toggleExactMatch"
              >
                <template #icon><NIcon><Target20Regular /></NIcon></template>
              </NButton>
            </template>
            {{ explorer.exactMatch ? "关闭精确匹配" : "启用精确匹配" }}
          </NTooltip>
        </template>
      </NInput>
    </div>

    <NSpin
      class="exp-spin"
      :show="archive.loading || explorer.searching || adding || exporting || copying || batching"
    >
      <div class="exp-body">
        <!-- 空态 -->
        <NEmpty
          v-if="!archive.open"
          description="未打开归档"
          class="exp-empty"
          size="small"
        />

        <template v-else>
          <div v-if="explorer.mode === 'search'" class="search-meta">
            <NTag size="tiny" :bordered="false">命中 {{ explorer.hits.length.toLocaleString() }} 条</NTag>
            <NButton quaternary size="tiny" @click="clearSearch">返回目录</NButton>
          </div>
          <NEmpty
            v-if="explorer.mode === 'search' && !explorer.searching && !searchTreeItems.length"
            description="无匹配结果"
            size="small"
            class="exp-empty"
          />
          <div v-else class="tree-viewport">
            <FileTree
              :key="treeKey"
              :items="visibleTreeItems"
              :expand-all="explorer.mode === 'search'"
              :open-mode="settings.explorerOpenMode"
              :selected-key="explorer.selectedKey"
              :load-children="onTreeLoad"
              @open="onTreeOpen"
              @contextmenu="onTreeContextMenu"
            />
          </div>
          <div v-if="explorer.mode === 'search' && explorer.nextCursor >= 0" class="load-more">
            <NButton size="tiny" quaternary :loading="explorer.searching" @click="explorer.loadMore()">
              加载更多
            </NButton>
          </div>
        </template>
      </div>
    </NSpin>
    <NDropdown
      trigger="manual"
      placement="bottom-start"
      :show="contextMenu.show"
      :x="contextMenu.x"
      :y="contextMenu.y"
      :options="contextMenuOptions"
      @select="onContextMenuSelect"
      @clickoutside="hideContextMenu"
    />
  </div>
</template>

<style scoped>
.exp-spin,
.exp-spin :deep(.n-spin-container),
.exp-spin :deep(.n-spin-content) {
  flex: 1;
  height: 100%;
  min-width: 0;
  min-height: 0;
}
.exp-spin {
  display: flex;
}
.exp-spin :deep(.n-spin-container) {
  display: flex;
  flex-direction: column;
}
.exp-spin :deep(.n-spin-content) {
  display: flex;
  flex-direction: column;
}
.explorer {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  border-right: 1px solid rgba(128, 128, 128, 0.2);
}
.exp-search {
  padding: 8px;
  flex-shrink: 0;
}
.exact-toggle {
  color: rgba(128, 128, 128, 0.85);
}
.exact-toggle--active {
  color: #6ba0ff;
  background: rgba(79, 140, 255, 0.15);
}
.index-status {
  display: flex;
  align-items: center;
  gap: 6px;
  min-height: 18px;
  margin-top: 4px;
  color: rgba(128, 128, 128, 0.9);
  font-size: 11px;
  line-height: 18px;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.index-status--error {
  color: #e88080;
}
.exp-body {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.tree-viewport {
  flex: 1;
  min-width: 0;
  min-height: 0;
  display: flex;
  overflow: auto;
}
.exp-empty {
  margin-top: 80px;
}
.search-meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 4px 8px;
}
.load-more {
  display: flex;
  justify-content: center;
  padding: 6px 0 10px;
}
</style>
