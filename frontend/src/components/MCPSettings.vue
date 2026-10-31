<script setup lang="ts">
import { onMounted, ref } from "vue";
import { NAlert, NButton, NIcon, NInputNumber, NSwitch, NTag, useMessage } from "naive-ui";
import {
  Bot24Regular,
  CheckmarkCircle24Regular,
  Copy24Regular,
  DismissCircle24Regular,
  Globe24Regular,
  Wrench24Regular,
} from "@vicons/fluent";
import { Clipboard } from "@wailsio/runtime";
import { MCPService } from "../../bindings/pvfine/services";
import type { MCPStatus } from "../../bindings/pvfine/services/models";
import BuiltinSkills from "./BuiltinSkills.vue";

const message = useMessage();
const status = ref<MCPStatus | null>(null);
const port = ref(19245);
const loading = ref(false);

async function refresh() {
  try {
    status.value = await MCPService.Status();
    port.value = status.value.port;
  } catch {
    message.error("无法读取 MCP 状态");
  }
}

async function configure(enabled: boolean) {
  loading.value = true;
  try {
    status.value = await MCPService.Configure(enabled, port.value);
  } catch (error: any) {
    message.error(String(error?.message ?? error));
    await refresh();
  } finally {
    loading.value = false;
  }
}

async function copyConfig() {
  try {
    await Clipboard.SetText(await MCPService.ConnectionConfig());
    message.success("连接配置已复制，请粘贴到 AI 客户端的 MCP 配置文件中");
  } catch {
    message.error("复制连接配置失败");
  }
}

onMounted(refresh);
</script>

<template>
  <div class="mcp-tab-container">
    <!-- Hero 概览卡片 -->
    <div class="hero-card">
      <div class="hero-card-icon">
        <NIcon :size="28"><Bot24Regular /></NIcon>
      </div>
      <div class="hero-card-body">
        <div class="hero-card-header">
          <span class="hero-card-title">Model Context Protocol (MCP) 服务</span>
          <NTag
            v-if="status?.enabled"
            type="success"
            size="small"
            round
            :bordered="false"
          >
            <template #icon><NIcon><CheckmarkCircle24Regular /></NIcon></template>
            服务监听中
          </NTag>
          <NTag
            v-else-if="status?.error"
            type="error"
            size="small"
            round
            :bordered="false"
          >
            <template #icon><NIcon><DismissCircle24Regular /></NIcon></template>
            服务异常
          </NTag>
          <NTag v-else size="small" round :bordered="false">未启用</NTag>
        </div>
        <div class="hero-card-desc">
          让 Cursor、Claude Code、VS Code 等外部 AI 编程助手直接连接当前 pvfine 实例，进行归档检索、脚本上下文分析与安全修改。
        </div>
      </div>
    </div>

    <!-- 异常提示 -->
    <NAlert v-if="status?.error" type="error" :bordered="false" class="mcp-error-alert">
      {{ status.error }}
    </NAlert>

    <!-- 服务配置 -->
    <section class="settings-group">
      <div class="group-header">
        <div class="group-title">服务配置</div>
        <div class="group-subtitle">配置本机 HTTP/SSE 监听端口与服务开关</div>
      </div>

      <div class="settings-card">
        <!-- 服务开关 -->
        <div class="setting-item">
          <div class="setting-item-icon">
            <NIcon :size="18"><Globe24Regular /></NIcon>
          </div>
          <div class="setting-item-content">
            <div class="setting-item-label">启用本机 MCP 服务</div>
            <div class="setting-item-desc">
              开启后在指定端口监听外部 AI 请求。AI 修改先进入草稿预览，由你确认后再保存至归档。
            </div>
          </div>
          <div class="setting-item-control">
            <NSwitch
              :value="status?.enabled ?? false"
              :loading="loading"
              :disabled="loading || status?.busy"
              @update:value="configure"
            />
          </div>
        </div>

        <div class="setting-card-divider" />

        <!-- 端口设置 -->
        <div class="setting-item">
          <div class="setting-item-icon">
            <NIcon :size="18"><Wrench24Regular /></NIcon>
          </div>
          <div class="setting-item-content">
            <div class="setting-item-label">监听端口</div>
            <div class="setting-item-desc">
              范围 1024–65535，默认 19245。更改端口需在未启用状态下修改。
            </div>
          </div>
          <div class="setting-item-control">
            <NInputNumber
              v-model:value="port"
              :min="1024"
              :max="65535"
              :disabled="loading || status?.enabled"
              size="small"
              style="width: 140px"
            />
          </div>
        </div>

        <!-- 运行状态与端点信息 -->
        <template v-if="status?.enabled">
          <div class="setting-card-divider" />
          <div class="setting-item">
            <div class="setting-item-icon">
              <NIcon :size="18"><CheckmarkCircle24Regular /></NIcon>
            </div>
            <div class="setting-item-content">
              <div class="setting-item-label">服务端点地址</div>
              <div class="setting-item-desc setting-item-desc--path">
                {{ status.endpoint }}
              </div>
              <div class="setting-item-desc">
                配置仅包含本机连接地址，无需鉴权。端口不变时，重启后可直接使用原配置。
              </div>
            </div>
            <div class="setting-item-control">
              <NButton type="primary" secondary size="small" @click="copyConfig">
                <template #icon><NIcon :size="14"><Copy24Regular /></NIcon></template>
                复制连接配置
              </NButton>
            </div>
          </div>
        </template>
      </div>
    </section>

    <!-- 内置技能 -->
    <BuiltinSkills />
  </div>
</template>

<style scoped>
.mcp-tab-container {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

/* Hero 说明卡片 */
.hero-card {
  display: flex;
  gap: 16px;
  padding: 16px;
  background: var(--pvf-surface-card);
  border: 1px solid var(--pvf-border-subtle);
  border-radius: 10px;
}
.hero-card-icon {
  flex: 0 0 44px;
  height: 44px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--pvf-primary-soft);
  color: var(--pvf-primary);
  border-radius: 10px;
}
.hero-card-body {
  flex: 1;
  min-width: 0;
}
.hero-card-header {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 4px;
}
.hero-card-title {
  color: var(--pvf-text-primary);
  font-size: 14px;
  font-weight: 600;
}
.hero-card-desc {
  color: var(--pvf-text-secondary);
  font-size: 12px;
  line-height: 1.5;
}

.mcp-error-alert {
  border-radius: 9px;
}

/* 分组与卡片通用 */
.settings-group {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.group-header {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.group-title {
  color: var(--pvf-text-primary);
  font-size: 13px;
  font-weight: 600;
}
.group-subtitle {
  color: var(--pvf-text-faint);
  font-size: 12px;
}

.settings-card {
  background: var(--pvf-surface-card);
  border: 1px solid var(--pvf-border-subtle);
  border-radius: 10px;
  overflow: hidden;
}
.setting-card-divider {
  height: 1px;
  margin: 0 16px;
  background: var(--pvf-border-faint);
}

.setting-item {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 14px 16px;
}
.setting-item-icon {
  flex: 0 0 34px;
  height: 34px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--pvf-surface-subtle);
  border: 1px solid var(--pvf-border-faint);
  border-radius: 8px;
  color: var(--pvf-primary);
}
.setting-item-content {
  flex: 1;
  min-width: 0;
}
.setting-item-label {
  color: var(--pvf-text-primary);
  font-size: 13px;
  font-weight: 500;
}
.setting-item-desc {
  margin-top: 3px;
  color: var(--pvf-text-faint);
  font-size: 12px;
  line-height: 1.45;
}
.setting-item-desc--path {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  color: var(--pvf-text-primary);
  word-break: break-all;
}
.setting-item-control {
  flex: 0 0 auto;
}
</style>
