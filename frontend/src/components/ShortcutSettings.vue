<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { NButton, NIcon, NInput, useDialog, useMessage } from "naive-ui";
import { Search24Regular } from "@vicons/fluent";
import { useSettingsStore } from "../stores/settings";
import {
  assignBinding,
  bindingFromEvent,
  effectiveBinding,
  formatBinding,
  reservedBindingReason,
  shortcutCommands,
  type ShortcutCommandId,
  type ShortcutOverrides,
} from "../shortcuts";

const settings = useSettingsStore();
const dialog = useDialog();
const message = useMessage();
const recording = ref<ShortcutCommandId | null>(null);
const recordError = ref("");
const searchQuery = ref("");

const filteredCommands = computed(() => {
  const query = searchQuery.value.trim().toLowerCase();
  if (!query) return shortcutCommands;
  return shortcutCommands.filter((command) => {
    const labelMatch = command.label.toLowerCase().includes(query);
    const groupMatch = command.group.toLowerCase().includes(query);
    const binding = effectiveBinding(command.id, settings.shortcutOverrides).toLowerCase();
    const bindingMatch = binding.includes(query);
    return labelMatch || groupMatch || bindingMatch;
  });
});

const groups = computed(() => [
  ...new Set(filteredCommands.value.map((command) => command.group)),
]);

watch([() => settings.visible, () => settings.activeTab], ([visible, tab]) => {
  if (!visible || tab !== "shortcuts") cancelRecording();
});

// 在捕获阶段监听整个窗口。按钮失焦、Naive UI 焦点陷阱或其他子组件
// 消费键盘事件时，录制仍能收到实际按键。
onMounted(() => window.addEventListener("keydown", onRecordKeydown, true));
onUnmounted(() => window.removeEventListener("keydown", onRecordKeydown, true));

function cancelRecording(): void {
  recording.value = null;
  recordError.value = "";
}

function beginRecording(command: ShortcutCommandId): void {
  recording.value = command;
  recordError.value = "";
}

function commandLabel(id: ShortcutCommandId): string {
  return shortcutCommands.find((command) => command.id === id)?.label ?? id;
}

async function save(overrides: ShortcutOverrides): Promise<boolean> {
  try {
    await settings.updateShortcutOverrides(overrides);
    return true;
  } catch (error: any) {
    message.error(`保存快捷键失败：${error?.message ?? error}`);
    return false;
  }
}

function requestBinding(command: ShortcutCommandId, binding: string): void {
  const result = assignBinding(command, binding, settings.shortcutOverrides);
  if (result.displaced) {
    dialog.warning({
      title: "快捷键冲突",
      content: `${formatBinding(binding)} 当前用于“${commandLabel(result.displaced)}”。替换后该命令将不再有快捷键。`,
      positiveText: "替换绑定",
      negativeText: "取消",
      onPositiveClick: async () => await save(result.overrides),
    });
  } else {
    void save(result.overrides);
  }
}

function onRecordKeydown(event: KeyboardEvent): void {
  const command = recording.value;
  if (!command) return;
  event.preventDefault();
  event.stopPropagation();
  if (event.isComposing || event.repeat) return;
  if (event.key === "Escape") {
    cancelRecording();
    return;
  }
  if (
    (event.key === "Backspace" || event.key === "Delete") &&
    !event.metaKey &&
    !event.ctrlKey &&
    !event.altKey &&
    !event.shiftKey
  ) {
    cancelRecording();
    requestBinding(command, "");
    return;
  }
  const binding = bindingFromEvent(event);
  if (!binding) {
    if (!/^(?:Control|Meta|Alt|Shift)$/.test(event.key)) {
      recordError.value = "普通按键需搭配 Cmd/Ctrl/Alt；F1–F12 可以单独使用";
    }
    return;
  }
  const current = effectiveBinding(command, settings.shortcutOverrides);
  if (binding !== current) {
    const reason = reservedBindingReason(binding);
    if (reason) {
      recordError.value = reason;
      return;
    }
  }
  cancelRecording();
  if (binding !== current) requestBinding(command, binding);
}

function clearBinding(command: ShortcutCommandId): void {
  cancelRecording();
  requestBinding(command, "");
}

function restoreBinding(command: ShortcutCommandId): void {
  cancelRecording();
  const defaultBinding =
    shortcutCommands.find((entry) => entry.id === command)?.defaultBinding ?? "";
  requestBinding(command, defaultBinding);
}

function restoreAll(): void {
  cancelRecording();
  dialog.warning({
    title: "恢复全部默认快捷键",
    content: "所有自定义绑定和主动解绑都会清除。",
    positiveText: "恢复默认",
    negativeText: "取消",
    onPositiveClick: async () => await save({}),
  });
}
</script>

<template>
  <div class="shortcut-settings">
    <!-- 顶部操作与搜索栏 -->
    <div class="shortcut-header-card">
      <div class="shortcut-search-row">
        <NInput
          v-model:value="searchQuery"
          size="small"
          placeholder="搜索命令名称或按键（例如：保存、查找、Cmd+S）…"
          clearable
          class="shortcut-search-input"
        >
          <template #prefix>
            <NIcon :size="14" class="search-icon">
              <Search24Regular />
            </NIcon>
          </template>
        </NInput>
        <NButton
          size="small"
          secondary
          :disabled="settings.saving || !Object.keys(settings.shortcutOverrides).length"
          @click="restoreAll"
        >
          全部恢复默认
        </NButton>
      </div>
      <div class="shortcut-tip">
        点击按键框后直接按下新快捷键。普通按键需搭配修饰键（Cmd/Ctrl/Alt），F1–F12 可单按；单按 Delete / Backspace 清除，按 Esc 取消录制。
      </div>
    </div>

    <!-- 无搜索结果提示 -->
    <div v-if="filteredCommands.length === 0" class="shortcut-empty">
      未找到与 “{{ searchQuery }}” 匹配的快捷键命令
    </div>

    <!-- 按分组展示命令列表 -->
    <section v-for="group in groups" :key="group" class="shortcut-group">
      <div class="shortcut-group-header">
        <span class="shortcut-group-title">{{ group }}</span>
        <span class="shortcut-group-badge">
          {{ filteredCommands.filter((entry) => entry.group === group).length }}
        </span>
      </div>
      <div class="shortcut-list">
        <div
          v-for="command in filteredCommands.filter((entry) => entry.group === group)"
          :key="command.id"
          class="shortcut-row"
        >
          <div class="shortcut-info">
            <span class="shortcut-name">{{ command.label }}</span>
            <span
              v-if="Object.hasOwn(settings.shortcutOverrides, command.id)"
              class="shortcut-custom-tag"
            >
              已自定义
            </span>
          </div>

          <div class="shortcut-binding-cell">
            <button
              type="button"
              class="shortcut-binding"
              :class="{ 'shortcut-binding--recording': recording === command.id }"
              :disabled="settings.saving"
              :aria-label="`录制${command.label}快捷键`"
              @click="beginRecording(command.id)"
            >
              <span v-if="recording === command.id" class="shortcut-recording-text">
                请按组合键…
              </span>
              <kbd v-else-if="effectiveBinding(command.id, settings.shortcutOverrides)" class="shortcut-kbd">
                {{ formatBinding(effectiveBinding(command.id, settings.shortcutOverrides)) }}
              </kbd>
              <span v-else class="shortcut-unassigned">未指定快捷键</span>
            </button>
            <span
              v-if="recording === command.id && recordError"
              class="shortcut-record-error"
              role="alert"
            >
              {{ recordError }}
            </span>
          </div>

          <div class="shortcut-actions">
            <NButton
              size="tiny"
              quaternary
              :aria-label="`清除${command.label}快捷键`"
              :disabled="settings.saving || !effectiveBinding(command.id, settings.shortcutOverrides)"
              @click="clearBinding(command.id)"
            >
              清除
            </NButton>
            <NButton
              size="tiny"
              quaternary
              :aria-label="`恢复${command.label}默认快捷键`"
              :disabled="settings.saving || !Object.hasOwn(settings.shortcutOverrides, command.id)"
              @click="restoreBinding(command.id)"
            >
              默认
            </NButton>
          </div>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.shortcut-settings {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.shortcut-header-card {
  padding: 12px 14px;
  background: var(--pvf-surface-card);
  border: 1px solid var(--pvf-border-subtle);
  border-radius: 9px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.shortcut-search-row {
  display: flex;
  align-items: center;
  gap: 10px;
}

.shortcut-search-input {
  flex: 1;
}

.search-icon {
  color: var(--pvf-text-faint);
  display: flex;
  align-items: center;
}

.shortcut-tip {
  color: var(--pvf-text-faint);
  font-size: 11.5px;
  line-height: 1.45;
}

.shortcut-empty {
  padding: 32px 16px;
  text-align: center;
  color: var(--pvf-text-faint);
  font-size: 13px;
  background: var(--pvf-surface-card);
  border: 1px dashed var(--pvf-border-subtle);
  border-radius: 9px;
}

.shortcut-group {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.shortcut-group-header {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 0 2px;
}

.shortcut-group-title {
  color: var(--pvf-text-primary);
  font-size: 12.5px;
  font-weight: 600;
}

.shortcut-group-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 18px;
  height: 18px;
  padding: 0 5px;
  border-radius: 9px;
  font-size: 11px;
  font-weight: 600;
  background: var(--pvf-surface-subtle);
  color: var(--pvf-text-faint);
}

.shortcut-list {
  border: 1px solid var(--pvf-border-subtle);
  border-radius: 9px;
  background: var(--pvf-surface-card);
  overflow: hidden;
}

.shortcut-row {
  display: flex;
  align-items: center;
  gap: 10px;
  min-height: 42px;
  padding: 6px 14px;
  transition: background-color 0.15s ease;
}

.shortcut-row:hover {
  background: var(--pvf-surface-hover);
}

.shortcut-row + .shortcut-row {
  border-top: 1px solid var(--pvf-border-faint);
}

.shortcut-info {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 6px;
}

.shortcut-name {
  color: var(--pvf-text-primary);
  font-size: 12.5px;
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.shortcut-custom-tag {
  display: inline-flex;
  padding: 1px 5px;
  font-size: 10px;
  font-weight: 600;
  color: var(--pvf-primary);
  background: var(--pvf-primary-soft);
  border-radius: 4px;
}

.shortcut-binding-cell {
  display: flex;
  flex-direction: column;
  gap: 3px;
  width: 150px;
  flex: 0 0 150px;
}

.shortcut-binding {
  width: 100%;
  padding: 4px 8px;
  min-height: 28px;
  border: 1px solid var(--pvf-border-subtle);
  border-radius: 6px;
  background: var(--pvf-surface-subtle);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.15s ease;
}

.shortcut-binding:hover,
.shortcut-binding:focus-visible {
  border-color: var(--pvf-primary-hover);
  background: var(--pvf-surface-hover);
  outline: none;
  box-shadow: 0 0 0 2px var(--pvf-effect-focus-ring);
}

.shortcut-binding--recording {
  border-color: var(--pvf-primary);
  background: var(--pvf-primary-soft);
  box-shadow: 0 0 0 2px var(--pvf-effect-focus-ring);
}

.shortcut-recording-text {
  color: var(--pvf-primary);
  font-size: 11px;
  font-weight: 600;
}

.shortcut-kbd {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 11.5px;
  font-weight: 500;
  color: var(--pvf-text-primary);
  letter-spacing: 0.2px;
}

.shortcut-unassigned {
  font-size: 11px;
  color: var(--pvf-text-faint);
  font-style: italic;
}

.shortcut-record-error {
  color: var(--pvf-error);
  font-size: 11px;
  line-height: 1.3;
}

.shortcut-actions {
  display: flex;
  align-items: center;
  gap: 4px;
  flex: 0 0 auto;
}

@media (max-width: 600px) {
  .shortcut-row {
    flex-wrap: wrap;
    padding: 8px 10px;
  }
  .shortcut-info {
    flex-basis: 100%;
  }
  .shortcut-binding-cell {
    flex: 1;
    width: auto;
  }
}
</style>
