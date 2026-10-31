<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from "vue";
import { NButton, NButtonGroup, NCheckbox, NEmpty, NInputNumber, NModal, NSlider, NSpin, useMessage } from "naive-ui";
import { FileGUIService } from "../../../bindings/pvfine/services";
import type { ForgeDocument, UpgradeRow } from "../../../bindings/pvfine/services/models";
import type { GUIFile } from "../../gui/types";
import { editForge, type ForgeScope } from "../../gui/forge";
import { upgradeRange } from "../../gui/upgrade";
import { useEditorStore } from "../../stores/editor";
import { useFileGUIStore } from "../../stores/fileGUI";
import ItemPicker from "../ItemPicker.vue";
const props = defineProps<{ file: GUIFile; active: boolean }>();
const editor = useEditorStore();
const gui = useFileGUIStore();
const message = useMessage();
const document = ref<ForgeDocument | null>(null);
const sourceText = ref("");
const loading = ref(false);
const error = ref("");
const section = ref<"table" | "grades" | "grades100">("table");
const sections = computed(() => [{ value: "table" as const, label: "锻造等级" }, { value: "grades" as const, label: "材料消耗" }, ...(document.value?.grades100?.length ? [{ value: "grades100" as const, label: "材料消耗（100lv）" }] : [])]);
const rows = computed(() => document.value?.[section.value === "table" ? "rows" : section.value] ?? []);
const extraCoefficients = computed(() => section.value === "table" && rows.value.some(row => row.fieldsT2?.length));
const columns = computed(() => section.value === "table" ? [{ column: 2, label: "成功率（%）" }, { column: 3, label: "材料消耗倍率" }] : [{ column: 0, label: "消耗道具" }, { column: 1, label: "数量基数" }]);
const ready = computed(() => !loading.value && !error.value && sourceText.value === props.file.text);
const canEdit = computed(() => ready.value && props.file.editable && !editor.saving && !editor.guiApplying);
const dirty = computed(() => { const tab = editor.getFileDraft(props.file.index); return !!tab && tab.path === props.file.path && editor.isTextDirty(tab); });
const form = reactive({ show: false, bulk: false, scope: "table" as ForgeScope, text: "", index: -1, epoch: 0, rows: [] as UpgradeRow[], range: [1, 1] as [number, number], changes: [] as { column: number; label: string; enabled: boolean; value: number | null }[], itemID: "", itemName: "" });
const targets = computed(() => form.bulk ? upgradeRange(form.rows, form.range) : form.rows);
const selected = computed(() => form.changes.some(change => change.enabled));
const dialogTitle = computed(() => form.scope === "maximum" ? "最大锻造等级" : `${form.bulk ? '批量设置' : form.scope === 'table' ? '+' + form.rows[0]?.level : '装备等级 ' + form.rows[0]?.level} · ${form.scope === 'table' ? '锻造等级' : '材料消耗'}`);
let generation = 0;
let timer: ReturnType<typeof setTimeout> | undefined;
async function load() {
  const request = ++generation, epoch = gui.epoch, text = props.file.text, index = props.file.index;
  loading.value = true; error.value = "";
  try {
    const result = await FileGUIService.ReadForge(index, text);
    if (request !== generation || epoch !== gui.epoch || index !== props.file.index || text !== props.file.text) return;
    if (!result) throw new Error("未返回锻造数据");
    document.value = result; sourceText.value = text;
    if (section.value === "grades100" && !result.grades100?.length) section.value = "grades";
  } catch (cause) { if (request === generation && epoch === gui.epoch) { error.value = String(cause); document.value = null; } }
  finally { if (request === generation) loading.value = false; }
}
watch(() => [props.file.index, props.file.text, gui.epoch, gui.revision, props.active], () => {
  generation++; clearTimeout(timer); form.show = false;
  if (props.active) timer = setTimeout(load, 80);
}, { immediate: true });
watch(() => gui.epoch, () => { document.value = null; });
watch(section, () => { form.show = false; });
onBeforeUnmount(() => { generation++; clearTimeout(timer); });
function displayed(row: UpgradeRow, column: number) { const value = row.fields?.[column]?.value ?? 0; return section.value === "table" && column === 2 ? Number((value / 100).toFixed(2)) : value; }
function openForm(row?: UpgradeRow, column?: number) {
  if (!canEdit.value || !rows.value.length) return;
  const chosen = row ? [row] : rows.value, first = chosen[0]!;
  Object.assign(form, { show: true, bulk: !row, scope: section.value === "table" ? "table" : "grades", text: props.file.text, index: props.file.index, epoch: gui.epoch,
    rows: chosen, range: [first.level, chosen.at(-1)!.level], itemID: String(first.fields?.[0]?.value ?? 0), itemName: first.costName,
    changes: columns.value.filter(c => column === undefined || c.column === column).map(c => ({ ...c, enabled: !!row, value: displayed(first, c.column) })) });
}
function openMaximum() {
  if (!canEdit.value || !document.value) return;
  Object.assign(form, { show: true, bulk: false, scope: "maximum", text: props.file.text, index: props.file.index, epoch: gui.epoch,
    rows: [{ level: 1, fields: [document.value.maximum], fieldsT2: [], costName: "" }], changes: [{ column: 0, label: "最大锻造等级", enabled: true, value: document.value.maximum.value }] });
}
function apply() {
  try {
    if (!canEdit.value || form.text !== props.file.text || form.index !== props.file.index || form.epoch !== gui.epoch) throw new Error("文件已变化，请重新打开表单");
    const changes = form.changes.filter(c => c.enabled).map(c => ({ column: c.column, value: form.scope === "grades" && c.column === 0 ? (form.itemID.trim() ? Number(form.itemID) : NaN) : c.value ?? NaN }));
    editor.updateContent(props.file.index, editForge(form.text, targets.value, form.scope, changes, document.value?.rows?.length ?? 0));
    form.show = false; message.success("已更新草稿，保存后写入工作区");
  } catch (cause) { message.error(String(cause)); }
}
async function save() {
  if (!canEdit.value || !dirty.value || form.show) return;
  const epoch = gui.epoch;
  try { if (await editor.saveFileTab(props.file.index) && epoch === gui.epoch) message.success("已保存到工作区；保存 PVF 后写入归档文件"); }
  catch (cause) { if (epoch === gui.epoch) message.error(String(cause)); }
}
function precision(column: number) { return form.scope === "table" ? column === 2 ? 2 : 6 : 0; }
function maximum(column: number) { return form.scope === "maximum" ? document.value?.rows?.length ?? 1 : form.scope === "table" && column === 2 ? 100 : 2147483647; }
</script>
<template>
  <div class="forge-viewer">
    <header class="forge-header">
      <div class="forge-heading"><h2>锻造配置</h2><NButton size="tiny" :disabled="!canEdit || form.show" @click="openMaximum">最大等级 +{{ document?.maximum.value ?? '—' }}</NButton></div>
      <div class="forge-actions"><NButton size="small" :disabled="!canEdit || form.show" @click="openForm()">批量设置</NButton><NButton size="small" type="primary" :loading="editor.saving" :disabled="!canEdit || !dirty || form.show" @click="save">保存锻造</NButton></div>
    </header>
    <div class="forge-group-bar"><NButtonGroup size="small" aria-label="锻造配置分类"><NButton v-for="s in sections" :key="s.value" :type="section === s.value ? 'primary' : 'default'" :disabled="loading || form.show" @click="section = s.value">{{ s.label }}</NButton></NButtonGroup></div>
    <p class="forge-hint">{{ section === 'table' ? '锻造失败不会损坏或降级；属性系数 A/B 暂按原始值只读展示。' : '按装备等级设置材料和数量基数，实际消耗还受锻造倍率等参数影响。' }}</p>
    <div v-if="error" class="error" role="alert">{{ error }} <NButton size="small" @click="load">重新加载</NButton></div>
    <NSpin :show="loading"><div v-if="rows.length" class="table-scroll"><table class="forge-table" :class="{ 'forge-table-wide': extraCoefficients }">
      <thead><tr><th>{{ section === 'table' ? '等级' : '装备等级' }}</th><th v-for="c in columns" :key="c.column">{{ c.label }}</th><template v-if="section === 'table'"><th>属性系数 A</th><th>属性系数 B</th><template v-if="extraCoefficients"><th>附加系数 1</th><th>附加系数 2</th></template></template></tr></thead>
      <tbody><tr v-for="row in rows" :key="row.level"><th>{{ section === 'table' ? '+' : '' }}{{ row.level }}</th><td v-for="c in columns" :key="c.column"><button :class="['forge-cell', { 'forge-material': section !== 'table' && c.column === 0 }]" :disabled="!canEdit || form.show" @click="openForm(row, c.column)"><template v-if="section !== 'table' && c.column === 0"><span>{{ row.costName || '物品' }}</span><small>#{{ row.fields?.[0]?.value }}</small></template><template v-else>{{ displayed(row, c.column) }}{{ section === 'table' && c.column === 2 ? '%' : '' }}</template></button></td><template v-if="section === 'table'"><td class="raw-cell">{{ row.fields?.[0]?.value }}</td><td class="raw-cell">{{ row.fields?.[1]?.value }}</td><template v-if="extraCoefficients"><td class="raw-cell">{{ row.fieldsT2?.[0]?.value }}</td><td class="raw-cell">{{ row.fieldsT2?.[1]?.value }}</td></template></template></tr></tbody>
    </table></div><NEmpty v-else-if="!loading && !error" description="没有锻造数据" /></NSpin>
    <details v-if="document?.extras?.length" class="forge-extras"><summary>其他参数（只读）</summary><div v-for="extra in document.extras" :key="extra.name"><strong>[{{ extra.name }}]</strong><p>{{ extra.fields?.map(field => field.value).join('　') }}</p></div></details>
    <NModal v-model:show="form.show" preset="card" :title="dialogTitle" style="width: 560px; max-width: 95vw" :mask-closable="false">
      <div class="forge-batch-form">
        <section v-if="form.bulk" class="forge-range"><div class="forge-range-heading"><strong>{{ form.scope === 'table' ? '锻造等级范围' : '装备等级范围' }}</strong><span>{{ form.range[0] }} ～ {{ form.range[1] }} · {{ targets.length }} 条</span></div>
          <NSlider v-model:value="form.range" range :min="form.rows[0]?.level ?? 1" :max="form.rows.at(-1)?.level ?? 1" :step="1" :disabled="!canEdit || form.rows.length < 2" />
          <div class="forge-range-inputs"><NInputNumber :value="form.range[0]" :min="form.rows[0]?.level ?? 1" :max="form.range[1]" :precision="0" aria-label="起始等级" :disabled="!canEdit" @update:value="form.range[0] = $event ?? form.rows[0]!.level" /><span>至</span><NInputNumber :value="form.range[1]" :min="form.range[0]" :max="form.rows.at(-1)?.level ?? 1" :precision="0" aria-label="结束等级" :disabled="!canEdit" @update:value="form.range[1] = $event ?? form.rows.at(-1)!.level" /></div>
        </section>
        <div v-for="c in form.changes" :key="c.column" class="forge-batch-row"><NCheckbox v-if="form.bulk" v-model:checked="c.enabled" :disabled="!canEdit">{{ c.label }}</NCheckbox><label v-else>{{ c.label }}</label>
          <div v-if="form.scope === 'grades' && c.column === 0" class="forge-batch-item"><NInputNumber :value="form.itemID.trim() ? Number(form.itemID) : null" :min="0" :max="2147483647" :precision="0" :disabled="!canEdit || !c.enabled" placeholder="道具 ID" @update:value="form.itemID = $event === null ? '' : String($event); form.itemName = ''" /><ItemPicker v-model="form.itemID" :label="form.itemName" :disabled="!canEdit || !c.enabled" @select="form.itemName = $event?.name ?? ''" /></div>
          <NInputNumber v-else v-model:value="c.value" :min="form.scope === 'maximum' ? 1 : 0" :max="maximum(c.column)" :precision="precision(c.column)" :disabled="!canEdit || !c.enabled" />
        </div>
        <p v-if="form.bulk" class="forge-hint">只修改所选范围内的勾选项。</p>
        <div class="forge-dialog-actions"><NButton @click="form.show = false">取消</NButton><NButton type="primary" :disabled="!canEdit || !selected || !targets.length" @click="apply">应用到草稿</NButton></div>
      </div>
    </NModal>
  </div>
</template>
<style scoped>
.forge-viewer { min-width: 0; width: 100%; color: var(--pvf-text-primary); font-size: 13px; }
.forge-header { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 12px; padding: 4px 0 14px; border-bottom: 1px solid var(--pvf-border-subtle); }
.forge-heading { display: flex; align-items: baseline; gap: 12px; min-width: 0; }
.forge-heading h2 { margin: 0; font-size: 16px; line-height: 1.5; font-weight: 600; white-space: nowrap; }
.forge-hint { color: var(--pvf-text-secondary); font-size: 12px; }
.forge-actions, .forge-dialog-actions { display: flex; align-items: center; gap: 8px; flex-shrink: 0; }
.forge-actions { margin-left: auto; }
.forge-dialog-actions { justify-content: flex-end; }
.forge-group-bar { display: flex; flex-wrap: wrap; align-items: center; gap: 12px; padding: 12px 0; }
.table-scroll { overflow: auto; border: 1px solid var(--pvf-border-subtle); border-radius: 4px; max-height: 70vh; margin-top: 12px; }
.forge-table { width: 100%; min-width: 650px; table-layout: fixed; border-collapse: separate; border-spacing: 0; font-size: 13px; font-variant-numeric: tabular-nums; }
.forge-table-wide { min-width: 850px; }
.forge-table th, .forge-table td { border-bottom: 1px solid var(--pvf-border-subtle); text-align: left; }
.forge-table thead th { position: sticky; top: 0; z-index: 1; background: var(--pvf-surface-elevated); padding: 10px 12px; font-size: 12px; font-weight: 500; white-space: nowrap; color: var(--pvf-text-secondary); }
.forge-table tbody th { padding: 8px 12px; font-weight: 500; }
.forge-table tbody tr:last-child th, .forge-table tbody tr:last-child td { border-bottom: none; }
.forge-table tbody tr:hover { background: var(--pvf-surface-hover); }
.forge-cell { display: block; border: none; background: transparent; color: inherit; font: inherit; line-height: 20px; padding: 7px 12px; width: 100%; text-align: left; cursor: pointer; }
.forge-cell:hover:enabled { color: var(--pvf-primary); background: var(--pvf-primary-soft); }
.forge-cell:disabled { cursor: default; color: var(--pvf-text-secondary); }
.forge-cell:focus-visible { outline: 2px solid var(--pvf-primary); outline-offset: -2px; }
.forge-material { display: flex; align-items: baseline; gap: 8px; }
.forge-material span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.forge-material small { color: var(--pvf-text-secondary); font-size: 12px; white-space: nowrap; }
.error { padding: 10px; margin: 12px 0; background: var(--pvf-surface-warning); font-size: 13px; }
.forge-batch-form { display: grid; gap: 16px; }
.forge-range { padding: 12px 14px; background: var(--pvf-surface-inset); border-radius: 4px; }
.forge-range-heading { display: flex; justify-content: space-between; gap: 12px; margin-bottom: 14px; font-size: 13px; }
.forge-range-heading span { color: var(--pvf-text-secondary); font-size: 12px; }
.forge-range-inputs { display: flex; align-items: center; gap: 10px; margin-top: 12px; }
.forge-range-inputs .n-input-number { flex: 1; min-width: 0; }
.forge-batch-row { display: grid; grid-template-columns: 130px minmax(0, 1fr); gap: 12px; align-items: start; }
.forge-batch-row > .n-checkbox { margin-top: 6px; }
.forge-batch-item { display: grid; gap: 8px; }
.forge-batch-form p { margin: 0; line-height: 1.6; }
@media (max-width: 550px) { .forge-header { align-items: flex-start; } .forge-actions { width: 100%; margin-left: 0; justify-content: flex-end; } }
.raw-cell { padding: 8px 12px; color: var(--pvf-text-secondary); }
.forge-extras { margin-top: 20px; font-size: 12px; color: var(--pvf-text-secondary); }
.forge-extras summary { cursor: pointer; padding: 8px 0; }
.forge-extras p { overflow-wrap: anywhere; line-height: 1.8; margin: 6px 0 14px; }
</style>
