<script setup lang="ts">
import { computed, h, ref, watch, type VNodeChild } from "vue";
import {
  NAlert,
  NButton,
  NModal,
  NRadioButton,
  NRadioGroup,
  NSpin,
  NTag,
  NText,
  NTree,
  type TreeOption,
  useMessage,
} from "naive-ui";
import { DocumentAdd24Regular, DocumentEdit24Regular } from "@vicons/fluent";
import { ArchiveService } from "../../bindings/pvfine/services";
import type { ImportPreviewEntry } from "../../bindings/pvfine/services/models";
import { useArchiveStore } from "../stores/archive";
import { useEditorStore } from "../stores/editor";
import { useExplorerStore } from "../stores/explorer";
import { useImportStore } from "../stores/import";

const importer = useImportStore();
const archive = useArchiveStore();
const editor = useEditorStore();
const explorer = useExplorerStore();
const message = useMessage();

const previewEntries = computed<ImportPreviewEntry[]>(() =>
  (importer.preview?.entries ?? []).filter(
    (entry): entry is ImportPreviewEntry => !!entry
  )
);

type ImportTreeNode = TreeOption & {
  children?: ImportTreeNode[];
  entry?: ImportPreviewEntry;
};

const treeData = computed<ImportTreeNode[]>(() => buildImportTree(previewEntries.value));
const expandedTreeKeys = ref<Array<string | number>>([]);

watch(
  treeData,
  (nodes) => {
    expandedTreeKeys.value = collectDirectoryKeys(nodes);
  },
  { immediate: true }
);

function modeLabel(mode: string): string {
  return mode === "raw" ? "原始字节" : "文本导入";
}

function dataTypeLabel(dataType: number): string {
  return dataType === 3 ? "文本" : "脚本";
}

function sizeLabel(size: number): string {
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
  return `${(size / 1024 / 1024).toFixed(1)} MB`;
}

function buildImportTree(entries: ImportPreviewEntry[]): ImportTreeNode[] {
  const roots: ImportTreeNode[] = [];
  const directories = new Map<string, ImportTreeNode>();

  for (const entry of entries) {
    const parts = entry.targetPath.split("/").filter(Boolean);
    if (parts.length === 0) continue;
    let children = roots;
    let parentPath = "";
    for (const part of parts.slice(0, -1)) {
      const path = parentPath ? `${parentPath}/${part}` : part;
      let directory = directories.get(path);
      if (!directory) {
        directory = { key: path, label: part, isLeaf: false, children: [] };
        directories.set(path, directory);
        children.push(directory);
      }
      children = directory.children ?? [];
      parentPath = path;
    }
    children.push({
      key: entry.targetPath,
      label: parts[parts.length - 1],
      isLeaf: true,
      entry,
    });
  }

  sortImportTree(roots);
  return roots;
}

function sortImportTree(nodes: ImportTreeNode[]): void {
  nodes.sort((left, right) => {
    if (left.isLeaf !== right.isLeaf) return left.isLeaf ? 1 : -1;
    return String(left.label).localeCompare(String(right.label));
  });
  for (const node of nodes) {
    if (node.children) sortImportTree(node.children);
  }
}

function collectDirectoryKeys(nodes: ImportTreeNode[]): Array<string | number> {
  const keys: Array<string | number> = [];
  for (const node of nodes) {
    if (node.children?.length) {
      if (node.key !== undefined) keys.push(node.key);
      keys.push(...collectDirectoryKeys(node.children));
    }
  }
  return keys;
}

function onExpandedTreeKeys(keys: Array<string | number>): void {
  expandedTreeKeys.value = keys;
}

function renderTreeLabel({ option }: { option: TreeOption }): VNodeChild {
  const node = option as ImportTreeNode;
  if (!node.entry) return h("span", { class: "import-tree-directory" }, node.label ?? "");
  const entry = node.entry;
  const status = entry.overwrite ? "覆盖" : "新增";
  const icon = entry.overwrite ? DocumentEdit24Regular : DocumentAdd24Regular;
  const color = entry.overwrite ? "var(--pvf-warning)" : "var(--pvf-success)";
  const title = `${status}\n来源：${entry.sourcePath}\n目标：${entry.targetPath}\n类型：${dataTypeLabel(entry.dataType)}\n大小：${sizeLabel(entry.size)}`;
  return h("span", {
    class: ["import-tree-file", entry.overwrite ? "import-tree-file--overwrite" : "import-tree-file--new"],
    title,
    style: {
      color,
      display: "inline-flex",
      flexDirection: "row",
      flexWrap: "nowrap",
      alignItems: "center",
      gap: "8px",
      width: "max-content",
      height: "20px",
      lineHeight: "20px",
      whiteSpace: "nowrap",
      verticalAlign: "middle",
    },
  }, [
    h("span", { class: "import-tree-icon", style: {
      color,
      display: "inline-flex",
      alignItems: "center",
      justifyContent: "center",
      flex: "0 0 16px",
      width: "16px",
      height: "20px",
      lineHeight: "0",
    } }, [
      h(icon, { width: 16, height: 16, style: { display: "block" } }),
    ]),
    h("span", { class: "import-tree-file-name", style: {
      color,
      display: "inline-block",
      flex: "0 0 auto",
      height: "20px",
      lineHeight: "20px",
      whiteSpace: "nowrap",
    } }, node.label ?? ""),
  ]);
}

function closeAfterCancel(): void {
  importer.visible = false;
  importer.sourcePaths = [];
  importer.preview = null;
  importer.error = "";
}

async function chooseAndPreview(): Promise<void> {
  if (importer.running || !archive.open) return;
  importer.running = true;
  importer.error = "";
  try {
    const paths = (await ArchiveService.SelectImportFilesDialog()) ?? [];
    if (paths.length === 0) {
      closeAfterCancel();
      return;
    }
    const preview = await ArchiveService.PreviewImport(
      paths,
      importer.targetDir,
      importer.mode
    );
    if (!preview) throw new Error("后端未返回导入预览");
    importer.sourcePaths = paths;
    importer.preview = preview;
  } catch (error: any) {
    const text = String(error?.message ?? error);
    if (text.toLowerCase().includes("cancel")) {
      closeAfterCancel();
    } else {
      importer.error = text;
      message.error(`生成导入预览失败: ${text}`);
    }
  } finally {
    importer.running = false;
  }
}

function backToOptions(): void {
  if (importer.running) return;
  importer.sourcePaths = [];
  importer.preview = null;
  importer.error = "";
}

async function confirmImport(): Promise<void> {
  const preview = importer.preview;
  if (importer.running || !preview || importer.sourcePaths.length === 0 || !archive.open) return;
  importer.running = true;
  importer.error = "";
  try {
    await editor.flushPending();
    const result = await ArchiveService.ImportFiles(
      importer.sourcePaths,
      importer.targetDir,
      importer.mode
    );
    if (!result) throw new Error("后端未返回导入结果");

    await editor.refreshAfterArchiveChange(result.changedPaths ?? []);
    await Promise.all([archive.refreshInfo(), explorer.reload()]);
    const parts = [`导入 ${result.importedCount.toLocaleString()} 个新文件`];
    if (result.overwrittenCount > 0) {
      parts.push(`覆盖 ${result.overwrittenCount.toLocaleString()} 个文件`);
    }
    closeAfterCancel();
    message.success(parts.join("，"));
  } catch (error: any) {
    const text = String(error?.message ?? error);
    if (text.toLowerCase().includes("cancel")) {
      closeAfterCancel();
    } else {
      importer.error = text;
      message.error(`导入失败: ${text}`);
    }
  } finally {
    importer.running = false;
  }
}

function close(): void {
  if (importer.running) return;
  closeAfterCancel();
}
</script>

<template>
  <NModal
    :show="importer.visible"
    preset="card"
    title="导入文件"
    :mask-closable="!importer.running"
    :close-on-esc="!importer.running"
    :style="{ width: 'min(760px, calc(100vw - 40px))' }"
    @update:show="(show) => !show && close()"
  >
    <NSpin :show="importer.running">
      <div class="import-modal">
        <div class="import-target">
          <NText depth="3">目标目录</NText>
          <NTag size="small" :bordered="false">
            {{ importer.targetDir ? `${importer.targetDir}/` : "归档根目录/" }}
          </NTag>
        </div>

        <template v-if="!importer.preview">
          <div class="import-field">
            <NText depth="3">读取方式</NText>
            <NRadioGroup v-model:value="importer.mode" size="small">
              <NRadioButton value="text">文本导入（默认）</NRadioButton>
              <NRadioButton value="raw">原始字节</NRadioButton>
            </NRadioGroup>
          </div>

          <NText depth="3" class="import-hint">
            可在系统文件选择器中多选文件或目录；目录会递归导入并保留目录名。<br />
            预览确认后才会修改内存归档；冲突文件将覆盖内存内容。
          </NText>
        </template>

        <template v-else>
          <div class="import-preview-heading">
            <NText>请确认以下导入内容</NText>
            <NTag size="small" :bordered="false">{{ modeLabel(importer.preview.mode) }}</NTag>
          </div>
          <div class="import-summary">
            <NTag type="info" :bordered="false">
              共 {{ importer.preview.totalFiles.toLocaleString() }} 个文件
            </NTag>
            <NTag type="success" :bordered="false">
              新增 {{ importer.preview.importedCount.toLocaleString() }}
            </NTag>
            <NTag v-if="importer.preview.overwrittenCount" type="warning" :bordered="false">
              覆盖 {{ importer.preview.overwrittenCount.toLocaleString() }}
            </NTag>
          </div>
          <NTree
            block-line
            :data="treeData"
            :expanded-keys="expandedTreeKeys"
            :on-update:expanded-keys="onExpandedTreeKeys"
            :render-label="renderTreeLabel"
            :animated="false"
            virtual-scroll
            class="import-preview-tree"
          />
          <NText v-if="importer.preview.entriesTruncated" depth="3">
            文件较多，仅显示前 500 项；上方统计包含全部文件。
          </NText>
        </template>

        <NAlert v-if="importer.error" type="error" :show-icon="false">
          {{ importer.error }}
        </NAlert>
      </div>
    </NSpin>

    <template #footer>
      <div class="import-modal-footer">
        <template v-if="!importer.preview">
          <NButton quaternary :disabled="importer.running" @click="close">取消</NButton>
          <NButton type="primary" :loading="importer.running" @click="chooseAndPreview">
            选择文件并预览
          </NButton>
        </template>
        <template v-else>
          <NButton quaternary :disabled="importer.running" @click="backToOptions">返回修改</NButton>
          <NButton quaternary :disabled="importer.running" @click="close">取消</NButton>
          <NButton type="primary" :loading="importer.running" @click="confirmImport">
            确认导入
          </NButton>
        </template>
      </div>
    </template>
  </NModal>
</template>

<style scoped>
.import-modal {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.import-target,
.import-field,
.import-preview-heading,
.import-summary {
  display: flex;
  align-items: center;
  gap: 10px;
}
.import-field {
  flex-direction: column;
  align-items: flex-start;
  gap: 8px;
}
.import-hint {
  line-height: 1.7;
}
.import-preview-heading {
  justify-content: space-between;
}
.import-summary {
  flex-wrap: wrap;
}
.import-preview-tree {
  width: 100%;
  min-height: 180px;
  max-height: min(48vh, 480px);
  overflow: auto;
  padding: 4px;
  border: 1px solid var(--pvf-border-normal);
  border-radius: 4px;
}
.import-tree-file {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  height: 20px;
  line-height: 20px;
  vertical-align: middle;
  min-width: 0;
  width: max-content;
  max-width: none;
}
.import-tree-file--new {
  color: var(--pvf-success);
}
.import-tree-file--overwrite {
  color: var(--pvf-warning);
}
.import-tree-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 16px;
  height: 20px;
  flex-shrink: 0;
  line-height: 0;
}
.import-tree-file-name {
  display: inline-block;
  height: 20px;
  flex-shrink: 0;
  line-height: 20px;
}
:deep(.n-tree-node) {
  width: max-content;
  min-width: 100%;
}
:deep(.n-tree-node-content) {
  width: max-content;
  min-width: 100%;
}
:deep(.n-tree-node-content__text) {
  flex: 0 0 auto;
  width: max-content;
  max-width: none;
}
.import-modal-footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>
