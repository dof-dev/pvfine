<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue';
import { NAlert, NButton, NInput, NModal, NSelect, NTag, useMessage } from 'naive-ui';
import type { GUIFile } from '../../gui/types';
import { editItem, itemFieldIssue, itemGroups, itemSection, parseItem, type ItemField } from '../../gui/item';
import { questReference, questReferenceAnnotation, questAnnotationText } from '../../gui/quest';
import { useEditorStore } from '../../stores/editor';
import { useFileGUIStore } from '../../stores/fileGUI';
import ItemCreateDialog from './ItemCreateDialog.vue';
import ItemBoosterEditor from './ItemBoosterEditor.vue';
import ItemSkillEffectsEditor from './ItemSkillEffectsEditor.vue';
import ItemTriggersEditor from './ItemTriggersEditor.vue';
import EquipmentPreview from '../previews/EquipmentPreview.vue';

const props = defineProps<{ file: GUIFile; active: boolean; paneId?: string }>();
const emit = defineEmits<{ (e: 'close'): void }>();
const editor = useEditorStore(), gui = useFileGUIStore(), message = useMessage();
const equipment = computed(() => props.file.path.toLowerCase().endsWith('.equ'));
const parsed = computed(() => {
  try { return { sections: parseItem(props.file.text), error: '' }; }
  catch (e) { return { sections: [], error: String(e) }; }
});
const groups = computed(() => itemGroups.map(group => ({ ...group, fields: group.fields.filter(f => (!f.equ || equipment.value) && (!f.stk || !equipment.value)) })).filter(g => g.fields.length));
const disabled = computed(() => !props.active || !props.file.editable || editor.saving || editor.guiApplying || !!parsed.value.error);
const dirty = computed(() => { const draft = editor.getFileDraft(props.file.index); return !!draft && editor.isTextDirty(draft); });
const createShow = ref(false), boosterPending = ref(false), skillPending = ref(false), triggerPending = ref(false);
const edit = reactive({ show: false, field: null as ItemField | null, values: [] as string[], source: '', epoch: 0 });
const pending = computed(() => edit.show && edit.values.some((v, i) => v !== (itemSection(parsed.value.sections, edit.field?.name ?? '')?.tokens[i]?.value ?? '')));
let unregister: (() => void) | undefined;
let unregisterGuard: (() => void) | undefined;
watch(() => JSON.stringify([props.file.index, props.paneId]), () => {
  unregister?.();
  unregisterGuard?.();
  unregister = editor.registerGUIDirtyChecker(props.file.index, () => pending.value || createShow.value);
  if (props.paneId) {
    unregisterGuard = editor.getGUIModes(props.paneId).registerGuard(props.file.index, () => {
      if (edit.show || createShow.value || boosterPending.value || skillPending.value || triggerPending.value) { message.warning('请先应用或取消物品字段编辑'); return false; }
      return true;
    });
  }
}, { immediate: true });
onBeforeUnmount(() => { unregister?.(); unregisterGuard?.(); });
function display(field: ItemField): string {
  const tokens = itemSection(parsed.value.sections, field.name)?.tokens;
  if (!tokens?.length) return '未配置';
  const annotations = editor.getFileDraft(props.file.index)?.annotations ?? [];
  return tokens.map(t => {
    const reference = t.kind === 'string' && questReference(t.value);
    const annotation = reference && questReferenceAnnotation(reference, annotations);
    return annotation ? questAnnotationText(annotation) : t.value;
  }).join(' · ');
}
function open(field: ItemField) {
  if (disabled.value || itemFieldIssue(parsed.value.sections, field)) return;
  const tokens = itemSection(parsed.value.sections, field.name)?.tokens;
  Object.assign(edit, { show: true, field, values: tokens ? tokens.map(t => t.value) : field.kinds.map(() => ''), source: props.file.text, epoch: gui.epoch });
}
function apply() {
  if (disabled.value || !edit.field) return;
  try {
    const draft = editor.getFileDraft(props.file.index);
    if (edit.epoch !== gui.epoch || !draft || draft.path !== props.file.path || draft.text !== edit.source || props.file.text !== edit.source) throw new Error('物品草稿已变化，请重新打开字段编辑');
    editor.updateContent(props.file.index, editItem(draft.text, edit.field, edit.values));
    edit.show = false;
  } catch (e) { message.error(String(e)); }
}
async function save() {
  if (disabled.value || edit.show || createShow.value || boosterPending.value || skillPending.value || triggerPending.value || !dirty.value) return;
  try { if (await editor.saveFileTab(props.file.index)) message.success('已保存物品到工作区'); }
  catch (e) { message.error(`保存物品失败：${String(e)}`); }
}
watch(() => JSON.stringify([props.file.index, props.file.path, gui.epoch]), () => { edit.show = false; });
async function openCreated(index: number) {
  try { await editor.openFile(index, props.paneId); editor.setGUIMode(index, 'gui', props.paneId); }
  catch (e) { message.warning(`物品已创建，打开文件失败：${String(e)}`); }
}
const titleField: ItemField = { name: 'name', label: '名称', kinds: ['string'] };
const rarityOptions = ['普通', '高级', '传承', '神器', '史诗', '勇者', '传说'].map((label, i) => ({ label: `${i} · ${label}`, value: String(i) }));
const attachOptions = ['free', 'trade', 'trade delete', 'sealing', 'account'].map(v => ({ label: `[${v}]`, value: `[${v}]` }));
function options(index: number) {
  if (index !== 0) return undefined;
  if (edit.field?.name === 'rarity') return rarityOptions;
  if (edit.field?.name === 'attach type') return attachOptions;
  return undefined;
}
function resize(count: number) { edit.values = Array.from({ length: count }, (_, i) => edit.values[i] ?? ''); }
</script>

<template>
  <section class="item-viewer" aria-label="物品编辑器">
    <header class="item-header">
      <div class="item-heading">
        <div class="title"><h2>{{ display(titleField) === '未配置' ? '物品编辑器' : display(titleField) }}</h2><NTag size="small">{{ equipment ? '装备' : '道具' }}</NTag><NTag v-if="dirty" size="small" type="warning">未保存</NTag><NTag v-if="!file.editable" size="small">只读</NTag></div>
        <div class="path">{{ file.path }}</div>
      </div>
      <div class="actions"><NButton :disabled="disabled || edit.show || boosterPending || skillPending || triggerPending" @click="createShow = true">新建物品</NButton><NButton @click="emit('close')">查看 DSL</NButton><NButton type="primary" :loading="editor.saving" :disabled="disabled || !dirty || edit.show || createShow || boosterPending || skillPending || triggerPending" @click="save">保存物品</NButton></div>
    </header>
    <NAlert v-if="parsed.error" type="error" title="无法解析物品">{{ parsed.error }}</NAlert>
    <div v-else class="item-layout" :class="{ 'with-preview': equipment }">
      <div class="item-fields">
        <section v-for="group in groups" :key="group.label" class="field-group">
          <h3>{{ group.label }}</h3>
          <div class="field-grid">
            <div v-for="field in group.fields" :key="field.name" class="field" :class="{ wide: field.multiline }">
              <div class="field-label"><span>{{ field.label }}</span><NButton size="tiny" secondary :aria-label="`编辑${field.label}`" :disabled="disabled || boosterPending || skillPending || triggerPending || !!itemFieldIssue(parsed.sections, field)" @click="open(field)">{{ itemSection(parsed.sections, field.name) ? '编辑' : '添加' }}</NButton></div>
              <div class="field-value" :class="{ empty: !itemSection(parsed.sections, field.name) }">{{ display(field) }}</div>
              <code>[{{ field.name }}]</code>
              <small v-if="itemFieldIssue(parsed.sections, field)">{{ itemFieldIssue(parsed.sections, field) }}</small>
            </div>
          </div>
        </section>
        <ItemBoosterEditor v-if="!equipment" :file="file" :active="active" @pending="boosterPending = $event" />
        <ItemSkillEffectsEditor v-if="equipment" :file="file" :active="active" :busy="edit.show || createShow || triggerPending" @pending="skillPending = $event" />
        <ItemTriggersEditor v-if="equipment" :file="file" :active="active" :busy="edit.show || createShow || skillPending || boosterPending" @pending="triggerPending = $event" />
      </div>
      <aside v-if="equipment" class="preview"><h3>装备预览</h3><EquipmentPreview :file="file" :active="active" /></aside>
    </div>
    <ItemCreateDialog v-model:show="createShow" :file="file" :active="active" :initial-name="display(titleField)" @created="openCreated" />
    <NModal :show="edit.show && active" @update:show="edit.show = $event" preset="card" :title="`编辑${edit.field?.label ?? '字段'}`" style="width: min(560px, 92vw)" :mask-closable="false" :close-on-esc="false">
      <div v-if="edit.field" class="edit-fields">
        <NAlert type="info" :show-icon="false">数值使用脚本原始单位。多值属性按原顺序填写，应用后可在文本编辑器中撤销。</NAlert>
        <NSelect v-if="!itemSection(parsed.sections, edit.field.name) && (edit.field.counts || edit.field.repeat)" :value="edit.values.length" :options="(edit.field.counts ?? [1, 2, 3, 4, 5, 6, 7, 8, 16]).map(n => ({ label: `${n} 个参数`, value: n }))" aria-label="参数数量" @update:value="resize" />
        <label v-for="(_, i) in edit.values" :key="i" class="parameter">
          <span>{{ edit.values.length > 1 ? `参数 ${i + 1}` : edit.field.label }} · {{ (edit.field.kinds[i] ?? edit.field.repeat) === 'integer' ? '整数' : '文本' }}</span>
          <NSelect v-if="options(i)" :value="edit.values[i]" :options="options(i)" filterable tag :input-props="{ 'aria-label': edit.field.label }" @update:value="edit.values[i] = $event" />
          <NInput v-else v-model:value="edit.values[i]" :type="edit.field.multiline ? 'textarea' : 'text'" :autosize="edit.field.multiline ? { minRows: 4, maxRows: 12 } : false" :input-props="{ 'aria-label': `${edit.field.label}参数${i + 1}` }" :placeholder="(edit.field.kinds[i] ?? edit.field.repeat) === 'integer' ? '请输入整数' : '请输入文本或 [类型名称]'" />
        </label>
      </div>
      <template #footer><div class="actions"><NButton @click="edit.show = false">取消</NButton><NButton type="primary" :disabled="disabled" @click="apply">应用修改</NButton></div></template>
    </NModal>
  </section>
</template>

<style scoped>
.item-viewer { width: 100%; max-width: 1440px; color: var(--pvf-text-primary); }
.item-header { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 14px 0 20px; border-bottom: 1px solid var(--pvf-border-subtle); margin-bottom: 20px; }
.item-heading { min-width: 0; } .title { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
h2 { margin: 0; font-size: 18px; overflow-wrap: anywhere; } h3 { margin: 0 0 14px; font-size: 14px; font-weight: 600; }
.path { color: var(--pvf-text-muted); font-size: 12px; margin-top: 8px; overflow-wrap: anywhere; }
.actions { display: flex; justify-content: flex-end; gap: 8px; flex-shrink: 0; }
.item-layout { display: grid; gap: 24px; align-items: start; }
.with-preview { grid-template-columns: minmax(0, 1fr) 310px; }
.field-group { margin-bottom: 24px; } .field-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0 20px; }
.field { min-width: 0; padding: 12px 0; border-top: 1px solid var(--pvf-border-subtle); display: grid; gap: 6px; align-content: start; }
.wide { grid-column: 1 / -1; } .field-label { display: flex; justify-content: space-between; align-items: center; gap: 8px; font-size: 13px; }
.field-value { white-space: pre-wrap; overflow-wrap: anywhere; font-size: 13px; max-height: 180px; overflow-y: auto; }
.empty, code { color: var(--pvf-text-muted); } code { font-size: 11px; } small { color: var(--pvf-warning); }
.preview { position: sticky; top: 0; min-width: 0; } .edit-fields { display: grid; gap: 16px; }
.parameter { display: grid; gap: 8px; } .parameter > span { font-size: 12px; color: var(--pvf-text-secondary); }
@media (max-width: 1000px) { .with-preview { grid-template-columns: minmax(0, 1fr); } .preview { position: static; } }
@media (max-width: 600px) { .item-header { align-items: flex-start; flex-direction: column; } .field-grid { grid-template-columns: minmax(0, 1fr); } }
</style>
