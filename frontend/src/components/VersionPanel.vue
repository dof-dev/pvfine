<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useMessage } from "naive-ui";
import {
  NAlert,
  NButton,
  NEmpty,
  NIcon,
  NInput,
  NModal,
  NPopconfirm,
  NSpace,
  NSpin,
  NTag,
  NText,
} from "naive-ui";
import {
  ArrowDownload20Regular,
  ArrowReset20Regular,
  ArrowUndo20Regular,
  Branch20Regular,
  Camera20Regular,
  Checkmark20Regular,
  Dismiss20Regular,
  Document20Regular,
  DocumentSync20Regular,
  Flash20Regular,
  History20Regular,
  ShieldCheckmark20Regular,
} from "@vicons/fluent";
import type {
  VersionChange,
  VersionCommit,
  VersionFileDiff,
} from "../../bindings/pvfine/services/models";
import { ArchiveService } from "../../bindings/pvfine/services";
import { useVersionStore } from "../stores/version";
import { useEditorStore } from "../stores/editor";

const version = useVersionStore();
const editor = useEditorStore();
const message = useMessage();

type ConfirmAction =
  | { type: "discard" }
  | { type: "checkout"; commit: VersionCommit }
  | { type: "remove" };

type ActiveTarget =
  | { type: "workspace" }
  | { type: "commit"; commit: VersionCommit };

const confirmAction = ref<ConfirmAction | null>(null);
const confirmRunning = ref(false);

const activeTarget = ref<ActiveTarget>({ type: "workspace" });
const selectedFile = ref<VersionChange | null>(null);
const activeDiff = ref<VersionFileDiff | null>(null);
const diffLoading = ref(false);
const diffError = ref("");
const collapsedGroups = ref<Record<string, boolean>>({
  add: false,
  modify: false,
  delete: false,
});

let diffFetchToken = 0;
let activeDiffKey = "";
let inFlightDiffPromise: Promise<void> | null = null;
let activeCommitLoadingID = "";
let activeCommitPromise: Promise<void> | null = null;
let openWatchStop: (() => void) | null = null;

function makeDiffKey(file: VersionChange, commitID?: string): string {
  return `${commitID ?? "working"}:${file.operation}:${file.path}:${file.beforeHash ?? ""}:${file.afterHash ?? ""}`;
}

function invalidateDiff(): void {
  diffFetchToken++;
  activeDiffKey = "";
  inFlightDiffPromise = null;
  diffLoading.value = false;
  diffError.value = "";
  activeDiff.value = null;
}

interface DisplayDiffLine {
  kind: "add" | "remove" | "context" | "hunk";
  text: string;
  oldLine?: number;
  newLine?: number;
}

function computeDiffLines(diff: VersionFileDiff | null): {
  lines: DisplayDiffLine[];
  truncated: boolean;
  addCount: number;
  removeCount: number;
} {
  if (!diff || !diff.textAvailable) {
    return { lines: [], truncated: false, addCount: 0, removeCount: 0 };
  }

  const splitLines = (text?: string): string[] => {
    if (text === undefined || text === null || text === "") return [];
    return text.replace(/\r\n/g, "\n").replace(/\r/g, "\n").split("\n");
  };

  const beforeLines = splitLines(diff.beforeText);
  const afterLines = splitLines(diff.afterText);

  // Case 1: Pure add
  if (diff.operation === "add" || beforeLines.length === 0) {
    const isTruncated = afterLines.length > 2000;
    const linesToUse = isTruncated ? afterLines.slice(0, 2000) : afterLines;
    const lines: DisplayDiffLine[] = linesToUse.map((text, idx) => ({
      kind: "add",
      text,
      newLine: idx + 1,
    }));
    return {
      lines,
      truncated: isTruncated,
      addCount: afterLines.length,
      removeCount: 0,
    };
  }

  // Case 2: Pure delete
  if (diff.operation === "delete" || afterLines.length === 0) {
    const isTruncated = beforeLines.length > 2000;
    const linesToUse = isTruncated ? beforeLines.slice(0, 2000) : beforeLines;
    const lines: DisplayDiffLine[] = linesToUse.map((text, idx) => ({
      kind: "remove",
      text,
      oldLine: idx + 1,
    }));
    return {
      lines,
      truncated: isTruncated,
      addCount: 0,
      removeCount: beforeLines.length,
    };
  }

  // Case 3: Modify
  let start = 0;
  while (
    start < beforeLines.length &&
    start < afterLines.length &&
    beforeLines[start] === afterLines[start]
  ) {
    start++;
  }

  let oldEnd = beforeLines.length - 1;
  let newEnd = afterLines.length - 1;
  while (
    oldEnd >= start &&
    newEnd >= start &&
    beforeLines[oldEnd] === afterLines[newEnd]
  ) {
    oldEnd--;
    newEnd--;
  }

  type RawOp = { kind: "=" | "-" | "+"; text: string };
  const allOps: RawOp[] = [];

  // Common prefix
  for (let idx = 0; idx < start; idx++) {
    allOps.push({ kind: "=", text: beforeLines[idx] });
  }

  const oldMid = beforeLines.slice(start, oldEnd + 1);
  const newMid = afterLines.slice(start, newEnd + 1);

  if (oldMid.length === 0) {
    for (const line of newMid) allOps.push({ kind: "+", text: line });
  } else if (newMid.length === 0) {
    for (const line of oldMid) allOps.push({ kind: "-", text: line });
  } else if (
    oldMid.length > 1000 ||
    newMid.length > 1000 ||
    oldMid.length * newMid.length > 400000
  ) {
    for (const line of oldMid) allOps.push({ kind: "-", text: line });
    for (const line of newMid) allOps.push({ kind: "+", text: line });
  } else {
    // Dynamic programming LCS
    const rows = oldMid.length;
    const cols = newMid.length;
    const dp: number[][] = Array.from({ length: rows + 1 }, () =>
      new Array(cols + 1).fill(0)
    );
    for (let r = rows - 1; r >= 0; r--) {
      for (let c = cols - 1; c >= 0; c--) {
        if (oldMid[r] === newMid[c]) {
          dp[r][c] = dp[r + 1][c + 1] + 1;
        } else {
          dp[r][c] = Math.max(dp[r + 1][c], dp[r][c + 1]);
        }
      }
    }
    let r = 0,
      c = 0;
    while (r < rows && c < cols) {
      if (oldMid[r] === newMid[c]) {
        allOps.push({ kind: "=", text: oldMid[r] });
        r++;
        c++;
      } else if (dp[r + 1][c] >= dp[r][c + 1]) {
        allOps.push({ kind: "-", text: oldMid[r] });
        r++;
      } else {
        allOps.push({ kind: "+", text: newMid[c] });
        c++;
      }
    }
    while (r < rows) {
      allOps.push({ kind: "-", text: oldMid[r] });
      r++;
    }
    while (c < cols) {
      allOps.push({ kind: "+", text: newMid[c] });
      c++;
    }
  }

  // Common suffix
  for (let idx = oldEnd + 1; idx < beforeLines.length; idx++) {
    allOps.push({ kind: "=", text: beforeLines[idx] });
  }

  let oldLineNum = 1;
  let newLineNum = 1;
  let addCount = 0;
  let removeCount = 0;

  const opsWithLines = allOps.map((op) => {
    const item = {
      kind: op.kind,
      text: op.text,
      oldLine: op.kind !== "+" ? oldLineNum : undefined,
      newLine: op.kind !== "-" ? newLineNum : undefined,
    };
    if (op.kind === "+") {
      addCount++;
      newLineNum++;
    } else if (op.kind === "-") {
      removeCount++;
      oldLineNum++;
    } else {
      oldLineNum++;
      newLineNum++;
    }
    return item;
  });

  // Context filtering with 3 lines of context
  const CONTEXT = 3;
  const visible = new Array(opsWithLines.length).fill(false);
  for (let i = 0; i < opsWithLines.length; i++) {
    if (opsWithLines[i].kind !== "=") {
      const from = Math.max(0, i - CONTEXT);
      const to = Math.min(opsWithLines.length - 1, i + CONTEXT);
      for (let k = from; k <= to; k++) {
        visible[k] = true;
      }
    }
  }

  const result: DisplayDiffLine[] = [];
  let lastVisible = -1;

  for (let i = 0; i < opsWithLines.length; i++) {
    if (!visible[i]) continue;
    if (lastVisible !== -1 && i > lastVisible + 1) {
      const cur = opsWithLines[i];
      result.push({
        kind: "hunk",
        text: `@@ -${cur.oldLine ?? 1}, +${cur.newLine ?? 1} @@`,
      });
    }
    const op = opsWithLines[i];
    result.push({
      kind: op.kind === "+" ? "add" : op.kind === "-" ? "remove" : "context",
      text: op.text,
      oldLine: op.oldLine,
      newLine: op.newLine,
    });
    lastVisible = i;
  }

  const isTruncated = result.length > 2000;
  const lines = isTruncated ? result.slice(0, 2000) : result;
  return { lines, truncated: isTruncated, addCount, removeCount };
}

const parsedDiff = computed(() => computeDiffLines(activeDiff.value));

const currentFileList = computed<VersionChange[]>(() => {
  if (activeTarget.value.type === "workspace") {
    return version.changes;
  }
  return version.commitChanges;
});

const groupedCurrentFiles = computed(() => {
  const add: VersionChange[] = [];
  const modify: VersionChange[] = [];
  const del: VersionChange[] = [];
  for (const c of currentFileList.value) {
    if (c.operation === "add") add.push(c);
    else if (c.operation === "delete") del.push(c);
    else modify.push(c);
  }
  return { add, modify, delete: del };
});

const currentFileStats = computed(() => {
  const { add, modify, delete: del } = groupedCurrentFiles.value;
  return {
    total: currentFileList.value.length,
    add: add.length,
    modify: modify.length,
    delete: del.length,
  };
});

const confirmMessage = computed(() => {
  if (!confirmAction.value) return "";
  if (confirmAction.value.type === "discard") {
    return "这会将工作区恢复到当前 HEAD，未提交的编辑将丢失。确定继续吗？";
  }
  if (confirmAction.value.type === "remove") {
    return "这会删除当前 PVF 旁边的 .pvfine 版本库、版本历史和对象文件，但不会删除 PVF 本体或当前内存编辑。确定继续吗？";
  }
  return `将 ${confirmAction.value.commit.message} 加载到当前工作区，当前未提交变更会被替换。确定继续吗？`;
});

const confirmPositiveText = computed(() =>
  confirmAction.value?.type === "checkout"
    ? "加载版本"
    : confirmAction.value?.type === "remove"
      ? "取消版本控制"
      : "放弃变更"
);

const confirmAlertType = computed<"warning" | "error">(() =>
  confirmAction.value?.type === "remove" ? "error" : "warning"
);

const confirmButtonType = computed<"warning" | "error">(() =>
  confirmAction.value?.type === "remove" ? "error" : "warning"
);

watch(
  () => version.status.enabled,
  (enabled) => {
    if (!enabled) {
      if (!confirmRunning.value) confirmAction.value = null;
      invalidateDiff();
      selectedFile.value = null;
      activeTarget.value = { type: "workspace" };
    } else if (version.visible) {
      void autoSelectInitialTarget();
    }
  }
);

watch(
  () => version.visible,
  (visible) => {
    if (openWatchStop) {
      openWatchStop();
      openWatchStop = null;
    }
    if (!visible) {
      if (!confirmRunning.value) confirmAction.value = null;
      invalidateDiff();
      selectedFile.value = null;
      activeCommitLoadingID = "";
      activeCommitPromise = null;
      return;
    }
    confirmAction.value = null;
    diffError.value = "";
    if (version.loading) {
      openWatchStop = watch(
        () => version.loading,
        (isLoading) => {
          if (!isLoading) {
            if (openWatchStop) {
              openWatchStop();
              openWatchStop = null;
            }
            void autoSelectInitialTarget();
          }
        }
      );
    } else {
      void autoSelectInitialTarget();
    }
  }
);

watch(confirmRunning, (running) => {
  if (!running && !version.status.enabled) confirmAction.value = null;
});

// Watch working changes for invalidated selection and diff cleanup
watch(
  () => version.changes,
  (changes) => {
    if (!version.enabled || !version.visible) return;
    if (activeTarget.value.type !== "workspace") return;

    if (!changes.length) {
      invalidateDiff();
      selectedFile.value = null;
      return;
    }

    const cur = selectedFile.value;
    if (cur) {
      const matched =
        changes.find((c) => c.path === cur.path && c.operation === cur.operation) ??
        changes.find((c) => c.path === cur.path);

      if (matched) {
        const contentChanged =
          matched.operation !== cur.operation ||
          matched.beforeHash !== cur.beforeHash ||
          matched.afterHash !== cur.afterHash ||
          matched.beforeType !== cur.beforeType ||
          matched.afterType !== cur.afterType;

        selectedFile.value = matched;

        if (contentChanged) {
          void loadDiff(matched);
        }
        return;
      }
    }

    invalidateDiff();
    const first = changes[0] ?? null;
    selectedFile.value = first;
    if (first) {
      void loadDiff(first);
    }
  }
);

// Watch commit history for removed / switched commits
watch(
  () => version.history,
  (history) => {
    if (!version.enabled || !version.visible) return;
    const target = activeTarget.value;
    if (target.type === "commit") {
      const commitStillExists = history.some(
        (c) => c.id === target.commit.id
      );
      if (!commitStillExists) {
        invalidateDiff();
        selectedFile.value = null;
        void autoSelectInitialTarget();
      }
    }
  }
);

async function autoSelectInitialTarget(force = false): Promise<void> {
  if (!version.enabled || !version.visible) return;
  if (version.changes.length > 0) {
    await selectWorkspace(force);
  } else if (version.history.length > 0) {
    await selectCommit(version.history[0], force);
  } else {
    activeTarget.value = { type: "workspace" };
    invalidateDiff();
    selectedFile.value = null;
  }
}

function toggleGroup(group: "add" | "modify" | "delete") {
  collapsedGroups.value[group] = !collapsedGroups.value[group];
}

function isGroupCollapsed(group: "add" | "modify" | "delete"): boolean {
  return !!collapsedGroups.value[group];
}

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

function operationType(operation: string): "success" | "warning" | "error" {
  if (operation === "add") return "success";
  if (operation === "delete") return "error";
  return "warning";
}

function shortID(id: string): string {
  return id ? id.slice(0, 10) : "—";
}

function formatTime(timestamp: number): string {
  const milliseconds =
    timestamp > 10_000_000_000 ? timestamp / 1_000_000 : timestamp * 1000;
  return new Date(milliseconds).toLocaleString();
}

async function loadDiff(file: VersionChange, commitID?: string): Promise<void> {
  const diffKey = makeDiffKey(file, commitID);

  if (
    activeDiff.value &&
    !diffLoading.value &&
    activeDiffKey === diffKey &&
    selectedFile.value?.path === file.path &&
    selectedFile.value?.operation === file.operation
  ) {
    return;
  }

  if (diffLoading.value && activeDiffKey === diffKey && inFlightDiffPromise) {
    return inFlightDiffPromise;
  }

  activeDiffKey = diffKey;
  const token = ++diffFetchToken;
  diffLoading.value = true;
  diffError.value = "";

  inFlightDiffPromise = (async () => {
    try {
      const diff = commitID
        ? await version.diffCommit(commitID, file.path)
        : await version.diffWorking(file.path);
      if (token === diffFetchToken) {
        activeDiff.value = diff;
      }
    } catch (err: any) {
      if (token === diffFetchToken) {
        diffError.value = String(err?.message ?? err ?? "获取 Diff 失败");
        activeDiff.value = null;
      }
    } finally {
      if (token === diffFetchToken) {
        diffLoading.value = false;
        inFlightDiffPromise = null;
      }
    }
  })();

  return inFlightDiffPromise;
}

async function selectWorkspace(forceOrEvent?: boolean | Event): Promise<void> {
  const force = typeof forceOrEvent === "boolean" ? forceOrEvent : false;
  const currentFirst = version.changes[0] ?? null;
  const firstKey = currentFirst ? makeDiffKey(currentFirst) : "";
  if (
    !force &&
    activeTarget.value.type === "workspace" &&
    selectedFile.value?.path === currentFirst?.path &&
    selectedFile.value?.operation === currentFirst?.operation &&
    (activeDiffKey === firstKey || !currentFirst) &&
    (activeDiff.value !== null || diffLoading.value || !currentFirst)
  ) {
    return;
  }
  activeTarget.value = { type: "workspace" };
  invalidateDiff();
  selectedFile.value = currentFirst;
  if (currentFirst) {
    await loadDiff(currentFirst);
  }
}

async function selectCommit(
  commit: VersionCommit,
  forceOrEvent?: boolean | Event
): Promise<void> {
  const force = typeof forceOrEvent === "boolean" ? forceOrEvent : false;

  if (
    !force &&
    activeTarget.value.type === "commit" &&
    activeTarget.value.commit.id === commit.id &&
    selectedFile.value !== null &&
    (activeDiff.value !== null || diffLoading.value)
  ) {
    return;
  }

  if (activeCommitLoadingID === commit.id && activeCommitPromise) {
    return activeCommitPromise;
  }

  activeCommitLoadingID = commit.id;
  activeTarget.value = { type: "commit", commit };
  invalidateDiff();
  selectedFile.value = null;

  activeCommitPromise = (async () => {
    try {
      const list = await version.loadCommitChanges(commit.id);
      if (
        activeTarget.value.type !== "commit" ||
        activeTarget.value.commit.id !== commit.id
      ) {
        return;
      }
      const first = list[0] ?? null;
      selectedFile.value = first;
      if (first) {
        await loadDiff(first, commit.id);
      }
    } catch (err: any) {
      message.error(`加载提交变更失败: ${err?.message ?? err}`);
    } finally {
      if (activeCommitLoadingID === commit.id) {
        activeCommitLoadingID = "";
        activeCommitPromise = null;
      }
    }
  })();

  return activeCommitPromise;
}

function onFileClick(file: VersionChange): void {
  const commitID =
    activeTarget.value.type === "commit"
      ? activeTarget.value.commit.id
      : undefined;
  const diffKey = makeDiffKey(file, commitID);
  if (
    selectedFile.value?.path === file.path &&
    selectedFile.value?.operation === file.operation &&
    activeDiffKey === diffKey &&
    activeDiff.value !== null
  ) {
    return;
  }
  selectedFile.value = file;
  invalidateDiff();
  void loadDiff(file, commitID);
}

async function initialize(): Promise<void> {
  try {
    await editor.flushPending();
    invalidateDiff();
    selectedFile.value = null;
    await version.initialize();
    message.success("版本库已初始化");
    await autoSelectInitialTarget();
  } catch (error: any) {
    message.error(`初始化版本库失败: ${error?.message ?? error}`);
  }
}

async function commit(): Promise<void> {
  try {
    await editor.flushPending();
    invalidateDiff();
    selectedFile.value = null;
    const result = await version.commit();
    if (result) message.success(`已创建版本 ${shortID(result.id)}`);
    await autoSelectInitialTarget();
  } catch (error: any) {
    message.error(`提交版本失败: ${error?.message ?? error}`);
  }
}

async function undo(): Promise<void> {
  try {
    await editor.flushPending();
    invalidateDiff();
    selectedFile.value = null;
    await version.undo();
    message.success("已撤销最近一次工作区变更");
    await autoSelectInitialTarget();
  } catch (error: any) {
    message.error(`撤销失败: ${error?.message ?? error}`);
  }
}

async function discard(): Promise<void> {
  invalidateDiff();
  selectedFile.value = null;
  await editor.flushPending();
  await version.discard();
  await autoSelectInitialTarget();
}

async function restoreSingleFile(path: string): Promise<void> {
  try {
    await editor.flushPending();
    const isCurrent = selectedFile.value?.path === path;
    if (isCurrent) {
      invalidateDiff();
      selectedFile.value = null;
    }
    await version.restorePath(path);
    message.success(`已还原文件 ${path}`);
    if (activeTarget.value.type === "workspace") {
      const remaining = version.changes.filter((c) => c.path !== path);
      if (isCurrent) {
        const next = remaining[0] ?? null;
        selectedFile.value = next;
        if (next) {
          void loadDiff(next);
        } else {
          invalidateDiff();
        }
      }
    }
  } catch (error: any) {
    message.error(`还原文件失败: ${error?.message ?? error}`);
  }
}

function confirmDiscard(): void {
  if (confirmRunning.value || version.busy || !version.status.changedFiles) return;
  confirmAction.value = { type: "discard" };
}

function confirmCheckout(commit: VersionCommit): void {
  if (confirmRunning.value || version.busy) return;
  confirmAction.value = { type: "checkout", commit };
}

function confirmRemove(): void {
  if (confirmRunning.value || version.busy) return;
  confirmAction.value = { type: "remove" };
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
    } else if (action.type === "checkout") {
      invalidateDiff();
      selectedFile.value = null;
      await editor.flushPending();
      await version.checkout(action.commit);
      message.success(`已加载版本 ${shortID(action.commit.id)}`);
      await autoSelectInitialTarget();
    } else {
      invalidateDiff();
      selectedFile.value = null;
      activeTarget.value = { type: "workspace" };
      await version.remove();
      message.success("已取消版本控制，相关版本文件已删除");
    }
    confirmAction.value = null;
  } catch (error: any) {
    const label =
      action.type === "discard"
        ? "放弃变更"
        : action.type === "checkout"
          ? "加载版本"
          : "取消版本控制";
    message.error(`${label}失败: ${error?.message ?? error}`);
  } finally {
    confirmRunning.value = false;
  }
}

async function openChange(change: VersionChange): Promise<void> {
  if (change.operation === "delete") {
    message.info("该文件已被删除，当前工作区中没有可打开的文件");
    return;
  }
  try {
    const nodes = (await ArchiveService.ResolveFiles([change.path])) ?? [];
    const node = nodes.find((item) => item && item.fileIndex >= 0);
    if (!node) {
      message.warning(`当前工作区找不到文件: ${change.path}`);
      return;
    }
    await editor.openFile(node.fileIndex);
  } catch (error: any) {
    message.error(`打开文件失败: ${error?.message ?? error}`);
  }
}

async function exportCommit(commit: VersionCommit): Promise<void> {
  try {
    const path = await version.exportCommit(commit);
    if (path) message.success(`已导出版本“${commit.message}”涉及的文件到 ${path}`);
  } catch (error: any) {
    message.error(`导出版本文件失败: ${error?.message ?? error}`);
  }
}
</script>

<template>
  <NModal
    v-model:show="version.visible"
    preset="card"
    title="版本控制"
    class="version-modal"
    :style="{ width: '1060px', maxWidth: 'calc(100vw - 32px)', height: '780px', maxHeight: 'calc(100vh - 48px)' }"
    :content-style="{ display: 'flex', flexDirection: 'column', minHeight: 0, overflow: 'hidden' }"
    :mask-closable="!version.busy && !confirmRunning"
    :close-on-esc="!version.busy && !confirmRunning"
  >
    <div class="version-panel-root">
      <NSpin :show="version.busy && !version.enabled" class="version-init-spin">
        <!-- 1. 加载中状态 -->
        <template v-if="version.status.loading">
          <NAlert type="info" :show-icon="false" class="version-loading-alert">
            PVF 已打开，正在后台加载版本控制。加载完成前不会阻塞普通浏览和读取操作，版本操作暂不可用。
          </NAlert>
        </template>

        <!-- 2. 未初始化引导卡片状态 -->
        <template v-else-if="!version.enabled">
          <div class="version-guide-wrapper">
            <div class="version-guide-banner">
              <div class="banner-badge">
                <NIcon size="18"><ShieldCheckmark20Regular /></NIcon>
                <span>本地快照系统</span>
              </div>
              <div class="banner-title">开启 PVF 本地版本控制</div>
              <div class="banner-subtitle">
                为当前 PVF 归档建立轻量只读快照仓库，随时记录、对比与回退文件变更，保障修改无损可控。
              </div>
            </div>

            <div class="version-guide-cards">
              <div class="guide-card">
                <div class="guide-card-icon guide-card-icon--camera">
                  <NIcon size="22"><Camera20Regular /></NIcon>
                </div>
                <div class="guide-card-title">本地快照原理</div>
                <div class="guide-card-desc">
                  在 PVF 旁边建立轻量 sidecar 存储（<code>.pvfine</code> 目录），精确记录逻辑文件的 SHA-256 与版本拓扑，不破坏原 PVF 文件二进制结构。
                </div>
              </div>

              <div class="guide-card">
                <div class="guide-card-icon guide-card-icon--shield">
                  <NIcon size="22"><ShieldCheckmark20Regular /></NIcon>
                </div>
                <div class="guide-card-title">无损只读保证</div>
                <div class="guide-card-desc">
                  版本控制服务对底层归档全程只读，所有修改与历史检出均在内存沙箱与撤销栈保护下运行，只有显式保存归档时才会更新物理文件。
                </div>
              </div>

              <div class="guide-card">
                <div class="guide-card-icon guide-card-icon--flash">
                  <NIcon size="22"><Flash20Regular /></NIcon>
                </div>
                <div class="guide-card-title">快速对比与还原</div>
                <div class="guide-card-desc">
                  支持单文件行级 Diff 对比、一键恢复单文件至 HEAD，并在误操作时一键撤销，历史版本随时可加载回工作区或导出修改文件。
                </div>
              </div>
            </div>

            <NAlert v-if="version.status.error" type="error" :show-icon="false" class="guide-alert">
              {{ version.status.error }}
            </NAlert>

            <NAlert type="info" :show-icon="false" class="guide-alert">
              初始化说明：版本库将扫描当前 PVF 内的所有逻辑文件并建立基础快照索引。首次扫描可能耗时 1~3 分钟，并会占用一定的额外磁盘空间；期间请勿关闭应用。
            </NAlert>

            <div class="guide-footer">
              <NText depth="3">初始化不会修改当前 PVF 文件本体。</NText>
              <NButton
                type="primary"
                size="medium"
                :loading="version.loading"
                @click="initialize"
              >
                立即初始化版本库
              </NButton>
            </div>
          </div>
        </template>

        <!-- 3. 已初始化双栏工作台状态 -->
        <template v-else>
          <!-- 顶部固定信息与快捷操作栏 -->
          <div class="version-top-panel">
            <div class="version-summary">
              <div class="version-head-info">
                <div class="version-branch-tag">
                  <NIcon size="16"><Branch20Regular /></NIcon>
                  <span>{{ version.status.branch }}</span>
                </div>
                <NText depth="3">·</NText>
                <NText strong>HEAD {{ shortID(version.status.headId) }}</NText>
                <NText depth="3" class="version-head-message" :title="version.status.headMessage">
                  {{ version.status.headMessage || "未命名版本" }}
                </NText>
              </div>

              <NSpace align="center" size="small" class="version-summary-tags">
                <NTag
                  :type="version.status.changedFiles ? 'warning' : 'default'"
                  size="small"
                  :bordered="false"
                >
                  {{ version.status.changedFiles }} 个工作区变更
                </NTag>
                <NTag
                  :type="version.status.needsSave ? 'warning' : 'success'"
                  size="small"
                  :bordered="false"
                >
                  {{ version.status.needsSave ? "PVF 未保存" : "PVF 已保存" }}
                </NTag>
                <NButton
                  size="small"
                  secondary
                  type="error"
                  :disabled="version.busy || !!confirmAction"
                  @click="confirmRemove"
                >
                  取消版本控制
                </NButton>
              </NSpace>
            </div>

            <!-- 快速提交栏 -->
            <div class="version-commit-row">
              <NInput
                v-model:value="version.commitMessage"
                placeholder="提交说明，例如：调整装备价格 (按 Enter 提交)"
                :disabled="version.busy || !!confirmAction"
                @keyup.enter="commit"
              />
              <NButton
                type="primary"
                :loading="version.committing"
                :disabled="!version.canCommit || version.busy || !!confirmAction"
                @click="commit"
              >
                <template #icon>
                  <NIcon><Checkmark20Regular /></NIcon>
                </template>
                创建版本
              </NButton>

              <NButton
                size="medium"
                secondary
                :loading="version.loading && !confirmAction"
                :disabled="!version.status.pendingChangeSets || version.busy || !!confirmAction"
                title="撤销最近一次未提交的编辑"
                @click="undo"
              >
                <template #icon>
                  <NIcon><ArrowUndo20Regular /></NIcon>
                </template>
                撤销变更
              </NButton>

              <NButton
                size="medium"
                secondary
                type="warning"
                :disabled="!version.status.changedFiles || version.busy || !!confirmAction"
                title="放弃所有未提交变更，恢复到 HEAD"
                @click="confirmDiscard"
              >
                <template #icon>
                  <NIcon><Dismiss20Regular /></NIcon>
                </template>
                放弃全部
              </NButton>
            </div>

            <NAlert v-if="version.error" type="error" :show-icon="false" class="version-alert">
              {{ version.error }}
            </NAlert>

            <NAlert
              v-if="confirmAction"
              :type="confirmAlertType"
              :show-icon="false"
              class="version-confirm"
            >
              <div class="version-confirm-content">
                <div>{{ confirmMessage }}</div>
                <NSpace size="small">
                  <NButton size="small" :disabled="confirmRunning" @click="cancelConfirmation">
                    取消
                  </NButton>
                  <NButton
                    size="small"
                    :type="confirmButtonType"
                    :loading="confirmRunning"
                    :disabled="confirmRunning || version.busy"
                    @click="executeConfirmation"
                  >
                    {{ confirmPositiveText }}
                  </NButton>
                </NSpace>
              </div>
            </NAlert>
          </div>

          <!-- 左右双栏主体 -->
          <div class="version-dual-pane">
            <!-- 左栏：工作区选择卡片与提交历史树 -->
            <div class="version-left-col">
              <!-- 工作区变更卡片 -->
              <div
                class="version-workspace-card"
                :class="{ 'version-workspace-card--active': activeTarget.type === 'workspace' }"
                @click="selectWorkspace"
              >
                <div class="version-workspace-card-header">
                  <div class="workspace-card-title">
                    <NIcon size="16"><DocumentSync20Regular /></NIcon>
                    <span>当前工作区变更</span>
                  </div>
                  <NTag
                    size="tiny"
                    :type="version.changes.length ? 'warning' : 'default'"
                    :bordered="false"
                    round
                  >
                    {{ version.changes.length }} 个文件
                  </NTag>
                </div>
                <div class="workspace-card-sub">
                  <span v-if="version.changes.length">
                    包含未提交的文件改动，点击右侧预览 Diff
                  </span>
                  <span v-else>工作区清洁，相对 HEAD 无变更</span>
                </div>
              </div>

              <!-- 提交历史树 -->
              <div class="version-history-section">
                <div class="history-section-title">
                  <div class="history-title-text">
                    <NIcon size="16"><History20Regular /></NIcon>
                    <span>版本提交历史</span>
                  </div>
                  <NTag size="tiny" :bordered="false" round>
                    {{ version.history.length }} 次
                  </NTag>
                </div>

                <div class="history-commits-list">
                  <template v-if="version.history.length">
                    <div
                      v-for="commitItem in version.history"
                      :key="commitItem.id"
                      class="commit-card"
                      :class="{
                        'commit-card--active':
                          activeTarget.type === 'commit' &&
                          activeTarget.commit.id === commitItem.id,
                      }"
                      @click="selectCommit(commitItem)"
                    >
                      <div class="commit-card-msg" :title="commitItem.message">
                        {{ commitItem.message }}
                      </div>
                      <div class="commit-card-meta">
                        <span>{{ formatTime(commitItem.createdAt) }}</span>
                        <span>·</span>
                        <span>{{ commitItem.changeCount }} 个文件</span>
                        <span>·</span>
                        <span class="commit-card-hash">{{ shortID(commitItem.id) }}</span>
                      </div>
                    </div>
                  </template>
                  <NEmpty v-else description="暂无版本历史" size="small" class="history-empty" />
                </div>
              </div>
            </div>

            <!-- 右栏：选定目标详情与文件列表及 Diff 预览 -->
            <div class="version-right-col">
              <!-- 目标头部 -->
              <div class="version-detail-header">
                <template v-if="activeTarget.type === 'workspace'">
                  <div class="detail-header-info">
                    <div class="detail-header-title">工作区未提交变更</div>
                    <div class="detail-header-meta">
                      <span>{{ currentFileStats.total }} 个文件发生变化</span>
                      <template v-if="currentFileStats.total">
                        <span>·</span>
                        <NText type="success">{{ currentFileStats.add }} 新增</NText>
                        <span>·</span>
                        <NText type="warning">{{ currentFileStats.modify }} 修改</NText>
                        <span>·</span>
                        <NText type="error">{{ currentFileStats.delete }} 删除</NText>
                      </template>
                    </div>
                  </div>
                  <div class="detail-header-actions">
                    <NButton
                      size="small"
                      secondary
                      :disabled="!version.status.pendingChangeSets || version.busy || !!confirmAction"
                      @click="undo"
                    >
                      撤销
                    </NButton>
                    <NButton
                      size="small"
                      secondary
                      type="warning"
                      :disabled="!version.status.changedFiles || version.busy || !!confirmAction"
                      @click="confirmDiscard"
                    >
                      放弃全部
                    </NButton>
                  </div>
                </template>

                <template v-else>
                  <div class="detail-header-info">
                    <div class="detail-header-title" :title="activeTarget.commit.message">
                      {{ activeTarget.commit.message }}
                    </div>
                    <div class="detail-header-meta">
                      <span>提交 {{ shortID(activeTarget.commit.id) }}</span>
                      <span>·</span>
                      <span>{{ formatTime(activeTarget.commit.createdAt) }}</span>
                      <span>·</span>
                      <span>{{ activeTarget.commit.changeCount }} 个文件</span>
                    </div>
                  </div>
                  <div class="detail-header-actions">
                    <NButton
                      size="small"
                      secondary
                      :disabled="version.busy || !!confirmAction"
                      @click="confirmCheckout(activeTarget.commit)"
                    >
                      加载到工作区
                    </NButton>
                    <NButton
                      size="small"
                      secondary
                      :loading="
                        version.exporting &&
                        version.exportingCommitID === activeTarget.commit.id
                      "
                      :disabled="
                        version.busy ||
                        !!confirmAction ||
                        !activeTarget.commit.changeCount
                      "
                      @click="exportCommit(activeTarget.commit)"
                    >
                      <template #icon>
                        <NIcon><ArrowDownload20Regular /></NIcon>
                      </template>
                      导出修改文件
                    </NButton>
                  </div>
                </template>
              </div>

              <!-- 文件列表（按 Add/Modify/Delete 分组） -->
              <div class="version-files-pane">
                <template v-if="currentFileList.length">
                  <!-- 新增文件 -->
                  <div v-if="groupedCurrentFiles.add.length" class="version-file-group">
                    <div class="version-group-header" @click="toggleGroup('add')">
                      <span class="group-dot group-dot--add"></span>
                      <span class="group-title">新增文件</span>
                      <NTag size="tiny" type="success" :bordered="false" round>
                        {{ groupedCurrentFiles.add.length }}
                      </NTag>
                      <span class="group-chevron">
                        {{ isGroupCollapsed('add') ? '▸' : '▾' }}
                      </span>
                    </div>
                    <div v-show="!isGroupCollapsed('add')" class="version-group-files">
                      <div
                        v-for="file in groupedCurrentFiles.add"
                        :key="file.path"
                        class="version-file-row"
                        :class="{ 'version-file-row--selected': selectedFile?.path === file.path }"
                        @click="onFileClick(file)"
                      >
                        <NTag size="tiny" type="success" :bordered="false">新增</NTag>
                        <span class="version-file-path" :title="file.path">{{ file.path }}</span>
                        <div class="version-file-actions" @click.stop>
                          <NButton
                            text
                            size="tiny"
                            type="primary"
                            class="file-action-btn"
                            title="在编辑器中打开"
                            @click="openChange(file)"
                          >
                            打开
                          </NButton>
                          <NPopconfirm
                            v-if="activeTarget.type === 'workspace'"
                            positive-text="确认还原"
                            negative-text="取消"
                            @positive-click="restoreSingleFile(file.path)"
                          >
                            <template #trigger>
                              <NButton
                                text
                                size="tiny"
                                type="warning"
                                class="file-action-btn"
                                title="还原此新增文件至 HEAD"
                              >
                                还原
                              </NButton>
                            </template>
                            确定将 {{ file.path }} 还原到当前 HEAD 吗？未提交的修改将被覆盖。
                          </NPopconfirm>
                        </div>
                      </div>
                    </div>
                  </div>

                  <!-- 修改文件 -->
                  <div v-if="groupedCurrentFiles.modify.length" class="version-file-group">
                    <div class="version-group-header" @click="toggleGroup('modify')">
                      <span class="group-dot group-dot--modify"></span>
                      <span class="group-title">修改文件</span>
                      <NTag size="tiny" type="warning" :bordered="false" round>
                        {{ groupedCurrentFiles.modify.length }}
                      </NTag>
                      <span class="group-chevron">
                        {{ isGroupCollapsed('modify') ? '▸' : '▾' }}
                      </span>
                    </div>
                    <div v-show="!isGroupCollapsed('modify')" class="version-group-files">
                      <div
                        v-for="file in groupedCurrentFiles.modify"
                        :key="file.path"
                        class="version-file-row"
                        :class="{ 'version-file-row--selected': selectedFile?.path === file.path }"
                        @click="onFileClick(file)"
                      >
                        <NTag size="tiny" type="warning" :bordered="false">修改</NTag>
                        <span class="version-file-path" :title="file.path">{{ file.path }}</span>
                        <div class="version-file-actions" @click.stop>
                          <NButton
                            text
                            size="tiny"
                            type="primary"
                            class="file-action-btn"
                            title="在编辑器中打开"
                            @click="openChange(file)"
                          >
                            打开
                          </NButton>
                          <NPopconfirm
                            v-if="activeTarget.type === 'workspace'"
                            positive-text="确认还原"
                            negative-text="取消"
                            @positive-click="restoreSingleFile(file.path)"
                          >
                            <template #trigger>
                              <NButton
                                text
                                size="tiny"
                                type="warning"
                                class="file-action-btn"
                                title="还原此修改文件至 HEAD"
                              >
                                还原
                              </NButton>
                            </template>
                            确定将 {{ file.path }} 还原到当前 HEAD 吗？未提交的修改将被覆盖。
                          </NPopconfirm>
                        </div>
                      </div>
                    </div>
                  </div>

                  <!-- 删除文件 -->
                  <div v-if="groupedCurrentFiles.delete.length" class="version-file-group">
                    <div class="version-group-header" @click="toggleGroup('delete')">
                      <span class="group-dot group-dot--delete"></span>
                      <span class="group-title">删除文件</span>
                      <NTag size="tiny" type="error" :bordered="false" round>
                        {{ groupedCurrentFiles.delete.length }}
                      </NTag>
                      <span class="group-chevron">
                        {{ isGroupCollapsed('delete') ? '▸' : '▾' }}
                      </span>
                    </div>
                    <div v-show="!isGroupCollapsed('delete')" class="version-group-files">
                      <div
                        v-for="file in groupedCurrentFiles.delete"
                        :key="file.path"
                        class="version-file-row"
                        :class="{ 'version-file-row--selected': selectedFile?.path === file.path }"
                        @click="onFileClick(file)"
                      >
                        <NTag size="tiny" type="error" :bordered="false">删除</NTag>
                        <span class="version-file-path" :title="file.path">{{ file.path }}</span>
                        <div class="version-file-actions" @click.stop>
                          <NButton
                            text
                            size="tiny"
                            type="primary"
                            class="file-action-btn"
                            disabled
                            title="文件已删除，无法在当前工作区打开"
                          >
                            打开
                          </NButton>
                          <NPopconfirm
                            v-if="activeTarget.type === 'workspace'"
                            positive-text="确认还原"
                            negative-text="取消"
                            @positive-click="restoreSingleFile(file.path)"
                          >
                            <template #trigger>
                              <NButton
                                text
                                size="tiny"
                                type="warning"
                                class="file-action-btn"
                                title="还原此删除文件至 HEAD"
                              >
                                还原
                              </NButton>
                            </template>
                            确定将 {{ file.path }} 还原到当前 HEAD 吗？已删除的文件将被恢复。
                          </NPopconfirm>
                        </div>
                      </div>
                    </div>
                  </div>
                </template>
                <div v-else class="version-files-empty">
                  <NEmpty
                    :description="
                      activeTarget.type === 'workspace'
                        ? '工作区没有相对 HEAD 的变更'
                        : '该版本没有文件变更'
                    "
                    size="small"
                  />
                </div>
              </div>

              <!-- 下方 Diff 预览区域 -->
              <div class="version-diff-container">
                <div class="version-diff-header">
                  <div class="diff-header-left">
                    <span class="diff-header-title">变更差异 (Diff)</span>
                    <template v-if="selectedFile">
                      <NTag
                        size="tiny"
                        :type="operationType(selectedFile.operation)"
                        :bordered="false"
                      >
                        {{ operationLabel(selectedFile.operation) }}
                      </NTag>
                      <span class="diff-file-path" :title="selectedFile.path">
                        {{ selectedFile.path }}
                      </span>
                    </template>
                  </div>
                  <div class="diff-header-right">
                    <template
                      v-if="activeDiff && activeDiff.textAvailable && parsedDiff.lines.length"
                    >
                      <NTag size="tiny" type="success" :bordered="false">
                        +{{ parsedDiff.addCount }}
                      </NTag>
                      <NTag size="tiny" type="error" :bordered="false">
                        -{{ parsedDiff.removeCount }}
                      </NTag>
                    </template>
                    <NButton
                      v-if="selectedFile && selectedFile.operation !== 'delete'"
                      size="tiny"
                      secondary
                      @click="openChange(selectedFile)"
                    >
                      在编辑器打开
                    </NButton>
                    <NPopconfirm
                      v-if="activeTarget.type === 'workspace' && selectedFile"
                      positive-text="确认还原"
                      negative-text="取消"
                      @positive-click="restoreSingleFile(selectedFile.path)"
                    >
                      <template #trigger>
                        <NButton size="tiny" secondary type="warning">
                          还原此文件
                        </NButton>
                      </template>
                      确定将 {{ selectedFile.path }} 还原到当前 HEAD 吗？
                    </NPopconfirm>
                  </div>
                </div>

                <div class="version-diff-body">
                  <NSpin :show="diffLoading" class="diff-spin-container">
                    <div v-if="diffError" class="diff-error-box">
                      <NAlert type="error" :show-icon="false">{{ diffError }}</NAlert>
                    </div>

                    <div v-else-if="!selectedFile" class="diff-empty-box">
                      <NEmpty description="请在上方选择文件以查看 Diff 差异" size="small" />
                    </div>

                    <div v-else-if="activeDiff && !activeDiff.textAvailable" class="diff-binary-box">
                      <div class="diff-binary-content">
                        <NIcon size="36" color="var(--pvf-text-muted)"><Document20Regular /></NIcon>
                        <div class="diff-binary-title">二进制或非文本资源</div>
                        <div class="diff-binary-desc">
                          该文件为非纯文本或复合二进制格式，不支持行级文本差异预览。
                        </div>
                      </div>
                    </div>

                    <div v-else-if="parsedDiff.lines.length" class="diff-lines-wrapper">
                      <div class="diff-lines">
                        <div
                          v-for="(line, idx) in parsedDiff.lines"
                          :key="idx"
                          :class="['diff-line', `diff-line--${line.kind}`]"
                        >
                          <template v-if="line.kind === 'hunk'">
                            <span class="diff-hunk-text">{{ line.text }}</span>
                          </template>
                          <template v-else>
                            <span class="diff-line-number">{{ line.oldLine ?? "" }}</span>
                            <span class="diff-line-number">{{ line.newLine ?? "" }}</span>
                            <span class="diff-marker">
                              {{ line.kind === 'add' ? '+' : line.kind === 'remove' ? '−' : ' ' }}
                            </span>
                            <span class="diff-text">{{ line.text }}</span>
                          </template>
                        </div>
                      </div>
                      <div v-if="parsedDiff.truncated" class="diff-truncated-notice">
                        Diff 已截断，仅展示前 2000 行变更。
                      </div>
                    </div>

                    <div v-else class="diff-empty-box">
                      <NEmpty description="该文件内容在本次变更中无文本行级差异" size="small" />
                    </div>
                  </NSpin>
                </div>
              </div>
            </div>
          </div>
        </template>
      </NSpin>
    </div>
  </NModal>
</template>

<style scoped>
:global(.n-card.version-modal) {
  width: 1060px !important;
  max-width: calc(100vw - 32px) !important;
  height: 780px !important;
  max-height: calc(100vh - 48px) !important;
  box-sizing: border-box;
}
:global(.n-card.version-modal > .n-card-content) {
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
}
.version-panel-root {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
}
.version-init-spin {
  display: flex;
  flex-direction: column;
  flex: 1;
  height: 100%;
  min-height: 0;
}
.version-init-spin :deep(.n-spin-container),
.version-init-spin :deep(.n-spin-content) {
  height: 100%;
  display: flex;
  flex-direction: column;
  min-height: 0;
}
.version-loading-alert {
  margin-top: 20px;
}

/* 引导页卡片化样式 */
.version-guide-wrapper {
  display: flex;
  flex-direction: column;
  gap: 14px;
  padding: 8px 4px;
  overflow-y: auto;
}
.version-guide-banner {
  text-align: center;
  padding: 10px 16px 6px;
}
.banner-badge {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 12px;
  border-radius: 12px;
  background: var(--pvf-primary-soft);
  color: var(--pvf-primary);
  font-size: 12px;
  font-weight: 600;
  margin-bottom: 8px;
}
.banner-title {
  font-size: 20px;
  font-weight: 700;
  color: var(--pvf-text-primary);
  margin-bottom: 6px;
}
.banner-subtitle {
  font-size: 13px;
  color: var(--pvf-text-secondary);
  max-width: 560px;
  margin: 0 auto;
  line-height: 1.5;
}
.version-guide-cards {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 12px;
  margin-top: 4px;
}
.guide-card {
  padding: 16px;
  background: var(--pvf-surface-card);
  border: 1px solid var(--pvf-border-subtle);
  border-radius: 8px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.guide-card-icon {
  width: 40px;
  height: 40px;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
}
.guide-card-icon--camera {
  background: var(--pvf-surface-subtle);
  color: var(--pvf-primary);
}
.guide-card-icon--shield {
  background: var(--pvf-surface-success);
  color: var(--pvf-success-hover);
}
.guide-card-icon--flash {
  background: var(--pvf-surface-warning);
  color: var(--pvf-warning-hover);
}
.guide-card-title {
  font-weight: 600;
  font-size: 14px;
  color: var(--pvf-text-primary);
}
.guide-card-desc {
  font-size: 12px;
  color: var(--pvf-text-secondary);
  line-height: 1.55;
}
.guide-card-desc code {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  background: var(--pvf-surface-code);
  padding: 1px 4px;
  border-radius: 4px;
  font-size: 11px;
}
.guide-alert {
  margin-top: 4px;
}
.guide-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-top: 8px;
  padding-top: 12px;
  border-top: 1px solid var(--pvf-border-subtle);
}

/* 顶部状态与提交栏 */
.version-top-panel {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--pvf-border-subtle);
  flex-shrink: 0;
}
.version-summary {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.version-head-info {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  flex: 1;
}
.version-branch-tag {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-weight: 600;
  color: var(--pvf-primary);
}
.version-head-message {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--pvf-text-secondary);
}
.version-summary-tags {
  flex-shrink: 0;
}
.version-commit-row {
  display: flex;
  align-items: center;
  gap: 8px;
}
.version-commit-row :deep(.n-input) {
  flex: 1;
}
.version-alert,
.version-confirm {
  margin-top: 2px;
}
.version-confirm-content {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}

/* 双栏主体 */
.version-dual-pane {
  display: flex;
  gap: 14px;
  flex: 1;
  min-height: 0;
  margin-top: 12px;
  overflow: hidden;
}

/* 左栏 */
.version-left-col {
  width: 320px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-height: 0;
  overflow: hidden;
}
.version-workspace-card {
  padding: 10px 12px;
  border-radius: 8px;
  border: 1px solid var(--pvf-border-subtle);
  background: var(--pvf-surface-card);
  cursor: pointer;
  transition: all 0.15s ease;
  display: flex;
  flex-direction: column;
  gap: 4px;
  flex-shrink: 0;
}
.version-workspace-card:hover {
  border-color: var(--pvf-primary-soft);
  background: var(--pvf-surface-hover);
}
.version-workspace-card--active {
  border-color: var(--pvf-primary);
  background: var(--pvf-surface-selected);
}
.version-workspace-card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.workspace-card-title {
  display: flex;
  align-items: center;
  gap: 6px;
  font-weight: 600;
  font-size: 13px;
}
.workspace-card-sub {
  font-size: 12px;
  color: var(--pvf-text-muted);
}
.version-history-section {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
  overflow: hidden;
}
.history-section-title {
  font-weight: 600;
  font-size: 13px;
  margin-bottom: 8px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-shrink: 0;
}
.history-title-text {
  display: flex;
  align-items: center;
  gap: 6px;
}
.history-commits-list {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding-right: 4px;
}
.commit-card {
  padding: 9px 12px;
  border-radius: 6px;
  border: 1px solid var(--pvf-border-subtle);
  background: var(--pvf-surface-card);
  cursor: pointer;
  transition: all 0.15s ease;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.commit-card:hover {
  border-color: var(--pvf-primary-soft);
  background: var(--pvf-surface-hover);
}
.commit-card--active {
  border-color: var(--pvf-primary);
  background: var(--pvf-surface-selected);
}
.commit-card-msg {
  font-size: 13px;
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.commit-card-meta {
  font-size: 11px;
  color: var(--pvf-text-muted);
  display: flex;
  align-items: center;
  gap: 6px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.commit-card-hash {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}
.history-empty {
  padding: 32px 0;
}

/* 右栏 */
.version-right-col {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
}
.version-detail-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding-bottom: 10px;
  border-bottom: 1px solid var(--pvf-border-subtle);
  gap: 12px;
  flex-shrink: 0;
}
.detail-header-info {
  min-width: 0;
  flex: 1;
}
.detail-header-title {
  font-size: 15px;
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.detail-header-meta {
  font-size: 12px;
  color: var(--pvf-text-muted);
  margin-top: 3px;
  display: flex;
  align-items: center;
  gap: 8px;
}
.detail-header-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}
.version-files-pane {
  flex: 0 0 clamp(128px, 30%, 240px);
  height: auto;
  max-height: none;
  min-height: 0;
  overflow-y: scroll;
  overscroll-behavior: contain;
  scrollbar-gutter: stable;
  margin-top: 8px;
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding-right: 4px;
}
.version-files-pane::-webkit-scrollbar {
  width: 8px;
}
.version-files-pane::-webkit-scrollbar-track {
  background: var(--pvf-surface-subtle);
  border-radius: 4px;
}
.version-files-pane::-webkit-scrollbar-thumb {
  background: var(--pvf-border-strong);
  border-radius: 4px;
}
.version-files-pane {
  scrollbar-width: thin;
  scrollbar-color: var(--pvf-border-strong) var(--pvf-surface-subtle);
}
.version-files-empty {
  padding: 16px 0;
}
.version-file-group {
  display: flex;
  flex-direction: column;
  flex: 0 0 auto;
  border: 1px solid var(--pvf-border-subtle);
  border-radius: 6px;
  overflow: hidden;
  background: var(--pvf-surface-card);
}
.version-group-header {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 10px;
  background: var(--pvf-surface-panel);
  cursor: pointer;
  user-select: none;
  font-size: 12px;
  font-weight: 500;
}
.version-group-header:hover {
  background: var(--pvf-surface-hover);
}
.group-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
}
.group-dot--add {
  background: var(--pvf-success);
}
.group-dot--modify {
  background: var(--pvf-warning);
}
.group-dot--delete {
  background: var(--pvf-error);
}
.group-title {
  flex: 1;
}
.group-chevron {
  color: var(--pvf-text-muted);
  font-size: 10px;
}
.version-group-files {
  display: flex;
  flex-direction: column;
}
.version-file-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 5px 10px;
  border-top: 1px solid var(--pvf-border-faint);
  cursor: pointer;
  transition: background 0.15s ease;
  font-size: 12px;
}
.version-file-row:hover {
  background: var(--pvf-surface-hover);
}
.version-file-row--selected {
  background: var(--pvf-surface-selected);
}
.version-file-path {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}
.version-file-actions {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
}
.file-action-btn {
  font-size: 11px;
}

/* Diff 预览容器 */
.version-diff-container {
  display: flex;
  flex-direction: column;
  flex: 1 1 0;
  min-height: 0;
  margin-top: 10px;
  border: 1px solid var(--pvf-border-subtle);
  border-radius: 6px;
  overflow: hidden;
  background: var(--pvf-surface-card);
}
.version-diff-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 6px 10px;
  background: var(--pvf-surface-panel);
  border-bottom: 1px solid var(--pvf-border-subtle);
  gap: 12px;
  flex-shrink: 0;
}
.diff-header-left,
.diff-header-right {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.diff-header-title {
  font-weight: 600;
  font-size: 12px;
  white-space: nowrap;
}
.diff-file-path {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
  color: var(--pvf-text-secondary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.version-diff-body {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  position: relative;
  overflow: hidden;
}
.diff-spin-container {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  height: 100%;
}
.diff-spin-container :deep(.n-spin-container),
.diff-spin-container :deep(.n-spin-content) {
  height: 100%;
  display: flex;
  flex-direction: column;
  min-height: 0;
}
.diff-lines-wrapper {
  flex: 1;
  min-height: 0;
  overflow: auto;
  background: var(--pvf-surface-code);
}
.diff-lines {
  display: flex;
  flex-direction: column;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
  line-height: 1.55;
  white-space: pre;
  min-width: 100%;
  width: max-content;
}
.diff-line {
  display: grid;
  grid-template-columns: 44px 44px 20px minmax(0, 1fr);
  min-height: 20px;
  padding: 0 8px;
}
.diff-line--add {
  color: var(--pvf-success-hover);
  background: var(--pvf-surface-success);
}
.diff-line--remove {
  color: var(--pvf-error-hover);
  background: var(--pvf-surface-error);
}
.diff-line--context {
  color: var(--pvf-text-primary);
}
.diff-line--hunk {
  display: block;
  padding: 3px 12px;
  background: var(--pvf-surface-subtle);
  color: var(--pvf-text-muted);
  font-style: italic;
  user-select: none;
}
.diff-line-number {
  padding-right: 8px;
  color: var(--pvf-text-faint);
  text-align: right;
  user-select: none;
}
.diff-marker {
  color: var(--pvf-text-faint);
  text-align: center;
  user-select: none;
}
.diff-text {
  min-width: 0;
}
.diff-truncated-notice {
  padding: 6px 12px;
  font-size: 11px;
  color: var(--pvf-text-muted);
  background: var(--pvf-surface-subtle);
  border-top: 1px solid var(--pvf-border-subtle);
}
.diff-binary-box,
.diff-empty-box,
.diff-error-box {
  display: flex;
  align-items: center;
  justify-content: center;
  flex: 1;
  padding: 24px 16px;
}
.diff-binary-content {
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  gap: 8px;
}
.diff-binary-title {
  font-weight: 600;
  font-size: 13px;
  color: var(--pvf-text-primary);
}
.diff-binary-desc {
  font-size: 12px;
  color: var(--pvf-text-secondary);
  max-width: 360px;
  line-height: 1.5;
}

@media (max-width: 768px) {
  .version-guide-cards {
    grid-template-columns: 1fr;
  }
  .version-summary,
  .version-commit-row,
  .version-confirm-content {
    flex-direction: column;
    align-items: stretch;
  }
  .version-commit-row :deep(.n-button) {
    width: 100%;
  }
  .version-dual-pane {
    flex-direction: column;
  }
  .version-left-col {
    width: 100%;
    max-height: 180px;
  }
}
</style>
