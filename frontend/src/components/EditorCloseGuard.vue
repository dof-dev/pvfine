<script setup lang="ts">
import { watch } from "vue";
import { useDialog } from "naive-ui";
import { useEditorStore } from "../stores/editor";

const dialog = useDialog();
const editor = useEditorStore();

watch(
  () => editor.pendingClose,
  (action) => {
    if (!action) return;
    dialog.warning({
      title: "未保存的修改",
      content: "当前文件有未保存的修改，关闭后这些修改将丢失。确定继续吗？",
      positiveText: "关闭",
      negativeText: "取消",
      onPositiveClick: () => editor.confirmPendingClose(),
      onNegativeClick: () => editor.cancelPendingClose(),
      onClose: () => editor.cancelPendingClose(),
    });
  }
);
</script>

<template />
