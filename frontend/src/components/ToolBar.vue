<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useMessage } from "naive-ui";
import {
  FolderOpen24Regular,
  Save24Regular,
  ArchiveMultiple24Regular,
  FolderArrowUp24Regular,
  Stop24Regular,
  Search24Regular,
  PanelRight24Regular,
  PanelRightContract24Regular,
  ArrowSync24Regular,
  DocumentSync24Regular,
  Settings24Regular,
} from "@vicons/fluent";
import { NButton, NIcon, NTooltip, NProgress, NText, useDialog } from "naive-ui";
import { useArchiveStore } from "../stores/archive";
import { useEditorStore } from "../stores/editor";
import { useAdvancedSearchStore } from "../stores/advancedSearch";
import { useFileSetStore } from "../stores/fileSets";
import { AnnotationService, UpdateService } from "../../bindings/pvfine/services";
import { useSettingsStore } from "../stores/settings";
import { useExplorerStore } from "../stores/explorer";

const archive = useArchiveStore();
const editor = useEditorStore();
const advancedSearch = useAdvancedSearchStore();
const fileSets = useFileSetStore();
const settings = useSettingsStore();
const explorer = useExplorerStore();
const message = useMessage();
const dialog = useDialog();
const checkingUpdates = ref(false);
const reloadingAnnotations = ref(false);

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

async function onSave() {
  try {
    if (archive.info?.path) {
      await editor.save();
      message.success("已保存到源文件");
    } else {
      await onSaveAs();
    }
  } catch (e: any) {
    if (!isCancel(e)) message.error(`保存失败: ${e?.message ?? e}`);
  }
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

function toggleFileSetSidebar() {
  fileSets.visible = !fileSets.visible;
}

async function onCheckUpdates() {
  if (checkingUpdates.value) return;
  checkingUpdates.value = true;
  try {
    await UpdateService.CheckForUpdates();
  } catch (e: any) {
    message.error(`检查更新失败: ${e?.message ?? e}`);
  } finally {
    checkingUpdates.value = false;
  }
}

async function onReloadAnnotations() {
  if (reloadingAnnotations.value) return;
  reloadingAnnotations.value = true;
  try {
    await editor.flushPending();
    const result = await AnnotationService.ReloadRules();
    await Promise.all([editor.refreshAnnotations(), explorer.refreshAnnotations()]);
    message.success(
      `已重载 ${result.ruleCount} 条标注规则、${result.relationCount} 个关联类型`
    );
  } catch (e: any) {
    message.error(`重载标注规则失败: ${e?.message ?? e}`);
  } finally {
    reloadingAnnotations.value = false;
  }
}

function isCancel(e: any): boolean {
  return String(e?.message ?? e).includes("cancel");
}
</script>

<template>
  <div class="toolbar">
    <div class="tb-group">
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
          <NButton quaternary :disabled="!canSaveToSource" @click="onSave">
            <template #icon><NIcon><Save24Regular /></NIcon></template>
            保存
          </NButton>
        </template>
        保存到源文件 (Cmd+S)
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
          <NButton quaternary :disabled="!archive.open" @click="advancedSearch.open">
            <template #icon><NIcon><Search24Regular /></NIcon></template>
            高级搜索
          </NButton>
        </template>
        在当前归档中搜索二进制或字符串池
      </NTooltip>
    </div>

    <div class="tb-sep" />

    <div class="tb-group">
      <NButton quaternary v-if="!archive.unpacking" :disabled="!archive.open" @click="onUnpack">
        <template #icon><NIcon><ArchiveMultiple24Regular /></NIcon></template>
        解包
      </NButton>
      <NButton quaternary v-else type="warning" @click="onCancelUnpack">
        <template #icon><NIcon><Stop24Regular /></NIcon></template>
        取消解包
      </NButton>
    </div>

    <div class="tb-spacer" />

    <NTooltip trigger="hover">
      <template #trigger>
        <NButton
          quaternary
          :loading="reloadingAnnotations"
          aria-label="重载标注规则"
          @click="onReloadAnnotations"
        >
          <template #icon><NIcon><DocumentSync24Regular /></NIcon></template>
        </NButton>
      </template>
      从磁盘重新加载标注规则
    </NTooltip>

    <NTooltip trigger="hover">
      <template #trigger>
        <NButton
          quaternary
          :loading="checkingUpdates"
          aria-label="检查更新"
          @click="onCheckUpdates"
        >
          <template #icon><NIcon><ArrowSync24Regular /></NIcon></template>
          检查更新
        </NButton>
      </template>
      检查 pvfine 更新
    </NTooltip>

    <NTooltip trigger="hover">
      <template #trigger>
        <NButton quaternary aria-label="设置" @click="settings.open">
          <template #icon><NIcon><Settings24Regular /></NIcon></template>
        </NButton>
      </template>
      设置
    </NTooltip>

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

    <NTooltip>
      <template #trigger>
        <NButton quaternary aria-label="切换文件集侧栏" @click="toggleFileSetSidebar">
          <template #icon>
            <NIcon>
              <PanelRightContract24Regular v-if="fileSets.visible" />
              <PanelRight24Regular v-else />
            </NIcon>
          </template>
        </NButton>
      </template>
      {{ fileSets.visible ? "收起文件集" : "显示文件集" }}
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
