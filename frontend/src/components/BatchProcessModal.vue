<script setup lang="ts">
import { ref } from "vue";
import {
  NAlert,
  NButton,
  NCheckbox,
  NIcon,
  NInput,
  NInputNumber,
  NModal,
  NPopover,
  NRadioButton,
  NRadioGroup,
  NSelect,
  NSpin,
  NSwitch,
  NTag,
  NText,
  useMessage,
} from "naive-ui";
import {
  Add24Regular,
  ArrowCollapseAll20Regular,
  ArrowExpand20Regular,
  ChevronRight16Regular,
  Delete24Regular,
  Lightbulb20Regular,
  QuestionCircle20Regular,
} from "@vicons/fluent";
import type { BatchDiffLine, BatchFilePreview } from "../../bindings/pvfine/services/models";
import { useArchiveStore } from "../stores/archive";
import { useBatchStore, type BatchOperationForm } from "../stores/batch";
import { useEditorStore } from "../stores/editor";
import { useExplorerStore } from "../stores/explorer";

const batch = useBatchStore();
const archive = useArchiveStore();
const editor = useEditorStore();
const explorer = useExplorerStore();
const message = useMessage();
const expandedPaths = ref<Set<string>>(new Set());

interface QuickTemplate {
  label: string;
  desc: string;
  operation: {
    kind: string;
    section: string;
    tokenIndex: number;
    operator?: string;
    operand?: string;
    operandEnd?: string;
    value?: string;
  };
}

const quickTemplates: QuickTemplate[] = [
  {
    label: "全量涨价 10%",
    desc: "将 [price] 基础价格增加 10%",
    operation: { kind: "number", section: "price", tokenIndex: 0, operator: "+%", operand: "10" },
  },
  {
    label: "全量降价 20%",
    desc: "将 [price] 基础价格降低 20%",
    operation: { kind: "number", section: "price", tokenIndex: 0, operator: "-%", operand: "20" },
  },
  {
    label: "金币归一 (1金币)",
    desc: "将 [price] 价格重置为 1",
    operation: { kind: "number", section: "price", tokenIndex: 0, operator: "=", operand: "1" },
  },
  {
    label: "金币免费 (0金币)",
    desc: "将 [price] 价格设为 0",
    operation: { kind: "number", section: "price", tokenIndex: 0, operator: "=", operand: "0" },
  },
  {
    label: "价格钳制 (1~100万)",
    desc: "将 [price] 价格限制在 1 ~ 1,000,000 区间内",
    operation: { kind: "number", section: "price", tokenIndex: 0, operator: "clamp", operand: "1", operandEnd: "1000000" },
  },
  {
    label: "无级别要求 (Lv.1)",
    desc: "将 [minimum level] 装备佩戴等级设为 1",
    operation: { kind: "number", section: "minimum level", tokenIndex: 0, operator: "=", operand: "1" },
  },
  {
    label: "数值四舍五入取整",
    desc: "将 [price] 数值四舍五入保留 0 位小数（取整）",
    operation: { kind: "number", section: "price", tokenIndex: 0, operator: "round", operand: "0" },
  },
];

function expandAll(): void {
  const next = new Set<string>();
  for (const row of batch.rows) {
    next.add(row.path);
  }
  expandedPaths.value = next;
}

function collapseAll(): void {
  expandedPaths.value = new Set();
}

function applyTemplate(tpl: QuickTemplate): void {
  batch.mode = "structured";
  if (batch.operations.length <= 1) {
    if (batch.operations.length === 0) {
      batch.addOperation();
    }
    const op = batch.operations[0];
    op.kind = tpl.operation.kind;
    op.section = tpl.operation.section;
    op.tokenIndex = tpl.operation.tokenIndex;
    op.operator = tpl.operation.operator ?? "+";
    op.operand = tpl.operation.operand ?? "";
    op.operandEnd = tpl.operation.operandEnd ?? "";
    op.value = tpl.operation.value ?? "";
    op.createIfMissing = false;
  } else {
    const lastOp = batch.operations[batch.operations.length - 1];
    if (!lastOp.section.trim() && !lastOp.value && !lastOp.operand) {
      lastOp.kind = tpl.operation.kind;
      lastOp.section = tpl.operation.section;
      lastOp.tokenIndex = tpl.operation.tokenIndex;
      lastOp.operator = tpl.operation.operator ?? "+";
      lastOp.operand = tpl.operation.operand ?? "";
      lastOp.operandEnd = tpl.operation.operandEnd ?? "";
      lastOp.value = tpl.operation.value ?? "";
      lastOp.createIfMissing = false;
    } else {
      batch.addOperation(tpl.operation);
      const newOp = batch.operations[batch.operations.length - 1];
      newOp.kind = tpl.operation.kind;
      newOp.section = tpl.operation.section;
      newOp.tokenIndex = tpl.operation.tokenIndex;
      newOp.operator = tpl.operation.operator ?? "+";
      newOp.operand = tpl.operation.operand ?? "";
      newOp.operandEnd = tpl.operation.operandEnd ?? "";
      newOp.value = tpl.operation.value ?? "";
      newOp.createIfMissing = false;
    }
  }
  message.success(`已应用模板：${tpl.label}`);
}

function getOperandPlaceholder(op: BatchOperationForm): string {
  switch (op.operator) {
    case "+%":
      return "增加百分比（例如 10 表示 +10%）";
    case "-%":
      return "减少百分比（例如 20 表示 -20%）";
    case "clamp":
      return "下限（最小值）";
    case "round":
      return "保留小数位 0~9（例如 0 表示取整）";
    case "min":
      return "上限（保留不超过此值）";
    case "max":
      return "下限（保留不低于此值）";
    case "=":
      return "目标数值（例如 1）";
    case "+":
      return "加数（例如 100）";
    case "-":
      return "减数（例如 50）";
    case "×":
      return "乘数（例如 1.5）";
    case "÷":
      return "除数（不能为 0）";
    default:
      return "操作数";
  }
}

function getOperatorExplanation(op: BatchOperationForm): string {
  const num = parseFloat(op.operand || "");
  const hasNum = !isNaN(num);
  switch (op.operator) {
    case "+%": {
      const pct = hasNum ? num : 10;
      const example = Math.round(100 * (1 + pct / 100));
      return `公式：原值 × (1 + ${pct}%)。示例：原数值 100 计算后变为 ${example}`;
    }
    case "-%": {
      const pct = hasNum ? num : 20;
      const example = Math.round(100 * (1 - pct / 100));
      return `公式：原值 × (1 - ${pct}%)。示例：原数值 100 计算后变为 ${example}`;
    }
    case "clamp": {
      const lower = op.operand || "下限";
      const upper = op.operandEnd || "上限";
      return `公式：限制在 [${lower}, ${upper}] 区间内。小于 ${lower} 变为 ${lower}，大于 ${upper} 变为 ${upper}`;
    }
    case "round": {
      const digits = hasNum ? Math.max(0, Math.min(9, Math.trunc(num))) : 0;
      const example = (12.3456).toFixed(digits);
      return `公式：四舍五入保留 ${digits} 位小数。示例：12.3456 计算后变为 ${example}`;
    }
    case "min": {
      const val = op.operand || "上限";
      return `公式：min(原值, ${val})。即设置上限，超过 ${val} 的数值将被截断为 ${val}`;
    }
    case "max": {
      const val = op.operand || "下限";
      return `公式：max(原值, ${val})。即设置下限，低于 ${val} 的数值将被提升为 ${val}`;
    }
    case "×": {
      const val = hasNum ? num : 2;
      return `公式：原值 × ${val}。示例：原数值 50 计算后变为 ${50 * val}`;
    }
    case "÷": {
      const val = hasNum ? num : 2;
      return `公式：原值 ÷ ${val}。示例：原数值 100 计算后变为 ${val !== 0 ? 100 / val : "除数不能为0"}`;
    }
    case "+": {
      const val = hasNum ? num : 100;
      return `公式：原值 + ${val}。示例：原数值 500 计算后变为 ${500 + val}`;
    }
    case "-": {
      const val = hasNum ? num : 100;
      return `公式：原值 - ${val}。示例：原数值 500 计算后变为 ${500 - val}`;
    }
    case "=": {
      const val = op.operand || "新数值";
      return `公式：直接赋值。将原数值替换为 ${val}`;
    }
    default:
      return "数值运算操作";
  }
}

const operationOptions = [
  { label: "设置值", value: "set" },
  { label: "数值运算", value: "number" },
  { label: "删除 section", value: "delete" },
  { label: "插入 section", value: "insert" },
];
const operatorOptions = [
  { label: "= 设置", value: "=" },
  { label: "+ 加", value: "+" },
  { label: "− 减", value: "-" },
  { label: "× 乘", value: "×" },
  { label: "÷ 除", value: "÷" },
  { label: "+% 增加百分比", value: "+%" },
  { label: "−% 减少百分比", value: "-%" },
  { label: "min", value: "min" },
  { label: "max", value: "max" },
  { label: "clamp", value: "clamp" },
  { label: "round", value: "round" },
];

function statusLabel(row: BatchFilePreview): string {
  if (row.status === "changed") return "可应用";
  if (row.status === "error") return "错误";
  return "跳过";
}

function statusType(row: BatchFilePreview): "success" | "warning" | "error" | "default" {
  if (row.status === "changed") return "success";
  if (row.status === "error") return "error";
  return "warning";
}

function warningItems(row: BatchFilePreview): string[] {
  return (row.warnings ?? [])
    .flatMap((warning) => warning.split("；"))
    .map((warning) => warning.trim())
    .filter(Boolean);
}

function warningSummary(row: BatchFilePreview): string {
  const warnings = warningItems(row);
  if (warnings.length === 0) return "";
  return warnings.length > 1 ? `${warnings[0]}…` : warnings[0];
}

function isExpanded(path: string): boolean {
  return expandedPaths.value.has(path);
}

function toggleExpanded(path: string): void {
  const next = new Set(expandedPaths.value);
  if (next.has(path)) next.delete(path);
  else next.add(path);
  expandedPaths.value = next;
}

function updateTokenIndex(operation: BatchOperationForm, value: number | null): void {
  const minimum = operation.kind === "set" ? -1 : 0;
  operation.tokenIndex = Math.max(minimum, Math.trunc(value ?? (operation.kind === "set" ? -1 : 0)));
}

function updateKind(operation: BatchOperationForm, value: string): void {
  operation.kind = value;
  if (value !== "set" && operation.tokenIndex < 0) operation.tokenIndex = 0;
  if (value === "number" && !operation.operator) operation.operator = "+";
}

function insertTab(event: KeyboardEvent): void {
  if (event.key !== "Tab") return;

  const target = event.target;
  if (!(target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement)) return;

  event.preventDefault();
  event.stopPropagation();

  const start = target.selectionStart ?? target.value.length;
  const end = target.selectionEnd ?? start;
  target.setRangeText("\t", start, end, "end");
  target.dispatchEvent(new Event("input", { bubbles: true }));
}

function diffPrefix(line: BatchDiffLine): string {
  if (line.kind === "remove") return "−";
  if (line.kind === "add") return "+";
  return " ";
}

async function preview(): Promise<void> {
  expandedPaths.value = new Set();
  await batch.preview();
  if (batch.error) message.error(batch.error);
}

async function apply(): Promise<void> {
  try {
    await editor.flushPending();
    const result = await batch.apply();
    await editor.refreshBatchFiles(result.fileIndexes ?? []);
    await Promise.all([
      archive.refreshInfo(),
      editor.refreshAnnotations(),
      explorer.refreshAnnotations(),
    ]);
    batch.visible = false;
    message.success(`已应用 ${result.appliedFiles} 个文件，当前有 ${result.modifiedCount} 个未保存修改`);
  } catch (value: any) {
    const error = String(value?.message ?? value);
    if (!error.includes("取消")) message.error(`应用批处理失败: ${error}`);
  }
}

function close(): void {
  batch.close();
}
</script>

<template>
  <NModal
    :show="batch.visible"
    preset="card"
    title="批量处理"
    :mask-closable="!batch.loading && !batch.applying"
    :close-on-esc="!batch.loading && !batch.applying"
    :style="{ width: 'min(1080px, calc(100vw - 40px))' }"
    @update:show="(show) => !show && close()"
  >
    <div class="batch-modal">
      <div class="batch-scope">
        <NText depth="3">范围：{{ batch.scopeLabel || "当前选择" }}</NText>
        <NTag size="small" :bordered="false">{{ batch.scopePaths.length.toLocaleString() }} 个路径</NTag>
      </div>

      <div class="batch-mode-row">
        <NRadioGroup v-model:value="batch.mode" size="small">
          <NRadioButton value="text">查找 / 替换</NRadioButton>
          <NRadioButton value="structured">结构化批处理</NRadioButton>
        </NRadioGroup>
      </div>

      <div v-if="batch.mode === 'text'" class="batch-text-form">
        <div class="batch-field">
          <label>查找</label>
          <NInput
            v-model:value="batch.text.find"
            type="textarea"
            :autosize="{ minRows: 2, maxRows: 5 }"
            placeholder="输入要查找的文本"
            clearable
            @keydown="insertTab"
          />
        </div>
        <div class="batch-field">
          <label>替换为</label>
          <NInput
            v-model:value="batch.text.replacement"
            type="textarea"
            :autosize="{ minRows: 2, maxRows: 5 }"
            placeholder="输入替换文本；正则支持 $1、$name 和 $$"
            @keydown="insertTab"
          />
        </div>
        <div class="batch-switch-row">
          <NSwitch v-model:value="batch.text.regex" />
          <span>使用正则（Go RE2）</span>
        </div>
      </div>

      <div v-else class="batch-structured-form">
        <div class="batch-operation-heading">
          <NText depth="3">操作列表</NText>
          <NButton size="small" secondary @click="() => batch.addOperation()">
            <template #icon><NIcon><Add24Regular /></NIcon></template>
            添加操作
          </NButton>
        </div>

        <div class="batch-template-bar">
          <span class="batch-template-label">
            <NIcon :component="Lightbulb20Regular" class="batch-template-icon" />
            常用模板:
          </span>
          <div class="batch-template-chips">
            <button
              v-for="tpl in quickTemplates"
              :key="tpl.label"
              type="button"
              class="batch-template-chip"
              :title="tpl.desc"
              @click="applyTemplate(tpl)"
            >
              {{ tpl.label }}
            </button>
          </div>
        </div>

        <div class="batch-operations">
          <div v-for="(operation, index) in batch.operations" :key="operation.id" class="batch-operation">
            <div class="batch-operation-topline">
              <NTag size="small" :bordered="false">{{ index + 1 }}</NTag>
              <NSelect
                :value="operation.kind"
                :options="operationOptions"
                size="small"
                style="width: 132px"
                @update:value="(value) => updateKind(operation, value)"
              />
              <NInput
                v-model:value="operation.section"
                size="small"
                placeholder="section，例如 price"
                @keydown="insertTab"
              />
              <NInputNumber
                v-if="operation.kind !== 'delete' && operation.kind !== 'insert'"
                :value="operation.kind === 'set' && operation.tokenIndex < 0 ? null : operation.tokenIndex"
                :min="operation.kind === 'set' ? -1 : 0"
                :precision="0"
                size="small"
                :placeholder="operation.kind === 'set' ? '值下标（-1=全部）' : '值下标'"
                style="width: 100px"
                @update:value="(value) => updateTokenIndex(operation, value)"
              />
              <NButton
                quaternary
                size="small"
                :disabled="index === 0"
                @click="batch.moveOperation(operation.id, -1)"
              >
                上移
              </NButton>
              <NButton
                quaternary
                size="small"
                :disabled="index === batch.operations.length - 1"
                @click="batch.moveOperation(operation.id, 1)"
              >
                下移
              </NButton>
              <NButton
                quaternary
                circle
                size="small"
                aria-label="删除操作"
                :disabled="batch.operations.length <= 1"
                @click="batch.removeOperation(operation.id)"
              >
                <template #icon><NIcon><Delete24Regular /></NIcon></template>
              </NButton>
            </div>

            <div v-if="operation.kind === 'set'" class="batch-operation-fields batch-set-fields">
              <NInput
                v-model:value="operation.value"
                type="textarea"
                :autosize="{ minRows: 2, maxRows: 5 }"
                size="small"
                placeholder="新值，例如 1000、`文本`"
                @keydown="insertTab"
              />
              <div class="batch-switch-row">
                <NSwitch v-model:value="operation.createIfMissing" size="small" />
                <span>section 不存在时插入</span>
              </div>
            </div>
            <div v-else-if="operation.kind === 'number'" class="batch-operation-fields batch-number-fields">
              <div class="batch-number-inputs-row">
                <NSelect
                  v-model:value="operation.operator"
                  :options="operatorOptions"
                  size="small"
                  style="width: 170px"
                />
                <NInput
                  v-model:value="operation.operand"
                  size="small"
                  :placeholder="getOperandPlaceholder(operation)"
                  @keydown="insertTab"
                />
                <NInput
                  v-if="operation.operator === 'clamp'"
                  v-model:value="operation.operandEnd"
                  size="small"
                  placeholder="上限（最大值）"
                  @keydown="insertTab"
                />
                <NPopover trigger="hover" placement="top-end" :width="330">
                  <template #trigger>
                    <button class="batch-help-icon-btn" type="button" aria-label="运算符帮助">
                      <NIcon :component="QuestionCircle20Regular" />
                    </button>
                  </template>
                  <div class="batch-operator-help-popover">
                    <div class="help-popover-title">数值运算帮助</div>
                    <div class="help-popover-item"><b>= 设置</b>：直接修改为目标数值</div>
                    <div class="help-popover-item"><b>+ / − / × / ÷</b>：基础四则运算</div>
                    <div class="help-popover-item"><b>+% 增加百分比</b>：原值 × (1 + 百分比/100)，如 10 增加 10%</div>
                    <div class="help-popover-item"><b>−% 减少百分比</b>：原值 × (1 - 百分比/100)，如 20 减少 20%</div>
                    <div class="help-popover-item"><b>min / max</b>：设置数值上限（不超过该值）/ 下限（不低于该值）</div>
                    <div class="help-popover-item"><b>clamp</b>：将数值限制在 [下限, 上限] 区间内</div>
                    <div class="help-popover-item"><b>round</b>：四舍五入，操作数填保留小数位数 (0~9)</div>
                  </div>
                </NPopover>
              </div>
              <div class="batch-number-hint">
                <span class="hint-bullet">💡</span>
                <span class="hint-text">{{ getOperatorExplanation(operation) }}</span>
              </div>
            </div>
            <div v-else-if="operation.kind === 'insert'" class="batch-operation-fields batch-insert-fields">
              <div class="batch-insert-anchor-row">
                <NInput
                  v-model:value="operation.anchorSection"
                  size="small"
                  placeholder="锚点 section；留空表示文件尾部追加"
                  @keydown="insertTab"
                />
                <div class="batch-switch-row">
                  <NSwitch v-model:value="operation.hasEndTag" size="small" />
                  <span>生成结束标签</span>
                </div>
              </div>
              <NInput
                v-model:value="operation.value"
                type="textarea"
                :autosize="{ minRows: 2, maxRows: 5 }"
                placeholder="插入值，例如 1 或 `文本`"
                @keydown="insertTab"
              />
            </div>
          </div>
        </div>
      </div>

      <NAlert v-if="batch.error" type="error" :show-icon="false">{{ batch.error }}</NAlert>
      <NAlert v-if="batch.stale" type="warning" :show-icon="false">
        归档内容或批处理条件已变化，当前预览已失效，请重新预览。
      </NAlert>

      <div class="batch-preview-toolbar">
        <div class="batch-summary">
          <NText v-if="batch.hasPreview">
            {{ batch.matchedFiles.toLocaleString() }} 个文件命中 /
            {{ batch.matchedOccurrences.toLocaleString() }} 处，
            {{ batch.changedFiles.toLocaleString() }} 个文件可变更
          </NText>
          <NText v-else depth="3">填写条件后点击“预览变更”</NText>
        </div>
        <div class="batch-selection-actions" v-if="batch.hasPreview">
          <NButton
            quaternary
            size="small"
            :disabled="batch.rows.length === 0"
            @click="expandAll"
          >
            <template #icon><NIcon :component="ArrowExpand20Regular" /></template>
            全部展开
          </NButton>
          <NButton
            quaternary
            size="small"
            :disabled="expandedPaths.size === 0"
            @click="collapseAll"
          >
            <template #icon><NIcon :component="ArrowCollapseAll20Regular" /></template>
            全部收起
          </NButton>
          <span class="batch-action-sep" />
          <NButton quaternary size="small" @click="batch.selectAll">全选</NButton>
          <NButton quaternary size="small" @click="batch.selectNone">全不选</NButton>
          <NTag size="small" type="info" :bordered="false">已选 {{ batch.selectedCount }} 个</NTag>
        </div>
      </div>

      <NSpin :show="batch.loading || batch.applying">
        <div class="batch-results">
          <div v-if="batch.rows.length === 0" class="batch-empty">
            <NText depth="3">暂无预览结果</NText>
          </div>
          <div v-for="row in batch.rows" :key="`${row.fileIndex}:${row.path}`" class="batch-result-row">
            <div class="batch-result-header">
              <NCheckbox
                :checked="batch.selectedIndexes.has(row.fileIndex)"
                :disabled="row.status !== 'changed'"
                @update:checked="batch.toggleSelected(row.fileIndex)"
              />
              <button
                class="batch-expand-toggle"
                type="button"
                :title="isExpanded(row.path) ? '收起差异' : '展开差异'"
                @click="toggleExpanded(row.path)"
              >
                <NIcon :class="['batch-chevron', { 'is-expanded': isExpanded(row.path) }]">
                  <ChevronRight16Regular />
                </NIcon>
              </button>
              <button class="batch-result-path" type="button" @click="toggleExpanded(row.path)">
                {{ row.path }}
              </button>
              <NTag size="tiny" :type="statusType(row)" :bordered="false">{{ statusLabel(row) }}</NTag>
              <NText v-if="row.matchCount > 0" depth="3">{{ row.matchCount }} 处</NText>
              <NText v-if="row.reason" depth="3" class="batch-result-reason">{{ row.reason }}</NText>
              <NText
                v-if="row.warnings?.length"
                depth="3"
                class="batch-result-warning"
                :title="row.warnings.join('；')"
              >
                {{ warningSummary(row) }}
              </NText>
            </div>
            <div v-if="isExpanded(row.path)" class="batch-result-detail">
              <div v-if="row.diff?.length" class="batch-diff">
                <div
                  v-for="(line, index) in row.diff"
                  :key="`${row.path}:${index}`"
                  :class="['batch-diff-line', `batch-diff-line--${line?.kind ?? 'context'}`]"
                >
                  <span class="batch-diff-prefix">{{ diffPrefix(line!) }}</span>
                  <span class="batch-diff-text">{{ line?.text }}</span>
                </div>
                <div v-if="row.diffTruncated" class="batch-diff-truncated">diff 已截断，仅显示前 2000 行</div>
              </div>
              <NText v-else depth="3">没有可显示的文本差异。</NText>
            </div>
          </div>
          <div v-if="batch.nextCursor >= 0" class="batch-load-more">
            <NButton quaternary :loading="batch.loading" @click="batch.loadMore">加载更多结果</NButton>
          </div>
        </div>
      </NSpin>

      <div class="batch-footer">
        <NButton quaternary :disabled="batch.loading || batch.applying" @click="close">取消</NButton>
        <div class="batch-footer-actions">
          <NButton :loading="batch.loading" :disabled="batch.applying" @click="preview">预览变更</NButton>
          <NButton type="primary" :loading="batch.applying" :disabled="!batch.canApply" @click="apply">
            应用 {{ batch.selectedCount }} 个文件
          </NButton>
        </div>
      </div>
    </div>
  </NModal>
</template>

<style scoped>
.batch-modal {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.batch-scope,
.batch-mode-row,
.batch-preview-toolbar,
.batch-footer,
.batch-footer-actions,
.batch-selection-actions,
.batch-operation-heading,
.batch-operation-topline,
.batch-operation-fields,
.batch-switch-row {
  display: flex;
  align-items: center;
}
.batch-scope,
.batch-mode-row,
.batch-preview-toolbar,
.batch-footer,
.batch-operation-heading {
  justify-content: space-between;
  gap: 12px;
}
.batch-text-form {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
}
.batch-field {
  display: flex;
  flex-direction: column;
  gap: 5px;
}
.batch-field label {
  color: var(--pvf-text-secondary);
  font-size: 12px;
}
.batch-switch-row {
  gap: 8px;
  color: var(--pvf-text-secondary);
  font-size: 12px;
}
.batch-text-form > .batch-switch-row {
  grid-column: 1 / -1;
}
.batch-structured-form {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.batch-operations {
  display: flex;
  flex-direction: column;
  gap: 7px;
  max-height: 220px;
  overflow: auto;
}
.batch-operation {
  display: flex;
  flex-direction: column;
  gap: 7px;
  padding: 8px;
  border: 1px solid var(--pvf-border-normal);
  border-radius: 5px;
  background: var(--pvf-surface-inset);
}
.batch-operation-topline,
.batch-operation-fields {
  gap: 7px;
}
.batch-operation-topline :deep(.n-input) {
  flex: 1;
  min-width: 100px;
}
.batch-operation-fields :deep(.n-input) {
  flex: 1;
}
.batch-set-fields {
  flex-direction: column;
  align-items: stretch;
}
.batch-set-fields > :deep(.n-input) {
  width: 100%;
}
.batch-template-bar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  padding: 6px 10px;
  background: var(--pvf-surface-subtle);
  border-radius: 6px;
  border: 1px solid var(--pvf-border-subtle);
}
.batch-template-label {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  font-weight: 500;
  color: var(--pvf-text-secondary);
  flex-shrink: 0;
}
.batch-template-icon {
  color: var(--pvf-warning);
}
.batch-template-chips {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
}
.batch-template-chip {
  display: inline-flex;
  align-items: center;
  padding: 2px 9px;
  font-size: 11px;
  color: var(--pvf-text-secondary);
  background: var(--pvf-surface-panel);
  border: 1px solid var(--pvf-border-subtle);
  border-radius: 12px;
  cursor: pointer;
  transition: all 0.15s ease;
}
.batch-template-chip:hover {
  color: var(--pvf-primary-hover);
  background: var(--pvf-surface-hover);
  border-color: var(--pvf-primary-base);
}
.batch-number-fields {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.batch-number-inputs-row {
  display: flex;
  align-items: center;
  gap: 7px;
  width: 100%;
}
.batch-number-inputs-row :deep(.n-input) {
  flex: 1;
}
.batch-number-hint {
  display: flex;
  align-items: baseline;
  gap: 6px;
  padding: 5px 8px;
  background: var(--pvf-surface-subtle);
  border-radius: 4px;
  font-size: 11px;
  color: var(--pvf-text-secondary);
  line-height: 1.45;
}
.batch-number-hint .hint-bullet {
  flex-shrink: 0;
}
.batch-help-icon-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 4px;
  background: transparent;
  border: 0;
  cursor: pointer;
  color: var(--pvf-text-muted);
  border-radius: 4px;
}
.batch-help-icon-btn:hover {
  color: var(--pvf-primary-hover);
  background: var(--pvf-surface-hover);
}
.batch-operator-help-popover {
  font-size: 12px;
  line-height: 1.65;
  color: var(--pvf-text-primary);
}
.help-popover-title {
  font-weight: 600;
  margin-bottom: 6px;
  color: var(--pvf-text-primary);
  border-bottom: 1px solid var(--pvf-border-subtle);
  padding-bottom: 4px;
}
.help-popover-item {
  margin: 3px 0;
  color: var(--pvf-text-secondary);
}
.help-popover-item b {
  color: var(--pvf-text-primary);
}
.batch-action-sep {
  width: 1px;
  height: 14px;
  background: var(--pvf-border-subtle);
  margin: 0 4px;
}
.batch-expand-toggle {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 3px;
  background: transparent;
  border: 0;
  cursor: pointer;
  color: var(--pvf-text-muted);
  border-radius: 3px;
}
.batch-expand-toggle:hover {
  color: var(--pvf-primary-hover);
  background: var(--pvf-surface-hover);
}
.batch-chevron {
  transition: transform 0.2s ease;
  display: inline-flex;
}
.batch-chevron.is-expanded {
  transform: rotate(90deg);
}
.batch-insert-fields {
  flex-direction: column;
  align-items: stretch;
}
.batch-insert-anchor-row {
  display: flex;
  align-items: center;
  gap: 7px;
  width: 100%;
}
.batch-insert-anchor-row :deep(.n-input),
.batch-insert-fields > :deep(.n-input) {
  width: 100%;
}
.batch-results {
  max-height: 420px;
  min-height: 150px;
  overflow: auto;
  border: 1px solid var(--pvf-border-normal);
  border-radius: 4px;
}
.batch-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 150px;
}
.batch-result-row + .batch-result-row {
  border-top: 1px solid var(--pvf-border-faint);
}
.batch-result-header {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  padding: 7px 9px;
}
.batch-result-path {
  flex: 0 1 380px;
  min-width: 100px;
  overflow: hidden;
  color: var(--pvf-text-code);
  text-align: left;
  text-overflow: ellipsis;
  white-space: nowrap;
  cursor: pointer;
  background: transparent;
  border: 0;
}
.batch-result-path:hover {
  color: var(--pvf-primary-hover);
}
.batch-result-reason {
  min-width: 0;
  overflow: hidden;
  color: var(--pvf-text-muted);
  text-overflow: ellipsis;
  white-space: nowrap;
}
.batch-result-warning {
  min-width: 0;
  overflow: hidden;
  color: var(--pvf-warning);
  text-overflow: ellipsis;
  white-space: nowrap;
}
.batch-result-detail {
  padding: 0 12px 10px 42px;
}
.batch-diff {
  overflow: auto;
  padding: 6px 8px;
  background: var(--pvf-surface-code);
  border-radius: 4px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
  line-height: 1.55;
  white-space: pre;
}
.batch-diff-line {
  display: flex;
  min-height: 19px;
}
.batch-diff-prefix {
  flex: 0 0 18px;
  color: var(--pvf-text-muted);
  text-align: center;
}
.batch-diff-text {
  min-width: 0;
}
.batch-diff-line--remove {
  color: var(--pvf-error-hover);
  background: var(--pvf-surface-error);
}
.batch-diff-line--add {
  color: var(--pvf-success-hover);
  background: var(--pvf-surface-success);
}
.batch-diff-truncated {
  padding-top: 5px;
  color: var(--pvf-text-faint);
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  font-size: 11px;
}
.batch-load-more {
  display: flex;
  justify-content: center;
  padding: 7px;
}
.batch-footer-actions,
.batch-selection-actions {
  gap: 8px;
}
@media (max-width: 760px) {
  .batch-text-form {
    grid-template-columns: 1fr;
  }
  .batch-operation-topline {
    flex-wrap: wrap;
  }
  .batch-result-reason {
    flex-basis: 100%;
    padding-left: 26px;
  }
}
</style>
