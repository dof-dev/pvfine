<script setup lang="ts">
import { computed, ref, watch } from "vue";
import {
  NButton,
  NEmpty,
  NInput,
  NSpin,
  NTag,
} from "naive-ui";
import { useArchiveStore } from "../stores/archive";
import { useExplorerStore, type SearchItem, type TreeItem } from "../stores/explorer";
import { useEditorStore } from "../stores/editor";
import FileTree from "./FileTree.vue";

const archive = useArchiveStore();
const explorer = useExplorerStore();
const editor = useEditorStore();

const searchInput = ref("");
const searchTreeItems = computed(() => buildSearchTree(explorer.hits));
const visibleTreeItems = computed(() =>
  explorer.mode === "search" ? searchTreeItems.value : explorer.roots
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

function submitSearch(event: KeyboardEvent): void {
  if (event.isComposing || !archive.indexReady) return;
  void explorer.search(searchInput.value);
}

function clearSearch(): void {
  searchInput.value = "";
  explorer.clearSearch();
}

async function onTreeLoad(item: TreeItem): Promise<void> {
  await explorer.loadChildren(item);
}

function onTreeSelect(item: TreeItem): void {
  if (item && !item.isDir) editor.openFile(item.fileIndex);
}

function buildSearchTree(items: SearchItem[]): TreeItem[] {
  const roots: TreeItem[] = [];
  const nodesByPath = new Map<string, TreeItem>();

  for (const item of items) {
    const parts = item.path
      .replaceAll("\\", "/")
      .split("/")
      .filter(Boolean);
    if (parts.length === 0) continue;

    let children = roots;
    let parentPath = "";
    for (const part of parts.slice(0, -1)) {
      const path = parentPath ? `${parentPath}/${part}` : part;
      let directory = nodesByPath.get(path);
      if (!directory) {
        directory = {
          key: path,
          label: part,
          isDir: true,
          isLeaf: false,
          children: [],
          fileIndex: -1,
          size: 0,
          dataType: 0,
          childCount: 0,
          tags: [],
        };
        nodesByPath.set(path, directory);
        children.push(directory);
      }
      children = directory.children ?? [];
      parentPath = path;
    }

    const filePath = parts.join("/");
    let file = nodesByPath.get(filePath);
    if (!file) {
      file = {
        key: filePath,
        label: parts[parts.length - 1],
        isDir: false,
        isLeaf: true,
        children: [],
        fileIndex: item.fileIndex,
        size: item.size,
        dataType: item.dataType,
        childCount: 0,
        tags: [],
      };
      nodesByPath.set(filePath, file);
      children.push(file);
    }

    if (item.category !== "file") {
      file.tags.push({
        id: item.id,
        name: item.label,
        category: item.category,
      });
    }
  }

  sortTree(roots);
  return roots;
}

function sortTree(items: TreeItem[]): void {
  items.sort((a, b) => {
    if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
    return a.label < b.label ? -1 : a.label > b.label ? 1 : 0;
  });
  for (const item of items) {
    if (item.children) sortTree(item.children);
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
        @clear="clearSearch"
        @keydown.enter.prevent="submitSearch"
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

        <template v-else>
          <div v-if="explorer.mode === 'search'" class="search-meta">
            <NTag size="tiny" :bordered="false">命中 {{ explorer.hits.length.toLocaleString() }} 条</NTag>
            <NButton quaternary size="tiny" @click="clearSearch">返回目录</NButton>
          </div>
          <NEmpty
            v-if="explorer.mode === 'search' && !explorer.searching && !searchTreeItems.length"
            description="无匹配结果"
            size="small"
            class="exp-empty"
          />
          <div v-else class="tree-viewport">
            <FileTree
              :key="explorer.mode"
              :items="visibleTreeItems"
              :expand-all="explorer.mode === 'search'"
              :load-children="onTreeLoad"
              @select="onTreeSelect"
            />
          </div>
          <div v-if="explorer.mode === 'search' && explorer.nextCursor >= 0" class="load-more">
            <NButton size="tiny" quaternary :loading="explorer.searching" @click="explorer.loadMore()">
              加载更多
            </NButton>
          </div>
        </template>
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
  overflow: auto;
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
.load-more {
  display: flex;
  justify-content: center;
  padding: 6px 0 10px;
}
</style>
