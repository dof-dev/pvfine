<script setup lang="ts">
import { computed, h, ref, watch, type VNodeChild } from "vue";
import { NTag, NTree, type TreeOption } from "naive-ui";
import { useArchiveStore } from "../stores/archive";
import type { TreeItem } from "../stores/explorer";

const props = withDefaults(
  defineProps<{
    items: TreeItem[];
    height?: string;
    expandAll?: boolean;
    loadChildren?: (item: TreeItem) => Promise<void>;
  }>(),
  { height: "100%", expandAll: false }
);

const emit = defineEmits<{
  select: [item: TreeItem];
}>();

const archive = useArchiveStore();

type FileTreeNode = TreeOption & {
  treeItem: TreeItem;
  children?: FileTreeNode[];
};

function toOption(item: TreeItem): FileTreeNode {
  return {
    key: item.key,
    label: item.label,
    treeItem: item,
    isLeaf: item.isLeaf,
    children: item.children ? item.children.map(toOption) : undefined,
  };
}

const treeData = computed(() => props.items.map(toOption));
const itemsByKey = computed(() => {
  const result = new Map<string, TreeItem>();

  function register(items: TreeItem[]) {
    for (const item of items) {
      result.set(item.key, item);
      if (item.children) register(item.children);
    }
  }

  register(props.items);
  return result;
});

const expandedKeys = ref<Array<string | number>>([]);

function collectExpandedKeys(items: TreeItem[], result: Array<string | number>): void {
  for (const item of items) {
    if (!item.isDir) continue;
    result.push(item.key);
    if (item.children) collectExpandedKeys(item.children, result);
  }
}

watch(
  () => [props.expandAll, props.items] as const,
  () => {
    if (!props.expandAll) {
      expandedKeys.value = [];
      return;
    }
    const next: Array<string | number> = [];
    collectExpandedKeys(props.items, next);
    expandedKeys.value = next;
  },
  { immediate: true }
);

async function onLoad(node: TreeOption): Promise<void> {
  const item = (node as FileTreeNode).treeItem;
  if (item?.isDir && item.children === null) {
    await props.loadChildren?.(item);
  }
}

function onSelect(keys: string[]): void {
  const item = itemsByKey.value.get(keys[0]);
  if (item && !item.isDir) emit("select", item);
}

function onExpandedKeys(keys: Array<string | number>): void {
  if (props.expandAll) expandedKeys.value = keys;
}

function renderLabel({ option }: { option: TreeOption }): VNodeChild {
  const item = (option as FileTreeNode).treeItem;
  if (!item) return option.label ?? "";

  const children: VNodeChild[] = [
    h("span", { class: "tree-item-name" }, item.label),
  ];
  if (archive.indexReady && !item.isDir) {
    for (const tag of item.tags) {
      if (tag.id) {
        children.push(
          h(
            NTag,
            {
              size: "tiny",
              bordered: false,
              type: "info",
              class: "tree-tag tree-tag-id",
              title: `id: ${tag.id}`,
            },
            { default: () => tag.id }
          )
        );
      }
      if (tag.name) {
        children.push(
          h(
            NTag,
            {
              size: "tiny",
              bordered: false,
              type: "success",
              class: "tree-tag tree-tag-name",
              title: tag.name,
            },
            { default: () => tag.name }
          )
        );
      }
    }
  }
  return h("div", { class: "tree-label" }, children);
}
</script>

<template>
  <NTree
    block-line
    selectable
    :animated="false"
    virtual-scroll
    class="file-tree"
    :data="treeData"
    :expanded-keys="expandAll ? expandedKeys : undefined"
    :on-load="onLoad"
    :on-update:expanded-keys="expandAll ? onExpandedKeys : undefined"
    :on-update:selected-keys="onSelect"
    :render-label="renderLabel"
    :scrollbar-props="{ xScrollable: true }"
    :style="{ height }"
    expand-on-click
  />
</template>

<style scoped>
.file-tree {
  width: 100%;
  min-width: 0;
  min-height: 0;
}

:deep(.n-tree-node) {
  width: max-content;
  min-width: 100%;
}

:deep(.n-tree-node-content) {
  width: max-content;
  min-width: 100%;
}

:deep(.n-tree-node-content__text) {
  flex: 0 0 auto;
  width: max-content;
  min-width: max-content;
  max-width: none;
  overflow: visible;
}

:deep(.tree-label) {
  display: flex;
  align-items: center;
  gap: 4px;
  min-width: max-content;
  width: max-content;
  max-width: none;
  overflow: visible;
  white-space: nowrap;
  vertical-align: middle;
}

:deep(.tree-item-name) {
  flex-shrink: 0;
  min-width: max-content;
  overflow: visible;
  white-space: nowrap;
}

:deep(.tree-tag) {
  flex-shrink: 0;
}

:deep(.tree-tag-name) {
  max-width: none;
}

:deep(.tree-tag-name .n-tag__content) {
  overflow: visible;
  white-space: nowrap;
}
</style>
