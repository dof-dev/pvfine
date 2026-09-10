<script setup lang="ts">
import { onMounted, onUnmounted, ref } from "vue";
import {
  NButton,
  NIcon,
  NSpin,
  NTag,
  NTooltip,
  useMessage,
} from "naive-ui";
import {
  Archive24Regular,
  ArrowDownload24Regular,
  Clock24Regular,
  Dismiss24Regular,
  DocumentSync24Regular,
  DocumentText24Regular,
  FolderOpen24Regular,
  Search24Regular,
} from "@vicons/fluent";
import { useArchiveStore } from "../stores/archive";
import { useEditorStore } from "../stores/editor";
import { useAdvancedSearchStore } from "../stores/advancedSearch";
import { useVersionStore } from "../stores/version";
import EditorLayout from "./EditorLayout.vue";
import type { ResolvedThemeId } from "../theme";

const archive = useArchiveStore();
const editor = useEditorStore();
const advancedSearch = useAdvancedSearchStore();
const version = useVersionStore();
const message = useMessage();

const openingRecent = ref("");
const isDragging = ref(false);
let dragCounter = 0;
let dropTargetObserver: MutationObserver | null = null;

const isMac = /Macintosh|Mac OS X|MacIntel/i.test(
  `${navigator.platform} ${navigator.userAgent}`
);

const props = defineProps<{
  themeId: ResolvedThemeId;
}>();

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

function removeRecent(path: string): void {
  archive.removeRecentArchive(path);
}

function onDragEnter(e: DragEvent): void {
  e.preventDefault();
  dragCounter++;
  if (e.dataTransfer?.types?.includes("Files")) {
    isDragging.value = true;
  }
}

function onDragOver(e: DragEvent): void {
  e.preventDefault();
  if (e.dataTransfer) {
    e.dataTransfer.dropEffect = "copy";
  }
}

function onDragLeave(e: DragEvent): void {
  e.preventDefault();
  dragCounter--;
  if (dragCounter <= 0) {
    dragCounter = 0;
    isDragging.value = false;
  }
}

function onDrop(e: DragEvent): void {
  e.preventDefault();
  dragCounter = 0;
  isDragging.value = false;
}

onMounted(() => {
  const dropTarget = document.querySelector<HTMLElement>(
    "[data-file-drop-target]",
  );
  if (!dropTarget) return;

  const syncDropState = () => {
    isDragging.value = dropTarget.classList.contains("file-drop-target-active");
  };
  syncDropState();
  dropTargetObserver = new MutationObserver(syncDropState);
  dropTargetObserver.observe(dropTarget, {
    attributes: true,
    attributeFilter: ["class"],
  });
});

onUnmounted(() => {
  dropTargetObserver?.disconnect();
  dropTargetObserver = null;
});

function isCancel(e: any): boolean {
  return String(e?.message ?? e).toLowerCase().includes("cancel");
}
</script>

<template>
  <div
    class="editor-area"
    @dragenter="onDragEnter"
    @dragover="onDragOver"
    @dragleave="onDragLeave"
    @drop="onDrop"
  >
    <!-- 全屏拖拽覆盖层 -->
    <div v-if="isDragging" class="dropzone-overlay">
      <div class="dropzone-box">
        <div class="dropzone-icon-wrap">
          <NIcon :size="48" class="dropzone-icon">
            <ArrowDownload24Regular />
          </NIcon>
        </div>
        <div class="dropzone-title">释放以打开 PVF 归档</div>
        <div class="dropzone-subtitle">支持直接拖入 .pvf 归档文件并立即解析</div>
      </div>
    </div>

    <!-- 无标签时 -->
    <div v-if="editor.tabs.length === 0" class="editor-empty">
      <!-- 归档已打开，但当前视口无打开的文件 -->
      <div v-if="archive.open" class="empty-archive-panel">
        <div class="empty-archive-icon-wrap">
          <NIcon :size="36"><DocumentText24Regular /></NIcon>
        </div>
        <div class="empty-archive-title">
          {{ archiveName(archive.info?.path || "") }}
        </div>
        <div class="empty-archive-path" :title="archive.info?.path">
          {{ archive.info?.path }}
        </div>

        <div class="empty-archive-tags">
          <NTag size="small" round :bordered="false" type="info">
            {{ archive.info?.fileCount?.toLocaleString() }} 个文件
          </NTag>
          <NTag size="small" round :bordered="false">
            {{ archive.info?.groupCount?.toLocaleString() }} 个数据块
          </NTag>
          <NTag
            v-if="archive.modifiedCount > 0"
            size="small"
            round
            :bordered="false"
            type="success"
          >
            {{ archive.modifiedCount }} 个未保存修改
          </NTag>
        </div>

        <div class="empty-archive-hint">
          从左侧资源管理器中点击任意文件即可在当前视口打开编辑
        </div>

        <div class="empty-archive-actions">
          <NButton secondary size="small" @click="advancedSearch.open">
            <template #icon><NIcon><Search24Regular /></NIcon></template>
            高级搜索
          </NButton>
          <NButton secondary size="small" @click="version.open">
            <template #icon><NIcon><DocumentSync24Regular /></NIcon></template>
            版本控制
          </NButton>
        </div>
      </div>

      <!-- 未打开归档：启动欢迎页 -->
      <div v-else class="start-page">
        <!-- Hero 主操作卡片 -->
        <div class="start-hero-card" @click="openDialog">
          <div class="hero-logo-box">
            <NIcon :size="28" class="hero-logo-icon">
              <Archive24Regular />
            </NIcon>
          </div>
          <div class="hero-text-col">
            <div class="hero-title-row">
              <span class="hero-title">打开 PVF 归档</span>
              <span class="hero-kbd-badge">{{ isMac ? "⌘ O" : "Ctrl + O" }}</span>
            </div>
            <div class="hero-subtitle">
              点击浏览选择，或将 <code>.pvf</code> 文件直接拖拽至窗口任意位置
            </div>
          </div>
          <NButton
            type="primary"
            size="medium"
            :loading="archive.loading"
            class="hero-open-btn"
            @click.stop="openDialog"
          >
            <template #icon><NIcon><FolderOpen24Regular /></NIcon></template>
            浏览文件
          </NButton>
        </div>

        <!-- 最近打开列表 -->
        <div class="recent-section">
          <div class="recent-heading">
            <div class="recent-heading-left">
              <NIcon :size="15"><Clock24Regular /></NIcon>
              <span>最近打开</span>
              <NTag
                v-if="archive.recentArchives.length"
                size="tiny"
                round
                :bordered="false"
              >
                {{ archive.recentArchives.length }}
              </NTag>
            </div>
            <NButton
              v-if="archive.recentArchives.length"
              text
              size="tiny"
              type="error"
              @click="archive.clearRecentArchives"
            >
              清空记录
            </NButton>
          </div>

          <div v-if="archive.recentArchives.length" class="recent-list">
            <div
              v-for="path in archive.recentArchives"
              :key="path"
              class="recent-item"
              :class="{ 'recent-item--loading': openingRecent === path }"
              role="button"
              tabindex="0"
              @click="openRecent(path)"
              @keydown.enter="openRecent(path)"
            >
              <div class="recent-icon-box">
                <NIcon :size="18"><Archive24Regular /></NIcon>
              </div>
              <div class="recent-item-text">
                <div class="recent-name">{{ archiveName(path) }}</div>
                <div class="recent-path" :title="path">{{ path }}</div>
              </div>

              <div class="recent-item-actions">
                <NSpin v-if="openingRecent === path" :size="16" />
                <NTooltip v-else trigger="hover">
                  <template #trigger>
                    <button
                      type="button"
                      class="recent-remove-btn"
                      aria-label="从最近记录移除"
                      @click.stop="removeRecent(path)"
                    >
                      <NIcon :size="14"><Dismiss24Regular /></NIcon>
                    </button>
                  </template>
                  从列表中移除
                </NTooltip>
              </div>
            </div>
          </div>

          <div v-else class="recent-empty-card">
            <NIcon :size="24" class="recent-empty-icon"><FolderOpen24Regular /></NIcon>
            <div class="recent-empty-text">暂无最近打开的历史记录</div>
          </div>
        </div>
      </div>
    </div>

    <div v-else class="editor-workspace">
      <EditorLayout :node="editor.layout" :theme-id="props.themeId" />
    </div>
  </div>
</template>

<style scoped>
.editor-area {
  position: relative;
  height: 100%;
  min-width: 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

/* 全屏拖拽覆盖层 */
.dropzone-overlay {
  position: absolute;
  inset: 0;
  z-index: 99;
  background: var(--pvf-window-background-glass);
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  pointer-events: none;
}
.dropzone-box {
  width: min(520px, 90%);
  border: 2px dashed var(--pvf-primary);
  border-radius: 16px;
  background: var(--pvf-primary-soft);
  padding: 44px 32px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
  text-align: center;
  box-shadow: 0 12px 36px var(--pvf-effect-tooltip-shadow);
  animation: dropzone-pop 0.2s cubic-bezier(0.16, 1, 0.3, 1);
}
@keyframes dropzone-pop {
  from {
    opacity: 0;
    transform: scale(0.96);
  }
  to {
    opacity: 1;
    transform: scale(1);
  }
}
.dropzone-icon-wrap {
  width: 72px;
  height: 72px;
  border-radius: 50%;
  background: var(--pvf-surface-card);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--pvf-primary);
  box-shadow: 0 4px 16px rgba(79, 140, 255, 0.25);
  animation: float-icon 1.2s ease-in-out infinite alternate;
}
@keyframes float-icon {
  from {
    transform: translateY(-4px);
  }
  to {
    transform: translateY(4px);
  }
}
.dropzone-title {
  color: var(--pvf-text-primary);
  font-size: 18px;
  font-weight: 600;
}
.dropzone-subtitle {
  color: var(--pvf-text-secondary);
  font-size: 13px;
}

/* 编辑区空态 */
.editor-empty {
  height: 100%;
  display: flex;
  justify-content: center;
  align-items: center;
  overflow: auto;
  padding: 20px;
}

/* 归档已打开但无标签状态 */
.empty-archive-panel {
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  gap: 12px;
  width: min(480px, 90%);
  padding: 36px 28px;
  background: transparent;
  border: 1px solid transparent;
  box-shadow: none;
}
.empty-archive-icon-wrap {
  width: 56px;
  height: 56px;
  border-radius: 14px;
  background: var(--pvf-primary-soft);
  color: var(--pvf-primary);
  display: flex;
  align-items: center;
  justify-content: center;
}
.empty-archive-title {
  font-size: 17px;
  font-weight: 600;
  color: var(--pvf-text-primary);
}
.empty-archive-path {
  font-size: 11px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  color: var(--pvf-text-faint);
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.empty-archive-tags {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
  justify-content: center;
  margin: 4px 0;
}
.empty-archive-hint {
  font-size: 12px;
  color: var(--pvf-text-secondary);
  line-height: 1.5;
}
.empty-archive-actions {
  display: flex;
  gap: 10px;
  margin-top: 8px;
}

/* 未打开归档：启动页 */
.start-page {
  width: min(700px, 92%);
  display: flex;
  flex-direction: column;
  gap: 18px;
  padding: 20px 0;
}

/* Hero 主操作卡片 */
.start-hero-card {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 18px 22px;
  background: var(--pvf-surface-card);
  border: 1.5px solid var(--pvf-border-subtle);
  border-radius: 14px;
  cursor: pointer;
  transition: all 0.18s ease;
  user-select: none;
}
.start-hero-card:hover {
  border-color: var(--pvf-primary);
  background: var(--pvf-surface-hover);
  transform: translateY(-1px);
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.08);
}
.hero-logo-box {
  width: 48px;
  height: 48px;
  border-radius: 12px;
  background: var(--pvf-primary-soft);
  color: var(--pvf-primary);
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}
.hero-text-col {
  flex: 1;
  min-width: 0;
}
.hero-title-row {
  display: flex;
  align-items: center;
  gap: 10px;
}
.hero-title {
  font-size: 16px;
  font-weight: 600;
  color: var(--pvf-text-primary);
}
.hero-kbd-badge {
  padding: 2px 7px;
  font-size: 11px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  background: var(--pvf-surface-subtle);
  border: 1px solid var(--pvf-border-faint);
  border-radius: 5px;
  color: var(--pvf-text-muted);
}
.hero-subtitle {
  font-size: 12px;
  color: var(--pvf-text-secondary);
  margin-top: 4px;
}
.hero-subtitle code {
  padding: 1px 5px;
  background: var(--pvf-surface-code);
  border-radius: 4px;
  font-family: ui-monospace, monospace;
}
.hero-open-btn {
  flex-shrink: 0;
}

/* 最近打开列表 */
.recent-section {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.recent-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 2px;
}
.recent-heading-left {
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--pvf-text-secondary);
  font-size: 13px;
  font-weight: 600;
}
.recent-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
  max-height: 240px;
  overflow-y: auto;
  padding-right: 2px;
}
.recent-item {
  position: relative;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 14px;
  background: var(--pvf-surface-card);
  border: 1px solid var(--pvf-border-subtle);
  border-radius: 9px;
  cursor: pointer;
  transition: all 0.15s ease;
  user-select: none;
}
.recent-item:hover {
  border-color: var(--pvf-primary-hover);
  background: var(--pvf-surface-hover);
  transform: translateY(-1px);
}
.recent-item:focus-visible {
  outline: none;
  box-shadow: 0 0 0 2px var(--pvf-effect-focus-ring);
}
.recent-item--loading {
  pointer-events: none;
  opacity: 0.75;
}
.recent-icon-box {
  width: 34px;
  height: 34px;
  border-radius: 8px;
  background: var(--pvf-surface-subtle);
  border: 1px solid var(--pvf-border-faint);
  color: var(--pvf-primary);
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}
.recent-item-text {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.recent-name {
  font-size: 13px;
  font-weight: 500;
  color: var(--pvf-text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.recent-path {
  font-size: 11px;
  color: var(--pvf-text-faint);
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.recent-item-actions {
  display: flex;
  align-items: center;
  flex-shrink: 0;
}
.recent-remove-btn {
  width: 24px;
  height: 24px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: none;
  background: transparent;
  border-radius: 5px;
  color: var(--pvf-text-faint);
  cursor: pointer;
  opacity: 0;
  transition: all 0.15s ease;
}
.recent-item:hover .recent-remove-btn {
  opacity: 1;
}
.recent-remove-btn:hover {
  color: var(--pvf-error);
  background: var(--pvf-surface-error);
}

.recent-empty-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 28px 16px;
  background: var(--pvf-surface-subtle);
  border: 1px dashed var(--pvf-border-subtle);
  border-radius: 10px;
}
.recent-empty-icon {
  color: var(--pvf-text-faint);
}
.recent-empty-text {
  font-size: 12px;
  color: var(--pvf-text-faint);
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

@media (max-width: 680px) {
  .start-hero-card {
    flex-direction: column;
    align-items: flex-start;
  }
  .hero-open-btn {
    width: 100%;
  }
}
</style>
