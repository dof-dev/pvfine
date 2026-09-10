<script setup lang="ts">
import { computed } from "vue";
import {
  Archive20Regular,
  ArrowSync20Regular,
  Branch20Regular,
  CheckmarkCircle20Regular,
  Database20Regular,
  DismissCircle20Regular,
  DocumentText20Regular,
  Edit20Regular,
  FolderOpen20Regular,
} from "@vicons/fluent";
import { NIcon, NProgress, NTooltip, useMessage } from "naive-ui";
import { useArchiveStore } from "../stores/archive";
import { useEditorStore } from "../stores/editor";
import { useImageStore } from "../stores/images";
import { useVersionStore } from "../stores/version";
import { useAdvancedSearchStore } from "../stores/advancedSearch";
import { useSettingsStore } from "../stores/settings";

const archive = useArchiveStore();
const editor = useEditorStore();
const images = useImageStore();
const version = useVersionStore();
const advancedSearch = useAdvancedSearchStore();
const settings = useSettingsStore();
const message = useMessage();

const currentPath = computed(() => editor.activeTab?.path ?? "");
const sizeText = computed(() => {
  const n = editor.activeTab?.size ?? 0;
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
});
const unpackPct = computed(() =>
  archive.unpackProgress.total
    ? Math.round((archive.unpackProgress.done / archive.unpackProgress.total) * 100)
    : 0
);
const indexPct = computed(() =>
  archive.indexStatus.total
    ? Math.round((archive.indexStatus.done / archive.indexStatus.total) * 100)
    : 0
);
const indexStateLabel = computed(() => {
  switch (archive.indexStatus.state) {
    case "building":
      return "索引中";
    case "ready":
      return "索引就绪";
    case "error":
      return "索引失败";
    default:
      return "索引准备中";
  }
});
const indexTimingTitle = computed(() => {
  let npkIndex = "未完成";
  if (!images.status.directory) {
    npkIndex = "未配置 NPK 目录";
  } else if (images.status.state === "ready" && images.status.stage === "ready-cache") {
    npkIndex = "缓存命中（本次未重建）";
  } else if (images.status.buildDurationMs > 0) {
    npkIndex = formatDuration(images.status.buildDurationMs);
  } else if (images.status.state === "building") {
    npkIndex = "构建中";
  } else if (images.status.state === "error") {
    npkIndex = "构建失败";
  }
  return [
    `打开 PVF 至可操作：${formatDuration(archive.indexStatus.openDurationMs)}`,
    `构建 PVF 索引：${formatDuration(archive.indexStatus.buildDurationMs)}`,
    `构建 NPK 索引：${npkIndex}`,
  ].join("\n");
});

function formatDuration(milliseconds: number): string {
  if (!Number.isFinite(milliseconds) || milliseconds <= 0) return "未完成";
  if (milliseconds < 1) return `${milliseconds.toFixed(2)} ms`;
  return `${milliseconds.toFixed(1)} ms`;
}

function onOpenArchive(): void {
  void archive.openDialog();
}

async function onCopyArchivePath(): Promise<void> {
  const path = archive.info?.path;
  if (!path) return;
  try {
    await navigator.clipboard.writeText(path);
    message.success("已复制归档完整路径");
  } catch {
    message.info(path);
  }
}

async function onCopyCurrentPath(): Promise<void> {
  if (!currentPath.value) return;
  try {
    await navigator.clipboard.writeText(currentPath.value);
    message.success("已复制当前文件路径");
  } catch {
    message.info(currentPath.value);
  }
}

function onOpenSearch(): void {
  advancedSearch.open();
}

function onOpenVersion(): void {
  version.open();
}

function onOpenIndexSettings(): void {
  settings.open("npk");
}
</script>

<template>
  <div class="statusbar" role="status" aria-label="应用状态栏">
    <!-- 归档路径 -->
    <NTooltip trigger="hover">
      <template #trigger>
        <span
          class="sb-item sb-interactive sb-path"
          tabindex="0"
          role="button"
          :aria-label="archive.open ? '复制归档路径' : '打开归档'"
          @click="archive.open ? onCopyArchivePath() : onOpenArchive()"
          @keydown.enter="archive.open ? onCopyArchivePath() : onOpenArchive()"
        >
          <NIcon :size="13" class="sb-icon">
            <Archive20Regular v-if="archive.open" />
            <FolderOpen20Regular v-else />
          </NIcon>
          <span class="sb-path-text">{{ archive.info?.path || "未打开归档" }}</span>
        </span>
      </template>
      <span v-if="archive.open">{{ archive.info?.path }}<br />点击复制完整路径</span>
      <span v-else>未打开归档 · 点击选择并打开 PVF 归档 (⌘O)</span>
    </NTooltip>

    <template v-if="archive.open">
      <!-- 文件总数 -->
      <span class="sb-sep" />
      <NTooltip trigger="hover">
        <template #trigger>
          <span
            class="sb-item sb-interactive"
            tabindex="0"
            role="button"
            aria-label="打开高级搜索"
            @click="onOpenSearch"
            @keydown.enter="onOpenSearch"
          >
            <NIcon :size="13" class="sb-icon"><DocumentText20Regular /></NIcon>
            <span>{{ archive.info?.fileCount?.toLocaleString() }} 文件</span>
          </span>
        </template>
        归档包含 {{ archive.info?.fileCount?.toLocaleString() }} 个文件<br />点击呼出高级搜索面板
      </NTooltip>

      <!-- 数据块总数 -->
      <span class="sb-sep" />
      <NTooltip trigger="hover">
        <template #trigger>
          <span
            class="sb-item sb-interactive"
            tabindex="0"
            role="button"
            aria-label="打开高级搜索"
            @click="onOpenSearch"
            @keydown.enter="onOpenSearch"
          >
            <NIcon :size="13" class="sb-icon"><Database20Regular /></NIcon>
            <span>{{ archive.info?.groupCount?.toLocaleString() }} 块</span>
          </span>
        </template>
        归档划分为 {{ archive.info?.groupCount?.toLocaleString() }} 个连续数据块<br />点击呼出高级搜索面板
      </NTooltip>

      <!-- 内存中未保存修改数 -->
      <template v-if="archive.modifiedCount > 0">
        <span class="sb-sep" />
        <NTooltip trigger="hover">
          <template #trigger>
            <span
              class="sb-item sb-interactive sb-modified"
              tabindex="0"
              role="button"
              aria-label="查看未提交修改"
              @click="onOpenVersion"
              @keydown.enter="onOpenVersion"
            >
              <NIcon :size="13" class="sb-icon"><Edit20Regular /></NIcon>
              <span>{{ archive.modifiedCount }} 个已修改</span>
            </span>
          </template>
          当前有 {{ archive.modifiedCount }} 个文件已在内存中修改<br />点击呼出版本控制面板
        </NTooltip>
      </template>

      <!-- 版本控制状态 -->
      <template v-if="version.status.loading">
        <span class="sb-sep" />
        <span class="sb-item sb-interactive sb-version-loading" @click="onOpenVersion">
          <NIcon :size="13" class="sb-icon spin-icon"><ArrowSync20Regular /></NIcon>
          <span>版本控制加载中</span>
        </span>
      </template>
      <template v-else-if="version.enabled">
        <span class="sb-sep" />
        <NTooltip trigger="hover">
          <template #trigger>
            <span
              class="sb-item sb-interactive sb-version"
              tabindex="0"
              role="button"
              aria-label="打开版本控制面板"
              @click="onOpenVersion"
              @keydown.enter="onOpenVersion"
            >
              <NIcon :size="13" class="sb-icon"><Branch20Regular /></NIcon>
              <span>{{ version.status.branch }} · {{ version.status.changedFiles }} 个版本变更</span>
            </span>
          </template>
          分支: {{ version.status.branch }} · {{ version.status.changedFiles }} 个变更待提交
          <template v-if="version.status.headMessage"><br />最新提交: {{ version.status.headMessage }}</template>
          <br />点击呼出版本控制面板
        </NTooltip>
      </template>
      <template v-else>
        <span class="sb-sep" />
        <NTooltip trigger="hover">
          <template #trigger>
            <span
              class="sb-item sb-interactive sb-version-muted"
              tabindex="0"
              role="button"
              aria-label="初始化版本控制"
              @click="onOpenVersion"
              @keydown.enter="onOpenVersion"
            >
              <NIcon :size="13" class="sb-icon"><Branch20Regular /></NIcon>
              <span>未启用版本控制</span>
            </span>
          </template>
          当前归档尚未初始化版本控制<br />点击呼出版本控制面板进行初始化
        </NTooltip>
      </template>

      <span class="sb-spacer" />

      <!-- 索引状态与耗时 -->
      <NTooltip trigger="hover">
        <template #trigger>
          <span
            class="sb-item sb-interactive index-state"
            :class="{ 'sb-error': archive.indexStatus.state === 'error' }"
            tabindex="0"
            role="button"
            aria-label="打开索引与素材设置"
            @click="onOpenIndexSettings"
            @keydown.enter="onOpenIndexSettings"
          >
            <NIcon :size="13" class="sb-icon" :class="{ 'spin-icon': archive.indexing }">
              <DismissCircle20Regular v-if="archive.indexStatus.state === 'error'" />
              <ArrowSync20Regular v-else-if="archive.indexing" />
              <CheckmarkCircle20Regular v-else />
            </NIcon>
            <span>{{ indexStateLabel }}</span>
            <NProgress
              v-if="archive.indexing"
              type="line"
              :show-indicator="false"
              :percentage="indexPct"
              style="width: 76px; display: inline-flex"
            />
          </span>
        </template>
        <div class="sb-tooltip-content">
          <div style="white-space: pre-line">{{ indexTimingTitle }}</div>
          <div class="sb-tooltip-action">点击呼出素材与索引设置</div>
        </div>
      </NTooltip>

      <!-- 解包进度 -->
      <span v-if="archive.unpacking" class="sb-item unpack">
        <NIcon :size="13" class="sb-icon spin-icon"><ArrowSync20Regular /></NIcon>
        <span>解包中</span>
        <NProgress
          type="line"
          :show-indicator="false"
          :percentage="unpackPct"
          style="width: 76px; display: inline-flex"
        />
      </span>

      <!-- 当前活动文件路径与大小 -->
      <template v-if="currentPath">
        <span class="sb-sep" />
        <NTooltip trigger="hover">
          <template #trigger>
            <span
              class="sb-item sb-interactive sb-cur"
              tabindex="0"
              role="button"
              aria-label="复制当前文件路径"
              @click="onCopyCurrentPath"
              @keydown.enter="onCopyCurrentPath"
            >
              <NIcon :size="13" class="sb-icon"><DocumentText20Regular /></NIcon>
              <span class="sb-cur-text">{{ currentPath }}</span>
            </span>
          </template>
          {{ currentPath }}<br />点击复制文件路径
        </NTooltip>
        <span class="sb-sep" />
        <NTooltip trigger="hover">
          <template #trigger>
            <span class="sb-item sb-interactive sb-size">
              {{ sizeText }}
            </span>
          </template>
          文件大小: {{ (editor.activeTab?.size ?? 0).toLocaleString() }} 字节
        </NTooltip>
      </template>
    </template>
    <span v-else class="sb-spacer" />
  </div>
</template>

<style scoped>
.statusbar {
  display: flex;
  align-items: center;
  height: 26px;
  padding: 0 10px;
  gap: 6px;
  font-size: 11px;
  color: var(--pvf-text-muted);
  border-top: 1px solid var(--pvf-border-normal);
  background: var(--pvf-surface-bar);
  flex-shrink: 0;
  white-space: nowrap;
  overflow: hidden;
}
.sb-item {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  overflow: hidden;
  text-overflow: ellipsis;
  flex-shrink: 0;
}
.sb-interactive {
  padding: 2px 6px;
  margin: -2px 0;
  border-radius: 4px;
  cursor: pointer;
  user-select: none;
  transition: all 0.15s ease;
  color: var(--pvf-text-secondary);
}
.sb-interactive:hover {
  background: var(--pvf-surface-hover);
  color: var(--pvf-text-primary);
}
.sb-interactive:active {
  background: var(--pvf-surface-active);
  transform: translateY(0.5px);
}
.sb-interactive:focus-visible {
  outline: none;
  box-shadow: 0 0 0 1px var(--pvf-primary);
}
.sb-icon {
  flex-shrink: 0;
}
.sb-path {
  max-width: 320px;
}
.sb-path-text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.sb-cur {
  max-width: 300px;
}
.sb-cur-text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.sb-size {
  cursor: default;
}
.sb-sep {
  width: 1px;
  height: 12px;
  background: var(--pvf-border-strong);
  flex-shrink: 0;
  margin: 0 1px;
}
.sb-spacer {
  flex: 1;
}
.sb-interactive.sb-modified {
  color: var(--pvf-success);
}
.sb-interactive.sb-modified:hover {
  background: var(--pvf-primary-soft);
  color: var(--pvf-success);
}
.sb-interactive.sb-version {
  color: var(--pvf-primary);
}
.sb-interactive.sb-version:hover {
  background: var(--pvf-primary-soft);
  color: var(--pvf-primary-hover);
}
.sb-interactive.sb-version-muted {
  color: var(--pvf-text-faint);
}
.sb-interactive.sb-version-muted:hover {
  color: var(--pvf-text-secondary);
  background: var(--pvf-surface-hover);
}
.sb-version-loading {
  color: var(--pvf-primary);
}
.unpack {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.index-state {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.sb-interactive.sb-error {
  color: var(--pvf-error);
}
.sb-interactive.sb-error:hover {
  background: var(--pvf-surface-error);
  color: var(--pvf-error);
}
.spin-icon {
  animation: sb-spin 1.2s linear infinite;
}
@keyframes sb-spin {
  from {
    transform: rotate(0deg);
  }
  to {
    transform: rotate(360deg);
  }
}
.sb-tooltip-content {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.sb-tooltip-action {
  font-size: 10px;
  opacity: 0.75;
  border-top: 1px solid rgba(255, 255, 255, 0.12);
  padding-top: 4px;
  margin-top: 2px;
}
</style>
