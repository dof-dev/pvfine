<script setup lang="ts">
import { computed, ref, watch } from "vue";
import {
  NButton,
  NEmpty,
  NInput,
  NSpin,
  NTag,
  NTree,
  NVirtualList,
  type TreeOption,
} from "naive-ui";
import { useArchiveStore } from "../stores/archive";
import { useExplorerStore, type SearchItem, type TreeItem } from "../stores/explorer";
import { useEditorStore } from "../stores/editor";

const archive = useArchiveStore();
const explorer = useExplorerStore();
const editor = useEditorStore();

const searchInput = ref("");
let searchDebounce: number | undefined;

watch(
  () => [searchInput.value, archive.indexReady] as const,
  ([v, ready]) => {
    window.clearTimeout(searchDebounce);
    if (!ready) return;
    searchDebounce = window.setTimeout(() => explorer.search(v), 300);
  }
);

watch(
  () => archive.info?.path ?? "",
  async (archivePath) => {
    if (archivePath) {
      explorer.reset();
      searchInput.value = "";
      await explorer.loadRoots();
    } else {
      explorer.reset();
    }
  }
);

// n-tree 数据适配(递归映射为 TreeOption,未加载目录 children=undefined)
function toOption(item: TreeItem): NTreeNode {
  return {
    key: item.key,
    label: item.label,
    isLeaf: item.isLeaf,
    children: item.children ? item.children.map(toOption) : undefined,
  };
}
const treeData = computed(() => explorer.roots.map(toOption));

type NTreeNode = { key: string; label: string; isLeaf?: boolean; children?: NTreeNode[] };

async function onTreeLoad(node: TreeOption): Promise<void> {
  if (typeof node.key !== "string") return;
  const item = explorer.getItem(node.key);
  if (item) await explorer.loadChildren(item);
}

function onTreeSelect(keys: string[]) {
  const key = keys[0];
  if (!key) return;
  const item = explorer.getItem(key);
  if (item && !item.isDir) editor.openFile(item.fileIndex);
}

function onHitClick(item: SearchItem) {
  editor.openFile(item.fileIndex);
}

function sizeText(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}

function categoryLabel(category: string): string {
  switch (category) {
    case "equipment":
      return "装备";
    case "stackable":
      return "道具";
    default:
      return "文件";
  }
}
</script>

<template>
  <div class="explorer">
    <div class="exp-search">
      <NInput
        v-model:value="searchInput"
        :placeholder="archive.indexReady ? '搜索路径、名称或 id…' : '索引完成后可搜索路径、名称或 id…'"
        clearable
        size="small"
        :disabled="!archive.open || !archive.indexReady"
      />
    </div>

    <NSpin class="exp-spin" :show="archive.loading || explorer.searching">
      <div class="exp-body">
        <!-- 空态 -->
        <NEmpty
          v-if="!archive.open"
          description="未打开归档"
          class="exp-empty"
          size="small"
        />

        <!-- 搜索结果 -->
        <template v-else-if="explorer.mode === 'search'">
          <div class="search-meta">
            <NTag size="tiny" :bordered="false">命中 {{ explorer.hits.length.toLocaleString() }} 条</NTag>
            <NButton quaternary size="tiny" @click="explorer.clearSearch(); searchInput = ''">
              返回目录
            </NButton>
          </div>
          <NVirtualList
            :items="explorer.hits"
            :item-size="32"
            class="search-list"
            v-if="explorer.hits.length"
          >
            <template #default="{ item }">
              <div class="hit-row" :title="(item as SearchItem).path" @click="onHitClick(item as SearchItem)">
                <NTag size="tiny" :bordered="false">{{ categoryLabel((item as SearchItem).category) }}</NTag>
                <span v-if="(item as SearchItem).id" class="hit-id">{{ (item as SearchItem).id }}</span>
                <span class="hit-name">{{ (item as SearchItem).label }}</span>
                <span class="hit-path">{{ (item as SearchItem).path }}</span>
              </div>
            </template>
          </NVirtualList>
          <NEmpty v-else description="无匹配结果" size="small" class="exp-empty" />
          <div v-if="explorer.nextCursor >= 0" class="load-more">
            <NButton size="tiny" quaternary :loading="explorer.searching" @click="explorer.loadMore()">
              加载更多
            </NButton>
          </div>
        </template>

        <!-- 目录树 -->
        <div v-else class="tree-viewport">
          <NTree
            block-line
            selectable
            :animated="false"
            virtual-scroll
            class="explorer-tree"
            :data="treeData"
            :expanded-keys="undefined"
            :on-load="onTreeLoad"
            :on-update:selected-keys="onTreeSelect"
            style="height: 100%"
            expand-on-click
          />
        </div>
      </div>
    </NSpin>
  </div>
</template>

<style scoped>
.exp-spin,
.exp-spin :deep(.n-spin-container),
.exp-spin :deep(.n-spin-content) {
  flex: 1;
  height: 100%;
  min-width: 0;
  min-height: 0;
}
.exp-spin {
  display: flex;
}
.exp-spin :deep(.n-spin-container) {
  display: flex;
  flex-direction: column;
}
.exp-spin :deep(.n-spin-content) {
  display: flex;
  flex-direction: column;
}
.explorer {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  border-right: 1px solid rgba(128, 128, 128, 0.2);
}
.exp-search {
  padding: 8px;
  flex-shrink: 0;
}
.index-status {
  display: flex;
  align-items: center;
  gap: 6px;
  min-height: 18px;
  margin-top: 4px;
  color: rgba(128, 128, 128, 0.9);
  font-size: 11px;
  line-height: 18px;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.index-status--error {
  color: #e88080;
}
.exp-body {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.tree-viewport {
  flex: 1;
  min-width: 0;
  min-height: 0;
  display: flex;
  overflow: hidden;
}
.explorer-tree,
.search-list {
  flex: 1;
  min-width: 0;
  min-height: 0;
}
.explorer-tree {
  height: 100%;
}
.exp-empty {
  margin-top: 80px;
}
.search-meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 4px 8px;
}
.hit-row {
  height: 32px;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 0 10px;
  cursor: pointer;
  overflow: hidden;
  white-space: nowrap;
}
.hit-row:hover {
  background: rgba(128, 128, 128, 0.15);
}
.hit-name {
  flex-shrink: 0;
  max-width: 180px;
  overflow: hidden;
  text-overflow: ellipsis;
}
.hit-id {
  color: #f2c97d;
  font-variant-numeric: tabular-nums;
  flex-shrink: 0;
}
.hit-path {
  color: rgba(128, 128, 128, 0.7);
  font-size: 11px;
  overflow: hidden;
  text-overflow: ellipsis;
}
.load-more {
  display: flex;
  justify-content: center;
  padding: 6px 0 10px;
}
</style>
