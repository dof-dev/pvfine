<script setup lang="ts">
import { computed, h, ref, watch } from "vue";
import {
  NAlert, NButton, NFormItem, NIcon, NInput, NModal, NRadioButton,
  NRadioGroup, NSelect, NSpace, NSpin, NSwitch, NText, NTooltip, NTree, type TreeOption,
} from "naive-ui";
import { ArrowDownload20Regular, ArrowClockwise20Regular } from "@vicons/fluent";
import { useExportStore } from "../stores/export";
import { buildExportTree } from "../utils/export-tree";

const exporter = useExportStore();
const options = computed(() => exporter.formats.map((format) => ({ label: format.name, value: format.id })));
const treeData = computed(() => buildExportTree(exporter.preview?.files ?? []));
const expandedKeys = ref<Array<string | number>>([]);
watch(() => exporter.preview?.id, () => {
  expandedKeys.value = treeData.value.filter((node) => !node.isLeaf).map((node) => node.key!);
}, { immediate: true });
function renderLabel({ option }: { option: TreeOption }) {
  return h("span", { title: String(option.path ?? option.label), class: "export-tree-label" },
    `${option.label}${option.required ? "（必需）" : ""}`);
}
</script>

<template>
  <NModal
    :show="exporter.visible"
    preset="card"
    :title="exporter.title"
    class="export-modal"
    :style="{ width: '600px', maxWidth: 'calc(100vw - 32px)', maxHeight: 'calc(100vh - 48px)' }"
    :content-style="{ overflowY: 'auto', minHeight: 0 }"
    :z-index="3000"
    :closable="!exporter.running"
    :mask-closable="!exporter.running"
    :close-on-esc="!exporter.running"
    @update:show="(show: boolean) => { if (!show) exporter.close(); }"
  >
    <NSpace vertical :size="16">
      <NRadioGroup v-model:value="exporter.mode" :disabled="exporter.running">
        <NRadioButton value="direct">直接导出</NRadioButton>
        <NRadioButton value="mod">导出为 mod</NRadioButton>
      </NRadioGroup>
      <template v-if="exporter.mode === 'mod'">
        <NFormItem label="导出格式" :show-feedback="false">
          <NSelect v-model:value="exporter.format" :options="options" :disabled="exporter.running" />
        </NFormItem>
        <div class="export-metadata">
          <NFormItem
            label="mod 名称"
            :validation-status="exporter.nameError && exporter.name ? 'error' : undefined"
            :feedback="exporter.name ? exporter.nameError : ''"
            :show-feedback="!!exporter.name && !!exporter.nameError"
          >
            <NInput v-model:value="exporter.name" placeholder="mod 名称" :disabled="exporter.running" />
          </NFormItem>
          <NFormItem
            label="mod 版本号"
            :validation-status="exporter.versionError ? 'error' : undefined"
            :feedback="exporter.versionError"
            :show-feedback="!!exporter.versionError"
          >
            <NInput v-model:value="exporter.version" placeholder="1.0" :disabled="exporter.running" />
          </NFormItem>
        </div>
        <div class="export-dependencies">
          <NText>补齐文字表与物品列表依赖</NText>
          <NSwitch v-model:value="exporter.includeDependencies" :disabled="exporter.running" />
        </div>
      </template>
      <NSpin :show="exporter.preparing">
        <div class="export-summary">
          <NText v-if="exporter.preview">
            {{ exporter.selectedCount }} / {{ exporter.preview.fileCount }} 个文件
            <template v-if="exporter.selectedDependencyCount">，补齐 {{ exporter.selectedDependencyCount }} 个依赖条目</template>
          </NText>
          <NText v-else depth="3">{{ exporter.preparing ? "准备中" : "尚无导出预览" }}</NText>
          <NTooltip>
            <template #trigger>
              <NButton circle quaternary size="small" aria-label="重新准备导出" :disabled="exporter.running || exporter.preparing || !!exporter.nameError || !!exporter.versionError" @click="exporter.refresh()">
                <template #icon><NIcon><ArrowClockwise20Regular /></NIcon></template>
              </NButton>
            </template>
            重新准备导出
          </NTooltip>
        </div>
      </NSpin>
      <template v-if="exporter.preview">
        <NText v-if="exporter.mode === 'mod'" depth="3">{{ exporter.name }}/</NText>
        <NTree
          checkable cascade check-strategy="child" block-line virtual-scroll
          :animated="false"
          :data="treeData"
          :checked-keys="exporter.checkedKeys"
          :expanded-keys="expandedKeys"
          :disabled="exporter.running || exporter.preparing"
          :render-label="renderLabel"
          :selectable="false"
          class="export-preview-tree"
          @update:checked-keys="exporter.selectFiles"
          @update:expanded-keys="(keys: Array<string | number>) => { expandedKeys = keys; }"
        />
        <NAlert v-if="exporter.includeDependencies && exporter.excludedPaths.length && exporter.mode === 'mod'" type="warning">
          已移除文件不会自动补回；缺少依赖可能影响 mod 加载。
        </NAlert>
      </template>
      <NAlert v-for="warning in exporter.preview?.warnings ?? []" :key="warning" type="warning">{{ warning }}</NAlert>
      <NAlert v-if="exporter.error" type="error">{{ exporter.error }}</NAlert>
    </NSpace>
    <template #footer>
      <NSpace justify="end">
        <NButton :disabled="exporter.running" @click="exporter.close()">取消</NButton>
        <NButton type="primary" :loading="exporter.running" :disabled="!exporter.canExport" @click="exporter.execute()">
          <template #icon><NIcon><ArrowDownload20Regular /></NIcon></template>
          导出
        </NButton>
      </NSpace>
    </template>
  </NModal>
</template>

<style scoped>
.export-dependencies, .export-summary {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.export-summary { min-height: 36px; }
.export-summary :deep(.n-text) { overflow-wrap: anywhere; }
.export-dependencies :deep(.n-text) { min-width: 0; flex: 1; }
.export-metadata { display: grid; grid-template-columns: minmax(0, 2fr) minmax(0, 1fr); gap: 12px; }
@media (max-width: 480px) {
  .export-metadata { grid-template-columns: minmax(0, 1fr); }
}
.export-modal :deep(.n-card__content) { min-height: 0; overflow-y: auto; }
.export-preview-tree { height: 240px; max-height: 32vh; min-height: 100px; }
.export-preview-tree :deep(.n-tree-node-content) { min-width: 0; overflow: hidden; }
.export-tree-label { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
