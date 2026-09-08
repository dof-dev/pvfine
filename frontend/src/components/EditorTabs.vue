<script setup lang="ts">
import { ref } from "vue";
import { NButton, NEmpty, NIcon, NSpin, useMessage } from "naive-ui";
import { FolderOpen24Regular } from "@vicons/fluent";
import { useArchiveStore } from "../stores/archive";
import { useEditorStore } from "../stores/editor";
import EditorLayout from "./EditorLayout.vue";

const archive = useArchiveStore();
const editor = useEditorStore();
const message = useMessage();
const openingRecent = ref("");

function archiveName(path: string): string {
  return path.split(/[\\/]/).pop() || path;
}

async function openDialog(): Promise<void> {
  try {
    await archive.openDialog();
  } catch (e: any) {
    if (!isCancel(e)) message.error(`打开失败: ${e?.message ?? e}`);
  }
}

async function openRecent(path: string): Promise<void> {
  if (archive.loading) return;
  openingRecent.value = path;
  try {
    await archive.openPath(path);
  } catch (e: any) {
    message.error(`打开失败: ${e?.message ?? e}`);
  } finally {
    openingRecent.value = "";
  }
}

function isCancel(e: any): boolean {
  return String(e?.message ?? e).toLowerCase().includes("cancel");
}
</script>

<template>
  <div class="editor-area">
    <!-- 无标签时 -->
    <div v-if="editor.tabs.length === 0" class="editor-empty">
      <NEmpty v-if="archive.open" size="large" description="从左侧选择一个文件开始编辑" />
      <div v-else class="start-page">
        <div class="start-page-title">打开 PVF 归档</div>
        <div class="start-page-subtitle">选择一个归档开始浏览和编辑</div>

        <div v-if="archive.recentArchives.length" class="recent-section">
          <div class="recent-heading">
            <span>最近打开</span>
            <NButton text size="tiny" @click="archive.clearRecentArchives">清空记录</NButton>
          </div>
          <div class="recent-list">
            <button
              v-for="path in archive.recentArchives"
              :key="path"
              class="recent-item"
              :disabled="archive.loading"
              type="button"
              @click="openRecent(path)"
            >
              <NIcon :size="20" class="recent-icon"><FolderOpen24Regular /></NIcon>
              <span class="recent-item-text">
                <span class="recent-name">{{ archiveName(path) }}</span>
                <span class="recent-path" :title="path">{{ path }}</span>
              </span>
              <NSpin v-if="openingRecent === path" :size="16" />
            </button>
          </div>
        </div>

        <NEmpty v-else size="large" description="还没有打开过 PVF 归档" />
        <NButton type="primary" :loading="archive.loading" @click="openDialog">
          <template #icon><NIcon><FolderOpen24Regular /></NIcon></template>
          打开 PVF 归档
        </NButton>
      </div>
    </div>

    <div v-else class="editor-workspace">
      <EditorLayout :node="editor.layout" />
    </div>
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
.editor-empty {
  height: 100%;
  display: flex;
  justify-content: center;
  align-items: flex-start;
  padding-top: 120px;
}
.start-page {
  width: min(620px, 86%);
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
}
.start-page-title {
  color: rgba(255, 255, 255, 0.9);
  font-size: 22px;
  font-weight: 600;
}
.start-page-subtitle {
  color: rgba(255, 255, 255, 0.5);
  font-size: 13px;
  margin-bottom: 14px;
}
.recent-section {
  width: 100%;
  max-width: 560px;
  margin-bottom: 8px;
}
.recent-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  color: rgba(255, 255, 255, 0.72);
  font-size: 12px;
  margin-bottom: 6px;
}
.recent-list {
  display: flex;
  flex-direction: column;
  gap: 4px;
  max-height: 310px;
  overflow: auto;
}
.recent-item {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border: 1px solid rgba(128, 128, 128, 0.22);
  border-radius: 6px;
  color: inherit;
  background: rgba(255, 255, 255, 0.025);
  text-align: left;
  cursor: pointer;
  transition: background 120ms ease, border-color 120ms ease;
}
.recent-item:hover:not(:disabled) {
  border-color: rgba(79, 140, 255, 0.6);
  background: rgba(79, 140, 255, 0.1);
}
.recent-item:disabled {
  cursor: wait;
  opacity: 0.65;
}
.recent-icon {
  color: #6ba0ff;
  flex-shrink: 0;
}
.recent-item-text {
  min-width: 0;
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.recent-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: rgba(255, 255, 255, 0.88);
  font-size: 13px;
}
.recent-path {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: rgba(255, 255, 255, 0.42);
  font-size: 11px;
}
.editor-workspace {
  flex: 1;
  width: 100%;
  height: 100%;
  min-width: 0;
  min-height: 0;
  display: flex;
  overflow: hidden;
}
</style>
