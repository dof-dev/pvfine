<script setup lang="ts">
import { computed, watch } from "vue";
import { useMessage } from "naive-ui";
import {
  FolderOpen24Regular,
  ArrowDownload24Regular,
  Save24Regular,
  ArchiveMultiple24Regular,
  FolderArrowUp24Regular,
  Stop24Regular,
  Search24Regular,
  BookmarkMultiple24Regular,
  PanelRight24Regular,
  PanelRightContract24Regular,
  DocumentSync24Regular,
  Settings24Regular,
} from "@vicons/fluent";
import {
  NButton,
  NIcon,
  NTooltip,
  NProgress,
  NText,
  useDialog,
} from "naive-ui";
import { useArchiveStore } from "../stores/archive";
import { useEditorStore } from "../stores/editor";
import { useAdvancedSearchStore } from "../stores/advancedSearch";
import { useSidebarStore } from "../stores/sidebar";
import { useSettingsStore } from "../stores/settings";
import { useImportStore } from "../stores/import";
import { useVersionStore } from "../stores/version";

const archive = useArchiveStore();
const editor = useEditorStore();
const advancedSearch = useAdvancedSearchStore();
const sidebar = useSidebarStore();
const settings = useSettingsStore();
const importer = useImportStore();
const version = useVersionStore();
const message = useMessage();
const dialog = useDialog();

const canSave = computed(() => archive.open && !editor.saving);
const canSaveToSource = computed(() => archive.open && !!archive.info?.path && !editor.saving);
watch(
  () => archive.unpackMessage,
  (msg) => {
    if (msg) message.info(msg);
  }
);

async function onOpen() {
  try {
    await archive.openDialog();
    if (archive.open) message.success(`已打开 ${archive.info?.fileCount.toLocaleString()} 个文件`);
  } catch (e: any) {
    if (!isCancel(e)) message.error(`打开失败: ${e?.message ?? e}`);
  }
}

function onImport(): void {
  importer.open("");
}

async function saveToSource() {
  try {
    await editor.save();
    message.success("已保存到源文件");
  } catch (e: any) {
    if (!isCancel(e)) message.error(`保存失败: ${e?.message ?? e}`);
  }
}

function onSave() {
  if (!archive.info?.path) {
    void onSaveAs();
    return;
  }
  const backupHint = settings.backupSourceOnSave
    ? "保存前会将当前源文件备份为同目录下的 .bak 文件。"
    : "当前未启用源文件备份。";
  dialog.warning({
    title: "确认保存到源文件",
    content: `保存会覆盖源文件中的当前内容。${backupHint}确定继续吗？`,
    positiveText: "确认保存",
    negativeText: "取消",
    onPositiveClick: saveToSource,
  });
}

async function onSaveAs() {
  try {
    const path = await editor.saveAs();
    if (path) message.success(`已另存为 ${path}`);
  } catch (e: any) {
    if (!isCancel(e)) message.error(`另存为失败: ${e?.message ?? e}`);
  }
}

function onUnpack() {
  dialog.warning({
    title: "解包归档",
    content: `将 ${archive.info?.fileCount.toLocaleString()} 个文件解包到所选目录(约 ${(archive.info! ? (archive.info!.bodySize * 10.79) / 1e6 : 0).toFixed(0)}MB)。继续?`,
    positiveText: "选择目录…",
    negativeText: "取消",
    onPositiveClick: async () => {
      try {
        const started = await archive.unpackDialog();
        if (!started) return;
        message.info("解包已开始");
      } catch (e: any) {
        if (!isCancel(e)) message.error(`解包失败: ${e?.message ?? e}`);
      }
    },
  });
}

function onCancelUnpack() {
  archive.cancelUnpack();
}

function openBookmarks(): void {
  sidebar.show("bookmarks");
}

function isCancel(e: any): boolean {
  return String(e?.message ?? e).includes("cancel");
}
</script>

<template>
  <div class="toolbar" role="toolbar" aria-label="主工具栏">
    <div class="tb-group" role="group" aria-label="文件">
      <NTooltip trigger="hover">
        <template #trigger>
          <NButton quaternary :loading="archive.loading" @click="onOpen">
            <template #icon><NIcon><FolderOpen24Regular /></NIcon></template>
            打开
          </NButton>
        </template>
        打开 PVF 归档 (Cmd+O)
      </NTooltip>

      <NTooltip trigger="hover">
        <template #trigger>
          <NButton quaternary :disabled="!archive.open" @click="version.open">
            <template #icon><NIcon><DocumentSync24Regular /></NIcon></template>
            版本
          </NButton>
        </template>
        管理工作区版本、提交和历史
      </NTooltip>

      <NTooltip trigger="hover">
        <template #trigger>
          <NButton quaternary :disabled="!canSaveToSource" @click="onSave">
            <template #icon><NIcon><Save24Regular /></NIcon></template>
            保存
          </NButton>
        </template>
        保存到源文件（需要确认）
      </NTooltip>

      <NTooltip trigger="hover">
        <template #trigger>
          <NButton quaternary :disabled="!canSave" @click="onSaveAs">
            <template #icon><NIcon><FolderArrowUp24Regular /></NIcon></template>
            另存为
          </NButton>
        </template>
        另存为新 PVF (Cmd+Shift+S)
      </NTooltip>

      <NTooltip trigger="hover">
        <template #trigger>
          <NButton
            quaternary
            :disabled="!archive.open || importer.running"
            @click="onImport"
          >
            <template #icon><NIcon><ArrowDownload24Regular /></NIcon></template>
            导入
          </NButton>
        </template>
        批量导入文件到归档根目录
      </NTooltip>
    </div>

    <div class="tb-sep" />

    <div class="tb-group" role="group" aria-label="编辑器">
      <NTooltip trigger="hover">
        <template #trigger>
          <NButton quaternary :disabled="!archive.open" @click="advancedSearch.open">
            <template #icon><NIcon><Search24Regular /></NIcon></template>
            高级搜索
          </NButton>
        </template>
        在当前归档中搜索二进制或字符串池
      </NTooltip>
    </div>

    <div class="tb-sep" />

    <div class="tb-group" role="group" aria-label="归档">
      <NButton quaternary v-if="!archive.unpacking" :disabled="!archive.open" @click="onUnpack">
        <template #icon><NIcon><ArchiveMultiple24Regular /></NIcon></template>
        解包
      </NButton>
      <NButton quaternary v-else type="warning" @click="onCancelUnpack">
        <template #icon><NIcon><Stop24Regular /></NIcon></template>
        取消解包
      </NButton>
      <div v-if="archive.unpacking" class="unpack-progress">
        <NText depth="3">
          解包中 {{ archive.unpackProgress.done.toLocaleString() }} /
          {{ archive.unpackProgress.total.toLocaleString() }}
        </NText>
        <NProgress
          type="line"
          :show-indicator="false"
          :percentage="
            archive.unpackProgress.total
              ? Math.round((archive.unpackProgress.done / archive.unpackProgress.total) * 100)
              : 0
          "
          style="width: 160px"
        />
      </div>
    </div>

    <div class="tb-spacer" />

    <div class="tb-group" role="group" aria-label="视图">
      <NTooltip>
        <template #trigger>
          <NButton quaternary aria-label="切换侧栏" @click="sidebar.toggle">
            <template #icon>
              <NIcon>
                <PanelRightContract24Regular v-if="sidebar.visible" />
                <PanelRight24Regular v-else />
              </NIcon>
            </template>
          </NButton>
        </template>
        {{ sidebar.visible ? "收起侧栏" : "显示侧栏" }}
      </NTooltip>

      <NTooltip>
        <template #trigger>
          <NButton
            quaternary
            :type="sidebar.activePanel === 'bookmarks' && sidebar.visible ? 'primary' : 'default'"
            aria-label="打开书签侧栏"
            @click="openBookmarks"
          >
            <template #icon><NIcon><BookmarkMultiple24Regular /></NIcon></template>
          </NButton>
        </template>
        打开书签侧栏
      </NTooltip>
    </div>

    <div class="tb-sep" />

    <NTooltip trigger="hover">
      <template #trigger>
        <NButton quaternary aria-label="设置" @click="settings.open">
          <template #icon><NIcon><Settings24Regular /></NIcon></template>
        </NButton>
      </template>
      设置
    </NTooltip>
  </div>
</template>

<style scoped>
.toolbar {
  display: flex;
  align-items: center;
  gap: 4px;
  height: 46px;
  padding: 0 10px;
  border-bottom: 1px solid rgba(128, 128, 128, 0.2);
  flex-shrink: 0;
}
.tb-group {
  display: flex;
  align-items: center;
  gap: 4px;
}
.tb-sep {
  width: 1px;
  height: 22px;
  margin: 0 8px;
  background: rgba(128, 128, 128, 0.25);
}
.tb-spacer {
  flex: 1;
}
.unpack-progress {
  display: flex;
  align-items: center;
  gap: 10px;
}
</style>
