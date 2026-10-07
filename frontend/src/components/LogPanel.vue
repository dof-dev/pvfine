<script setup lang="ts">
import { nextTick, ref, watch } from "vue";
import { NButton, NCheckbox, NCheckboxGroup, NIcon, NTooltip } from "naive-ui";
import { ChevronDown20Regular, ChevronUp20Regular, Delete20Regular } from "@vicons/fluent";
import { logLevels, useLogStore } from "../stores/logs";

const logs = useLogStore();
const viewport = ref<HTMLElement | null>(null);
let scrollRevision = 0;

async function scrollToLatest() {
  const revision = ++scrollRevision;
  await nextTick();
  if (revision === scrollRevision && logs.follow && viewport.value) {
    viewport.value.scrollTop = viewport.value.scrollHeight;
  }
}
watch(() => [logs.filtered, logs.expanded, logs.follow], () => {
  if (logs.expanded && logs.follow) void scrollToLatest();
});

function onScroll() {
  const element = viewport.value;
  if (element) logs.follow = element.scrollHeight - element.scrollTop - element.clientHeight < 24;
}

function timeText(timestamp: string): string {
  const date = new Date(timestamp);
  if (!Number.isFinite(date.getTime())) return timestamp;
  return `${date.toLocaleTimeString("zh-CN", { hour12: false })}.${String(date.getMilliseconds()).padStart(3, "0")}`;
}
</script>

<template>
  <section class="log-panel" :class="{ 'log-panel--expanded': logs.expanded }" aria-label="日志">
    <div class="log-toolbar">
      <button class="log-toggle" :aria-expanded="logs.expanded" aria-controls="app-log-content"
        @click="logs.expanded = !logs.expanded">
        <NIcon size="16"><ChevronDown20Regular v-if="logs.expanded" /><ChevronUp20Regular v-else /></NIcon>
        <span>日志</span>
        <span class="log-count">{{ logs.entries.length }}</span>
        <span v-if="logs.errorCount" class="log-errors">{{ logs.errorCount }} ERR</span>
      </button>
      <div v-if="logs.expanded" class="log-controls">
        <NCheckboxGroup v-model:value="logs.levels" aria-label="日志等级筛选">
          <NCheckbox v-for="level in logLevels" :key="level" :value="level" :label="level === 'ERROR' ? 'ERR' : level" />
        </NCheckboxGroup>
        <NCheckbox v-model:checked="logs.follow">跟随</NCheckbox>
        <NTooltip>
          <template #trigger>
            <NButton quaternary circle size="tiny" aria-label="清空日志" :disabled="!logs.entries.length" @click="logs.clear">
              <template #icon><NIcon><Delete20Regular /></NIcon></template>
            </NButton>
          </template>
          清空日志
        </NTooltip>
      </div>
    </div>
    <div v-if="logs.expanded" id="app-log-content" ref="viewport" class="log-content"
      role="region" aria-label="日志记录" tabindex="0" @scroll="onScroll">
      <div v-if="!logs.filtered.length" class="log-empty">
        {{ logs.entries.length ? "当前等级下暂无日志" : "暂无日志" }}
      </div>
      <div v-for="entry in logs.filtered" :key="entry.id" class="log-line" :class="`log-line--${entry.level.toLowerCase()}`">
        <time :datetime="entry.timestamp" :title="entry.timestamp">{{ timeText(entry.timestamp) }}</time>
        <span class="log-level">{{ entry.level === "ERROR" ? "ERR" : entry.level }}</span>
        <span class="log-message"><span class="log-source">[{{ entry.source }}]</span> {{ entry.message }}</span>
      </div>
    </div>
  </section>
</template>

<style scoped>
.log-panel { flex: 0 0 auto; min-width: 0; border-top: 1px solid var(--pvf-border-normal); background: var(--pvf-surface-panel); }
.log-panel--expanded { height: 240px; max-height: 45vh; display: flex; flex-direction: column; }
.log-toolbar { flex: 0 0 auto; min-height: 34px; display: flex; align-items: center; justify-content: space-between; gap: 8px; padding: 2px 10px; flex-wrap: wrap; }
.log-toggle { display: flex; align-items: center; gap: 6px; border: 0; background: transparent; color: var(--pvf-text-secondary); font: inherit; cursor: pointer; padding: 4px 0; }
.log-toggle:focus-visible { outline: 1px solid var(--pvf-primary); outline-offset: 2px; }
.log-count, .log-source, time { color: var(--pvf-text-muted); }
.log-errors { color: var(--pvf-error); font-size: 11px; }
.log-controls { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
.log-controls :deep(.n-checkbox-group) { display: flex; gap: 10px; flex-wrap: wrap; }
.log-controls :deep(.n-checkbox__label) { font-size: 11px; }
.log-content { flex: 1; min-height: 0; overflow: auto; padding: 5px 10px; font-family: Consolas, "Microsoft YaHei", monospace; font-size: 12px; line-height: 1.65; user-select: text; -webkit-user-select: text; }
.log-content * { user-select: text; -webkit-user-select: text; }
.log-line { display: grid; grid-template-columns: 96px 42px minmax(0, 1fr); gap: 8px; padding: 2px 0; align-items: baseline; }
.log-message { white-space: pre-wrap; overflow-wrap: anywhere; min-width: 0; }
.log-level { font-size: 11px; }
.log-line--debug { color: var(--pvf-text-muted); }
.log-line--info .log-level { color: var(--pvf-info); }
.log-line--warn { color: var(--pvf-warning); }
.log-line--error { color: var(--pvf-error); }
.log-empty { color: var(--pvf-text-muted); padding: 14px 0; }
</style>
