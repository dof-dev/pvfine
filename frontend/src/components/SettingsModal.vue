<script setup lang="ts">
import { ref } from "vue";
import { ArrowSync24Regular, DocumentSync24Regular } from "@vicons/fluent";
import {
  NButton,
  NIcon,
  NModal,
  NRadioButton,
  NRadioGroup,
  NSpin,
  NSwitch,
  useMessage,
} from "naive-ui";
import { AnnotationService, UpdateService } from "../../bindings/pvfine/services";
import {
  useSettingsStore,
  type AnnotationTagPlacement,
  type ExplorerOpenMode,
} from "../stores/settings";
import { useEditorStore } from "../stores/editor";
import { useExplorerStore } from "../stores/explorer";

const settings = useSettingsStore();
const editor = useEditorStore();
const explorer = useExplorerStore();
const message = useMessage();
const checkingUpdates = ref(false);
const reloadingAnnotations = ref(false);

async function onPlacementChange(value: string | number | boolean) {
  try {
    await settings.savePlacement(value as AnnotationTagPlacement);
  } catch (error: any) {
    message.error(`保存设置失败: ${error?.message ?? error}`);
  }
}

async function onExplorerOpenModeChange(value: string | number | boolean) {
  try {
    await settings.saveExplorerOpenMode(value as ExplorerOpenMode);
  } catch (error: any) {
    message.error(`保存设置失败: ${error?.message ?? error}`);
  }
}

async function onVimModeChange(value: boolean) {
  try {
    await settings.saveVimMode(value);
  } catch (error: any) {
    message.error(`保存设置失败: ${error?.message ?? error}`);
  }
}

async function onBackupSourceOnSaveChange(value: boolean) {
  try {
    await settings.saveBackupSourceOnSave(value);
  } catch (error: any) {
    message.error(`保存设置失败: ${error?.message ?? error}`);
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
  } catch (error: any) {
    message.error(`重载标注规则失败: ${error?.message ?? error}`);
  } finally {
    reloadingAnnotations.value = false;
  }
}

async function onCheckUpdates() {
  if (checkingUpdates.value) return;
  checkingUpdates.value = true;
  try {
    await UpdateService.CheckForUpdates();
  } catch (error: any) {
    message.error(`检查更新失败: ${error?.message ?? error}`);
  } finally {
    checkingUpdates.value = false;
  }
}
</script>

<template>
  <NModal
    v-model:show="settings.visible"
    preset="card"
    title="设置"
    :bordered="false"
    :style="{ width: '520px' }"
  >
    <NSpin :show="!settings.loaded || settings.saving">
      <section class="settings-section">
        <div class="section-title">编辑器标注</div>
        <div class="setting-row">
          <div>
            <div class="setting-label">Tag 显示位置</div>
            <div class="setting-description">调整规则标注在编辑器中的显示方式</div>
          </div>
          <NRadioGroup
            :value="settings.annotationTagPlacement"
            size="small"
            @update:value="onPlacementChange"
          >
            <NRadioButton value="after-target">内容后</NRadioButton>
            <NRadioButton value="line-end">行末</NRadioButton>
            <NRadioButton value="hidden">隐藏</NRadioButton>
          </NRadioGroup>
        </div>
        <div class="setting-row">
          <div>
            <div class="setting-label">Vim 模式</div>
            <div class="setting-description">使用 Vim 的普通、插入和可视模式编辑文本</div>
          </div>
          <NSwitch :value="settings.vimMode" @update:value="onVimModeChange" />
        </div>
      </section>
      <section class="settings-section">
        <div class="section-title">保存</div>
        <div class="setting-row">
          <div>
            <div class="setting-label">保存前备份源文件</div>
            <div class="setting-description">覆盖源文件前，将原文件备份为同目录下的 .bak 文件</div>
          </div>
          <NSwitch
            :value="settings.backupSourceOnSave"
            @update:value="onBackupSourceOnSaveChange"
          />
        </div>
      </section>
      <section class="settings-section">
        <div class="section-title">资源管理器</div>
        <div class="setting-row">
          <div>
            <div class="setting-label">打开节点方式</div>
            <div class="setting-description">设置文件和目录使用单击还是双击操作</div>
          </div>
          <NRadioGroup
            :value="settings.explorerOpenMode"
            size="small"
            @update:value="onExplorerOpenModeChange"
          >
            <NRadioButton value="single-click">单击打开</NRadioButton>
            <NRadioButton value="double-click">双击打开</NRadioButton>
          </NRadioGroup>
        </div>
      </section>
      <section class="settings-section">
        <div class="section-title">维护</div>
        <div class="setting-row">
          <div>
            <div class="setting-label">重载标注规则</div>
            <div class="setting-description">从磁盘重新加载标注规则，并刷新当前编辑器中的标注</div>
          </div>
          <NButton
            size="small"
            secondary
            :loading="reloadingAnnotations"
            aria-label="重载标注规则"
            @click="onReloadAnnotations"
          >
            <template #icon><NIcon><DocumentSync24Regular /></NIcon></template>
            重载
          </NButton>
        </div>
        <div class="setting-row">
          <div>
            <div class="setting-label">检查更新</div>
            <div class="setting-description">检查 pvfine 是否有可用的新版本</div>
          </div>
          <NButton
            size="small"
            secondary
            :loading="checkingUpdates"
            aria-label="检查更新"
            @click="onCheckUpdates"
          >
            <template #icon><NIcon><ArrowSync24Regular /></NIcon></template>
            检查
          </NButton>
        </div>
      </section>
    </NSpin>
  </NModal>
</template>

<style scoped>
.settings-section {
  min-height: 76px;
}
.section-title {
  margin-bottom: 12px;
  color: rgba(255, 255, 255, 0.72);
  font-size: 12px;
  font-weight: 600;
}
.setting-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
}
.setting-row + .setting-row {
  margin-top: 16px;
}
.setting-label {
  color: rgba(255, 255, 255, 0.92);
  font-weight: 500;
}
.setting-description {
  margin-top: 4px;
  color: rgba(255, 255, 255, 0.45);
  font-size: 12px;
}
</style>
