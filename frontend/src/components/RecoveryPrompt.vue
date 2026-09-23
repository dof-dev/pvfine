<script setup lang="ts">
import { h, onMounted } from "vue";
import { useDialog, useMessage, type DialogReactive } from "naive-ui";
import { useArchiveStore } from "../stores/archive";
import { useAutosaveStore } from "../stores/autosave";
import { setDialogBusy } from "../dialogBusy";
import type { RecoveryInfo } from "../../bindings/pvfine/services/models";

const dialog = useDialog();
const message = useMessage();
const archive = useArchiveStore();
const autosave = useAutosaveStore();

onMounted(() => {
  void promptForRecovery();
});

function formatTime(seconds: number): string {
  if (!seconds) return "未知";
  const date = new Date(seconds * 1000);
  return Number.isNaN(date.getTime()) ? "未知" : date.toLocaleString();
}

function formatSize(bytes: number): string {
  if (!bytes || bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`;
}

function detailLines(info: RecoveryInfo): string[] {
  const lines = [
    `原文件：${info.sourceName || info.sourcePath || "未知"}`,
    `备份时间：${formatTime(info.cachedAt)}`,
    `备份大小：${formatSize(info.sizeBytes)}`,
    `未保存改动：${info.pendingFiles} 个文件`,
  ];
  if (!info.sourceExists) {
    lines.push("原文件已不存在：请先找回原文件，再重新启动恢复。");
  } else if (info.sourceChanged) {
    lines.push("原文件在备份之后被外部修改过：未列入备份的文件将以磁盘内容为准。");
  }
  lines.push("恢复后工作区仍标记为未保存，需要重新保存才会写回原文件。");
  return lines;
}

/** 恢复期间追加进度提示：重开原文件与重放内容都在后端进行。 */
function contentLines(info: RecoveryInfo) {
  const lines = detailLines(info).map((line) =>
    h("div", { class: "recovery-prompt-line" }, line)
  );
  if (autosave.restoring) {
    lines.push(
      h(
        "div",
        { class: "recovery-prompt-line recovery-prompt-line--busy" },
        "正在恢复：重新打开原文件并重放备份中的修改，归档较大时需要一些时间，请勿关闭窗口。"
      )
    );
  }
  return lines;
}

async function promptForRecovery(): Promise<void> {
  const info = await autosave.checkRecovery();
  if (!info || archive.open) return;
  const positiveText = info.sourceExists ? "恢复" : "仍然尝试恢复";
  const instance = dialog.warning({
    title: "发现未保存的工作区备份",
    content: () => h("div", { class: "recovery-prompt" }, contentLines(info)),
    positiveText,
    negativeText: "丢弃备份",
    // 关闭时机完全由流程控制：失败时保留弹窗以便重试。
    onPositiveClick: () => runRestore(instance, positiveText),
    onNegativeClick: () => runDiscard(instance),
    // ESC、遮罩与关闭按钮都表示“稍后再决定”，备份保留到下次启动。
  });
}

async function runRestore(instance: DialogReactive, positiveText: string): Promise<boolean> {
  if (autosave.restoring) return false;
  setDialogBusy(instance, true, positiveText, "正在恢复…");
  try {
    await autosave.restore();
    message.success("已恢复备份，工作区仍未保存，请确认后保存");
    instance.destroy();
  } catch (error: any) {
    message.error(`恢复备份失败：${error?.message ?? error}`);
    setDialogBusy(instance, false, positiveText);
  }
  return false;
}

async function runDiscard(instance: DialogReactive): Promise<boolean> {
  try {
    await autosave.discard();
    message.success("已丢弃备份缓存");
    instance.destroy();
  } catch (error: any) {
    message.error(`丢弃备份失败：${error?.message ?? error}`);
  }
  return false;
}
</script>

<template />
