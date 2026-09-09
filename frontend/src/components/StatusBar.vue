<script setup lang="ts">
import { computed } from "vue";
import { NProgress, NText } from "naive-ui";
import { useArchiveStore } from "../stores/archive";
import { useEditorStore } from "../stores/editor";
import { useImageStore } from "../stores/images";
import { useVersionStore } from "../stores/version";

const archive = useArchiveStore();
const editor = useEditorStore();
const images = useImageStore();
const version = useVersionStore();

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
</script>

<template>
  <div class="statusbar">
    <span class="sb-item sb-path" :title="archive.info?.path">
      {{ archive.info?.path || "未打开归档" }}
    </span>
    <template v-if="archive.open">
      <span class="sb-sep" />
      <span class="sb-item">{{ archive.info?.fileCount?.toLocaleString() }} 文件</span>
      <span class="sb-sep" />
      <span class="sb-item">{{ archive.info?.groupCount?.toLocaleString() }} 块</span>
      <template v-if="archive.modifiedCount > 0">
        <span class="sb-sep" />
        <span class="sb-item sb-modified">{{ archive.modifiedCount }} 个已修改</span>
      </template>
      <template v-if="version.status.loading">
        <span class="sb-sep" />
        <span class="sb-item sb-version">版本控制加载中</span>
      </template>
      <template v-else-if="version.enabled">
        <span class="sb-sep" />
        <span class="sb-item sb-version" :title="version.status.headMessage">
          {{ version.status.branch }} · {{ version.status.changedFiles }} 个版本变更
        </span>
      </template>
      <span class="sb-spacer" />
      <span v-if="archive.open" class="sb-item index-state" :title="indexTimingTitle" :class="{ 'sb-error': archive.indexStatus.state === 'error' }">
        {{ indexStateLabel }}
        <NProgress
          v-if="archive.indexing"
          type="line"
          :show-indicator="false"
          :percentage="indexPct"
          style="width: 90px; display: inline-flex"
        />
      </span>
      <span v-if="archive.unpacking" class="sb-item unpack">
        解包中
        <NProgress
          type="line"
          :show-indicator="false"
          :percentage="unpackPct"
          style="width: 90px; display: inline-flex"
        />
      </span>
      <template v-if="currentPath">
        <span class="sb-sep" />
        <span class="sb-item sb-cur" :title="currentPath">{{ currentPath }}</span>
        <span class="sb-sep" />
        <span class="sb-item">{{ sizeText }}</span>
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
  gap: 10px;
  font-size: 11px;
  color: rgba(128, 128, 128, 0.9);
  border-top: 1px solid rgba(128, 128, 128, 0.2);
  flex-shrink: 0;
  white-space: nowrap;
  overflow: hidden;
}
.sb-item {
  overflow: hidden;
  text-overflow: ellipsis;
  flex-shrink: 0;
}
.sb-path {
  max-width: 340px;
}
.sb-cur {
  max-width: 320px;
}
.sb-sep {
  width: 1px;
  height: 12px;
  background: rgba(128, 128, 128, 0.3);
  flex-shrink: 0;
}
.sb-spacer {
  flex: 1;
}
.sb-modified {
  color: #63e2b7;
}
.sb-version {
  color: #8ab4ff;
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
  cursor: help;
}
.sb-error {
  color: #e88080;
}
</style>
