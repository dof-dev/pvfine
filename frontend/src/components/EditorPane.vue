<script setup lang="ts">
import { computed, ref } from "vue";
import {
  NButton,
  NEmpty,
  NIcon,
  NSpin,
  NTabPane,
  NTabs,
  NTooltip,
} from "naive-ui";
import { Dismiss16Regular } from "@vicons/fluent";
import {
  useEditorStore,
  type DraggedEditorTab,
  type EditorPaneId,
  type EditorTab,
} from "../stores/editor";
import CodeEditor from "./CodeEditor.vue";
import { useSettingsStore } from "../stores/settings";

const props = defineProps<{
  paneId: EditorPaneId;
}>();

const paneId = props.paneId;
const editor = useEditorStore();
const settings = useSettingsStore();
const host = ref<HTMLDivElement | null>(null);
const draggingIndex = ref<number | null>(null);
const dragOver = ref(false);
const dragOverEdge = ref<DropEdge | null>(null);

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

function activatePane(): void {
  editor.activatePane(paneId);
}

function onActive(key: string | number): void {
  editor.activateTab(paneId, Number(key));
}

function onClose(index: number): void {
  editor.closeTab(index, paneId);
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
          >
            <span :class="['tab-dot', { dirty: tab.text !== tab.original }]" />
            {{ tab.title }}
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
            @change="(text: string) => editor.updateContent(tab.index, text)"
            @open-reference="(fileIndex: number) => editor.openFile(fileIndex, paneId)"
          />
        </div>
      </NTabPane>
    </NTabs>
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
  box-shadow: inset 0 0 0 1px rgba(79, 140, 255, 0.25);
}
.editor-pane-view--drop-target {
  box-shadow: inset 0 0 0 1px rgba(99, 226, 183, 0.55);
}
.drop-preview {
  position: absolute;
  z-index: 20;
  pointer-events: none;
  inset: 0;
  border: 1px solid rgba(99, 226, 183, 0.85);
}
.drop-preview-pane {
  position: absolute;
  background: rgba(79, 140, 255, 0.22);
}
.drop-preview-divider {
  position: absolute;
  background: rgba(99, 226, 183, 0.9);
  box-shadow: 0 0 8px rgba(99, 226, 183, 0.35);
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
}
.tab-label--dragging {
  opacity: 0.45;
}
.tab-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: transparent;
  border: 1px solid rgba(128, 128, 128, 0.4);
  flex-shrink: 0;
}
.tab-dot.dirty {
  background: #63e2b7;
  border-color: #63e2b7;
}
.tab-close {
  padding: 0 2px;
  height: auto;
}
.pane-body {
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
  color: rgba(230, 180, 100, 0.9);
  background: rgba(230, 180, 100, 0.08);
  font-size: 12px;
  flex-shrink: 0;
}
</style>
