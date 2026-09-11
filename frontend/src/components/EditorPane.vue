<script setup lang="ts">
import { computed, reactive, ref, watch } from "vue";
import { Clipboard } from "@wailsio/runtime";
import {
  NButton,
  NDropdown,
  NEmpty,
  NIcon,
  NSpin,
  NTag,
  NTabPane,
  NTabs,
  NTooltip,
  useDialog,
  useMessage,
} from "naive-ui";
import {
  BookmarkAdd24Regular,
  Dismiss16Regular,
  DocumentSearch24Regular,
  Eye24Regular,
  EyeOff24Regular,
  SplitHorizontal24Regular,
  SplitVertical24Regular,
} from "@vicons/fluent";
import {
  useEditorStore,
  type DraggedEditorTab,
  type EditorPaneId,
  type EditorTab,
} from "../stores/editor";
import { useArchiveStore } from "../stores/archive";
import { useExplorerStore } from "../stores/explorer";
import { useBookmarkStore } from "../stores/bookmarks";
import CodeEditor from "./CodeEditor.vue";
import { useSettingsStore } from "../stores/settings";
import ImageThumbnail from "./ImageThumbnail.vue";
import PreviewHost from "./previews/PreviewHost.vue";
import { getPreviewProvider } from "../previews/registry";
import type { PreviewFile } from "../previews/types";
import type { ResolvedThemeId } from "../theme";

const props = defineProps<{
  paneId: EditorPaneId;
  themeId: ResolvedThemeId;
}>();

const paneId = props.paneId;
const editor = useEditorStore();
const archive = useArchiveStore();
const explorer = useExplorerStore();
const bookmarks = useBookmarkStore();
const settings = useSettingsStore();
const message = useMessage();
const dialog = useDialog();
const host = ref<HTMLDivElement | null>(null);
const draggingIndex = ref<number | null>(null);
const revealingFile = ref(false);
const bookmarking = ref(false);
const dragOver = ref(false);
const dragOverEdge = ref<DropEdge | null>(null);
const previewVisibility = reactive(new Map<number, boolean>());
const tabContextMenu = ref({
  show: false,
  x: 0,
  y: 0,
  paneId: null as EditorPaneId | null,
  index: null as number | null,
});

type DropEdge = "left" | "right" | "top" | "bottom";
const dragMime = "application/x-pvfine-editor-tab";

const pane = computed(() => editor.panes.find((item) => item.id === paneId));
const paneTabs = computed(() => {
  const indexes = pane.value?.tabIndexes ?? [];
  return indexes
    .map((index) => editor.tabs.find((tab) => tab.index === index))
    .filter((tab): tab is EditorTab => !!tab);
});
const activeKeyStr = computed(() => {
  const key = pane.value?.activeKey;
  return key === null || key === undefined ? undefined : String(key);
});
const activeTab = computed(() => {
  const activeKey = pane.value?.activeKey;
  return paneTabs.value.find((tab) => tab.index === activeKey) ?? null;
});
const fileTags = computed(() => {
  const ids = new Set<string>();
  const names = new Set<string>();
  const result: Array<{ kind: "id" | "name"; value: string }> = [];
  for (const tag of activeTab.value?.tags ?? []) {
    const id = tag.id.trim();
    if (id && !ids.has(id)) {
      ids.add(id);
      result.push({ kind: "id", value: id });
    }
    const name = tag.name.trim();
    if (name && !names.has(name)) {
      names.add(name);
      result.push({ kind: "name", value: name });
    }
  }
  return result;
});
const canRevealActiveFile = computed(
  () => archive.open && !!activeTab.value && !revealingFile.value
);
const activeBookmarked = computed(
  () => !!activeTab.value && bookmarks.isBookmarkedInGroup(activeTab.value.path)
);
const canBookmarkActiveFile = computed(
  () => archive.open && bookmarks.loaded && !!activeTab.value && !bookmarking.value
);
function previewFile(tab: EditorTab): PreviewFile {
  return { index: tab.index, path: tab.path, text: tab.text, editable: tab.editable };
}

function previewProviderFor(tab: EditorTab) {
  return getPreviewProvider(previewFile(tab));
}

function isPreviewOpen(index: number): boolean {
  return previewVisibility.get(index) ?? true;
}

function togglePreview(index: number): void {
  previewVisibility.set(index, !isPreviewOpen(index));
}

function closePreview(index: number): void {
  previewVisibility.set(index, false);
}

watch(
  activeTab,
  (tab) => {
    if (tab && previewProviderFor(tab) && !previewVisibility.has(tab.index)) {
      previewVisibility.set(tab.index, true);
    }
  },
  { immediate: true },
);

function activatePane(): void {
  editor.activatePane(paneId);
}

function onActive(key: string | number): void {
  editor.activateTab(paneId, Number(key));
}

function onClose(index: number): void {
  editor.closeTab(index, paneId);
}

const tabContextMenuOptions = computed(() => [
  {
    label: "关闭当前",
    key: "close",
    disabled: tabContextMenu.value.index === null,
  },
  {
    label: "关闭所有",
    key: "close-all",
    disabled: editor.tabs.length === 0,
  },
  {
    label: "关闭其它",
    key: "close-others",
    disabled: editor.tabs.length <= 1 || tabContextMenu.value.index === null,
  },
]);

function hideTabContextMenu(): void {
  tabContextMenu.value.show = false;
  tabContextMenu.value.paneId = null;
  tabContextMenu.value.index = null;
}

function onTabContextMenu(event: MouseEvent, index: number): void {
  event.preventDefault();
  editor.activateTab(paneId, index);
  tabContextMenu.value = {
    show: true,
    x: event.clientX,
    y: event.clientY,
    paneId,
    index,
  };
}

function onTabContextMenuSelect(key: string | number): void {
  const index = tabContextMenu.value.index;
  const targetPaneId = tabContextMenu.value.paneId;
  hideTabContextMenu();
  if (index === null) return;

  if (key === "close") {
    editor.closeTab(index, targetPaneId ?? paneId);
  } else if (key === "close-all") {
    editor.closeAllTabs();
  } else if (key === "close-others") {
    editor.closeOtherTabs(index);
  }
}

async function onRevealActiveFile(): Promise<void> {
  if (revealingFile.value) return;
  const tab = activeTab.value;
  if (!archive.open || !tab) return;

  revealingFile.value = true;
  try {
    if (explorer.mode === "search") explorer.clearSearch();
    const found = await explorer.revealPath(tab.path);
    if (!found) message.info("当前文件未在资源管理器中找到");
  } catch (error: any) {
    message.error(`定位文件失败: ${error?.message ?? error}`);
  } finally {
    revealingFile.value = false;
  }
}

function confirmBuiltinBookmarkCopy(): Promise<boolean> {
  const active = bookmarks.activeBook;
  if (!active || active.editable) return Promise.resolve(true);
  return new Promise((resolve) => {
    let settled = false;
    const finish = (value: boolean) => {
      if (settled) return;
      settled = true;
      resolve(value);
    };
    dialog.warning({
      title: "内置书签簿不可编辑",
      content: `将复制“${active.name}”为新的可编辑书签簿，并把当前文件加入副本。继续吗？`,
      positiveText: "复制并加入",
      negativeText: "取消",
      onPositiveClick: () => finish(!!bookmarks.copyBook(active.id)),
      onNegativeClick: () => finish(false),
      onClose: () => finish(false),
    });
  });
}

async function onBookmarkActive(): Promise<void> {
  const tab = activeTab.value;
  if (!tab || !canBookmarkActiveFile.value || activeBookmarked.value) return;
  bookmarking.value = true;
  try {
    if (!(await confirmBuiltinBookmarkCopy())) return;
    const result = bookmarks.addEntries([
      { path: tab.path, name: tab.title, fileIndex: tab.index },
    ]);
    if (result.added > 0) message.success(`已加入书签：${tab.path}`);
    else message.info("当前文件已在当前书签分组中");
  } catch (error: any) {
    message.error(`加入书签失败: ${error?.message ?? error}`);
  } finally {
    bookmarking.value = false;
  }
}

async function copyTag(value: string): Promise<void> {
  try {
    // Wails 桌面端使用原生剪贴板,不受 WebView 的 Clipboard 权限限制。
    await Clipboard.SetText(value);
    message.success(`已复制 ${value}`);
  } catch {
    try {
      // 浏览器开发模式没有 Wails runtime 时使用 Web Clipboard API。
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(value);
      } else {
        const textarea = document.createElement("textarea");
        textarea.value = value;
        textarea.setAttribute("readonly", "");
        textarea.style.position = "fixed";
        textarea.style.opacity = "0";
        document.body.appendChild(textarea);
        textarea.focus();
        textarea.select();
        const copied = document.execCommand("copy");
        textarea.remove();
        if (!copied) throw new Error("clipboard unavailable");
      }
      message.success(`已复制 ${value}`);
    } catch {
      message.error("复制失败,请重试");
    }
  }
}

function sizeText(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}

function isOpeningTab(index: number): boolean {
  return editor.openingPaneId === paneId && pane.value?.activeKey === index;
}

function onDragStart(event: DragEvent, index: number): void {
  draggingIndex.value = index;
  editor.beginTabDrag(paneId, index);
  if (!event.dataTransfer) return;
  event.dataTransfer.effectAllowed = "move";
  event.dataTransfer.setData(dragMime, JSON.stringify({ paneId, index }));
}

function onDragEnd(): void {
  draggingIndex.value = null;
  editor.endTabDrag();
  dragOver.value = false;
  dragOverEdge.value = null;
}

function readDraggedTab(event: DragEvent): DraggedEditorTab | null {
  if (editor.draggingTab) return editor.draggingTab;
  const raw = event.dataTransfer?.getData(dragMime);
  if (!raw) return null;
  try {
    const value = JSON.parse(raw) as { paneId?: unknown; index?: unknown };
    if (typeof value.paneId !== "string" || typeof value.index !== "number") return null;
    return { paneId: value.paneId, index: value.index };
  } catch {
    return null;
  }
}

function getDropEdge(event: DragEvent): DropEdge | null {
  if (editor.isSplit) return null;
  if (!host.value) return null;
  const rect = host.value.getBoundingClientRect();
  if (rect.width <= 0 || rect.height <= 0) return null;
  const edgeSize = Math.min(80, Math.max(36, Math.round(Math.min(rect.width, rect.height) * 0.14)));
  const x = event.clientX - rect.left;
  const y = event.clientY - rect.top;
  if (x <= edgeSize) return "left";
  if (x >= rect.width - edgeSize) return "right";
  if (y <= edgeSize) return "top";
  if (y >= rect.height - edgeSize) return "bottom";
  return null;
}

function onDragOver(event: DragEvent): void {
  const dragged = readDraggedTab(event);
  if (!dragged) return;
  dragOver.value = true;
  dragOverEdge.value = getDropEdge(event);
  if (event.dataTransfer) event.dataTransfer.dropEffect = "move";
  editor.activatePane(paneId);
}

function onDragLeave(event: DragEvent): void {
  const related = event.relatedTarget as Node | null;
  if (related && host.value?.contains(related)) return;
  dragOver.value = false;
  dragOverEdge.value = null;
}

function onDrop(event: DragEvent): void {
  const dragged = readDraggedTab(event);
  const edge = dragOverEdge.value ?? getDropEdge(event);
  dragOver.value = false;
  dragOverEdge.value = null;
  editor.endTabDrag();
  if (!dragged) return;

  if (edge) {
    const orientation = edge === "left" || edge === "right" ? "columns" : "rows";
    const insertBefore = edge === "left" || edge === "top";
    editor.splitAndMoveTab(dragged.index, dragged.paneId, paneId, orientation, insertBefore);
    return;
  }
  editor.moveTab(dragged.index, dragged.paneId, paneId);
}
</script>

<template>
  <div
    ref="host"
    class="editor-pane-view"
    :class="{
      'editor-pane-view--active': editor.activePaneId === paneId,
      'editor-pane-view--drop-target': dragOver,
    }"
    @pointerdown="activatePane"
    @focusin="activatePane"
    @dragover.capture.prevent="onDragOver"
    @dragleave="onDragLeave"
    @drop.capture.prevent="onDrop"
  >
    <div
      v-if="dragOverEdge"
      class="drop-preview"
      :class="`drop-preview--${dragOverEdge}`"
      aria-hidden="true"
    >
      <div class="drop-preview-pane" />
      <div class="drop-preview-divider" />
    </div>
    <div v-if="paneTabs.length === 0" class="pane-empty">
      <NSpin v-if="editor.openingPaneId === paneId" />
      <NEmpty v-else size="small" description="从左侧选择一个文件" />
    </div>

    <NTabs
      v-else
      type="card"
      size="small"
      :value="activeKeyStr"
      class="editor-tabs"
      style="height: 100%"
      @update:value="onActive"
    >
      <NTabPane
        v-for="tab in paneTabs"
        :key="`${paneId}:${tab.index}`"
        :name="String(tab.index)"
        display-directive="show:lazy"
        style="height: 100%"
      >
        <template #tab>
          <span
            class="tab-label"
            :class="{ 'tab-label--dragging': draggingIndex === tab.index }"
            :title="tab.path"
            draggable="true"
            @dragstart="onDragStart($event, tab.index)"
            @dragend="onDragEnd"
            @contextmenu.stop="onTabContextMenu($event, tab.index)"
          >
            <ImageThumbnail v-if="tab.icon" :reference="tab.icon" :size="16" />
            <span :class="['tab-dot', { dirty: tab.text !== tab.original }]" />
            <span class="tab-title">{{ tab.title }}</span>
            <NTooltip>
              <template #trigger>
                <NButton
                  quaternary
                  size="tiny"
                  class="tab-close"
                  @click.stop="onClose(tab.index)"
                >
                  <template #icon><NIcon :size="12"><Dismiss16Regular /></NIcon></template>
                </NButton>
              </template>
              关闭 (Cmd+W)
            </NTooltip>
          </span>
        </template>

        <div class="editor-info-bar" role="toolbar" aria-label="当前文件操作">
          <div class="editor-file-tags" aria-label="当前文件关联信息">
            <NTag
              v-for="tag in fileTags"
              :key="`${tag.kind}:${tag.value}`"
              size="tiny"
              :bordered="false"
              :type="tag.kind === 'id' ? 'info' : 'success'"
              class="editor-file-tag"
              role="button"
              tabindex="0"
              :title="`点击复制${tag.kind === 'id' ? 'id' : 'name'}: ${tag.value}`"
              @click="copyTag(tag.value)"
              @keydown.enter.prevent="copyTag(tag.value)"
              @keydown.space.prevent="copyTag(tag.value)"
            >
              {{ tag.value }}
            </NTag>
          </div>

          <div class="editor-pane-actions" role="group" aria-label="编辑器操作">
            <NTooltip trigger="hover">
              <template #trigger>
                <NButton
                  quaternary
                  size="tiny"
                  :type="activeBookmarked ? 'primary' : 'default'"
                  :loading="bookmarking"
                  :disabled="!canBookmarkActiveFile || activeBookmarked"
                  aria-label="加入书签"
                  @click="onBookmarkActive"
                >
                  <template #icon><NIcon><BookmarkAdd24Regular /></NIcon></template>
                  {{ activeBookmarked ? "已在书签" : "加入书签" }}
                </NButton>
              </template>
              {{ activeBookmarked ? "当前文件已在当前书签簿" : "加入当前书签簿" }}
            </NTooltip>

            <NTooltip trigger="hover">
              <template #trigger>
                <NButton
                  quaternary
                  size="tiny"
                  :loading="revealingFile"
                  :disabled="!canRevealActiveFile"
                  @click="onRevealActiveFile"
                >
                  <template #icon><NIcon><DocumentSearch24Regular /></NIcon></template>
                  在资源管理器中选中
                </NButton>
              </template>
              定位当前文件
            </NTooltip>

            <NTooltip trigger="hover">
              <template #trigger>
                <NButton
                  quaternary
                  size="tiny"
                  :disabled="!activeTab"
                  aria-label="左右分屏"
                  @click="editor.split('columns', paneId)"
                >
                  <template #icon><NIcon><SplitVertical24Regular /></NIcon></template>
                  左右分屏
                </NButton>
              </template>
              左右分屏 (Cmd/Ctrl+\)
            </NTooltip>

            <NTooltip trigger="hover">
              <template #trigger>
                <NButton
                  quaternary
                  size="tiny"
                  :disabled="!activeTab"
                  aria-label="上下分屏"
                  @click="editor.split('rows', paneId)"
                >
                  <template #icon><NIcon><SplitHorizontal24Regular /></NIcon></template>
                  上下分屏
                </NButton>
              </template>
              上下分屏 (Cmd/Ctrl+Shift+\)
            </NTooltip>

            <NTooltip v-if="previewProviderFor(tab)" trigger="hover">
              <template #trigger>
                <NButton
                  quaternary
                  size="tiny"
                  :type="isPreviewOpen(tab.index) ? 'primary' : 'default'"
                  aria-label="切换文件预览"
                  @click="togglePreview(tab.index)"
                >
                  <template #icon>
                    <NIcon><EyeOff24Regular v-if="isPreviewOpen(tab.index)" /><Eye24Regular v-else /></NIcon>
                  </template>
                  预览
                </NButton>
              </template>
              {{ isPreviewOpen(tab.index) ? "收起文件预览" : "打开文件预览" }}
            </NTooltip>
          </div>
        </div>

        <div class="pane-body">
          <div v-if="!tab.editable" class="readonly-hint">
            该文件类型(text {{ tab.dataType }},{{ sizeText(tab.size) }})暂不支持编辑
          </div>
          <NSpin v-if="isOpeningTab(tab.index)" style="margin-top: 120px" />
          <CodeEditor
            v-else
            :doc="tab.text"
            :read-only="!tab.editable"
            :annotations="tab.annotations"
            :tag-placement="settings.annotationTagPlacement"
            :vim-mode="settings.vimMode"
            :theme-id="props.themeId"
            @change="(text: string) => editor.updateContent(tab.index, text)"
            @open-reference="(fileIndex: number) => editor.openFile(fileIndex, paneId)"
          />
          <PreviewHost
            v-if="previewProviderFor(tab)"
            :file="previewFile(tab)"
            :active="editor.activePaneId === paneId && activeTab?.index === tab.index"
            :open="isPreviewOpen(tab.index)"
            @close="closePreview(tab.index)"
          />
        </div>
      </NTabPane>
    </NTabs>
    <NDropdown
      trigger="manual"
      placement="bottom-start"
      :show="tabContextMenu.show"
      :x="tabContextMenu.x"
      :y="tabContextMenu.y"
      :options="tabContextMenuOptions"
      @select="onTabContextMenuSelect"
      @clickoutside="hideTabContextMenu"
    />
  </div>
</template>

<style scoped>
.editor-pane-view {
  position: relative;
  height: 100%;
  min-width: 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.editor-pane-view--active {
  box-shadow: inset 0 0 0 1px var(--pvf-effect-active-pane-ring);
}
.editor-pane-view--drop-target {
  box-shadow: inset 0 0 0 1px var(--pvf-effect-success-ring);
}
.drop-preview {
  position: absolute;
  z-index: 20;
  pointer-events: none;
  inset: 0;
  border: 1px solid var(--pvf-effect-success-edge);
}
.drop-preview-pane {
  position: absolute;
  background: var(--pvf-surface-drag-preview);
}
.drop-preview-divider {
  position: absolute;
  background: var(--pvf-success);
  box-shadow: 0 0 8px var(--pvf-effect-success-glow);
}
.drop-preview--left .drop-preview-pane {
  top: 0;
  bottom: 0;
  left: 0;
  width: 50%;
}
.drop-preview--left .drop-preview-divider {
  top: 0;
  bottom: 0;
  left: 50%;
  width: 2px;
}
.drop-preview--right .drop-preview-pane {
  top: 0;
  right: 0;
  bottom: 0;
  width: 50%;
}
.drop-preview--right .drop-preview-divider {
  top: 0;
  right: 50%;
  bottom: 0;
  width: 2px;
}
.drop-preview--top .drop-preview-pane {
  top: 0;
  right: 0;
  left: 0;
  height: 50%;
}
.drop-preview--top .drop-preview-divider {
  top: 50%;
  right: 0;
  left: 0;
  height: 2px;
}
.drop-preview--bottom .drop-preview-pane {
  right: 0;
  bottom: 0;
  left: 0;
  height: 50%;
}
.drop-preview--bottom .drop-preview-divider {
  right: 0;
  bottom: 50%;
  left: 0;
  height: 2px;
}
.pane-empty {
  flex: 1;
  min-width: 0;
  min-height: 0;
  display: flex;
  align-items: center;
  justify-content: center;
}
.editor-tabs {
  flex: 1;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
}
.editor-tabs :deep(.n-tabs-nav) {
  padding: 0 6px;
}
.editor-tabs :deep(.n-tabs-pane-wrapper),
.editor-tabs :deep(.n-tab-pane) {
  height: 100%;
  min-height: 0;
  padding: 0 !important;
}
.editor-tabs :deep(.n-tabs-pane-wrapper) {
  flex: 1;
  min-width: 0;
  overflow: hidden;
}
.editor-tabs :deep(.n-tab-pane) {
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.tab-label {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  max-width: 260px;
  min-width: 0;
  overflow: hidden;
}
.tab-title {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.tab-label--dragging {
  opacity: 0.45;
}
.tab-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: transparent;
  border: 1px solid var(--pvf-border-strong);
  flex-shrink: 0;
}
.tab-dot.dirty {
  background: var(--pvf-success);
  border-color: var(--pvf-success);
}
.tab-close {
  padding: 0 2px;
  height: auto;
  flex-shrink: 0;
}
.editor-info-bar {
  flex: 0 0 auto;
  min-width: 0;
  min-height: 30px;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 2px 8px;
  border-bottom: 1px solid var(--pvf-border-subtle);
  background: var(--pvf-surface-subtle);
}
.editor-file-tags {
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 4px;
  overflow-x: auto;
  scrollbar-width: none;
}
.editor-file-tags::-webkit-scrollbar {
  display: none;
}
.editor-file-tag {
  flex: 0 0 auto;
  max-width: 240px;
  cursor: pointer;
  user-select: none;
}
.editor-file-tag :deep(.n-tag__content) {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.editor-pane-actions {
  flex-shrink: 0;
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 2px;
}
.pane-body {
  position: relative;
  flex: 1;
  min-width: 0;
  min-height: 0;
  height: auto;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.readonly-hint {
  padding: 6px 12px;
  color: var(--pvf-warning);
  background: var(--pvf-surface-warning);
  font-size: 12px;
  flex-shrink: 0;
}
</style>
