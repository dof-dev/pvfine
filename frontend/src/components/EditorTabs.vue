<script setup lang="ts">
import { computed } from "vue";
import { NButton, NEmpty, NIcon, NSpin, NTabPane, NTabs, NTag, NTooltip } from "naive-ui";
import { Dismiss16Regular } from "@vicons/fluent";
import { useArchiveStore } from "../stores/archive";
import { useEditorStore } from "../stores/editor";
import CodeEditor from "./CodeEditor.vue";
import { useSettingsStore } from "../stores/settings";

const archive = useArchiveStore();
const editor = useEditorStore();
const settings = useSettingsStore();

function typeTag(t: number): string {
  if (t === 1) return "script";
  if (t === 3) return "text";
  return `type ${t}`;
}

function onClose(index: number) {
  editor.closeTab(index);
}

const tabKeys = computed(() => editor.tabs.map((t) => String(t.index)));
const activeKeyStr = computed(() =>
  editor.activeKey === null ? undefined : String(editor.activeKey)
);

function onActive(key: string) {
  editor.activeKey = Number(key);
}

function sizeText(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}
</script>

<template>
  <div class="editor-area">
    <!-- 无标签时 -->
    <div v-if="editor.tabs.length === 0" class="editor-empty">
      <NEmpty size="large" :description="archive.open ? '从左侧选择一个文件开始编辑' : '打开一个 PVF 归档开始'">
      </NEmpty>
    </div>

    <NTabs
      v-else
      type="card"
      size="small"
      :value="activeKeyStr"
      @update:value="onActive"
      class="editor-tabs"
      style="height: 100%"
    >
      <NTabPane
        v-for="tab in editor.tabs"
        :key="String(tab.index)"
        :name="String(tab.index)"
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
          <NSpin v-if="editor.opening" style="margin-top: 120px" />
          <CodeEditor
            v-else
            :doc="tab.text"
            :read-only="!tab.editable"
            :annotations="tab.annotations"
            :tag-placement="settings.annotationTagPlacement"
            @change="(text: string) => editor.updateContent(tab.index, text)"
            @open-reference="editor.openFile"
          />
        </div>
      </NTabPane>
    </NTabs>
  </div>
</template>

<style scoped>
.editor-area {
  height: 100%;
  min-width: 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.editor-tabs {
  flex: 1;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
}
.editor-empty {
  margin-top: 160px;
}
.empty-hint {
  color: rgba(128, 128, 128, 0.6);
  font-size: 12px;
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
