<script setup lang="ts">
import { onUnmounted } from "vue";
import { Events } from "@wailsio/runtime";
import { useDialog, useMessage } from "naive-ui";

const dialog = useDialog();
const message = useMessage();
let dismiss: (() => void) | null = null;

const offAvailable = Events.On("app:update-available", (event) => {
  const { currentVersion, version, responseEvent } = event.data;
  if (dismiss) return;
  let answered = false;
  const respond = async (accepted: boolean): Promise<boolean> => {
    if (answered) return false;
    answered = true;
    try {
      await Events.Emit(responseEvent, { accepted });
      dismiss = null;
      return true;
    } catch (error: any) {
      answered = false;
      message.error(`提交更新选择失败：${error?.message ?? error}`);
      return false;
    }
  };
  const prompt = dialog.info({
    title: "发现新版本",
    content: `当前版本：${currentVersion}，可用版本：${version}。是否下载更新？下载完成后可自行选择重启应用。`,
    positiveText: "下载更新",
    negativeText: "暂不更新",
    autoFocus: false,
    maskClosable: false,
    closeOnEsc: false,
    onPositiveClick: () => respond(true),
    onNegativeClick: () => respond(false),
    onClose: () => respond(false),
    onEsc: async () => {
      if (await respond(false)) prompt.destroy();
    },
  });
  dismiss = () => {
    void respond(false);
    prompt.destroy();
  };
});
const offMessage = Events.On("app:update-message", (event) => {
  const { kind, message: text } = event.data;
  if (kind === "error") message.error(text);
  else message.info(text);
});

onUnmounted(() => {
  dismiss?.();
  offAvailable();
  offMessage();
});
</script>

<template />
