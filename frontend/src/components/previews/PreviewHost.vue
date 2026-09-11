<script setup lang="ts">
import { computed } from "vue";
import { Dismiss24Regular } from "@vicons/fluent";
import { NButton, NEmpty, NIcon, NTooltip } from "naive-ui";
import { getPreviewProvider } from "../../previews/registry";
import type { PreviewFile } from "../../previews/types";

const props = defineProps<{
  file: PreviewFile;
  active: boolean;
  open: boolean;
}>();

const emit = defineEmits<{
  (event: "close"): void;
}>();

const provider = computed(() => getPreviewProvider(props.file));
const gameTooltip = computed(() => provider.value?.chrome === "game-tooltip");
</script>

<template>
  <div v-if="open" class="preview-host" :class="{ 'preview-host--game-tooltip': gameTooltip }">
    <div v-if="!gameTooltip" class="preview-host-header">
      <div class="preview-host-title">
        <span class="preview-host-title-dot" />
        <span>{{ provider?.label ?? "文件预览" }}</span>
      </div>
      <NTooltip trigger="hover">
        <template #trigger>
          <NButton
            quaternary
            size="tiny"
            aria-label="收起预览"
            @click="emit('close')"
          >
            <template #icon><NIcon :size="14"><Dismiss24Regular /></NIcon></template>
          </NButton>
        </template>
        收起预览
      </NTooltip>
    </div>

    <div class="preview-host-body" :class="{ 'preview-host-body--game-tooltip': gameTooltip }">
      <component
        :is="provider.component"
        v-if="provider && file.editable"
        :file="file"
        :active="active"
      />
      <NEmpty
        v-else-if="provider && !file.editable"
        size="small"
        description="当前文件不是可解析的文本类型"
      />
      <NEmpty v-else size="small" description="当前文件暂无预览" />
    </div>
  </div>
</template>

<style scoped>
.preview-host {
  position: absolute;
  z-index: 30;
  top: 8px;
  right: 8px;
  display: flex;
  flex-direction: column;
  width: 33.3333%;
  max-width: calc(100% - 16px);
  max-height: calc(100% - 16px);
  overflow: hidden;
  color: var(--pvf-text-primary);
  background: var(--pvf-surface-elevated);
  background: color-mix(in srgb, var(--pvf-surface-elevated) 68%, transparent);
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
  border: 1px solid var(--pvf-border-normal);
  border-radius: 8px;
  box-shadow: 0 10px 28px var(--pvf-effect-tooltip-shadow);
}
.preview-host--game-tooltip {
  width: 300px;
  flex: 0 0 300px;
  background: rgba(0, 0, 0, 0.74);
  border: 0;
  border-radius: 0;
  box-shadow: none;
  backdrop-filter: blur(4px);
  -webkit-backdrop-filter: blur(4px);
}
.preview-host-header {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  justify-content: space-between;
  min-height: 30px;
  padding: 2px 4px 2px 10px;
  border-bottom: 1px solid var(--pvf-border-subtle);
  background: var(--pvf-surface-subtle);
}
.preview-host-title {
  display: inline-flex;
  align-items: center;
  min-width: 0;
  gap: 6px;
  color: var(--pvf-text-secondary);
  font-size: 12px;
  font-weight: 600;
}
.preview-host-title-dot {
  width: 7px;
  height: 7px;
  flex: 0 0 auto;
  border-radius: 50%;
  background: var(--pvf-primary);
  box-shadow: 0 0 8px var(--pvf-effect-focus-ring);
}
.preview-host-body {
  display: flex;
  flex: 1 1 auto;
  min-height: 0;
  overflow: hidden;
}
.preview-host-body--game-tooltip {
  overflow: auto;
}
</style>
