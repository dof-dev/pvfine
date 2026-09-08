<script setup lang="ts">
import { NModal, NRadioButton, NRadioGroup, NSpin, useMessage } from "naive-ui";
import {
  useSettingsStore,
  type AnnotationTagPlacement,
} from "../stores/settings";

const settings = useSettingsStore();
const message = useMessage();

async function onPlacementChange(value: string | number | boolean) {
  try {
    await settings.savePlacement(value as AnnotationTagPlacement);
  } catch (error: any) {
    message.error(`保存设置失败: ${error?.message ?? error}`);
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
