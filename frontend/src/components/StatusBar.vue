<script setup lang="ts">
import { computed } from "vue";
import { NProgress, NText } from "naive-ui";
import { useArchiveStore } from "../stores/archive";
import { useEditorStore } from "../stores/editor";

const archive = useArchiveStore();
const editor = useEditorStore();

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
</script>

<template>
  <div class="statusbar">
    <span class="sb-item sb-path" :title="archive.info?.path">
      {{ archive.info?.path || "未打开归档" }}
    </span>
    <template v-if="archive.open">
      <span class="sb-sep" />
      <span class="sb-item">{{ archive.info?.fileCount.toLocaleString() }} 文件</span>
      <span class="sb-sep" />
      <span class="sb-item">{{ archive.info?.groupCount.toLocaleString() }} 块</span>
      <template v-if="archive.modifiedCount > 0">
        <span class="sb-sep" />
        <span class="sb-item sb-modified">{{ archive.modifiedCount }} 个已修改</span>
      </template>
      <span class="sb-spacer" />
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
.unpack {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
</style>
