<script setup lang="ts">
import { computed, h, ref, watch, type VNodeChild } from "vue";
import {
  NAlert,
  NAutoComplete,
  NButton,
  NDataTable,
  NEmpty,
  NIcon,
  NInput,
  NModal,
  NRadioButton,
  NRadioGroup,
  NSpin,
  NTag,
  NText,
  useMessage,
  type DataTableColumns,
} from "naive-ui";
import { Add24Regular, Search24Regular } from "@vicons/fluent";
import { ArchiveService } from "../../bindings/pvfine/services";
import {
  useAdvancedSearchStore,
  type AdvancedSearchItem,
} from "../stores/advancedSearch";
import type {
  AdvancedSearchDetail,
  TreeNode,
} from "../../bindings/pvfine/services/models";
import { useEditorStore } from "../stores/editor";
import { useFileSetStore, type FileSetEntry } from "../stores/fileSets";

const search = useAdvancedSearchStore();
const editor = useEditorStore();
const fileSets = useFileSetStore();
const message = useMessage();
const expandedRowKeys = ref<string[]>([]);
const directoryOptions = ref<string[]>([]);
const directorySuggesting = ref(false);
const addingResults = ref(false);
let directoryRequest = 0;

const modeOptions = [
  { label: "字符串", value: "string" },
  { label: "二进制", value: "binary" },
];

const stringMatchOptions = [
  { label: "普通文本", value: "text" },
  { label: "正则", value: "regex" },
];

const queryPlaceholder = computed(() =>
  search.mode === "binary"
    ? "输入脚本片段，例如 [name]\\n`alpha`"
    : search.regexEnabled
      ? "输入 RE2 正则，例如 ^烈火.*项链$"
      : "输入字符串池关键词"
);

watch(
  () => search.hits,
  (hits) => {
    expandedRowKeys.value = hits
      .filter((row) => row.details.length > 0)
      .map((row) => row.key);
  },
  { deep: true, immediate: true }
);

const columns: DataTableColumns<AdvancedSearchItem> = [
  {
    type: "expand",
    expandable: (row) => row.details.length > 0,
    renderExpand: renderDetails,
  },
  {
    title: "路径",
    key: "path",
    ellipsis: { tooltip: true },
  },
  {
    title: "名称",
    key: "name",
    width: 220,
    ellipsis: { tooltip: true },
    render: (row) => row.name || "—",
  },
];

function typeLabel(dataType: number): string {
  if (dataType === 1) return "script";
  if (dataType === 3) return "text";
  return `type ${dataType}`;
}

function sizeText(size: number): string {
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
  return `${(size / 1024 / 1024).toFixed(1)} MB`;
}

function renderDetails(row: AdvancedSearchItem): VNodeChild {
  return h("div", {
    class: "advanced-search-details",
    onClick: (event: MouseEvent) => event.stopPropagation(),
  }, [
    ...row.details.map((detail, index) => renderDetail(detail, index)),
  ]);
}

function renderDetail(detail: AdvancedSearchDetail, index: number): VNodeChild {
  if (detail.kind === "binary") {
    const lines: VNodeChild[] = [
      h("span", { class: "advanced-search-detail-label" }, `第 ${index + 1} 项`),
      h("span", { class: "advanced-search-detail-muted" }, `命中 ${detail.occurrences} 次`),
    ];
    if (detail.hex) {
      lines.push(h("code", { class: "advanced-search-detail-hit advanced-search-detail-code" }, detail.hex));
    }
    if (detail.byteOffsets?.length) {
      lines.push(
        h("span", { class: "advanced-search-detail-muted" }, `字节偏移: ${detail.byteOffsets.join(", ")}`)
      );
    }
    if (detail.tokenOffsets?.length) {
      lines.push(
        h("span", { class: "advanced-search-detail-muted" }, `token 偏移: ${detail.tokenOffsets.join(", ")}`)
      );
    }
    if (detail.offsetsTruncated) {
      lines.push(h("span", { class: "advanced-search-detail-muted" }, "仅展示前 32 个偏移"));
    }
    return h("div", { class: "advanced-search-detail" }, lines);
  }

  const metadata = [
    detail.pool ?? "未知池",
    `offset ${detail.poolOffset ?? "-"}`,
    `引用 ${detail.occurrences} 次`,
  ].join(" · ");
  const sources = [
    detail.tokenTypes?.length ? `token ${detail.tokenTypes.join(", ")}` : "",
    detail.fileFields?.length ? `文件字段 ${detail.fileFields.join(", ")}` : "",
  ]
    .filter(Boolean)
    .join(" · ");

  return h("div", { class: "advanced-search-detail" }, [
    h("span", { class: "advanced-search-detail-label" }, `第 ${index + 1} 项`),
    h("span", { class: "advanced-search-detail-hit advanced-search-detail-value" }, `「${detail.value ?? ""}」`),
    h("span", { class: "advanced-search-detail-muted" }, metadata),
    h("span", { class: "advanced-search-detail-muted" }, sources || "无引用来源信息"),
  ]);
}

function rowProps(row: AdvancedSearchItem) {
  return {
    class: "advanced-search-row",
    onDblclick: () => {
      void openFile(row);
    },
  };
}

function onExpandedRowKeys(keys: Array<string | number>): void {
  expandedRowKeys.value = keys.map(String);
}

async function suggestDirectories(prefix: string): Promise<void> {
  const request = ++directoryRequest;
  const value = prefix.trim();
  if (!value) {
    directoryOptions.value = [];
    return;
  }
  directorySuggesting.value = true;
  try {
    const paths = await ArchiveService.SuggestDirectories(value, 50);
    if (request === directoryRequest) directoryOptions.value = paths ?? [];
  } catch {
    if (request === directoryRequest) directoryOptions.value = [];
  } finally {
    if (request === directoryRequest) directorySuggesting.value = false;
  }
}

function onScopePathUpdate(value: string): void {
  search.scopePath = value;
  void suggestDirectories(value);
}

function onScopePathSelect(value: string): void {
  search.scopePath = value;
}

async function openFile(row: AdvancedSearchItem): Promise<void> {
  await editor.openFile(row.fileIndex);
  search.close();
}

function serviceEntry(node: TreeNode): FileSetEntry {
  const names = [
    ...new Set((node.tags ?? []).map((tag) => tag.name.trim()).filter(Boolean)),
  ];
  return {
    fileIndex: node.fileIndex,
    path: node.path,
    name: names.join(" / ") || node.name || node.path.slice(node.path.lastIndexOf("/") + 1),
    ids: [...new Set((node.tags ?? []).map((tag) => tag.id).filter(Boolean))],
    size: node.size,
    dataType: node.dataType,
    icon: node.icon ?? null,
    fieldImage: node.fieldImage ?? null,
  };
}

async function addAllResults(): Promise<void> {
  if (!search.hasResults || search.searching || search.stale || addingResults.value) return;
  const session = fileSets.sessionId;
  addingResults.value = true;
  try {
    const results = await search.loadAll();
    if (!results || session !== fileSets.sessionId || search.stale) return;
    const nodes = await ArchiveService.ResolveFiles(results.map((row) => row.path));
    if (session !== fileSets.sessionId || search.stale) return;
    const entries = (nodes ?? [])
      .filter((node): node is TreeNode => !!node && !node.isDir && node.fileIndex >= 0)
      .map(serviceEntry);
    const result = fileSets.addEntries(entries);
    if (result.added === 0 && result.skipped === 0) {
      message.info("当前搜索没有可加入的文件");
      return;
    }
    const duplicateText = result.skipped > 0 ? `，跳过 ${result.skipped} 个重复项` : "";
    message.success(`已加入 ${result.added} 个文件${duplicateText}`);
  } catch (error: any) {
    if (session === fileSets.sessionId) {
      message.error(`加入文件集失败: ${error?.message ?? error}`);
    }
  } finally {
    addingResults.value = false;
  }
}

function onKeydown(event: KeyboardEvent): void {
  if (event.key !== "Enter" || (!event.metaKey && !event.ctrlKey)) return;
  event.preventDefault();
  void search.search();
}
</script>

<template>
  <NModal
    :show="search.visible"
    preset="card"
    title="高级搜索"
    :mask-closable="true"
    :close-on-esc="true"
    :style="{ width: 'min(920px, calc(100vw - 48px))' }"
    @update:show="search.visible = $event"
  >
    <div class="advanced-search-modal">
      <div class="advanced-search-toolbar">
        <NRadioGroup v-model:value="search.mode" size="small">
          <NRadioButton v-for="option in modeOptions" :key="option.value" :value="option.value">
            {{ option.label }}
          </NRadioButton>
        </NRadioGroup>

        <NRadioGroup v-if="search.mode === 'string'" v-model:value="search.stringMatch" size="small">
          <NRadioButton v-for="option in stringMatchOptions" :key="option.value" :value="option.value">
            {{ option.label }}
          </NRadioButton>
        </NRadioGroup>
      </div>

      <div class="advanced-search-inputs">
        <NInput
          v-model:value="search.query"
          :type="search.mode === 'binary' || search.regexEnabled ? 'textarea' : 'text'"
          :autosize="search.mode === 'binary' || search.regexEnabled ? { minRows: 2, maxRows: 6 } : false"
          :placeholder="queryPlaceholder"
          clearable
          @keydown="onKeydown"
        />
        <NAutoComplete
          :value="search.scopePath"
          :options="directoryOptions"
          placeholder="目录范围，可选，例如 equipment/character"
          clearable
          :loading="directorySuggesting"
          @update:value="onScopePathUpdate"
          @select="onScopePathSelect"
          @focus="suggestDirectories(search.scopePath)"
        />
      </div>

      <NAlert v-if="search.stale" type="warning" :show-icon="false" class="advanced-search-alert">
        归档内容已变化，当前结果可能已过期，请重新搜索。
      </NAlert>
      <NAlert v-if="search.error" type="error" :show-icon="false" class="advanced-search-alert">
        {{ search.error }}
      </NAlert>

      <div class="advanced-search-meta">
        <NText depth="3">命中 {{ search.hits.length.toLocaleString() }} 个文件</NText>
        <NTag v-if="search.indexStatus.state === 'building'" size="small" type="info" :bordered="false">
          正在构建字符串索引，首次搜索可能慢一些
        </NTag>
        <NTag v-else-if="search.stale" size="small" type="warning" :bordered="false">结果已过期</NTag>
      </div>

      <NSpin :show="search.searching">
        <div class="advanced-search-results">
          <NDataTable
            v-if="search.hasResults"
            :columns="columns"
            :data="search.hits"
            :row-key="(row: AdvancedSearchItem) => row.key"
            :row-props="rowProps"
            :expanded-row-keys="expandedRowKeys"
            :on-update:expanded-row-keys="onExpandedRowKeys"
            :max-height="460"
            virtual-scroll
            size="small"
            :bordered="false"
          />
          <NEmpty v-else description="暂无搜索结果" size="small" />
        </div>
      </NSpin>

      <div class="advanced-search-footer">
        <NButton quaternary @click="search.clear">清空</NButton>
        <div class="advanced-search-footer-actions">
          <NButton
            quaternary
            :disabled="!search.hasResults || search.searching || search.stale"
            :loading="addingResults"
            @click="addAllResults"
          >
            <template #icon><NIcon><Add24Regular /></NIcon></template>
            全部加入文件集
          </NButton>
          <NButton v-if="search.nextCursor >= 0" quaternary :loading="search.searching" @click="search.loadMore">
            加载更多
          </NButton>
          <NButton type="primary" :loading="search.searching" @click="search.search">
            <template #icon><NIcon><Search24Regular /></NIcon></template>
            搜索
          </NButton>
        </div>
      </div>
    </div>
  </NModal>
</template>

<style scoped>
.advanced-search-modal {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.advanced-search-toolbar,
.advanced-search-footer,
.advanced-search-footer-actions,
.advanced-search-meta {
  display: flex;
  align-items: center;
}
.advanced-search-toolbar,
.advanced-search-footer {
  justify-content: space-between;
  gap: 12px;
}
.advanced-search-inputs {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(220px, 0.42fr);
  gap: 8px;
}
.advanced-search-meta {
  justify-content: space-between;
  min-height: 22px;
}
.advanced-search-results {
  min-height: 180px;
  border: 1px solid var(--pvf-border-normal);
}
.advanced-search-results :deep(.n-empty) {
  padding: 64px 0;
}
.advanced-search-results :deep(.advanced-search-row) {
  cursor: pointer;
}
.advanced-search-results :deep(.advanced-search-row:hover) {
  background: var(--pvf-surface-hover);
}
:deep(.advanced-search-details) {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 8px 16px 10px 48px;
  background: var(--pvf-surface-inset);
}
:deep(.advanced-search-details-title) {
  color: var(--pvf-text-secondary);
  font-size: 12px;
  font-weight: 600;
}
:deep(.advanced-search-detail) {
  display: flex;
  align-items: baseline;
  flex-wrap: wrap;
  gap: 8px;
  min-width: 0;
  font-size: 12px;
}
:deep(.advanced-search-detail-label) {
  color: var(--pvf-text-muted);
  flex: 0 0 auto;
}
:deep(.advanced-search-detail-value) {
  color: var(--pvf-success);
  max-width: 360px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
:deep(.advanced-search-detail-hit) {
  font-weight: 650;
  text-shadow: 0 0 12px var(--pvf-effect-success-glow);
}
:deep(.advanced-search-detail-muted) {
  color: var(--pvf-text-muted);
}
:deep(.advanced-search-detail-code) {
  padding: 2px 5px;
  color: var(--pvf-warning);
  background: var(--pvf-surface-warning);
  border-radius: 3px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 11px;
}
:deep(.advanced-search-detail-code.advanced-search-detail-hit) {
  box-shadow: inset 0 0 0 1px var(--pvf-border-subtle);
}
.advanced-search-alert {
  margin-top: -4px;
}
.advanced-search-footer-actions {
  gap: 8px;
}

@media (max-width: 720px) {
  .advanced-search-inputs {
    grid-template-columns: 1fr;
  }
  .advanced-search-toolbar {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
