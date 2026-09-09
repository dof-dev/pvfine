<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useMessage } from "naive-ui";
import {
  NAlert,
  NButton,
  NCard,
  NEmpty,
  NInput,
  NModal,
  NSpin,
  NSpace,
  NTag,
  NText,
} from "naive-ui";
import type { VersionChange, VersionCommit } from "../../bindings/pvfine/services/models";
import { useVersionStore } from "../stores/version";
import { useEditorStore } from "../stores/editor";

const version = useVersionStore();
const editor = useEditorStore();
const message = useMessage();

type ConfirmAction =
  | { type: "discard" }
  | { type: "checkout"; commit: VersionCommit };

const confirmAction = ref<ConfirmAction | null>(null);
const confirmRunning = ref(false);
const confirmMessage = computed(() => {
  if (!confirmAction.value) return "";
  if (confirmAction.value.type === "discard") {
    return "这会将工作区恢复到当前 HEAD，未提交的编辑将丢失。确定继续吗？";
  }
  return `将 ${confirmAction.value.commit.message} 加载到当前工作区，当前未提交变更会被替换。确定继续吗？`;
});
const confirmPositiveText = computed(() =>
  confirmAction.value?.type === "checkout" ? "加载版本" : "放弃变更"
);

watch(
  () => version.status.enabled,
  (enabled) => {
    if (!enabled && !confirmRunning.value) confirmAction.value = null;
  }
);
watch(
  () => version.visible,
  (visible) => {
    if (!visible && !confirmRunning.value) confirmAction.value = null;
  }
);
watch(confirmRunning, (running) => {
  if (!running && !version.status.enabled) confirmAction.value = null;
});

function operationLabel(operation: string): string {
  switch (operation) {
    case "add":
      return "新增";
    case "delete":
      return "删除";
    case "modify":
      return "修改";
    default:
      return operation;
  }
}

function operationType(operation: string): "success" | "error" | "info" {
  if (operation === "add") return "success";
  if (operation === "delete") return "error";
  return "info";
}

function shortID(id: string): string {
  return id ? id.slice(0, 10) : "—";
}

function formatTime(timestamp: number): string {
  const milliseconds = timestamp > 10_000_000_000 ? timestamp / 1_000_000 : timestamp * 1000;
  return new Date(milliseconds).toLocaleString();
}

async function initialize(): Promise<void> {
  try {
    await editor.flushPending();
    await version.initialize();
    message.success("版本库已初始化");
  } catch (error: any) {
    message.error(`初始化版本库失败: ${error?.message ?? error}`);
  }
}

async function commit(): Promise<void> {
  try {
    await editor.flushPending();
    const result = await version.commit();
    if (result) message.success(`已创建版本 ${shortID(result.id)}`);
  } catch (error: any) {
    message.error(`提交版本失败: ${error?.message ?? error}`);
  }
}

async function undo(): Promise<void> {
  try {
    await editor.flushPending();
    await version.undo();
    message.success("已撤销最近一次工作区变更");
  } catch (error: any) {
    message.error(`撤销失败: ${error?.message ?? error}`);
  }
}

async function discard(): Promise<void> {
  await editor.flushPending();
  await version.discard();
}

function confirmDiscard(): void {
  if (confirmRunning.value || version.loading || !version.status.changedFiles) return;
  confirmAction.value = { type: "discard" };
}

function confirmCheckout(commit: VersionCommit): void {
  if (confirmRunning.value || version.loading || version.committing) return;
  confirmAction.value = { type: "checkout", commit };
}

function cancelConfirmation(): void {
  if (confirmRunning.value) return;
  confirmAction.value = null;
}

async function executeConfirmation(): Promise<void> {
  const action = confirmAction.value;
  if (!action || confirmRunning.value) return;
  confirmRunning.value = true;
  try {
    if (action.type === "discard") {
      await discard();
      message.success("已放弃未提交变更");
    } else {
      await editor.flushPending();
      await version.checkout(action.commit);
      message.success(`已加载版本 ${shortID(action.commit.id)}`);
    }
    confirmAction.value = null;
  } catch (error: any) {
    const label = action.type === "discard" ? "放弃变更" : "加载版本";
    message.error(`${label}失败: ${error?.message ?? error}`);
  } finally {
    confirmRunning.value = false;
  }
}

function changeTitle(change: VersionChange): string {
  return `${operationLabel(change.operation)} · ${change.path}`;
}
</script>

<template>
  <NModal
    v-model:show="version.visible"
    preset="card"
    title="版本控制"
    style="width: min(860px, calc(100vw - 48px)); max-height: calc(100vh - 80px);"
    :mask-closable="!version.loading && !version.committing && !confirmRunning"
    :close-on-esc="!version.loading && !version.committing && !confirmRunning"
  >
    <NSpin :show="version.loading && !version.enabled">
      <template v-if="!version.enabled">
        <NAlert type="info" :show-icon="false">
          版本库使用当前 PVF 的逻辑文件作为基线，创建后会在旁边生成 .pvfine 目录。
        </NAlert>
        <div class="version-init">
          <NText depth="3">初始化不会修改当前 PVF 文件。</NText>
          <NButton type="primary" :loading="version.loading" @click="initialize">初始化版本库</NButton>
        </div>
      </template>

      <template v-else>
        <div class="version-summary">
          <div>
            <NText strong>{{ version.status.branch }}</NText>
            <NText depth="3"> · HEAD {{ shortID(version.status.headId) }}</NText>
            <div class="version-head-message">{{ version.status.headMessage || "未命名版本" }}</div>
          </div>
          <NSpace align="center" size="small">
            <NTag v-if="version.status.changedFiles" type="warning" size="small">
              {{ version.status.changedFiles }} 个工作区变更
            </NTag>
            <NTag v-if="version.status.needsSave" type="warning" size="small">PVF 未保存</NTag>
            <NTag v-else type="success" size="small">PVF 已保存</NTag>
          </NSpace>
        </div>

        <NAlert v-if="version.error" type="error" :show-icon="false" class="version-alert">
          {{ version.error }}
        </NAlert>

        <NAlert v-if="confirmAction" type="warning" :show-icon="false" class="version-confirm">
          <div class="version-confirm-content">
            <div>{{ confirmMessage }}</div>
            <NSpace size="small">
              <NButton size="small" :disabled="confirmRunning" @click="cancelConfirmation">取消</NButton>
              <NButton
                size="small"
                type="warning"
                :loading="confirmRunning"
                :disabled="confirmRunning || version.loading || version.committing"
                @click="executeConfirmation"
              >
                {{ confirmPositiveText }}
              </NButton>
            </NSpace>
          </div>
        </NAlert>

        <div class="version-section">
          <div class="version-section-title">当前工作区</div>
          <div class="version-commit-row">
            <NInput
              v-model:value="version.commitMessage"
              placeholder="提交说明，例如：调整装备价格"
              :disabled="version.committing || version.loading || !!confirmAction"
              @keyup.enter="commit"
            />
            <NButton
              type="primary"
              :loading="version.committing"
              :disabled="!version.canCommit || version.loading || !!confirmAction"
              @click="commit"
            >
              创建版本
            </NButton>
          </div>
          <NSpace size="small" class="version-actions">
            <NButton
              size="small"
              :loading="version.loading && !confirmAction"
              :disabled="!version.status.pendingChangeSets || version.loading || !!confirmAction"
              @click="undo"
            >
              撤销最近变更
            </NButton>
            <NButton
              size="small"
              :disabled="!version.status.changedFiles || version.loading || !!confirmAction"
              @click="confirmDiscard"
            >
              放弃未提交变更
            </NButton>
          </NSpace>
          <div v-if="version.changes.length" class="version-changes">
            <div v-for="change in version.changes" :key="`${change.operation}:${change.path}`" class="version-change">
              <NTag :type="operationType(change.operation)" size="small" :bordered="false">
                {{ operationLabel(change.operation) }}
              </NTag>
              <NText :title="changeTitle(change)">{{ change.path }}</NText>
            </div>
          </div>
          <NEmpty v-else description="工作区没有相对 HEAD 的变更" size="small" />
        </div>

        <div class="version-section">
          <div class="version-section-title">版本历史</div>
          <div v-if="version.history.length" class="version-history">
            <NCard
              v-for="commitItem in version.history"
              :key="commitItem.id"
              size="small"
              embedded
              class="version-commit"
            >
              <div class="version-commit-main">
                <div>
                  <NText strong>{{ commitItem.message }}</NText>
                  <div class="version-commit-meta">
                    {{ formatTime(commitItem.createdAt) }} · {{ commitItem.changeCount }} 个文件 · {{ shortID(commitItem.id) }}
                  </div>
                </div>
                <NSpace size="small">
                  <NButton
                    size="small"
                    secondary
                    :disabled="version.loading || version.committing || !!confirmAction"
                    @click="version.toggleCommitChanges(commitItem)"
                  >
                    {{ version.expandedCommitID === commitItem.id ? "收起变更" : "查看变更" }}
                  </NButton>
                  <NButton
                    size="small"
                    secondary
                    :disabled="version.loading || version.committing || !!confirmAction"
                    @click="confirmCheckout(commitItem)"
                  >
                    加载到工作区
                  </NButton>
                </NSpace>
              </div>
              <div v-if="version.expandedCommitID === commitItem.id" class="commit-change-list">
                <div
                  v-for="change in version.commitChanges"
                  :key="`${change.operation}:${change.path}`"
                  class="version-change"
                >
                  <NTag :type="operationType(change.operation)" size="small" :bordered="false">
                    {{ operationLabel(change.operation) }}
                  </NTag>
                  <NText :title="changeTitle(change)">{{ change.path }}</NText>
                </div>
                <NEmpty v-if="version.commitChanges.length === 0" description="该版本没有文件变更" size="small" />
              </div>
            </NCard>
          </div>
          <NEmpty v-else description="暂无版本历史" size="small" />
        </div>
      </template>
    </NSpin>
  </NModal>
</template>

<style scoped>
.version-init {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin-top: 18px;
}
.version-summary,
.version-commit-main,
.version-commit-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.version-head-message {
  margin-top: 5px;
  color: rgba(255, 255, 255, 0.78);
}
.version-alert {
  margin-top: 14px;
}
.version-confirm {
  margin-top: 14px;
}
.version-confirm-content {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}
.version-section {
  margin-top: 20px;
}
.version-section-title {
  margin-bottom: 10px;
  font-weight: 600;
}
.version-commit-row :deep(.n-input) {
  flex: 1;
}
.version-actions {
  margin-top: 8px;
}
.version-changes {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 6px 16px;
  max-height: 190px;
  margin-top: 12px;
  overflow: auto;
}
.version-change {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.version-change :deep(.n-text) {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.version-history {
  display: flex;
  flex-direction: column;
  gap: 8px;
  max-height: 320px;
  overflow: auto;
}
.version-commit-meta {
  margin-top: 4px;
  color: rgba(255, 255, 255, 0.5);
  font-size: 12px;
}
.commit-change-list {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 6px 16px;
  margin-top: 12px;
  padding-top: 10px;
  border-top: 1px solid rgba(128, 128, 128, 0.2);
}
@media (max-width: 720px) {
  .version-summary,
  .version-commit-main,
  .version-commit-row,
  .version-confirm-content {
    align-items: stretch;
    flex-direction: column;
  }
  .version-commit-row :deep(.n-button) {
    width: 100%;
  }
  .version-changes {
    grid-template-columns: 1fr;
  }
  .commit-change-list {
    grid-template-columns: 1fr;
  }
}
</style>
