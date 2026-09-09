<script setup lang="ts">
import { computed, h, nextTick, ref, watch, type VNodeChild } from "vue";
import { NTag, NTree, type TreeInst, type TreeOption } from "naive-ui";
import { useArchiveStore } from "../stores/archive";
import type { TreeItem } from "../stores/explorer";
import type { ExplorerOpenMode } from "../stores/settings";
import ImageThumbnail from "./ImageThumbnail.vue";

const props = withDefaults(
  defineProps<{
    items: TreeItem[];
    height?: string;
    expandAll?: boolean;
    openMode?: ExplorerOpenMode;
    selectedKey?: string | null;
    loadChildren?: (item: TreeItem) => Promise<void>;
  }>(),
  { height: "100%", expandAll: false, openMode: "single-click", selectedKey: null }
);

const emit = defineEmits<{
  open: [item: TreeItem];
  contextmenu: [event: MouseEvent, item: TreeItem | null, items: TreeItem[]];
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
const checkedKeys = ref<string[]>([]);
const treeRef = ref<TreeInst | null>(null);
const directoryToggleKeys = new Set<string>();
const handledClickEvents = new WeakSet<MouseEvent>();
const selectedKeys = computed(() => (props.selectedKey ? [props.selectedKey] : []));

function isTreeControl(event: MouseEvent): boolean {
  const target = event.target;
  return target instanceof Element && !!target.closest("[data-switcher], [data-checkbox]");
}

function collectExpandedKeys(items: TreeItem[], result: Array<string | number>): void {
  for (const item of items) {
    if (!item.isDir) continue;
    result.push(item.key);
    if (item.children) collectExpandedKeys(item.children, result);
  }
}

watch(
  () => props.expandAll,
  (expandAll) => {
    if (!expandAll) {
      expandedKeys.value = [];
      return;
    }
    const next: Array<string | number> = [];
    collectExpandedKeys(props.items, next);
    expandedKeys.value = next;
  },
  { immediate: true }
);

watch(
  () => props.items,
  () => {
    if (props.expandAll) {
      const next: Array<string | number> = [];
      collectExpandedKeys(props.items, next);
      expandedKeys.value = next;
    }
    if (props.selectedKey) void revealSelectedKey(props.selectedKey);
  },
  { deep: true }
);

watch(
  () => props.selectedKey,
  (key) => {
    if (key) void revealSelectedKey(key);
  },
  { immediate: true }
);

function findAncestorKeys(key: string): string[] {
  const parts = key.split("/").filter(Boolean);
  if (parts.length < 2) return [];

  const ancestors: string[] = [];
  let items = props.items;
  for (let index = 0; index < parts.length - 1; index++) {
    const ancestorKey = parts.slice(0, index + 1).join("/");
    const item = items.find((candidate) => candidate.key === ancestorKey);
    if (!item || !item.isDir) return [];
    ancestors.push(item.key);
    items = item.children ?? [];
  }
  return ancestors;
}

async function revealSelectedKey(key: string): Promise<void> {
  const ancestors = findAncestorKeys(key);
  if (ancestors.length > 0) {
    expandedKeys.value = [...new Set([...expandedKeys.value, ...ancestors])];
  }
  await nextTick();
  if (props.selectedKey === key) treeRef.value?.scrollTo({ key, behavior: "smooth" });
}

async function onLoad(node: TreeOption): Promise<void> {
  const item = (node as FileTreeNode).treeItem;
  if (item?.isDir && item.children === null) {
    await props.loadChildren?.(item);
  }
}

function onChecked(keys: Array<string | number>): void {
  checkedKeys.value = keys.map(String);
}

async function toggleDirectory(item: TreeItem): Promise<void> {
  if (!item.isDir || directoryToggleKeys.has(item.key)) return;
  directoryToggleKeys.add(item.key);
  try {
    if (item.children === null) {
      await props.loadChildren?.(item);
      if (item.children === null) return;
    }
    const next = new Set(expandedKeys.value);
    if (next.has(item.key)) next.delete(item.key);
    else next.add(item.key);
    expandedKeys.value = [...next];
  } finally {
    directoryToggleKeys.delete(item.key);
  }
}

function openNode(item: TreeItem): void {
  if (item.isDir) {
    void toggleDirectory(item).catch((error) => {
      console.error("load explorer directory failed", error);
    });
    return;
  }
  emit("open", item);
}

function onNodeClick(event: MouseEvent, item: TreeItem): void {
  // NTree calls nodeProps.onClick once from its own handler and once from the
  // merged DOM handler when block-line is enabled. Handle the same event only once.
  if (handledClickEvents.has(event)) return;
  handledClickEvents.add(event);
  if (isTreeControl(event)) return;
  if (props.openMode !== "single-click") return;
  event.stopPropagation();
  openNode(item);
}

function onNodeDblclick(event: MouseEvent, item: TreeItem): void {
  if (isTreeControl(event)) return;
  if (props.openMode !== "double-click") return;
  event.stopPropagation();
  openNode(item);
}

function onNodeContextMenu(event: MouseEvent, item: TreeItem): void {
  event.preventDefault();
  event.stopPropagation();
  const checkedItems = checkedKeys.value
    .map((selectedKey) => itemsByKey.value.get(selectedKey))
    .filter((selectedItem): selectedItem is TreeItem => !!selectedItem);
  emit("contextmenu", event, item, checkedItems.length > 0 ? checkedItems : [item]);
}

function nodeProps({ option }: { option: TreeOption }) {
  const item = (option as FileTreeNode).treeItem;
  return {
    onContextmenu: (event: MouseEvent) => onNodeContextMenu(event, item),
    onClick: (event: MouseEvent) => onNodeClick(event, item),
    onDblclick: (event: MouseEvent) => onNodeDblclick(event, item),
  };
}

function onShellContextMenu(event: MouseEvent): void {
  event.preventDefault();
  emit("contextmenu", event, null, []);
}

function onExpandedKeys(keys: Array<string | number>): void {
  expandedKeys.value = keys;
}

function renderLabel({ option }: { option: TreeOption }): VNodeChild {
  const item = (option as FileTreeNode).treeItem;
  if (!item) return option.label ?? "";

  const changeColor =
    item.changeKind === "added"
      ? "#63e2b7"
      : item.changeKind === "modified"
        ? "#f2c97d"
        : undefined;
  const children: VNodeChild[] = [];
  if (!item.isDir && item.icon) {
    children.push(h(ImageThumbnail, { reference: item.icon, size: 16 }));
  }
  children.push(
    h("span", { class: "tree-item-name", style: { color: changeColor } }, item.label),
  );
  for (const annotation of item.annotations) {
    children.push(
      h(
        NTag,
        {
          size: "tiny",
          bordered: false,
          type: annotation.type === "reference" ? "success" : annotation.type === "enum" ? "warning" : "info",
          class: "tree-tag tree-tag-annotation",
          title: annotation.content,
        },
        { default: () => annotation.title }
      )
    );
  }
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
  <div
    class="file-tree-shell"
    @contextmenu="onShellContextMenu"
  >
    <!-- 目录是逻辑选择，允许在子节点尚未加载时勾选目录本身。 -->
    <NTree
      ref="treeRef"
      block-line
      checkable
      cascade
      :selectable="false"
      :animated="false"
      virtual-scroll
      class="file-tree"
      :data="treeData"
      allow-checking-not-loaded
      :expanded-keys="expandedKeys"
      :checked-keys="checkedKeys"
      :selected-keys="selectedKeys"
      :on-load="onLoad"
      :on-update:expanded-keys="onExpandedKeys"
      :on-update:checked-keys="onChecked"
      :node-props="nodeProps"
      :render-label="renderLabel"
      :scrollbar-props="{ xScrollable: true }"
      :style="{ height }"
      :expand-on-click="false"
    />
  </div>
</template>

<style scoped>
.file-tree-shell {
  width: 100%;
  height: 100%;
  min-width: 0;
  min-height: 0;
}
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
