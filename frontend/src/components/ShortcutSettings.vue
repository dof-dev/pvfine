<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { NButton, useDialog, useMessage } from "naive-ui";
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
const groups = computed(() => [...new Set(shortcutCommands.map((command) => command.group))]);

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
  if ((event.key === "Backspace" || event.key === "Delete") &&
    !event.metaKey && !event.ctrlKey && !event.altKey && !event.shiftKey) {
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
  const defaultBinding = shortcutCommands.find((entry) => entry.id === command)?.defaultBinding ?? "";
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
    <div class="shortcut-intro">
      <div>
        <div class="shortcut-intro-title">应用快捷键</div>
        <div class="shortcut-intro-desc">点击按键开始录制。普通按键需搭配 Cmd/Ctrl/Alt，F1–F12 可单独使用；Esc 取消，单按 Delete 或 Backspace 清除绑定。</div>
      </div>
      <NButton size="small" secondary :disabled="settings.saving || !Object.keys(settings.shortcutOverrides).length" @click="restoreAll">
        全部恢复默认
      </NButton>
    </div>

    <section v-for="group in groups" :key="group" class="shortcut-group">
      <div class="shortcut-group-title">{{ group }}</div>
      <div class="shortcut-list">
        <div v-for="command in shortcutCommands.filter((entry) => entry.group === group)" :key="command.id" class="shortcut-row">
          <span class="shortcut-name">{{ command.label }}</span>
          <div class="shortcut-binding-cell">
            <button
              type="button"
              class="shortcut-binding"
              :class="{ 'shortcut-binding--recording': recording === command.id }"
              :disabled="settings.saving"
              :aria-label="`录制${command.label}快捷键`"
              @click="beginRecording(command.id)"
            >
              {{ recording === command.id ? "请按组合键…" : formatBinding(effectiveBinding(command.id, settings.shortcutOverrides)) }}
            </button>
            <span v-if="recording === command.id && recordError" class="shortcut-record-error" role="alert">{{ recordError }}</span>
          </div>
          <NButton size="tiny" quaternary :aria-label="`清除${command.label}快捷键`" :disabled="settings.saving || !effectiveBinding(command.id, settings.shortcutOverrides)" @click="clearBinding(command.id)">清除</NButton>
          <NButton size="tiny" quaternary :aria-label="`恢复${command.label}默认快捷键`" :disabled="settings.saving || !Object.hasOwn(settings.shortcutOverrides, command.id)" @click="restoreBinding(command.id)">默认</NButton>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.shortcut-settings { display: flex; flex-direction: column; gap: 18px; max-height: min(720px, calc(100vh - 220px)); overflow-y: auto; padding-right: 4px; }
.shortcut-intro { display: flex; justify-content: space-between; align-items: flex-start; gap: 12px; }
.shortcut-intro-title, .shortcut-group-title { color: var(--pvf-text-primary); font-size: 13px; font-weight: 600; }
.shortcut-intro-desc { margin-top: 4px; color: var(--pvf-text-faint); font-size: 12px; line-height: 1.5; }
.shortcut-record-error { color: var(--pvf-error); font-size: 11px; line-height: 1.4; }
.shortcut-group { display: flex; flex-direction: column; gap: 8px; }
.shortcut-list { overflow: hidden; border: 1px solid var(--pvf-border-subtle); border-radius: 10px; background: var(--pvf-surface-card); }
.shortcut-row { display: flex; align-items: center; gap: 8px; min-height: 48px; padding: 8px 12px; }
.shortcut-row + .shortcut-row { border-top: 1px solid var(--pvf-border-faint); }
.shortcut-name { flex: 1; min-width: 0; color: var(--pvf-text-primary); font-size: 13px; }
.shortcut-binding-cell { display: flex; flex-direction: column; gap: 4px; width: 145px; flex: 0 0 145px; }
.shortcut-binding { width: 100%; padding: 6px 9px; border: 1px solid var(--pvf-border-normal); border-radius: 6px; background: var(--pvf-surface-subtle); color: var(--pvf-text-primary); font: 12px ui-monospace, SFMono-Regular, Menlo, monospace; cursor: pointer; }
.shortcut-binding:hover, .shortcut-binding:focus-visible, .shortcut-binding--recording { border-color: var(--pvf-primary); outline: none; box-shadow: 0 0 0 2px var(--pvf-effect-focus-ring); }
.shortcut-binding--recording { color: var(--pvf-primary); }
@media (max-width: 600px) { .shortcut-row { flex-wrap: wrap; } .shortcut-name { flex-basis: 100%; } .shortcut-binding-cell { flex: 1; width: auto; } }
</style>
