<script setup lang="ts">
import { computed, h, ref, watch, type VNodeChild } from "vue";
import {
  NAlert,
  NButton,
  NCheckbox,
  NCheckboxGroup,
  NFormItem,
  NSelect,
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
import { ArchiveService, ModImportService } from "../../bindings/pvfine/services";
import type { Descriptor } from "../../bindings/pvfine/internal/mod/models";
import type { ImportPreviewEntry, ModImportPreview } from "../../bindings/pvfine/services/models";
import { useArchiveStore } from "../stores/archive";
import { useEditorStore } from "../stores/editor";
import { useExplorerStore } from "../stores/explorer";
import { useImportStore } from "../stores/import";

const importer = useImportStore();
const archive = useArchiveStore();
const editor = useEditorStore();
const explorer = useExplorerStore();
const message = useMessage();
const importKind = ref<"files" | "mod">("files");
const modPreview = ref<ModImportPreview | null>(null);
const modFormats = ref<Descriptor[]>([]);
const modFormat = ref("");
const loadingModFormats = ref(false);
const modFormatOptions = computed(() => modFormats.value.map((format) => ({ label: format.name, value: format.id })));
const modExpandedKeys = ref<Array<string | number>>([]);
const selectedComponents = ref<string[]>([]);
const hasPreview = computed(() => !!importer.preview || !!modPreview.value);
const operationLabel: Record<string, string> = { add: "仅新增", replace: "整文件替换", "merge-list": "按编号合并", "merge-strings": "按文字键合并" };
const modTree = computed<TreeOption[]>(() => (modPreview.value?.files ?? []).map((file) => ({
  key: file.path,
  label: `${file.path} · ${operationLabel[file.operation] ?? file.operation} · ${file.overwrite ? "已有文件" : "新增文件"}`,
  children: file.pairs?.length ? file.pairs.map((pair) => ({
    key: `${file.path}:${pair.key}`,
    label: `${pair.overwrite ? "覆盖" : "新增"} ${pair.key} → ${pair.value === "" ? "（空值）" : pair.value}`,
    prefix: () => h("span", { style: { color: pair.overwrite ? "var(--pvf-warning)" : "var(--pvf-success)" } }, pair.overwrite ? "↻" : "+"),
    // Values may be long; expose the full before/after content on hover.
    suffix: () => h("span", { title: pair.overwrite ? `原值：${pair.previous}\n新值：${pair.value}` : `新值：${pair.value}` }, "ⓘ"),
  })) : undefined,
})));

async function releaseModPreview(): Promise<void> {
  const id = modPreview.value?.id;
  modPreview.value = null;
  if (id) await ModImportService.Release(id);
}

watch(() => importer.visible, async (visible, _previous, onCleanup) => {
  let current = true;
  onCleanup(() => { current = false; });
  if (!visible) {
    loadingModFormats.value = false;
    void releaseModPreview();
    return;
  }
  importKind.value = "files";
  modFormat.value = "";
  modFormats.value = [];
  loadingModFormats.value = true;
  try {
    const formats = await ModImportService.Formats();
    if (!current) return;
    modFormats.value = (formats ?? []).filter((format) => format.canRead);
    modFormat.value = modFormats.value[0]?.id ?? "";
  } catch (error: any) {
    if (current) importer.error = String(error?.message ?? error);
  } finally {
    if (current) loadingModFormats.value = false;
  }
});

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
  const encodingLabel = entry.sourceEncoding && entry.sourceEncoding !== "UTF-8" ? `${entry.sourceEncoding} → UTF-8` : "";
  const title = `${encodingLabel ? `自动识别：${encodingLabel}\n` : ""}${status}\n来源：${entry.sourcePath}\n目标：${entry.targetPath}\n类型：${dataTypeLabel(entry.dataType)}\n大小：${sizeLabel(entry.size)}`;
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
    encodingLabel ? h(NTag, { size: "small", type: "info", bordered: false }, { default: () => encodingLabel }) : null,
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
  if (importKind.value === "mod" && (loadingModFormats.value || !modFormat.value)) return;
  importer.running = true;
  importer.error = "";
  try {
    if (importKind.value === "mod") {
      await releaseModPreview();
      modPreview.value = await ModImportService.PrepareDialog(modFormat.value);
      selectedComponents.value = modPreview.value?.selectedComponents ?? [];
      modExpandedKeys.value = [];
      return;
    }
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

async function changeComponents(values: Array<string | number>): Promise<void> {
  if (importer.running || !modPreview.value) return;
  selectedComponents.value = values.map(String);
  importer.running = true;
  importer.error = "";
  try {
    const next = await ModImportService.SelectComponents(modPreview.value.id, selectedComponents.value);
    if (!next) throw new Error("后端未返回组件预览");
    modPreview.value = next;
    selectedComponents.value = next.selectedComponents ?? [];
  } catch (error: any) {
    importer.error = String(error?.message ?? error);
  } finally {
    importer.running = false;
  }
}

function backToOptions(): void {
  if (importer.running) return;
  void releaseModPreview();
  importer.sourcePaths = [];
  importer.preview = null;
  importer.error = "";
}

async function confirmImport(): Promise<void> {
  const preview = importer.preview;
  if (importer.running || !hasPreview.value || !archive.open) return;
  if (importKind.value === "files" && (!preview || importer.sourcePaths.length === 0)) return;
  importer.running = true;
  importer.error = "";
  try {
    const result = importKind.value === "mod" && modPreview.value
      ? await ModImportService.Execute(modPreview.value.id)
      : await ArchiveService.ImportFiles(importer.sourcePaths, importer.targetDir, importer.mode);
    if (!result) throw new Error("后端未返回导入结果");

    await editor.refreshAfterArchiveChange(result.changedPaths ?? []);
    await Promise.all([archive.refreshInfo(), explorer.reload()]);
    const parts = [`导入 ${result.importedCount.toLocaleString()} 个新文件`];
    if (result.overwrittenCount > 0) {
      parts.push(`覆盖 ${result.overwrittenCount.toLocaleString()} 个文件`);
    }
    closeAfterCancel();
    message.success(importKind.value === "mod" ? `Mod 已导入内存：${parts.join("，")}，请保存 PVF` : parts.join("，"));
  } catch (error: any) {
    const text = String(error?.message ?? error);
    if (text.toLowerCase().includes("cancel")) {
      closeAfterCancel();
    } else {
      importer.error = text;
      message.error(`导入失败: ${text}`);
      if (importKind.value === "mod") await releaseModPreview();
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
    title="导入"
    :closable="!importer.running"
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
            {{ importKind === "mod" || !importer.targetDir ? "归档根目录/" : `${importer.targetDir}/` }}
          </NTag>
        </div>

        <template v-if="!hasPreview">
          <div class="import-field">
            <NText depth="3">导入内容</NText>
            <NRadioGroup v-model:value="importKind" :disabled="importer.running" size="small">
              <NRadioButton value="files">文件（默认）</NRadioButton>
              <NRadioButton value="mod">Mod</NRadioButton>
            </NRadioGroup>
          </div>
          <template v-if="importKind === 'files'">
            <div class="import-field">
              <NText depth="3">读取方式</NText>
              <NRadioGroup v-model:value="importer.mode" :disabled="importer.running" size="small">
                <NRadioButton value="text">文本导入（默认）</NRadioButton>
                <NRadioButton value="raw">原始字节</NRadioButton>
              </NRadioGroup>
            </div>

            <NText depth="3" class="import-hint">
              可在系统文件选择器中多选文件或目录；目录会递归导入并保留目录名。<br />
              预览确认后才会修改内存归档；冲突文件将覆盖内存内容。<br />
              <template v-if="importer.mode === 'text'">自动识别文本编码，非 UTF-8 文件将转为 UTF-8 导入，源文件保持不变。</template>
            </NText>
          </template>
          <template v-else>
            <NFormItem label="导入格式" :show-feedback="false">
              <NSelect v-model:value="modFormat" :options="modFormatOptions" :loading="loadingModFormats"
                :disabled="importer.running || loadingModFormats || !modFormatOptions.length"
                :placeholder="loadingModFormats ? '正在加载格式' : '暂无可导入的格式'" />
            </NFormItem>
            <NText depth="3" class="import-hint">
              按所选格式读取 mod 目录或 115CN ZIP / .dnfmod，按包内路径导入归档根目录。<br />
              确认后写入当前内存，再手动保存 PVF。
            </NText>
          </template>
        </template>

        <template v-else-if="modPreview">
          <div class="import-summary">
            <NText strong>{{ modPreview.name }} {{ modPreview.version }}</NText>
            <NTag :bordered="false">{{ modPreview.format }}</NTag>
            <NTag type="success" :bordered="false">新增 {{ modPreview.importedCount }} 个文件</NTag>
            <NTag type="warning" :bordered="false">更新 {{ modPreview.overwrittenCount }} 个文件</NTag>
          </div>
          <NCheckboxGroup v-if="modPreview.components?.length" :value="selectedComponents" :disabled="importer.running"
            @update:value="changeComponents">
            <div class="mod-components">
              <NCheckbox v-for="component in modPreview.components" :key="component.id" :value="component.id"
                :title="[component.description, component.requires?.length ? `依赖：${component.requires.join('、')}` : '', component.exclusiveGroup ? `互斥组：${component.exclusiveGroup}` : ''].filter(Boolean).join('\n')">
                {{ component.title }}
              </NCheckbox>
            </div>
          </NCheckboxGroup>
          <NAlert v-for="warning in modPreview.warnings ?? []" :key="warning" type="warning" :show-icon="false">{{ warning }}</NAlert>
          <NAlert type="warning" :show-icon="false">整文件替换会覆盖当前内存内容；合并表同键覆盖，保留无关条目。展开文件可查看合并条目，悬停 ⓘ 查看原值和新值。</NAlert>
          <NTree v-model:expanded-keys="modExpandedKeys" :data="modTree" block-line virtual-scroll :animated="false" class="import-preview-tree" />
        </template>

        <template v-else-if="importer.preview">
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
          <NAlert v-if="importer.preview.convertedCount" type="info" :show-icon="false">
            自动识别到 {{ importer.preview.convertedCount.toLocaleString() }} 个非 UTF-8 文件，将转为 UTF-8 导入，源文件保持不变。文件旁显示识别编码；自动识别可能存在偏差，请确认后导入。
          </NAlert>
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
        <template v-if="!hasPreview">
          <NButton quaternary :disabled="importer.running" @click="close">取消</NButton>
          <NButton type="primary" :loading="importer.running"
            :disabled="!archive.open || (importKind === 'mod' && (loadingModFormats || !modFormat))" @click="chooseAndPreview">
            {{ importKind === "mod" ? "选择 MOD 并预览" : "选择文件并预览" }}
          </NButton>
        </template>
        <template v-else>
          <NButton quaternary :disabled="importer.running" @click="backToOptions">返回修改</NButton>
          <NButton quaternary :disabled="importer.running" @click="close">取消</NButton>
          <NButton type="primary" :loading="importer.running"
            :disabled="importKind === 'mod' && (!!importer.error || !modPreview?.files?.length)" @click="confirmImport">
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
.mod-components {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
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
