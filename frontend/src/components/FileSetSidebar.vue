<script setup lang="ts">
import { computed, ref, watch } from "vue";
import {
  Add24Regular,
  Collections24Regular,
  Delete24Regular,
  DeleteDismiss24Regular,
  Dismiss24Regular,
  DocumentSync24Regular,
  Edit24Regular,
  ArrowExportLtr24Regular,
  BookmarkMultiple24Regular,
  Save24Regular,
} from "@vicons/fluent";
import {
  NButton,
  NEmpty,
  NIcon,
  NInput,
  NModal,
  NSelect,
  NTag,
  NText,
  NTooltip,
  useMessage,
  useDialog,
} from "naive-ui";
import { EditorService } from "../../bindings/pvfine/services";
import { useArchiveStore } from "../stores/archive";
import { useEditorStore } from "../stores/editor";
import { useFileSetStore, type FileSetEntry } from "../stores/fileSets";
import { useBatchStore } from "../stores/batch";
import { useSidebarStore } from "../stores/sidebar";
import { useSettingsStore } from "../stores/settings";
import BookmarkSidebar from "./BookmarkSidebar.vue";
import ImageThumbnail from "./ImageThumbnail.vue";

const fileSets = useFileSetStore();
const archive = useArchiveStore();
const editor = useEditorStore();
const batch = useBatchStore();
const sidebar = useSidebarStore();
const settings = useSettingsStore();
const message = useMessage();
const dialog = useDialog();

const namingVisible = ref(false);
const namingMode = ref<"create" | "rename">("create");
const namingSetId = ref("");
const namingValue = ref("");
const namingError = ref("");
const exporting = ref(false);

const setOptions = computed(() =>
  fileSets.fileSets.map((fileSet) => ({
    label: fileSet.name,
    value: fileSet.id,
  }))
);
const activeEntries = computed(() => fileSets.activeSet?.entries ?? []);
const namingTitle = computed(() =>
  namingMode.value === "create" ? "新建文件集" : "重命名文件集"
);

function suggestedName(): string {
  let number = fileSets.fileSets.length + 1;
  let name = `文件集 ${number}`;
  const names = new Set(fileSets.fileSets.map((fileSet) => fileSet.name));
  while (names.has(name)) {
    name = `文件集 ${++number}`;
  }
  return name;
}

function openCreate(): void {
  namingMode.value = "create";
  namingSetId.value = "";
  namingValue.value = suggestedName();
  namingError.value = "";
  namingVisible.value = true;
}

function openRename(): void {
  const active = fileSets.activeSet;
  if (!active) return;
  namingMode.value = "rename";
  namingSetId.value = active.id;
  namingValue.value = active.name;
  namingError.value = "";
  namingVisible.value = true;
}

function closeNaming(): void {
  namingVisible.value = false;
  namingError.value = "";
}

function submitNaming(): void {
  try {
    if (namingMode.value === "create") {
      fileSets.createSet(namingValue.value);
    } else {
      fileSets.renameSet(namingSetId.value, namingValue.value);
    }
    closeNaming();
  } catch (error: any) {
    namingError.value = String(error?.message ?? error);
  }
}

function deleteCurrent(): void {
  const active = fileSets.activeSet;
  if (!active) return;
  dialog.warning({
    title: "删除文件集",
    content: `确定删除“${active.name}”及其中的 ${active.entries.length} 个文件吗？`,
    positiveText: "删除",
    negativeText: "取消",
    onPositiveClick: async () => {
      if (!fileSets.deleteSet(active.id)) return;
      try {
        await fileSets.save();
        message.success("文件集已删除并保存");
      } catch (error: any) {
        message.error(`删除文件集后保存失败: ${error?.message ?? error}`);
      }
    },
  });
}

function clearCurrent(): void {
  const active = fileSets.activeSet;
  if (!active || active.entries.length === 0) return;
  dialog.warning({
    title: "清空文件集",
    content: `确定清空“${active.name}”中的 ${active.entries.length} 个文件吗？`,
    positiveText: "清空",
    negativeText: "取消",
    onPositiveClick: () => fileSets.clearActive(),
  });
}

async function saveFileSets(): Promise<void> {
  if (fileSets.saving) return;
  try {
    await fileSets.save();
    message.success("文件集已保存到硬盘");
  } catch (error: any) {
    message.error(`保存文件集失败: ${error?.message ?? error}`);
  }
}

async function exportCurrent(): Promise<void> {
  const active = fileSets.activeSet;
  if (
    !active ||
    active.entries.length === 0 ||
    exporting.value ||
    fileSets.resolving ||
    !archive.open
  ) {
    return;
  }

  const unavailableCount = active.entries.filter((entry) => entry.fileIndex < 0).length;
  const paths = [
    ...new Set(
      active.entries
        .filter((entry) => entry.fileIndex >= 0)
        .map((entry) => entry.path)
        .filter(Boolean)
    ),
  ];
  if (paths.length === 0) {
    message.warning("当前归档中没有可导出的文件");
    return;
  }

  const archivePath = archive.info?.path ?? "";
  const session = fileSets.sessionId;
  const setId = active.id;
  exporting.value = true;
  try {
    await editor.flushPending();
    if (
      session !== fileSets.sessionId ||
      archive.info?.path !== archivePath ||
      fileSets.activeSet?.id !== setId
    ) {
      return;
    }
    const path = await EditorService.ExportFilesDialog(paths);
    if (path) {
      const skippedText = unavailableCount > 0 ? `，跳过 ${unavailableCount} 个不存在的文件` : "";
      message.success(`已导出文件集“${active.name}”到 ${path}${skippedText}`);
    }
  } catch (error: any) {
    if (!isCancel(error)) message.error(`导出文件集失败: ${error?.message ?? error}`);
  } finally {
    exporting.value = false;
  }
}

async function openBatch(): Promise<void> {
  const active = fileSets.activeSet;
  if (!active || !archive.open || fileSets.resolving) return;
  const paths = active.entries
    .filter((entry) => entry.fileIndex >= 0)
    .map((entry) => entry.path)
    .filter(Boolean);
  if (paths.length === 0) {
    message.warning("当前文件集中没有可处理的文件");
    return;
  }
  try {
    await editor.flushPending();
    batch.open(paths, `文件集“${active.name}”`);
  } catch (error: any) {
    message.error(`打开批处理失败: ${error?.message ?? error}`);
  }
}

function openEntry(entry: FileSetEntry): void {
  if (!archive.open) {
    message.info("请先打开一个 PVF 归档");
    return;
  }
  if (fileSets.resolving) {
    message.info("正在匹配当前归档中的文件");
    return;
  }
  if (entry.fileIndex < 0) {
    message.warning(`当前归档中不存在文件: ${entry.path}`);
    return;
  }
  void editor.openFile(entry.fileIndex);
}

function onEntryClick(event: MouseEvent, entry: FileSetEntry): void {
  if (settings.explorerOpenMode !== "single-click") return;
  event.stopPropagation();
  openEntry(entry);
}

function onEntryDblclick(event: MouseEvent, entry: FileSetEntry): void {
  if (settings.explorerOpenMode !== "double-click") return;
  event.stopPropagation();
  openEntry(entry);
}

function isCancel(error: any): boolean {
  return String(error?.message ?? error).toLowerCase().includes("cancel");
}

watch(
  () => fileSets.sessionId,
  () => {
    closeNaming();
  }
);
watch(
  () => fileSets.loadError,
  (error) => {
    if (error) message.error(`读取文件集失败: ${error}`);
  }
);
</script>

<template>
  <aside class="file-set-sidebar">
    <div class="collection-switcher">
      <NButton
        quaternary
        size="small"
        :type="sidebar.activePanel === 'filesets' ? 'primary' : 'default'"
        @click="sidebar.show('filesets')"
      >
        <template #icon><NIcon><Collections24Regular /></NIcon></template>
        文件集
      </NButton>
      <NButton
        quaternary
        size="small"
        :type="sidebar.activePanel === 'bookmarks' ? 'primary' : 'default'"
        @click="sidebar.show('bookmarks')"
      >
        <template #icon><NIcon><BookmarkMultiple24Regular /></NIcon></template>
        书签
      </NButton>
    </div>

    <template v-if="sidebar.activePanel === 'filesets'">
    <div class="fileset-heading">
      <div class="fileset-title">
        <NIcon :size="16"><Collections24Regular /></NIcon>
        <span>文件集</span>
        <NTag size="tiny" :bordered="false">{{ activeEntries.length }}</NTag>
      </div>
      <div class="fileset-actions">
        <NTooltip>
          <template #trigger>
            <NButton quaternary circle size="tiny" aria-label="新建文件集" @click="openCreate">
              <template #icon><NIcon><Add24Regular /></NIcon></template>
            </NButton>
          </template>
          新建文件集
        </NTooltip>
        <NTooltip>
          <template #trigger>
            <NButton
              quaternary
              circle
              size="tiny"
              aria-label="重命名文件集"
              :disabled="!fileSets.loaded || !fileSets.activeSet"
              @click="openRename"
            >
              <template #icon><NIcon><Edit24Regular /></NIcon></template>
            </NButton>
          </template>
          重命名文件集
        </NTooltip>
        <NTooltip>
          <template #trigger>
            <NButton
              quaternary
              circle
              size="tiny"
              aria-label="保存文件集"
              :loading="fileSets.saving"
              :disabled="!fileSets.dirty"
              @click="saveFileSets"
            >
              <template #icon><NIcon><Save24Regular /></NIcon></template>
            </NButton>
          </template>
          保存文件集到硬盘
        </NTooltip>
        <NTooltip>
          <template #trigger>
            <NButton
              quaternary
              circle
              size="tiny"
              aria-label="删除文件集"
              :disabled="!fileSets.activeSet"
              @click="deleteCurrent"
            >
              <template #icon><NIcon><Delete24Regular /></NIcon></template>
            </NButton>
          </template>
          删除当前文件集
        </NTooltip>
      </div>
    </div>

    <div class="fileset-switcher">
      <NSelect
        v-model:value="fileSets.activeSetId"
        size="small"
        :options="setOptions"
        placeholder="选择文件集"
      />
      <NTooltip>
        <template #trigger>
          <NButton
            quaternary
            circle
            size="small"
            aria-label="导出当前文件集"
            :loading="exporting"
            :disabled="!archive.open || fileSets.resolving || activeEntries.length === 0"
            @click="exportCurrent"
          >
            <template #icon><NIcon><ArrowExportLtr24Regular /></NIcon></template>
          </NButton>
        </template>
        导出当前文件集
      </NTooltip>
      <NTooltip>
        <template #trigger>
          <NButton
            quaternary
            circle
            size="small"
            aria-label="批量处理当前文件集"
            :disabled="!archive.open || fileSets.resolving || activeEntries.length === 0"
            @click="openBatch"
          >
            <template #icon><NIcon><DocumentSync24Regular /></NIcon></template>
          </NButton>
        </template>
        批量处理当前文件集
      </NTooltip>
      <NTooltip>
        <template #trigger>
          <NButton
            quaternary
            circle
            size="small"
            aria-label="清空当前文件集"
            :disabled="activeEntries.length === 0"
            @click="clearCurrent"
          >
            <template #icon><NIcon><DeleteDismiss24Regular /></NIcon></template>
          </NButton>
        </template>
        清空当前文件集
      </NTooltip>
    </div>

    <div v-if="activeEntries.length === 0" class="fileset-empty">
      <NEmpty description="暂无文件" size="small" />
    </div>
    <div v-else class="fileset-entries">
      <div
        v-for="entry in activeEntries"
        :key="entry.path"
        :class="['fileset-entry', { 'fileset-entry--missing': entry.fileIndex < 0 }]"
        :title="entry.path"
        @click="onEntryClick($event, entry)"
        @dblclick="onEntryDblclick($event, entry)"
      >
        <ImageThumbnail
          class="fileset-entry-icon"
          :reference="entry.icon"
          :size="16"
          :show-fallback="true"
        />
        <div class="fileset-entry-main">
          <div class="fileset-entry-name-line">
            <span class="fileset-entry-name">{{ entry.name }}</span>
            <NTag
              v-for="id in entry.ids"
              :key="id"
              size="tiny"
              type="info"
              :bordered="false"
              class="fileset-entry-id"
            >
              {{ id }}
            </NTag>
            <NTag
              v-if="entry.fileIndex < 0"
              size="tiny"
              type="warning"
              :bordered="false"
              class="fileset-entry-missing"
            >
              {{
                fileSets.resolving
                  ? "正在匹配"
                  : archive.open
                    ? "当前归档不存在"
                    : "未打开归档"
              }}
            </NTag>
          </div>
          <span class="fileset-entry-path">{{ entry.path }}</span>
        </div>
        <NTooltip>
          <template #trigger>
            <NButton
              quaternary
              circle
              size="tiny"
              class="fileset-entry-remove"
              aria-label="移除文件"
              @click.stop="fileSets.removeEntry(entry.path)"
            >
              <template #icon><NIcon :size="14"><Dismiss24Regular /></NIcon></template>
            </NButton>
          </template>
          从文件集移除
        </NTooltip>
      </div>
    </div>

    <NModal
      :show="namingVisible"
      preset="card"
      :title="namingTitle"
      :style="{ width: 'min(360px, calc(100vw - 48px))' }"
      :mask-closable="false"
      @update:show="(show) => !show && closeNaming()"
    >
      <NInput
        v-model:value="namingValue"
        autofocus
        placeholder="输入文件集名称"
        :status="namingError ? 'error' : undefined"
        @keydown.enter.prevent="submitNaming"
      />
      <NText v-if="namingError" type="error" class="fileset-name-error">
        {{ namingError }}
      </NText>
      <template #footer>
        <div class="fileset-modal-footer">
          <NButton quaternary @click="closeNaming">取消</NButton>
          <NButton type="primary" @click="submitNaming">确定</NButton>
        </div>
      </template>
    </NModal>
    </template>

    <BookmarkSidebar v-else />
  </aside>
</template>

<style scoped>
.file-set-sidebar {
  display: flex;
  flex: 0 0 300px;
  flex-direction: column;
  width: 300px;
  min-width: 0;
  min-height: 0;
  border-left: 1px solid var(--pvf-border-normal);
  background: transparent;
}
.fileset-heading,
.fileset-switcher,
.fileset-title,
.fileset-actions,
.fileset-entry,
.fileset-entry-meta,
.fileset-modal-footer {
  display: flex;
  align-items: center;
}
.collection-switcher {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 6px 8px;
  flex-shrink: 0;
  border-bottom: 1px solid var(--pvf-border-subtle);
}
.collection-switcher > .n-button {
  flex: 1;
}
.fileset-heading {
  justify-content: space-between;
  gap: 8px;
  padding: 8px 8px 6px 12px;
  flex-shrink: 0;
}
.fileset-title {
  min-width: 0;
  gap: 6px;
  color: var(--pvf-text-primary);
  font-weight: 600;
}
.fileset-title :deep(.n-icon) {
  color: var(--pvf-text-secondary);
}
.fileset-title > span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.fileset-actions {
  gap: 1px;
  flex-shrink: 0;
}
.fileset-switcher {
  gap: 6px;
  padding: 0 8px 8px 12px;
  flex-shrink: 0;
  border-bottom: 1px solid var(--pvf-border-subtle);
}
.fileset-switcher .n-select {
  min-width: 0;
  flex: 1;
}
.fileset-empty {
  margin-top: 64px;
}
.fileset-entries {
  min-height: 0;
  flex: 1;
  overflow: auto;
  padding: 4px 0;
}
.fileset-entry {
  position: relative;
  gap: 6px;
  min-width: 0;
  min-height: 48px;
  padding: 6px 6px 6px 12px;
  cursor: default;
}
.fileset-entry:hover {
  background: var(--pvf-surface-hover);
}
.fileset-entry--missing {
  opacity: 0.72;
}
.fileset-entry-icon {
  flex: 0 0 auto;
  color: var(--pvf-primary-hover);
}
.fileset-entry-main {
  display: flex;
  flex: 1;
  min-width: 0;
  flex-direction: column;
  gap: 2px;
}
.fileset-entry-name,
.fileset-entry-path {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.fileset-entry-name-line {
  display: flex;
  align-items: center;
  min-width: 0;
  gap: 4px;
  overflow: hidden;
}
.fileset-entry-name {
  color: var(--pvf-text-tertiary);
}
.fileset-entry-id {
  flex: 0 0 auto;
}
.fileset-entry-missing {
  flex: 0 0 auto;
}
.fileset-entry-path {
  color: var(--pvf-text-muted);
  font-size: 11px;
}
.fileset-entry-meta {
  flex: 0 0 auto;
  flex-direction: column;
  align-items: flex-end;
  gap: 2px;
}
.fileset-entry-size {
  font-size: 10px;
}
.fileset-entry-remove {
  flex: 0 0 auto;
  opacity: 0;
}
.fileset-entry:hover .fileset-entry-remove,
.fileset-entry-remove:focus-visible {
  opacity: 1;
}
.fileset-name-error {
  display: block;
  margin-top: 6px;
  font-size: 12px;
}
.fileset-modal-footer {
  justify-content: flex-end;
  gap: 8px;
}
</style>
