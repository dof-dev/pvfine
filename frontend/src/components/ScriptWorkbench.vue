<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from "vue";
import {
  NAlert,
  NBadge,
  NButton,
  NEmpty,
  NIcon,
  NInput,
  NProgress,
  NScrollbar,
  NSpin,
  NTag,
  NText,
  useDialog,
  useMessage,
} from "naive-ui";
import {
  Add24Regular,
  ArrowSync24Regular,
  Checkmark24Regular,
  ChevronLeft24Regular,
  Code24Regular,
  Dismiss24Regular,
  DocumentText24Regular,
  Eraser24Regular,
  FolderOpen24Regular,
  PanelBottomContract20Regular,
  PanelBottomExpand20Regular,
  PanelRightContract20Regular,
  PanelRightExpand20Regular,
  Save24Regular,
  Stop24Regular,
} from "@vicons/fluent";
import CodeEditor from "./CodeEditor.vue";
import { useScriptStore } from "../stores/script";
import { useEditorStore } from "../stores/editor";
import { useSettingsStore } from "../stores/settings";
import type { BatchDiffLine, ScriptDiagnostic } from "../../bindings/pvfine/services/models";
import type { ResolvedThemeId } from "../theme";

defineProps<{
  themeId: ResolvedThemeId;
}>();

const emit = defineEmits<{
  (event: "close"): void;
}>();

const script = useScriptStore();
const editor = useEditorStore();
const settings = useSettingsStore();
const message = useMessage();
const dialog = useDialog();
const scriptLibraryVisible = ref(false);
const scriptEditor = ref<{ revealPosition: (line: number, column?: number) => void } | null>(null);
const scriptMain = ref<HTMLDivElement | null>(null);
const scriptEditorPanel = ref<HTMLElement | null>(null);
const diffVisible = ref(true);
const diffWidth = ref(38);
const diffResizing = ref(false);
const consoleTab = ref<"logs" | "diagnostics">("logs");
const consoleCollapsed = ref(false);
const consoleHeight = ref(175);
const consoleResizing = ref(false);
let diffResizeMove: ((event: PointerEvent) => void) | null = null;
let diffResizeEnd: (() => void) | null = null;
let consoleResizeMove: ((event: PointerEvent) => void) | null = null;
let consoleResizeEnd: (() => void) | null = null;

const minDiffWidth = 25;
const maxDiffWidth = 70;
const minConsoleHeight = 120;
const minEditorHeight = 120;

const progressPercent = computed(() => {
  if (!script.progress.total) return 0;
  return Math.max(0, Math.min(100, Math.round((script.progress.done / script.progress.total) * 100)));
});

const statusLabel = computed(() => {
  if (script.running) return script.stopping ? "正在停止" : "运行中";
  if (script.stale) return "预览已过期";
  if (script.hasPreview) return "等待应用";
  if (script.runResult?.status === "failed") return "运行失败";
  if (script.runResult?.status === "cancelled") return "已停止";
  return "就绪";
});

const statusType = computed<"default" | "success" | "warning" | "error" | "info">(() => {
  if (script.running) return "info";
  if (script.stale || script.runResult?.status === "cancelled") return "warning";
  if (script.runResult?.status === "failed") return "error";
  if (script.hasPreview) return "success";
  return "default";
});

const runSummary = computed(() => {
  const result = script.runResult;
  if (!result || result.status !== "completed") return "";
  return `耗时 ${formatDuration(result.durationMs)}，扫描 ${formatCount(result.scannedFiles)} 个文件，产生 ${formatCount(result.modifiedFiles)} 个变更`;
});

function formatCount(value: number): string {
  return Math.max(0, Math.trunc(Number(value) || 0)).toLocaleString("zh-CN");
}

function formatDuration(value: number): string {
  return `${Math.max(0, Math.round(Number(value) || 0)).toLocaleString("zh-CN")}ms`;
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

function toggleDiff(): void {
  diffVisible.value = !diffVisible.value;
}

function toggleConsole(): void {
  consoleCollapsed.value = !consoleCollapsed.value;
}

function onDiffResizeStart(): void {
  if (!diffVisible.value || diffResizing.value || !scriptMain.value) return;
  const rect = scriptMain.value.getBoundingClientRect();
  if (rect.width <= 0) return;

  diffResizing.value = true;
  document.body.style.cursor = "col-resize";
  document.body.style.userSelect = "none";
  diffResizeMove = (event: PointerEvent) => {
    const nextWidth = ((rect.right - event.clientX) / rect.width) * 100;
    diffWidth.value = clamp(nextWidth, minDiffWidth, maxDiffWidth);
  };
  diffResizeEnd = () => onDiffResizeEnd();
  window.addEventListener("pointermove", diffResizeMove);
  window.addEventListener("pointerup", diffResizeEnd);
  window.addEventListener("pointercancel", diffResizeEnd);
}

function onDiffResizeEnd(): void {
  if (!diffResizing.value) return;
  diffResizing.value = false;
  document.body.style.cursor = "";
  document.body.style.userSelect = "";
  if (diffResizeMove) window.removeEventListener("pointermove", diffResizeMove);
  if (diffResizeEnd) {
    window.removeEventListener("pointerup", diffResizeEnd);
    window.removeEventListener("pointercancel", diffResizeEnd);
  }
  diffResizeMove = null;
  diffResizeEnd = null;
}

function onConsoleResizeStart(event: PointerEvent): void {
  if (consoleCollapsed.value || consoleResizing.value || !scriptEditorPanel.value) return;
  const rect = scriptEditorPanel.value.getBoundingClientRect();
  if (rect.height <= 0) return;
  const startY = event.clientY;
  const startHeight = consoleHeight.value;

  consoleResizing.value = true;
  document.body.style.cursor = "row-resize";
  document.body.style.userSelect = "none";
  consoleResizeMove = (event: PointerEvent) => {
    const maxHeight = Math.max(
      minConsoleHeight,
      rect.height - 34 - minEditorHeight - 5,
    );
    consoleHeight.value = clamp(
      startHeight - (event.clientY - startY),
      minConsoleHeight,
      maxHeight,
    );
  };
  consoleResizeEnd = () => onConsoleResizeEnd();
  window.addEventListener("pointermove", consoleResizeMove);
  window.addEventListener("pointerup", consoleResizeEnd);
  window.addEventListener("pointercancel", consoleResizeEnd);
}

function onConsoleResizeEnd(): void {
  if (!consoleResizing.value) return;
  consoleResizing.value = false;
  document.body.style.cursor = "";
  document.body.style.userSelect = "";
  if (consoleResizeMove) window.removeEventListener("pointermove", consoleResizeMove);
  if (consoleResizeEnd) {
    window.removeEventListener("pointerup", consoleResizeEnd);
    window.removeEventListener("pointercancel", consoleResizeEnd);
  }
  consoleResizeMove = null;
  consoleResizeEnd = null;
}

function focusDiagnostic(diagnostic: ScriptDiagnostic): void {
  if (!diagnostic.line || diagnostic.line < 1) return;
  consoleTab.value = "diagnostics";
  scriptEditor.value?.revealPosition(diagnostic.line, diagnostic.column ?? 1);
}

function isSelected(fileIndex: number): boolean {
  return script.selectedIndexes.has(fileIndex);
}

function formatSize(size: number): string {
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
  return `${(size / 1024 / 1024).toFixed(1)} MB`;
}

function diffPrefix(line: BatchDiffLine | null | undefined): string {
  if (line?.kind === "remove") return "−";
  if (line?.kind === "add") return "+";
  return " ";
}

function diffLineNumber(
  line: BatchDiffLine | null | undefined,
  side: "old" | "new",
): string {
  if (!line) return "";
  if (side === "old" && line.kind === "add") return "";
  if (side === "new" && line.kind === "remove") return "";
  const value = side === "old" ? line.oldLine : line.newLine;
  return value > 0 ? String(value) : "";
}

function isCancel(error: any): boolean {
  return String(error?.message ?? error).toLowerCase().includes("cancel");
}

function confirmDiscard(action: () => void): void {
  if (!script.dirty) {
    action();
    return;
  }
  dialog.warning({
    title: "脚本尚未保存",
    content: "切换脚本会丢失当前未保存内容，确定继续吗？",
    positiveText: "继续切换",
    negativeText: "取消",
    onPositiveClick: action,
  });
}

function onNew(): void {
  confirmDiscard(() => script.newScript());
}

function showScriptLibrary(): void {
  if (script.running) return;
  scriptLibraryVisible.value = true;
  void script.refreshFiles();
}

function hideScriptLibrary(): void {
  if (script.loadingScript) return;
  scriptLibraryVisible.value = false;
}

function onLoad(name: string): void {
  confirmDiscard(() => {
    void script.loadScript(name).then(() => {
      if (!script.error) scriptLibraryVisible.value = false;
    });
  });
}

async function onSave(): Promise<void> {
  try {
    await script.saveScript();
    message.success(`已保存 ${script.currentName}`);
  } catch (error: any) {
    if (!isCancel(error)) message.error(`保存失败: ${error?.message ?? error}`);
  }
}

async function onCheck(): Promise<void> {
  try {
    const result = await script.compile();
    if (result.valid) message.success("脚本语法检查通过");
    else {
      consoleTab.value = "diagnostics";
      message.error("脚本存在语法错误，请查看下方诊断");
    }
  } catch (error: any) {
    message.error(`检查失败: ${error?.message ?? error}`);
  }
}

async function onRun(): Promise<void> {
  try {
    consoleTab.value = "logs";
    await script.run();
    if (script.runResult?.status === "completed") {
      message.success(`运行完成，产生 ${script.modifiedFiles} 个预览变更`);
    }
  } catch (error: any) {
    if (!isCancel(error)) message.error(`运行失败: ${error?.message ?? error}`);
  }
}

async function onStop(): Promise<void> {
  await script.cancel();
}

async function onApply(): Promise<void> {
  try {
    const result = await script.apply();
    await editor.refreshBatchFiles(result.fileIndexes ?? []);
    message.success("已应用选中的脚本修改；请继续手动保存 PVF");
  } catch (error: any) {
    if (!isCancel(error)) message.error(`应用失败: ${error?.message ?? error}`);
  }
}

async function onDiscard(): Promise<void> {
  await script.discard();
}

async function onOpenDirectory(): Promise<void> {
  try {
    await script.openDirectory();
  } catch (error: any) {
    message.error(`打开目录失败: ${error?.message ?? error}`);
  }
}

onBeforeUnmount(() => {
  onDiffResizeEnd();
  onConsoleResizeEnd();
});
</script>

<template>
  <div class="script-workbench">
    <template v-if="!scriptLibraryVisible">
      <div class="script-toolbar">
        <div class="script-toolbar-title">
          <NIcon :size="18"><Code24Regular /></NIcon>
          <span>脚本工作区</span>
        </div>
        <NInput
          v-model:value="script.currentName"
          size="small"
          class="script-name-input"
          placeholder="脚本名称，例如 balance.pvf.js"
          :disabled="script.running"
        />
        <NButton size="small" secondary @click="onNew" :disabled="script.running">
          <template #icon><NIcon><Add24Regular /></NIcon></template>
          新建
        </NButton>
        <NButton size="small" secondary @click="showScriptLibrary" :disabled="script.running">
          <template #icon><NIcon><FolderOpen24Regular /></NIcon></template>
          打开脚本
        </NButton>
        <NButton size="small" secondary :loading="script.saving" @click="onSave" :disabled="script.running">
          <template #icon><NIcon><Save24Regular /></NIcon></template>
          保存
        </NButton>
        <NButton size="small" secondary @click="onCheck" :disabled="script.running">
          <template #icon><NIcon><Checkmark24Regular /></NIcon></template>
          检查
        </NButton>
        <NButton size="small" type="primary" :loading="script.running" :disabled="!script.canRun" @click="onRun">
          <template #icon><NIcon><Code24Regular /></NIcon></template>
          预览运行
        </NButton>
        <NButton v-if="script.running" size="small" type="warning" :loading="script.stopping" @click="onStop">
          <template #icon><NIcon><Stop24Regular /></NIcon></template>
          停止
        </NButton>
      </div>

      <div ref="scriptMain" class="script-main">
        <section ref="scriptEditorPanel" class="script-editor-panel">
          <div class="script-editor-heading">
            <span>{{ script.currentName }}</span>
            <NTag v-if="script.dirty" size="tiny" type="warning" :bordered="false">未保存</NTag>
            <NTag size="tiny" :type="statusType" :bordered="false">{{ statusLabel }}</NTag>
            <NText v-if="runSummary" depth="3" class="script-run-summary" :title="runSummary">
              {{ runSummary }}
            </NText>
            <div class="script-editor-heading-actions">
              <NButton
                quaternary
                size="tiny"
                class="script-layout-button"
                :title="diffVisible ? '隐藏 Diff 面板' : '展开 Diff 面板'"
                :aria-label="diffVisible ? '隐藏 Diff 面板' : '展开 Diff 面板'"
                @click="toggleDiff"
              >
                <template #icon>
                  <NIcon>
                    <PanelRightContract20Regular v-if="diffVisible" />
                    <PanelRightExpand20Regular v-else />
                  </NIcon>
                </template>
                {{ diffVisible ? "隐藏 Diff" : "展开 Diff" }}
              </NButton>
              <NButton
                quaternary
                size="tiny"
                class="script-layout-button"
                :title="consoleCollapsed ? '展开控制台' : '折叠控制台'"
                :aria-label="consoleCollapsed ? '展开控制台' : '折叠控制台'"
                @click="toggleConsole"
              >
                <template #icon>
                  <NIcon>
                    <PanelBottomExpand20Regular v-if="consoleCollapsed" />
                    <PanelBottomContract20Regular v-else />
                  </NIcon>
                </template>
                {{ consoleCollapsed ? "展开控制台" : "折叠控制台" }}
              </NButton>
            </div>
          </div>
          <div class="script-editor-host">
            <CodeEditor
              ref="scriptEditor"
              :doc="script.source"
              language="javascript"
              :theme-id="themeId"
              :vim-mode="settings.vimMode"
              @change="script.updateSource"
            />
          </div>
          <div
            v-if="!consoleCollapsed"
            class="script-console-resizer"
            role="separator"
            aria-orientation="horizontal"
            aria-label="调整控制台高度"
            tabindex="0"
            @pointerdown.prevent="onConsoleResizeStart"
          />
          <div
            v-if="!consoleCollapsed"
            class="script-bottom-panel"
            :style="{ height: `${consoleHeight}px` }"
          >
            <div v-if="script.progress.total || script.running" class="script-progress-row">
              <NProgress type="line" :percentage="progressPercent" :show-indicator="false" />
              <span>{{ script.progress.done }} / {{ script.progress.total || "?" }}</span>
              <NText depth="3" class="script-progress-message" :title="script.progress.currentPath || script.progress.message">
                {{ script.progress.currentPath || script.progress.message || "执行中" }}
              </NText>
            </div>
            <div class="script-console-heading">
              <div class="script-console-tabs" role="tablist" aria-label="脚本控制台">
                <button
                  type="button"
                  role="tab"
                  class="script-console-tab"
                  :class="{ 'script-console-tab--active': consoleTab === 'logs' }"
                  :aria-selected="consoleTab === 'logs'"
                  @click="consoleTab = 'logs'"
                >
                  运行日志
                </button>
                <button
                  type="button"
                  role="tab"
                  class="script-console-tab"
                  :class="{ 'script-console-tab--active': consoleTab === 'diagnostics' }"
                  :aria-selected="consoleTab === 'diagnostics'"
                  @click="consoleTab = 'diagnostics'"
                >
                  <span>问题诊断</span>
                  <NBadge
                    v-if="script.diagnostics.length"
                    :value="script.diagnostics.length"
                    :max="99"
                    type="error"
                  />
                </button>
              </div>
              <NButton
                v-if="consoleTab === 'logs'"
                quaternary
                size="tiny"
                class="script-clear-logs"
                title="清空日志"
                aria-label="清空日志"
                :disabled="script.logs.length === 0"
                @click="script.clearLogs"
              >
                <template #icon><NIcon><Eraser24Regular /></NIcon></template>
                清空日志
              </NButton>
            </div>
            <NScrollbar v-if="consoleTab === 'logs'" class="script-console-scroll">
              <div v-if="script.logs.length === 0" class="script-log-empty">运行日志会显示在这里</div>
              <div v-for="(log, index) in script.logs" :key="index" class="script-log-line" :class="`script-log-line--${log.level}`">
                <span class="script-log-level">{{ log.level }}</span>
                <span>{{ log.message }}</span>
              </div>
            </NScrollbar>
            <NScrollbar v-else class="script-console-scroll">
              <div v-if="script.diagnostics.length === 0" class="script-log-empty">检查脚本后，问题诊断会显示在这里</div>
              <button
                v-for="(diagnostic, index) in script.diagnostics"
                :key="index"
                type="button"
                class="script-diagnostic"
                :class="{ 'script-diagnostic--clickable': !!diagnostic.line }"
                :disabled="!diagnostic.line"
                @click="focusDiagnostic(diagnostic)"
              >
                <NTag size="tiny" type="error" :bordered="false">{{ diagnostic.kind }}</NTag>
                <span class="script-diagnostic-message">{{ diagnostic.message }}</span>
                <NText v-if="diagnostic.line" depth="3">
                  第 {{ diagnostic.line }} 行{{ diagnostic.column ? `，第 ${diagnostic.column} 列` : "" }}
                </NText>
              </button>
            </NScrollbar>
          </div>
        </section>

        <div
          v-if="diffVisible"
          class="script-diff-resizer"
          role="separator"
          aria-orientation="vertical"
          aria-label="调整 Diff 面板宽度"
          :aria-valuenow="Math.round(diffWidth)"
          aria-valuemin="25"
          aria-valuemax="70"
          tabindex="0"
          @pointerdown.prevent="onDiffResizeStart"
        />

        <aside v-if="diffVisible" class="script-preview-panel" :style="{ flex: `0 0 ${diffWidth}%` }">
          <div class="script-panel-heading">
            <div class="script-preview-title">
              <span>预览 diff</span>
              <NTag size="tiny" :bordered="false" type="info">{{ script.modifiedFiles }} 个文件</NTag>
            </div>
            <NButton v-if="script.hasPreview" text size="tiny" type="error" @click="onDiscard">丢弃</NButton>
          </div>
          <NAlert v-if="script.error" type="error" :bordered="false" class="script-error">
            {{ script.error }}
          </NAlert>
          <NAlert v-if="script.stale" type="warning" :bordered="false" class="script-error">
            归档内容已变化，本次预览不能继续应用，请重新运行脚本。
          </NAlert>
          <div class="script-preview-actions" v-if="script.hasPreview">
            <NButton size="small" secondary @click="script.selectAll">全选</NButton>
            <NButton size="small" secondary @click="script.clearSelection">清空</NButton>
            <NButton size="small" type="primary" :disabled="!script.canApply" :loading="script.applying" @click="onApply">
              应用选中 ({{ script.selectedCount }})
            </NButton>
          </div>
          <NScrollbar class="script-preview-scroll">
            <NEmpty v-if="script.rows.length === 0" description="运行脚本后显示文件 diff" size="small" />
            <template v-else>
              <div v-for="row in script.rows" :key="row.fileIndex" class="script-preview-row">
                <label class="script-preview-file">
                  <input
                    type="checkbox"
                    :checked="isSelected(row.fileIndex)"
                    :disabled="row.status !== 'changed' || script.stale"
                    @change="script.toggleSelected(row.fileIndex)"
                  />
                  <span class="script-preview-path" :title="row.path">{{ row.path }}</span>
                  <NTag size="tiny" :bordered="false" :type="row.status === 'changed' ? 'success' : 'default'">
                    {{ row.status === "changed" ? "changed" : row.status }}
                  </NTag>
                </label>
                <div v-if="row.reason" class="script-preview-reason">{{ row.reason }}</div>
                <div v-if="row.diff?.length" class="script-diff">
                  <div v-for="(line, index) in row.diff" :key="index" class="script-diff-line" :class="`script-diff-line--${line?.kind ?? 'context'}`">
                    <span class="script-diff-line-number">{{ diffLineNumber(line, "old") }}</span>
                    <span class="script-diff-line-number">{{ diffLineNumber(line, "new") }}</span>
                    <span class="script-diff-marker">{{ diffPrefix(line) }}</span>
                    <span class="script-diff-text">{{ line?.text }}</span>
                  </div>
                </div>
                <NText v-if="row.diffTruncated" depth="3" class="script-diff-truncated">diff 已截断</NText>
              </div>
            </template>
          </NScrollbar>
          <NButton v-if="script.nextCursor >= 0" block secondary size="small" class="script-load-more" @click="script.loadMore">
            加载更多预览
          </NButton>
        </aside>
      </div>
    </template>

    <section v-else class="script-library-page">
      <div class="script-library-toolbar">
        <NButton quaternary size="small" @click="hideScriptLibrary" :disabled="script.loadingScript">
          <template #icon><NIcon><ChevronLeft24Regular /></NIcon></template>
          返回编辑器
        </NButton>
        <div class="script-library-title">
          <NIcon :size="18"><DocumentText24Regular /></NIcon>
          <div class="script-library-title-copy">
            <span>加载已有脚本</span>
            <NText v-if="script.directory" depth="3" :title="script.directory">{{ script.directory }}</NText>
          </div>
        </div>
        <div class="script-library-actions">
          <NButton size="small" secondary :loading="script.loadingFiles" @click="script.refreshFiles">
            <template #icon><NIcon><ArrowSync24Regular /></NIcon></template>
            刷新
          </NButton>
          <NButton size="small" secondary @click="onOpenDirectory">
            <template #icon><NIcon><FolderOpen24Regular /></NIcon></template>
            打开目录
          </NButton>
        </div>
      </div>
      <NAlert v-if="script.error" type="error" :bordered="false" class="script-library-error">
        {{ script.error }}
      </NAlert>
      <NScrollbar class="script-library-scroll">
        <div class="script-library-content">
          <NSpin v-if="script.loadingFiles" size="small" class="script-list-loading" />
          <NEmpty v-else-if="script.files.length === 0" description="暂无 .pvf.js 脚本" size="small" />
          <div v-else class="script-file-grid">
            <button
              v-for="file in script.files"
              :key="file.name"
              type="button"
              class="script-file-item"
              :class="{ 'script-file-item--active': file.name === script.currentName }"
              :disabled="script.loadingScript"
              @click="onLoad(file.name)"
            >
              <NIcon :size="18"><DocumentText24Regular /></NIcon>
              <span class="script-file-copy">
                <span class="script-file-name">{{ file.name }}</span>
                <span class="script-file-size">{{ formatSize(file.size) }}</span>
              </span>
              <NTag v-if="file.name === script.currentName" size="tiny" type="info" :bordered="false">当前</NTag>
            </button>
          </div>
        </div>
      </NScrollbar>
    </section>
  </div>
</template>

<style scoped>
.script-workbench {
  display: flex;
  flex-direction: column;
  width: 100%;
  height: 100%;
  min-width: 0;
  min-height: 0;
  background: var(--pvf-window-background-solid);
}
.script-toolbar {
  display: flex;
  align-items: center;
  gap: 6px;
  min-height: 44px;
  padding: 5px 10px;
  border-bottom: 1px solid var(--pvf-border-normal);
  flex-shrink: 0;
}
.script-toolbar-title {
  display: flex;
  align-items: center;
  gap: 7px;
  margin-right: 8px;
  color: var(--pvf-text-primary);
  font-weight: 600;
  white-space: nowrap;
}
.script-name-input {
  width: 220px;
}
.script-main {
  display: flex;
  flex: 1;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
}
.script-preview-panel {
  display: flex;
  flex-direction: column;
  flex: 0 0 38%;
  min-width: 0;
  min-height: 0;
  box-sizing: border-box;
  padding: 10px;
  background: var(--pvf-surface-subtle);
}
.script-diff-resizer {
  flex: 0 0 5px;
  z-index: 5;
  cursor: col-resize;
  background: var(--pvf-surface-inset);
}
.script-diff-resizer:hover,
.script-diff-resizer:focus-visible {
  background: var(--pvf-effect-split-hover);
  outline: none;
}
.script-preview-panel {
  min-width: 300px;
  border-left: 1px solid var(--pvf-border-normal);
}
.script-panel-heading,
.script-editor-heading,
.script-preview-title {
  display: flex;
  align-items: center;
}
.script-panel-heading {
  justify-content: space-between;
  min-height: 28px;
  color: var(--pvf-text-primary);
  font-weight: 600;
}
.script-preview-title {
  gap: 7px;
}
.script-preview-scroll {
  flex: 1;
  min-height: 0;
}
.script-list-loading {
  display: block;
  margin: 24px auto;
}
.script-library-page {
  display: flex;
  flex: 1;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
}
.script-library-toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  min-height: 52px;
  padding: 7px 12px;
  border-bottom: 1px solid var(--pvf-border-normal);
  flex-shrink: 0;
}
.script-library-title {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  color: var(--pvf-text-primary);
}
.script-library-title-copy {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 2px;
  font-weight: 600;
}
.script-library-title-copy .n-text {
  max-width: 42vw;
  overflow: hidden;
  color: var(--pvf-text-faint);
  font-size: 10px;
  font-weight: 400;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.script-library-actions {
  display: flex;
  gap: 6px;
  margin-left: auto;
}
.script-library-error {
  margin: 8px 16px 0;
  font-size: 11px;
}
.script-library-scroll {
  flex: 1;
  min-height: 0;
}
.script-library-content {
  box-sizing: border-box;
  width: min(920px, 100%);
  margin: 0 auto;
  padding: 24px;
}
.script-file-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
  gap: 9px;
}
.script-file-item {
  display: flex;
  align-items: center;
  gap: 9px;
  min-width: 0;
  padding: 12px 13px;
  color: var(--pvf-text-secondary);
  text-align: left;
  cursor: pointer;
  background: var(--pvf-surface-card);
  border: 1px solid var(--pvf-border-subtle);
  border-radius: 7px;
  transition: border-color 120ms ease, background 120ms ease;
}
.script-file-item:hover,
.script-file-item--active {
  color: var(--pvf-text-primary);
  background: var(--pvf-surface-hover);
  border-color: var(--pvf-primary);
}
.script-file-item:disabled {
  cursor: progress;
  opacity: 0.65;
}
.script-file-copy {
  display: flex;
  flex: 1;
  min-width: 0;
  flex-direction: column;
  gap: 3px;
}
.script-file-name {
  overflow: hidden;
  color: inherit;
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.script-file-size {
  color: var(--pvf-text-faint);
  font-size: 10px;
}
.script-editor-panel {
  display: flex;
  flex: 1 1 0;
  flex-direction: column;
  min-width: 240px;
  min-height: 0;
  overflow: hidden;
}
.script-editor-heading {
  gap: 7px;
  min-height: 34px;
  padding: 0 12px;
  flex-shrink: 0;
  color: var(--pvf-text-secondary);
  border-bottom: 1px solid var(--pvf-border-subtle);
}
.script-editor-heading > span {
  max-width: 260px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.script-run-summary {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-align: right;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.script-editor-heading-actions {
  display: flex;
  flex: 0 0 auto;
  gap: 2px;
  margin-left: auto;
}
.script-layout-button {
  white-space: nowrap;
}
.script-editor-host {
  display: flex;
  flex: 1 1 0;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
}
.script-console-resizer {
  flex: 0 0 5px;
  z-index: 5;
  cursor: row-resize;
  background: var(--pvf-surface-inset);
}
.script-console-resizer:hover,
.script-console-resizer:focus-visible {
  background: var(--pvf-effect-split-hover);
  outline: none;
}
.script-bottom-panel {
  display: flex;
  flex: 0 0 auto;
  flex-direction: column;
  box-sizing: border-box;
  min-height: 120px;
  padding: 6px 10px;
  overflow: hidden;
  border-top: 1px solid var(--pvf-border-normal);
}
.script-progress-row {
  display: grid;
  grid-template-columns: minmax(80px, 1fr) auto minmax(80px, 1fr);
  align-items: center;
  gap: 8px;
  margin-bottom: 5px;
  color: var(--pvf-text-muted);
  font-size: 11px;
}
.script-progress-message {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.script-console-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  min-height: 26px;
  flex-shrink: 0;
  border-bottom: 1px solid var(--pvf-border-subtle);
}
.script-console-tabs {
  display: flex;
  align-items: stretch;
  gap: 2px;
  height: 26px;
}
.script-console-tab {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 0 8px;
  color: var(--pvf-text-muted);
  font: inherit;
  font-size: 11px;
  cursor: pointer;
  background: transparent;
  border: 0;
  border-bottom: 2px solid transparent;
}
.script-console-tab:hover {
  color: var(--pvf-text-primary);
  background: var(--pvf-surface-hover);
}
.script-console-tab--active {
  color: var(--pvf-primary);
  font-weight: 600;
  border-bottom-color: var(--pvf-primary);
}
.script-clear-logs {
  margin-right: -4px;
}
.script-console-scroll {
  flex: 1 1 0;
  min-height: 0;
}
.script-diagnostic {
  display: flex;
  width: 100%;
  align-items: baseline;
  gap: 6px;
  padding: 2px 0;
  color: var(--pvf-error);
  font: inherit;
  font-size: 11px;
  text-align: left;
  background: transparent;
  border: 0;
}
.script-diagnostic--clickable {
  cursor: pointer;
}
.script-diagnostic--clickable:hover {
  background: var(--pvf-surface-hover);
}
.script-diagnostic:disabled {
  cursor: default;
  opacity: 0.8;
}
.script-diagnostic-message {
  min-width: 0;
  flex: 1;
  overflow-wrap: anywhere;
}
.script-log-line {
  display: flex;
  gap: 8px;
  padding: 2px 0;
  color: var(--pvf-text-secondary);
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 11px;
  white-space: pre-wrap;
  word-break: break-word;
}
.script-log-level {
  flex: 0 0 38px;
  color: var(--pvf-text-faint);
}
.script-log-line--warn {
  color: var(--pvf-warning);
}
.script-log-line--error {
  color: var(--pvf-error);
}
.script-log-empty {
  padding: 16px 0;
  color: var(--pvf-text-faint);
  font-size: 11px;
  text-align: center;
}
.script-error {
  margin: 5px 0;
  font-size: 11px;
}
.script-preview-actions {
  display: flex;
  gap: 5px;
  margin: 5px 0 7px;
}
.script-preview-actions .n-button:last-child {
  margin-left: auto;
}
.script-preview-row {
  margin-bottom: 9px;
  overflow: hidden;
  background: var(--pvf-surface-card);
  border: 1px solid var(--pvf-border-subtle);
  border-radius: 5px;
}
.script-preview-file {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 7px 8px;
  cursor: pointer;
}
.script-preview-path {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  color: var(--pvf-text-primary);
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 11px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.script-preview-reason,
.script-diff-truncated {
  padding: 0 8px 5px;
  font-size: 10px;
}
.script-diff {
  max-height: 260px;
  overflow: auto;
  padding: 6px 0;
  background: var(--pvf-surface-code);
  border-top: 1px solid var(--pvf-border-subtle);
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
  line-height: 1.55;
  white-space: pre;
}
.script-diff-line {
  display: grid;
  grid-template-columns: 42px 42px 18px minmax(0, 1fr);
  min-height: 19px;
  padding: 0 6px;
}
.script-diff-line--remove {
  color: var(--pvf-error-hover);
  background: var(--pvf-surface-error);
}
.script-diff-line--add {
  color: var(--pvf-success-hover);
  background: var(--pvf-surface-success);
}
.script-diff-line-number {
  padding-right: 8px;
  color: var(--pvf-text-faint);
  text-align: right;
  user-select: none;
}
.script-diff-marker {
  color: var(--pvf-text-faint);
  text-align: center;
  user-select: none;
}
.script-diff-text {
  min-width: 0;
}
.script-load-more {
  margin-top: 7px;
  flex-shrink: 0;
}
</style>
