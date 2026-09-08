<script setup lang="ts">
import { computed } from "vue";
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
import { useEditorStore, type EditorPaneId, type EditorTab } from "../stores/editor";
import CodeEditor from "./CodeEditor.vue";
import { useSettingsStore } from "../stores/settings";

const props = defineProps<{
  paneId: EditorPaneId;
}>();

const paneId = props.paneId;
const editor = useEditorStore();
const settings = useSettingsStore();

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
</script>

<template>
  <div
    class="editor-pane-view"
    :class="{ 'editor-pane-view--active': editor.activePaneId === paneId }"
    @pointerdown="activatePane"
    @focusin="activatePane"
  >
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
          <span class="tab-label" :title="tab.path">
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
