<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue';
import { NAlert, NButton, NEmpty, NInput, NInputNumber, NModal, useDialog, useMessage } from 'naive-ui';
import { FileGUIService } from '../../../bindings/pvfine/services';
import type { GUIFile } from '../../gui/types';
import { addCraneItem, editCraneItem, editCraneMaterial, parseCraneMinigame, removeCraneItem } from '../../gui/craneMinigame';
import { createItemNameSession } from '../../gui/itemNames';
import { useEditorStore } from '../../stores/editor';
import { useFileGUIStore } from '../../stores/fileGUI';
import ItemPicker from '../ItemPicker.vue';

const props = defineProps<{ file: GUIFile; active: boolean }>();
const editor = useEditorStore(), gui = useFileGUIStore(), message = useMessage(), dialog = useDialog();
const query = ref('');
const parsed = computed(() => {
  try { return { document: parseCraneMinigame(props.file.text), error: '' }; }
  catch (cause) { return { document: null, error: String(cause) }; }
});
const document = computed(() => parsed.value.document);
const names = createItemNameSession(FileGUIService.ReadItemNames);
const itemName = (id: string) => names.names.value[id] || '未解析物品';
const rows = computed(() => (document.value?.items ?? []).map((item, index) => ({ item, index })).filter(({ item }) => {
  const id = item.fields[0].value;
  return `${id} ${itemName(id)}`.toLowerCase().includes(query.value.trim().toLowerCase());
}));
const canEdit = computed(() => props.active && !!document.value && props.file.editable && !editor.saving && !editor.guiApplying);
const dirty = computed(() => { const tab = editor.getFileDraft(props.file.index); return !!tab && tab.path === props.file.path && editor.isTextDirty(tab); });
const form = reactive({ show: false, material: false, row: -1, text: '', index: -1, path: '', epoch: -1, id: '', name: '', count: 1 as number | null, viewRatio: 43.96 as number | null, pickRatio: 90 as number | null });
watch(() => [props.file.index, props.file.path, props.file.text, props.active, gui.epoch, gui.revision], () => {
  form.show = false;
  names.invalidate(true);
  if (props.active && document.value) void names.load([...document.value.items.map(item => item.fields[0].value), ...document.value.material.slice(0, 1).map(t => t.value)]);
}, { immediate: true });
onBeforeUnmount(() => names.invalidate(true));
function open(material = false, row = -1) {
  if (!canEdit.value) return;
  const fields = material ? document.value!.material : document.value!.items[row]?.fields;
  const id = fields?.[0]?.value ?? '';
  Object.assign(form, { show: true, material, row, text: props.file.text, index: props.file.index, path: props.file.path, epoch: gui.epoch,
    id, name: names.names.value[id] ?? '', count: Number(fields?.[1]?.value ?? 1), viewRatio: Number(fields?.[2]?.value ?? 43.96), pickRatio: Number(fields?.[3]?.value ?? 90) });
}
function current(text: string, index: number, path: string, epoch: number) {
  if (!canEdit.value || text !== props.file.text || index !== props.file.index || path !== props.file.path || epoch !== gui.epoch) throw new Error('文件已变化，请重新操作');
}
function apply() {
  try {
    current(form.text, form.index, form.path, form.epoch);
    if (!/^\d+$/.test(form.id.trim())) throw new Error('请选择物品或输入整数物品 ID');
    const values = { id: Number(form.id), count: form.count ?? NaN, viewRatio: form.viewRatio ?? NaN, pickRatio: form.pickRatio ?? NaN };
    const text = form.material ? editCraneMaterial(form.text, values.id, values.count) : form.row < 0 ? addCraneItem(form.text, values) : editCraneItem(form.text, form.row, values);
    editor.updateContent(form.index, text);
    form.show = false;
    message.success('已更新草稿，保存后写入工作区');
  } catch (cause) { message.error(String(cause)); }
}
function remove(index: number) {
  if (!canEdit.value || form.show) return;
  const text = props.file.text, fileIndex = props.file.index, path = props.file.path, epoch = gui.epoch;
  const id = document.value!.items[index]!.fields[0].value;
  dialog.warning({ title: '删除奖品', content: `删除 ${itemName(id)}（#${id}）这条记录？`, positiveText: '删除', negativeText: '取消', onPositiveClick: () => {
    try { current(text, fileIndex, path, epoch); editor.updateContent(fileIndex, removeCraneItem(text, index)); }
    catch (cause) { message.error(String(cause)); }
  } });
}
async function save() {
  if (!canEdit.value || !dirty.value || form.show) return;
  const epoch = gui.epoch;
  try { if (await editor.saveFileTab(props.file.index) && epoch === gui.epoch) message.success('已保存到工作区；保存 PVF 后写入归档文件'); }
  catch (cause) { if (epoch === gui.epoch) message.error(String(cause)); }
}
</script>

<template>
  <div class="crane-viewer">
    <header>
      <div><h2>娃娃机</h2><span class="muted">{{ document?.items.length ?? 0 }} 条奖品记录<span v-if="dirty"> · 有未保存修改</span></span></div>
      <div class="actions"><NButton size="small" :disabled="!canEdit || form.show" @click="open()">新增奖品</NButton><NButton size="small" type="primary" :disabled="!canEdit || !dirty || form.show" :loading="editor.saving" @click="save">保存娃娃机</NButton></div>
    </header>
    <NAlert v-if="parsed.error" type="error" title="无法读取娃娃机配置" role="alert">{{ parsed.error }}</NAlert>
    <template v-else-if="document">
      <section class="material-bar" aria-label="启动材料">
        <div><span class="muted">每次启动消耗</span><p v-if="document.material.length"><strong>{{ itemName(document.material[0]!.value) }}</strong> <span class="muted">#{{ document.material[0]!.value }}</span> <b>× {{ document.material[1]!.value }}</b></p><p v-else class="muted">尚未配置启动材料</p></div>
        <NButton size="small" :disabled="!canEdit || form.show" @click="open(true)">编辑启动材料</NButton>
      </section>
      <NAlert v-if="names.error.value" type="warning" :show-icon="false">物品名称读取失败，可继续按 ID 编辑。<NButton size="tiny" @click="names.load([...document.items.map(item => item.fields[0].value), ...document.material.slice(0, 1).map(t => t.value)])">重试</NButton></NAlert>
      <div class="list-heading"><h3>奖品列表</h3><NInput v-model:value="query" clearable placeholder="搜索物品 ID 或名称" aria-label="搜索奖品" style="max-width: 300px" /></div>
      <div v-if="rows.length" class="table-scroll"><table><thead><tr><th>序号</th><th>物品</th><th>数量</th><th>视野比率（抓取范围）</th><th>抓取几率（%）</th><th>操作</th></tr></thead><tbody>
        <tr v-for="{ item, index } in rows" :key="index"><td class="muted">{{ index + 1 }}</td><td><strong>{{ itemName(item.fields[0].value) }}</strong><small>#{{ item.fields[0].value }}</small></td><td>{{ item.fields[1].value }}</td><td>{{ item.fields[2].value }}</td><td>{{ item.fields[3].value }}%</td><td><div class="actions"><NButton size="tiny" :disabled="!canEdit || form.show" @click="open(false, index)">编辑</NButton><NButton size="tiny" quaternary type="error" :disabled="!canEdit || form.show" @click="remove(index)">删除</NButton></div></td></tr>
      </tbody></table></div>
      <NEmpty v-else :description="query ? '没有匹配的奖品' : '暂无奖品，点击“新增奖品”开始配置'" />
    </template>
    <NModal v-model:show="form.show" preset="card" :title="form.material ? '编辑启动材料' : form.row < 0 ? '新增奖品' : '编辑奖品'" style="width: 560px; max-width: 95vw" :mask-closable="false">
      <div class="crane-form">
        <label>{{ form.material ? '启动材料' : '奖品物品' }}<ItemPicker v-model="form.id" :label="form.name" :disabled="!canEdit" @select="form.name = $event?.name ?? ''" /></label>
        <label>物品 ID<NInputNumber :value="form.id.trim() ? Number(form.id) : null" :min="0" :max="2147483647" :precision="0" :disabled="!canEdit" aria-label="物品 ID" @update:value="form.id = $event === null ? '' : String($event); form.name = ''" /></label>
        <label>数量<NInputNumber v-model:value="form.count" :min="0" :max="2147483647" :precision="0" :disabled="!canEdit" aria-label="数量" /></label>
        <template v-if="!form.material"><label>视野比率（抓取范围）<NInputNumber v-model:value="form.viewRatio" :min="0" :disabled="!canEdit" aria-label="视野比率" /></label><label>抓取几率（%）<NInputNumber v-model:value="form.pickRatio" :min="0" :max="100" :disabled="!canEdit" aria-label="抓取几率" /></label></template>
        <div class="actions form-actions"><NButton @click="form.show = false">取消</NButton><NButton type="primary" :disabled="!canEdit" @click="apply">应用到草稿</NButton></div>
      </div>
    </NModal>
  </div>
</template>

<style scoped>
.crane-viewer { width: 100%; min-width: 0; color: var(--pvf-text-primary); font-size: 13px; }
header, .material-bar, .list-heading, .actions { display: flex; align-items: center; gap: 12px; }
header { justify-content: space-between; flex-wrap: wrap; padding: 4px 0 16px; border-bottom: 1px solid var(--pvf-border-subtle); }
h2 { margin: 0 0 4px; font-size: 16px; } h3 { margin: 0; font-size: 14px; }
.muted, small { color: var(--pvf-text-secondary); font-size: 12px; }
.material-bar { justify-content: space-between; flex-wrap: wrap; padding: 18px 0; border-bottom: 1px solid var(--pvf-border-subtle); }.material-bar p { margin: 6px 0 0; }.material-bar b { margin-left: 12px; }
.list-heading { justify-content: space-between; margin: 16px 0; }.table-scroll { overflow-x: auto; max-height: 65vh; }
table { width: 100%; min-width: 700px; border-collapse: collapse; text-align: left; font-variant-numeric: tabular-nums; } th { color: var(--pvf-text-secondary); font-size: 12px; font-weight: 500; } th, td { padding: 10px 12px; border-bottom: 1px solid var(--pvf-border-subtle); } td strong { font-weight: 500; } td small { display: block; margin-top: 3px; } tbody tr:hover { background: var(--pvf-primary-soft); }
.actions { gap: 8px; flex-shrink: 0; }.crane-form { display: grid; gap: 14px; }.crane-form label { display: grid; gap: 6px; }.form-actions { justify-content: flex-end; margin-top: 8px; }
</style>
