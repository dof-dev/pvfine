<script setup lang="ts">
import { computed, h, ref, watch, type VNodeChild } from "vue";
import {
  Add24Regular,
  ArrowImport24Regular,
  ArrowExportLtr24Regular,
  Bookmark24Regular,
  Delete24Regular,
  Dismiss24Regular,
  Edit24Regular,
  FolderAdd24Regular,
  FolderOpen24Regular,
  LockClosed24Regular,
  Copy24Regular,
  ArrowMove24Regular,
  Search24Regular,
} from "@vicons/fluent";
import {
  NButton,
  NEmpty,
  NIcon,
  NInput,
  NModal,
  NSelect,
  NSpin,
  NTag,
  NText,
  NTooltip,
  NTree,
  useDialog,
  useMessage,
  type TreeOption,
} from "naive-ui";
import { useArchiveStore } from "../stores/archive";
import {
  useBookmarkStore,
  type BookmarkBookState,
  type BookmarkEntry,
  type BookmarkGroup,
} from "../stores/bookmarks";
import { useEditorStore } from "../stores/editor";
import { useSettingsStore } from "../stores/settings";

const archive = useArchiveStore();
const bookmarks = useBookmarkStore();
const editor = useEditorStore();
const settings = useSettingsStore();
const message = useMessage();
const dialog = useDialog();

const namingVisible = ref(false);
const namingMode = ref<"create" | "rename">("create");
const namingBookID = ref("");
const namingValue = ref("");
const namingError = ref("");

const groupVisible = ref(false);
const groupMode = ref<"create" | "rename">("create");
const groupID = ref("");
const groupParentID = ref("");
const groupValue = ref("");
const groupError = ref("");

const entryVisible = ref(false);
const entryPath = ref("");
const entryGroupID = ref("");
const entryValue = ref("");
const entryError = ref("");

const moveVisible = ref(false);
const movePath = ref("");
const moveSourceGroupID = ref("");
const moveTargetGroupID = ref("");
const moving = ref(false);
const importing = ref(false);
const exporting = ref(false);
const filterText = ref("");
const handledClickEvents = new WeakSet<MouseEvent>();

interface BookmarkTreeNode extends TreeOption {
  kind: "group" | "root" | "entry";
  groupID: string;
  parentGroupID: string;
  group?: BookmarkGroup;
  entry?: BookmarkEntry;
}

const activeBook = computed(() => bookmarks.activeBook as BookmarkBookState | null);
const activeEntriesCount = computed(() => (activeBook.value ? countEntries(activeBook.value) : 0));
const bookOptions = computed(() =>
  bookmarks.books.map((book) => ({
    label: book.name,
    value: book.id,
  }))
);
const treeData = computed<BookmarkTreeNode[]>(() => {
  const book = activeBook.value;
  if (!book) return [];
  const query = filterText.value.trim().toLocaleLowerCase();
  const result: BookmarkTreeNode[] = [];
  const entries = filterEntries(book.entries, query);
  if (entries.length > 0) {
    result.push({
      key: "root",
      label: "未分组",
      kind: "root",
      groupID: "",
      parentGroupID: "",
      isLeaf: false,
      children: entries.map((entry) => toEntryOption(entry, "")),
    });
  }
  const groups = query
    ? book.groups
        .map((group) => filterGroup(group, query))
        .filter((group): group is BookmarkGroup => !!group)
    : book.groups;
  result.push(...groups.map((group) => toGroupOption(group, "")));
  return result;
});
const treeExpandedKeys = ref<Array<string | number>>([]);
const treeSelectedKeys = computed(() => {
  if (bookmarks.selectedGroupId) return [`g:${bookmarks.selectedGroupId}`];
  return [];
});
const moveOptions = computed(() => {
  const options = [{ label: "未分组", value: "" }];
  if (activeBook.value) appendGroupOptions(activeBook.value.groups, options, "");
  return options;
});
const namingTitle = computed(() => (namingMode.value === "create" ? "新建书签簿" : "重命名书签簿"));
const groupTitle = computed(() => (groupMode.value === "create" ? "新建分组" : "重命名分组"));
const entryTitle = "重命名书签";

watch(
  () => {
    const book = activeBook.value;
    return book ? `${book.id}:${groupStructureKey(book.groups)}:${filterText.value}` : "";
  },
  () => {
    const next: Array<string | number> = [];
    collectExpandedKeys(treeData.value, next);
    treeExpandedKeys.value = next;
  },
  { immediate: true }
);

function groupStructureKey(groups: BookmarkGroup[]): string {
  return groups.map((group) => `${group.id}[${groupStructureKey(group.groups)}]`).join("|");
}

function filterEntries(entries: BookmarkEntry[], query: string): BookmarkEntry[] {
  if (!query) return entries;
  return entries.filter((entry) =>
    `${entry.name}\n${entry.path}`.toLocaleLowerCase().includes(query)
  );
}

function filterGroup(group: BookmarkGroup, query: string): BookmarkGroup | null {
  if (group.name.toLocaleLowerCase().includes(query)) return group;
  const entries = filterEntries(group.entries, query);
  const groups = group.groups
    .map((child) => filterGroup(child, query))
    .filter((child): child is BookmarkGroup => !!child);
  if (entries.length === 0 && groups.length === 0) return null;
  return { ...group, groups, entries };
}

function countEntries(book: BookmarkBookState): number {
  let count = book.entries.length;
  const walk = (groups: BookmarkGroup[]) => {
    for (const group of groups) {
      count += group.entries.length;
      walk(group.groups);
    }
  };
  walk(book.groups);
  return count;
}

function countGroupEntries(group: BookmarkGroup): number {
  let count = group.entries.length;
  for (const child of group.groups) count += countGroupEntries(child);
  return count;
}

function collectExpandedKeys(items: BookmarkTreeNode[], result: Array<string | number>): void {
  for (const item of items) {
    if (item.kind === "group" || item.kind === "root") {
      result.push(item.key as string);
      if (item.children) collectExpandedKeys(item.children as BookmarkTreeNode[], result);
    }
  }
}

function toggleGroup(groupID: string): void {
  const key = `g:${groupID}`;
  const expanded = new Set(treeExpandedKeys.value.map(String));
  if (expanded.has(key)) expanded.delete(key);
  else expanded.add(key);
  treeExpandedKeys.value = [...expanded];
}

function toGroupOption(group: BookmarkGroup, parentGroupID: string): BookmarkTreeNode {
  const children: BookmarkTreeNode[] = [
    ...group.groups.map((child) => toGroupOption(child, group.id)),
    ...group.entries.map((entry) => toEntryOption(entry, group.id)),
  ];
  return {
    key: `g:${group.id}`,
    label: group.name,
    kind: "group",
    groupID: group.id,
    parentGroupID,
    group,
    isLeaf: children.length === 0,
    children,
  };
}

function toEntryOption(entry: BookmarkEntry, parentGroupID: string): BookmarkTreeNode {
  return {
    key: `b:${parentGroupID}:${entry.path}`,
    label: entry.name,
    kind: "entry",
    groupID: parentGroupID,
    parentGroupID,
    entry,
    isLeaf: true,
  };
}

function appendGroupOptions(
  groups: BookmarkGroup[],
  result: Array<{ label: string; value: string }>,
  prefix: string
): void {
  for (const group of groups) {
    result.push({ label: `${prefix}${group.name}`, value: group.id });
    appendGroupOptions(group.groups, result, `${prefix}　`);
  }
}

function onBookChange(id: string): void {
  bookmarks.setActiveBook(id);
}

function openCreateBook(): void {
  namingMode.value = "create";
  namingBookID.value = "";
  namingValue.value = suggestedBookName();
  namingError.value = "";
  namingVisible.value = true;
}

function openRenameBook(): void {
  const book = activeBook.value;
  if (!book || !book.editable) return;
  namingMode.value = "rename";
  namingBookID.value = book.id;
  namingValue.value = book.name;
  namingError.value = "";
  namingVisible.value = true;
}

function suggestedBookName(): string {
  let number = 1;
  const names = new Set(bookmarks.books.map((book) => book.name));
  let name = `书签簿 ${number}`;
  while (names.has(name)) name = `书签簿 ${++number}`;
  return name;
}

function closeNaming(): void {
  namingVisible.value = false;
  namingError.value = "";
}

function submitNaming(): void {
  try {
    if (namingMode.value === "create") bookmarks.createBook(namingValue.value);
    else bookmarks.renameBook(namingBookID.value, namingValue.value);
    closeNaming();
  } catch (error: any) {
    namingError.value = String(error?.message ?? error);
  }
}

function openCopyBook(): void {
  const book = activeBook.value;
  if (!book) return;
  const copy = bookmarks.copyBook(book.id);
  if (copy) message.success(`已复制书签簿“${book.name}”为“${copy.name}”`);
}

function deleteBook(): void {
  const book = activeBook.value;
  if (!book || !book.editable) return;
  dialog.warning({
    title: "删除书签簿",
    content: `确定删除“${book.name}”及其中的 ${countEntries(book)} 个书签吗？`,
    positiveText: "删除",
    negativeText: "取消",
    onPositiveClick: () => {
      if (bookmarks.deleteBook(book.id)) message.success(`已删除书签簿“${book.name}”`);
    },
  });
}

function openCreateGroup(parentID = ""): void {
  if (!activeBook.value?.editable) return;
  groupMode.value = "create";
  groupID.value = "";
  groupParentID.value = parentID || "";
  groupValue.value = "";
  groupError.value = "";
  groupVisible.value = true;
}

function openRenameGroup(group: BookmarkGroup): void {
  if (!activeBook.value?.editable) return;
  groupMode.value = "rename";
  groupID.value = group.id;
  groupParentID.value = "";
  groupValue.value = group.name;
  groupError.value = "";
  groupVisible.value = true;
}

function closeGroupDialog(): void {
  groupVisible.value = false;
  groupError.value = "";
}

function submitGroup(): void {
  try {
    if (groupMode.value === "create") bookmarks.createGroup(groupParentID.value, groupValue.value);
    else bookmarks.renameGroup(groupID.value, groupValue.value);
    closeGroupDialog();
  } catch (error: any) {
    groupError.value = String(error?.message ?? error);
  }
}

function deleteGroup(group: BookmarkGroup): void {
  if (!activeBook.value?.editable) return;
  dialog.warning({
    title: "删除分组",
    content: `确定删除分组“${group.name}”吗？其中的 ${countGroupEntries(group)} 个书签会移动到父分组，子分组会提升到当前层级。`,
    positiveText: "删除",
    negativeText: "取消",
    onPositiveClick: () => {
      if (bookmarks.deleteGroup(group.id)) message.success(`已删除分组“${group.name}”`);
    },
  });
}

function openRenameEntry(entry: BookmarkEntry, groupID: string): void {
  if (!activeBook.value?.editable) return;
  entryPath.value = entry.path;
  entryGroupID.value = groupID;
  entryValue.value = entry.name;
  entryError.value = "";
  entryVisible.value = true;
}

function closeEntryDialog(): void {
  entryVisible.value = false;
  entryError.value = "";
}

function submitEntry(): void {
  try {
    bookmarks.renameEntry(entryPath.value, entryValue.value, entryGroupID.value);
    closeEntryDialog();
  } catch (error: any) {
    entryError.value = String(error?.message ?? error);
  }
}

function removeEntry(entry: BookmarkEntry, groupID: string): void {
  if (!activeBook.value?.editable) return;
  if (bookmarks.removeEntry(entry.path, groupID)) message.success(`已移除书签“${entry.name}”`);
}

function openMoveEntry(entry: BookmarkEntry, groupID: string): void {
  if (!activeBook.value?.editable) return;
  movePath.value = entry.path;
  moveSourceGroupID.value = groupID;
  moveTargetGroupID.value = groupID;
  moveVisible.value = true;
}

function closeMoveDialog(): void {
  if (moving.value) return;
  moveVisible.value = false;
}

async function submitMove(): Promise<void> {
  if (moving.value) return;
  if (moveTargetGroupID.value === moveSourceGroupID.value) {
    closeMoveDialog();
    return;
  }
  moving.value = true;
  try {
    bookmarks.selectGroup(moveTargetGroupID.value);
    bookmarks.moveEntry(movePath.value, moveTargetGroupID.value, moveSourceGroupID.value);
    moveVisible.value = false;
    message.success("已移动书签");
  } catch (error: any) {
    message.error(`移动书签失败: ${error?.message ?? error}`);
  } finally {
    moving.value = false;
  }
}

function onSelected(keys: Array<string | number>): void {
  const key = String(keys[0] ?? "");
  const node = findTreeNode(treeData.value, key);
  if (!node) return;
  if (node.kind === "group") bookmarks.selectGroup(node.groupID);
  else if (node.kind === "root") bookmarks.selectGroup("");
  else bookmarks.selectGroup(node.parentGroupID);
}

function findTreeNode(items: BookmarkTreeNode[], key: string): BookmarkTreeNode | null {
  for (const item of items) {
    if (String(item.key) === key) return item;
    if (item.children) {
      const nested = findTreeNode(item.children as BookmarkTreeNode[], key);
      if (nested) return nested;
    }
  }
  return null;
}

function findGroup(groups: BookmarkGroup[], id: string): BookmarkGroup | null {
  for (const group of groups) {
    if (group.id === id) return group;
    const nested = findGroup(group.groups, id);
    if (nested) return nested;
  }
  return null;
}

function nodeProps({ option }: { option: TreeOption }) {
  const node = option as BookmarkTreeNode;
  return {
    onClick: (event: MouseEvent) => {
      if (isActionTarget(event) || isTreeControl(event)) return;
      if (handledClickEvents.has(event)) return;
      handledClickEvents.add(event);
      if (node.kind === "group") {
        if (event.detail > 1) return;
        toggleGroup(node.groupID);
        return;
      }
      if (node.kind !== "entry" || !node.entry) return;
      if (settings.explorerOpenMode !== "single-click") return;
      event.stopPropagation();
      openEntry(node.entry);
    },
    onDblclick: (event: MouseEvent) => {
      if (
        isActionTarget(event) ||
        node.kind !== "entry" ||
        !node.entry ||
        settings.explorerOpenMode !== "double-click"
      ) {
        return;
      }
      event.stopPropagation();
      openEntry(node.entry);
    },
  };
}

function isTreeControl(event: MouseEvent): boolean {
  const target = event.target;
  return target instanceof Element && !!target.closest("[data-switcher], [data-checkbox]");
}

function isActionTarget(event: MouseEvent): boolean {
  const target = event.target;
  return target instanceof Element && !!target.closest("button, [data-bookmark-action]");
}

function renderLabel({ option }: { option: TreeOption }): VNodeChild {
  const node = option as BookmarkTreeNode;
  if (node.kind === "entry" && node.entry) {
    const entry = node.entry;
    const actions: VNodeChild[] = [];
    if (activeBook.value?.editable) {
      actions.push(
        h(NButton, {
          quaternary: true,
          circle: true,
          size: "tiny",
          class: "bookmark-row-action",
          title: "移动书签",
          "aria-label": "移动书签",
          "data-bookmark-action": "true",
          onClick: (event: MouseEvent) => {
            event.stopPropagation();
            openMoveEntry(entry, node.parentGroupID);
          },
        }, { icon: () => h(NIcon, { size: 14 }, { default: () => h(ArrowMove24Regular) }) })
      );
      actions.push(
        h(NButton, {
          quaternary: true,
          circle: true,
          size: "tiny",
          class: "bookmark-row-action",
          title: "重命名书签",
          "aria-label": "重命名书签",
          "data-bookmark-action": "true",
          onClick: (event: MouseEvent) => {
            event.stopPropagation();
            openRenameEntry(entry, node.parentGroupID);
          },
        }, { icon: () => h(NIcon, { size: 14 }, { default: () => h(Edit24Regular) }) })
      );
      actions.push(
        h(NButton, {
          quaternary: true,
          circle: true,
          size: "tiny",
          class: "bookmark-row-action",
          title: "移除书签",
          "aria-label": "移除书签",
          "data-bookmark-action": "true",
          onClick: (event: MouseEvent) => {
            event.stopPropagation();
            removeEntry(entry, node.parentGroupID);
          },
        }, { icon: () => h(NIcon, { size: 14 }, { default: () => h(Dismiss24Regular) }) })
      );
    }
    return h("div", { class: ["bookmark-tree-label", "bookmark-entry-label"] }, [
      h(NIcon, { size: 16, class: "bookmark-entry-icon" }, { default: () => h(Bookmark24Regular) }),
      h("div", { class: "bookmark-entry-name", title: entry.name }, entry.name),
      ...(entry.fileIndex < 0
        ? [h(NTag, { size: "tiny", type: "warning", bordered: false }, { default: () => "当前归档不存在" })]
        : []),
      h("div", { class: "bookmark-row-actions" }, actions),
    ]);
  }

  const group = node.group;
  const actions: VNodeChild[] = [];
  if (group && activeBook.value?.editable) {
    actions.push(
      h(NButton, {
        quaternary: true,
        circle: true,
        size: "tiny",
        class: "bookmark-row-action",
        title: "新建子分组",
        "aria-label": "新建子分组",
        "data-bookmark-action": "true",
        onClick: (event: MouseEvent) => {
          event.stopPropagation();
          openCreateGroup(group.id);
        },
      }, { icon: () => h(NIcon, { size: 14 }, { default: () => h(FolderAdd24Regular) }) })
    );
    actions.push(
      h(NButton, {
        quaternary: true,
        circle: true,
        size: "tiny",
        class: "bookmark-row-action",
        title: "重命名分组",
        "aria-label": "重命名分组",
        "data-bookmark-action": "true",
        onClick: (event: MouseEvent) => {
          event.stopPropagation();
          openRenameGroup(group);
        },
      }, { icon: () => h(NIcon, { size: 14 }, { default: () => h(Edit24Regular) }) })
    );
    actions.push(
      h(NButton, {
        quaternary: true,
        circle: true,
        size: "tiny",
        class: "bookmark-row-action",
        title: "删除分组",
        "aria-label": "删除分组",
        "data-bookmark-action": "true",
        onClick: (event: MouseEvent) => {
          event.stopPropagation();
          deleteGroup(group);
        },
      }, { icon: () => h(NIcon, { size: 14 }, { default: () => h(Delete24Regular) }) })
    );
  }
  const isRoot = node.kind === "root";
  return h("div", { class: "bookmark-tree-label" }, [
    h(NIcon, { size: 16, class: "bookmark-group-icon" }, { default: () => h(isRoot ? FolderOpen24Regular : FolderOpen24Regular) }),
    h("span", { class: "bookmark-group-name" }, node.label),
    group ? h(NTag, { size: "tiny", bordered: false }, { default: () => countGroupEntries(group) }) : null,
    h("div", { class: "bookmark-row-actions" }, actions),
  ]);
}

function openEntry(entry: BookmarkEntry): void {
  if (!archive.open) {
    message.info("请先打开一个 PVF 归档");
    return;
  }
  if (bookmarks.resolving) {
    message.info("正在匹配当前归档中的书签");
    return;
  }
  if (entry.fileIndex < 0) {
    message.warning(`当前归档中不存在文件: ${entry.path}`);
    return;
  }
  void editor.openFile(entry.fileIndex);
}

async function importCurrent(): Promise<void> {
  if (importing.value) return;
  importing.value = true;
  try {
    const imported = await bookmarks.importBook();
    if (imported) message.success(`已导入书签簿“${imported.name}”`);
  } catch (error: any) {
    if (!isCancel(error)) message.error(`导入书签簿失败: ${error?.message ?? error}`);
  } finally {
    importing.value = false;
  }
}

async function exportCurrent(): Promise<void> {
  if (!activeBook.value || exporting.value) return;
  exporting.value = true;
  try {
    const path = await bookmarks.exportActiveBook();
    if (path) message.success(`已导出书签簿到 ${path}`);
  } catch (error: any) {
    if (!isCancel(error)) message.error(`导出书签簿失败: ${error?.message ?? error}`);
  } finally {
    exporting.value = false;
  }
}

function retrySave(): void {
  void bookmarks.retrySave().then(() => {
    if (!bookmarks.saveError) message.success("书签簿已保存");
  });
}

function onTreeExpanded(keys: Array<string | number>): void {
  treeExpandedKeys.value = keys;
}

function isCancel(error: any): boolean {
  return String(error?.message ?? error).toLowerCase().includes("cancel");
}

watch(
  () => bookmarks.loadError,
  (error) => {
    if (error) message.error(`读取书签簿失败: ${error}`);
  }
);

watch(
  () => bookmarks.saveError,
  (error) => {
    if (error) message.error(`保存书签簿失败: ${error}`);
  }
);

watch(
  () => bookmarks.activeBookId,
  () => {
    closeNaming();
    closeGroupDialog();
    closeEntryDialog();
    if (!moving.value) closeMoveDialog();
  }
);
</script>

<template>
  <section class="bookmark-sidebar">
    <div class="bookmark-heading">
      <div class="bookmark-title">
        <NIcon :size="16"><Bookmark24Regular /></NIcon>
        <span>书签</span>
        <NTag size="tiny" :bordered="false">{{ activeEntriesCount }}</NTag>
        <NTag v-if="activeBook?.builtin" size="tiny" :type="activeBook.editable ? 'info' : 'warning'" :bordered="false">
          <template #icon><NIcon><LockClosed24Regular /></NIcon></template>
          {{ activeBook.editable ? "开发可编辑" : "内置" }}
        </NTag>
      </div>
      <div class="bookmark-actions">
        <NTooltip>
          <template #trigger>
            <NButton quaternary circle size="tiny" :disabled="!bookmarks.loaded" aria-label="新建书签簿" @click="openCreateBook">
              <template #icon><NIcon><Add24Regular /></NIcon></template>
            </NButton>
          </template>
          新建书签簿
        </NTooltip>
        <NTooltip>
          <template #trigger>
            <NButton quaternary circle size="tiny" :disabled="!activeBook" aria-label="复制书签簿" @click="openCopyBook">
              <template #icon><NIcon><Copy24Regular /></NIcon></template>
            </NButton>
          </template>
          复制当前书签簿
        </NTooltip>
        <NTooltip>
          <template #trigger>
            <NButton quaternary circle size="tiny" :disabled="!activeBook?.editable" aria-label="重命名书签簿" @click="openRenameBook">
              <template #icon><NIcon><Edit24Regular /></NIcon></template>
            </NButton>
          </template>
          重命名书签簿
        </NTooltip>
        <NTooltip>
          <template #trigger>
            <NButton quaternary circle size="tiny" :disabled="!activeBook?.editable || activeBook.builtin" aria-label="删除书签簿" @click="deleteBook">
              <template #icon><NIcon><Delete24Regular /></NIcon></template>
            </NButton>
          </template>
          删除书签簿
        </NTooltip>
      </div>
    </div>

    <div class="bookmark-switcher">
      <NSelect
        :value="bookmarks.activeBookId"
        size="small"
        :options="bookOptions"
        placeholder="选择书签簿"
        @update:value="onBookChange"
      />
      <NTooltip>
        <template #trigger>
          <NButton quaternary circle size="small" :loading="importing" aria-label="导入书签簿" @click="importCurrent">
            <template #icon><NIcon><ArrowImport24Regular /></NIcon></template>
          </NButton>
        </template>
        导入书签簿
      </NTooltip>
      <NTooltip>
        <template #trigger>
          <NButton quaternary circle size="small" :loading="exporting" :disabled="!activeBook" aria-label="导出书签簿" @click="exportCurrent">
            <template #icon><NIcon><ArrowExportLtr24Regular /></NIcon></template>
          </NButton>
        </template>
        导出当前书签簿
      </NTooltip>
    </div>

    <div class="bookmark-filter">
      <NInput
        v-model:value="filterText"
        clearable
        size="small"
        placeholder="筛选书签名称、路径或分组"
        aria-label="筛选书签"
      >
        <template #prefix><NIcon><Search24Regular /></NIcon></template>
      </NInput>
    </div>

    <div class="bookmark-group-toolbar">
      <NButton size="tiny" quaternary :disabled="!activeBook?.editable" @click="openCreateGroup()">
        <template #icon><NIcon><FolderAdd24Regular /></NIcon></template>
        新建分组
      </NButton>
      <NText v-if="bookmarks.saving" depth="3" class="bookmark-save-status">保存中…</NText>
      <NButton v-else-if="bookmarks.saveError" size="tiny" quaternary type="error" @click="retrySave">
        保存失败，重试
      </NButton>
    </div>

    <NSpin :show="!bookmarks.loaded || bookmarks.resolving">
      <div v-if="!bookmarks.loaded" class="bookmark-empty"><NEmpty description="正在读取书签簿" size="small" /></div>
      <div v-else-if="!treeData.length" class="bookmark-empty">
        <NEmpty :description="filterText.trim() ? '没有匹配的书签' : '暂无书签'" size="small" />
      </div>
      <NTree
        v-else
        block-line
        selectable
        :data="treeData"
        :expanded-keys="treeExpandedKeys"
        :selected-keys="treeSelectedKeys"
        :node-props="nodeProps"
        :render-label="renderLabel"
        :on-update:expanded-keys="onTreeExpanded"
        :on-update:selected-keys="onSelected"
        class="bookmark-tree"
        :style="{ height: '100%' }"
        virtual-scroll
      />
    </NSpin>

    <NModal
      :show="namingVisible"
      preset="card"
      :title="namingTitle"
      :style="{ width: 'min(380px, calc(100vw - 48px))' }"
      :mask-closable="false"
      @update:show="(show) => !show && closeNaming()"
    >
      <NInput v-model:value="namingValue" autofocus placeholder="输入书签簿名" :status="namingError ? 'error' : undefined" @keydown.enter.prevent="submitNaming" />
      <NText v-if="namingError" type="error" class="bookmark-form-error">{{ namingError }}</NText>
      <template #footer><div class="bookmark-modal-footer"><NButton quaternary @click="closeNaming">取消</NButton><NButton type="primary" @click="submitNaming">确定</NButton></div></template>
    </NModal>

    <NModal
      :show="groupVisible"
      preset="card"
      :title="groupTitle"
      :style="{ width: 'min(380px, calc(100vw - 48px))' }"
      :mask-closable="false"
      @update:show="(show) => !show && closeGroupDialog()"
    >
      <NText v-if="groupMode === 'create'" depth="3">父分组：{{ groupParentID ? findGroup(activeBook?.groups ?? [], groupParentID)?.name : '未分组（根）' }}</NText>
      <NInput v-model:value="groupValue" autofocus placeholder="输入分组名称" :status="groupError ? 'error' : undefined" @keydown.enter.prevent="submitGroup" />
      <NText v-if="groupError" type="error" class="bookmark-form-error">{{ groupError }}</NText>
      <template #footer><div class="bookmark-modal-footer"><NButton quaternary @click="closeGroupDialog">取消</NButton><NButton type="primary" @click="submitGroup">确定</NButton></div></template>
    </NModal>

    <NModal
      :show="entryVisible"
      preset="card"
      :title="entryTitle"
      :style="{ width: 'min(420px, calc(100vw - 48px))' }"
      :mask-closable="false"
      @update:show="(show) => !show && closeEntryDialog()"
    >
      <NText depth="3" class="bookmark-entry-form-path">{{ entryPath }}</NText>
      <NInput v-model:value="entryValue" autofocus placeholder="输入书签名称" :status="entryError ? 'error' : undefined" @keydown.enter.prevent="submitEntry" />
      <NText v-if="entryError" type="error" class="bookmark-form-error">{{ entryError }}</NText>
      <template #footer><div class="bookmark-modal-footer"><NButton quaternary @click="closeEntryDialog">取消</NButton><NButton type="primary" @click="submitEntry">确定</NButton></div></template>
    </NModal>

    <NModal
      :show="moveVisible"
      preset="card"
      title="移动书签"
      :style="{ width: 'min(420px, calc(100vw - 48px))' }"
      :mask-closable="false"
      @update:show="(show) => !show && closeMoveDialog()"
    >
      <NText depth="3" class="bookmark-entry-form-path">{{ movePath }}</NText>
      <NSelect v-model:value="moveTargetGroupID" :options="moveOptions" placeholder="选择目标分组" />
      <template #footer><div class="bookmark-modal-footer"><NButton quaternary :disabled="moving" @click="closeMoveDialog">取消</NButton><NButton type="primary" :loading="moving" @click="submitMove">移动</NButton></div></template>
    </NModal>
  </section>
</template>

<style scoped>
.bookmark-sidebar {
  display: flex;
  flex: 1;
  min-width: 0;
  min-height: 0;
  flex-direction: column;
}
.bookmark-heading,
.bookmark-title,
.bookmark-actions,
.bookmark-switcher,
.bookmark-group-toolbar,
.bookmark-modal-footer {
  display: flex;
  align-items: center;
}
.bookmark-sidebar :deep(.bookmark-tree-label),
.bookmark-sidebar :deep(.bookmark-entry-label),
.bookmark-sidebar :deep(.bookmark-row-actions) {
  display: flex;
  align-items: center;
}
.bookmark-heading {
  justify-content: space-between;
  gap: 8px;
  padding: 8px 8px 6px 12px;
  flex-shrink: 0;
}
.bookmark-title {
  min-width: 0;
  gap: 6px;
  color: var(--pvf-text-primary);
  font-weight: 600;
}
.bookmark-title > span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.bookmark-actions {
  gap: 1px;
  flex-shrink: 0;
}
.bookmark-switcher {
  gap: 6px;
  padding: 0 8px 8px 12px;
  flex-shrink: 0;
  border-bottom: 1px solid var(--pvf-border-subtle);
}
.bookmark-switcher .n-select {
  min-width: 0;
  flex: 1;
}
.bookmark-filter {
  flex-shrink: 0;
  padding: 0 8px 8px 12px;
}
.bookmark-filter :deep(.n-input) {
  width: 100%;
}
.bookmark-group-toolbar {
  gap: 8px;
  min-height: 34px;
  padding: 2px 8px 2px 12px;
  flex-shrink: 0;
}
.bookmark-save-status {
  margin-left: auto;
  font-size: 11px;
}
.bookmark-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 180px;
}
.bookmark-sidebar :deep(.n-spin-container) {
  display: flex;
  flex: 1;
  min-height: 0;
  flex-direction: column;
}
.bookmark-sidebar :deep(.n-spin-content) {
  display: flex;
  flex: 1;
  min-height: 0;
  flex-direction: column;
}
.bookmark-tree {
  flex: 1;
  min-height: 0;
  padding: 2px 4px 4px 8px;
}
.bookmark-sidebar :deep(.bookmark-tree-label) {
  width: 100%;
  min-width: 0;
  gap: 6px;
}
.bookmark-sidebar :deep(.bookmark-group-icon),
.bookmark-sidebar :deep(.bookmark-entry-icon) {
  flex: 0 0 auto;
  color: var(--pvf-primary-hover);
}
.bookmark-sidebar :deep(.bookmark-group-name),
.bookmark-sidebar :deep(.bookmark-entry-name) {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.bookmark-sidebar :deep(.bookmark-group-name) {
  flex: 1;
}
.bookmark-sidebar :deep(.bookmark-entry-label) {
  min-width: 0;
}
.bookmark-sidebar :deep(.bookmark-entry-name) {
  min-width: 0;
  flex: 1;
  color: var(--pvf-text-tertiary);
}
.bookmark-sidebar :deep(.bookmark-row-actions) {
  flex: 0 0 auto;
  gap: 1px;
  opacity: 0;
}
.bookmark-sidebar :deep(.bookmark-tree-label:hover .bookmark-row-actions),
.bookmark-sidebar :deep(.bookmark-row-actions:focus-within) {
  opacity: 1;
}
.bookmark-sidebar :deep(.bookmark-row-action) {
  flex: 0 0 auto;
}
.bookmark-form-error {
  display: block;
  margin-top: 6px;
  font-size: 12px;
}
.bookmark-entry-form-path {
  display: block;
  margin-bottom: 8px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.bookmark-modal-footer {
  justify-content: flex-end;
  gap: 8px;
}
</style>
