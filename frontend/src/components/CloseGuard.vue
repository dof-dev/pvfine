<script setup lang="ts">
import { computed, onUnmounted, ref } from "vue";
import { Events } from "@wailsio/runtime";
import { useDialog } from "naive-ui";
import { useAutosaveStore } from "../stores/autosave";
import { hasUnsavedWorkspaceChanges } from "../unsaved";

type CloseAction = "close" | "quit";

const dialog = useDialog();
const autosave = useAutosaveStore();
const pendingAction = ref<CloseAction | null>(null);
const closing = ref(false);

const hasUnsavedChanges = computed(() => hasUnsavedWorkspaceChanges());

function eventData(event: any): any {
  return event?.data ?? event;
}

function requestClose(action: CloseAction): void {
  if (pendingAction.value || closing.value) return;
  pendingAction.value = action;

  if (!hasUnsavedChanges.value) {
    void confirmClose(action);
    return;
  }

  dialog.warning({
    title: "未保存的修改",
    content: "当前工作区有未保存的修改，退出后这些修改将丢失。确定继续吗？",
    positiveText: "退出",
    negativeText: "取消",
    onPositiveClick: () => confirmClose(action),
    onNegativeClick: cancelClose,
    onClose: cancelClose,
  });
}

async function confirmClose(action: CloseAction): Promise<void> {
  if (pendingAction.value !== action || closing.value) return;
  closing.value = true;
  // 用户已经确认放弃这些修改:配套的备份缓存也没有保护对象了,不删除会在下次
  // 启动时提示恢复一个被主动丢弃的工作区。
  await autosave.discardForWorkspace();
  const eventName = action === "quit" ? "app:quit-confirmed" : "app:close-confirmed";
  try {
    await Events.Emit(eventName);
  } catch (error) {
    console.error("confirm close failed", error);
    closing.value = false;
    pendingAction.value = null;
  }
}

function cancelClose(): void {
  if (closing.value) return;
  pendingAction.value = null;
}

const offQuitRequested = Events.On("app:quit-requested", () => requestClose("quit"));
const offCloseRequested = Events.On("app:close-requested", (event) => {
  const action = eventData(event)?.action === "quit" ? "quit" : "close";
  requestClose(action);
});

onUnmounted(() => {
  offQuitRequested();
  offCloseRequested();
});
</script>

<template />
