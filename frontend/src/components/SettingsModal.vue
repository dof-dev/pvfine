<script setup lang="ts">
import { ref } from "vue";
import { ArrowSync24Regular, DocumentSync24Regular, FolderOpen24Regular } from "@vicons/fluent";
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
import { useImageStore } from "../stores/images";

const settings = useSettingsStore();
const editor = useEditorStore();
const explorer = useExplorerStore();
const images = useImageStore();
const message = useMessage();
const checkingUpdates = ref(false);
const reloadingAnnotations = ref(false);
const selectingNPK = ref(false);
const rebuildingNPK = ref(false);

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

function imageProgressText(): string {
  const value = images.status;
  if (value.state === "building") {
    if (value.total > 0) return `正在扫描 ${value.done}/${value.total} 个 NPK`;
    return "正在扫描 NPK 目录…";
  }
  if (value.state === "ready") {
    const details = [`已索引 ${value.npkFiles} 个 NPK、${value.imgFiles} 个 IMG、${value.imageCount} 张图片`];
    if (value.skipped > 0) details.push(`跳过 ${value.skipped} 项`);
    if (value.duplicates > 0) details.push(`重复 ${value.duplicates} 项`);
    return details.join("，");
  }
  if (value.state === "error") return value.error || "图像索引失败";
  return "未配置 NPK 目录";
}

async function onSelectNPKDirectory(): Promise<void> {
  if (selectingNPK.value) return;
  selectingNPK.value = true;
  try {
    const next = await images.selectDirectory();
    settings.npkDirectory = next.directory ?? "";
    if (next.directory) message.success("已选择 NPK 目录，开始建立图标索引");
  } catch (error: any) {
    message.error(`选择 NPK 目录失败: ${error?.message ?? error}`);
  } finally {
    selectingNPK.value = false;
  }
}

async function onRebuildNPKIndex(): Promise<void> {
  if (rebuildingNPK.value || !images.status.directory) return;
  rebuildingNPK.value = true;
  try {
    await images.rebuild();
  } catch (error: any) {
    message.error(`重建图标索引失败: ${error?.message ?? error}`);
  } finally {
    rebuildingNPK.value = false;
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
        <div class="section-title">图标资源</div>
        <div class="setting-row setting-row--stacked">
          <div class="image-index-main">
            <div class="setting-label">NPK 目录</div>
            <div class="setting-description setting-path" :title="settings.npkDirectory">
              {{ settings.npkDirectory || "未选择" }}
            </div>
            <div class="setting-description image-index-status" :class="{ 'image-index-status--error': images.status.state === 'error' }">
              {{ imageProgressText() }}
            </div>
            <div v-if="images.status.state === 'building' && images.status.total > 0" class="image-progress">
              <span :style="{ width: `${Math.round((images.status.done / images.status.total) * 100)}%` }" />
            </div>
          </div>
          <div class="image-index-actions">
            <NButton size="small" secondary :loading="selectingNPK" @click="onSelectNPKDirectory">
              <template #icon><NIcon><FolderOpen24Regular /></NIcon></template>
              选择目录
            </NButton>
            <NButton
              size="small"
              secondary
              :loading="rebuildingNPK"
              :disabled="!images.status.directory || images.status.state === 'building'"
              @click="onRebuildNPKIndex"
            >
              <template #icon><NIcon><ArrowSync24Regular /></NIcon></template>
              重建
            </NButton>
          </div>
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
.setting-row--stacked {
  align-items: flex-start;
}
.image-index-main {
  min-width: 0;
  flex: 1;
}
.setting-path {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.image-index-status--error {
  color: #e88b8b;
}
.image-index-actions {
  display: flex;
  flex: 0 0 auto;
  gap: 8px;
}
.image-progress {
  height: 4px;
  margin-top: 8px;
  overflow: hidden;
  background: rgba(255, 255, 255, 0.12);
  border-radius: 2px;
}
.image-progress span {
  display: block;
  height: 100%;
  background: #4f8cff;
  transition: width 0.2s ease;
}
</style>
