<script setup lang="ts">
import { computed, defineAsyncComponent, onBeforeUnmount, ref, watch } from 'vue';
import { NAlert, NButton, NCheckbox, NCollapse, NCollapseItem, NInput, NModal, NSelect, NTag } from 'naive-ui';
import type { GUIFile } from '../../gui/types';
import { parseItem, type ItemSection } from '../../gui/item';
import { editItemTrigger, readItemTriggers, triggerClauses, triggerClauseSpec, triggerClauseRows, type TriggerEdit } from '../../gui/itemTriggers';
import { useEditorStore } from '../../stores/editor';
import { useFileGUIStore } from '../../stores/fileGUI';
import EffectResourcePicker from './EffectResourcePicker.vue';
const ItemEffectPreview = defineAsyncComponent(() => import('./ItemEffectPreview.vue'));
const props = defineProps<{ file: GUIFile; active: boolean; busy?: boolean }>();
const emit = defineEmits<{ (e: 'pending', value: boolean): void }>();
const editor = useEditorStore(), gui = useFileGUIStore();
const document = computed(() => readItemTriggers(props.file.text));
const disabled = computed(() => !props.active || !props.file.editable || props.busy || editor.saving || editor.guiApplying);
interface Form { action: TriggerEdit; source: string; epoch: number; index: number; path: string; name: string; rows: string[][]; title: string; scope: 'if' | 'then'; multiple: boolean }
const form = ref<Form | null>(null), error = ref(''), preview = ref<{objectID: string; particle: string} | null>(null);
const specification = computed(() => triggerClauses.find(spec => spec.scope === form.value?.scope && spec.name === form.value?.name));
const commandAvailable = computed(() => { try { return parseItem(props.file.text).some(section => section.name === 'command' && section.tokens.length > 0); } catch { return false; } });
const selection = computed(() => triggerClauses.filter(spec => spec.scope === form.value?.scope && (spec.name !== 'use command' || commandAvailable.value)).map(spec => ({ value: spec.name, label: `${spec.label} [${spec.name}]` })));
const confirmation = computed(() => !!form.value && ['delete-trigger', 'delete-clause', 'delete-row', 'delete-branch', 'add-branch'].includes(form.value.action.kind));
let unregister: (() => void) | undefined;
watch(() => props.file.index, index => { unregister?.(); unregister = editor.registerGUIDirtyChecker(index, () => !!form.value); }, {immediate: true});
watch(() => !!form.value, value => emit('pending', value), {flush: 'sync'});
watch(() => [gui.epoch, props.file.index, props.file.path], () => { form.value = null; preview.value = null; error.value = ''; });
watch(() => props.file.text, () => { if (form.value) { form.value = null; error.value = '物品草稿已变化，请重新编辑触发器。'; } preview.value = null; });
watch(() => props.active, value => { if (!value) preview.value = null; });
onBeforeUnmount(() => { unregister?.(); emit('pending', false); });
function spec(scope: 'if' | 'then', section: ItemSection) { return triggerClauseSpec(scope, section); }
function rows(scope: 'if' | 'then', section: ItemSection) { const definition = spec(scope, section); return definition ? triggerClauseRows(section, definition) : null; }
function summary(section: ItemSection) { return section.tokens.map(token => token.value || '（空）').join(' · '); }
function open(action: TriggerEdit, title: string, scope: 'if' | 'then' = 'if') {
  if (disabled.value || form.value) return;
  let name = action.kind === 'add-trigger' ? action.condition : action.kind === 'add-clause' ? action.name : '';
  let values = action.kind === 'add-trigger' || action.kind === 'add-clause' ? action.rows : [];
  if (action.kind === 'edit-clause') {
    const trigger = document.value.triggers[action.trigger];
    const section = (action.branch === undefined ? trigger?.condition : trigger?.branches[action.branch])?.children[action.clause];
    if (!section) return;
    name = section.name; values = rows(scope, section) ?? [];
    if (!values.length) return;
  }
  form.value = {action, title, scope, name, rows: values.map(row => [...row]), source: props.file.text, epoch: gui.epoch, index: props.file.index, path: props.file.path, multiple: action.kind === 'add-trigger' && action.multiple}; error.value = '';
}
function choose(name: string) {
  if (!form.value) return;
  const definition = triggerClauses.find(spec => spec.scope === form.value!.scope && spec.name === name);
  if (!definition) return;
  form.value.name = name; form.value.rows = [definition.params.map(param => param.default)];
}
function addClause(trigger: number, branch?: number) {
  const scope = branch === undefined ? 'if' : 'then', name = scope === 'if' ? 'cooltime' : 'passive object';
  const definition = triggerClauses.find(spec => spec.scope === scope && spec.name === name)!;
  open({kind: 'add-clause', trigger, branch, name, rows: [definition.params.map(param => param.default)]}, scope === 'if' ? '添加触发条件' : '添加动作或设置', scope);
}
function apply() {
  const current = form.value;
  if (!current || disabled.value) return;
  try {
    const tab = editor.getFileDraft(props.file.index);
    if (current.epoch !== gui.epoch || current.index !== props.file.index || current.path !== props.file.path || !tab || tab.path !== current.path || tab.text !== current.source || props.file.text !== current.source) throw new Error('触发器草稿已变化，请重新编辑');
    let action = current.action;
    if (action.kind === 'add-trigger') action = {...action, condition: current.name, rows: current.rows, multiple: current.multiple};
    else if (action.kind === 'add-clause') action = {...action, name: current.name, rows: current.rows};
    else if (action.kind === 'edit-clause') action = {...action, rows: current.rows};
    editor.updateContent(props.file.index, editItemTrigger(current.source, action)); form.value = null; error.value = '';
  } catch (cause) { error.value = String(cause); }
}
function resize(count: number) { if (form.value && specification.value) form.value.rows = form.value.rows.map(row => Array.from({length: count}, (_, i) => row[i] ?? specification.value!.params[i]!.default)); }
function previewRow(values: string[]) { preview.value = {objectID: values[0] ?? '', particle: values[6] ?? ''}; }
function previewParticle(value: string) { preview.value = {objectID: '', particle: value}; }
</script>
<template>
  <section class="item-triggers" aria-label="物品触发器">
    <header><h3>触发器与特效</h3><NButton size="small" secondary :disabled="disabled || !!form || !!document.issues.length" @click="open({kind:'add-trigger',condition:'attack success',rows:[['1']],multiple:false},'新增触发器')">新增触发器</NButton></header>
    <NAlert v-for="(issue,i) in document.issues" :key="i" type="warning" :show-icon="false">{{ issue }}</NAlert>
    <p v-if="!document.triggers.length" class="hint">尚未配置触发器。新增后可叠加触发条件、配置动作和随机分支。</p>
    <NCollapse :default-expanded-names="[0]">
      <NCollapseItem v-for="(trigger,ti) in document.triggers" :key="trigger.condition.start" :name="ti" :title="`触发器 ${ti+1} · ${trigger.condition.children.map(child => spec('if', child)?.label ?? '['+child.name+']').join('、') || '未配置条件'}`">
        <div class="trigger-content">
          <div class="section-heading"><strong>触发条件</strong><div class="actions"><NButton size="tiny" :disabled="disabled || !!form" @click="addClause(ti)">添加条件</NButton><NButton size="tiny" :disabled="disabled || !!form" @click="open({kind:'delete-trigger',trigger:ti},'删除整个触发器？')">删除触发器</NButton></div></div>
          <div v-for="(clause,ci) in trigger.condition.children" :key="clause.start" class="clause">
            <div class="clause-info"><span>{{ spec('if',clause)?.label ?? '特殊条件' }}</span><code>[{{clause.name}}]</code><small>{{summary(clause) || '嵌套结构'}}</small></div>
            <div class="actions" v-if="rows('if',clause)"><NButton size="tiny" :disabled="disabled || !!form" @click="open({kind:'edit-clause',trigger:ti,clause:ci,rows:[]},'编辑触发条件')">编辑</NButton><NButton size="tiny" :disabled="disabled || !!form" @click="open({kind:'delete-clause',trigger:ti,clause:ci},'删除此触发条件？')">删除</NButton></div><NTag v-else size="small">DSL 编辑</NTag>
          </div>
          <div v-for="(branch,bi) in trigger.branches" :key="branch.start" class="branch">
            <div class="section-heading"><strong>{{trigger.multiple ? `随机分支 ${bi+1}` : '执行动作'}}</strong><div class="actions"><NButton size="tiny" :disabled="disabled || !!form" @click="addClause(ti,bi)">添加动作 / 设置</NButton><NButton v-if="trigger.branches.length>1" size="tiny" :disabled="disabled || !!form" @click="open({kind:'delete-branch',trigger:ti,branch:bi},'删除此分支及其中的动作？')">删除分支</NButton></div></div>
            <div v-for="(clause,ci) in branch.children" :key="clause.start" class="clause-block">
              <div class="clause"><div class="clause-info"><span>{{spec('then',clause)?.label ?? '特殊动作'}}</span><code>[{{clause.name}}]</code><small v-if="clause.name!=='passive object'">{{summary(clause) || '嵌套结构'}}</small></div><div class="actions" v-if="rows('then',clause)"><NButton size="tiny" :disabled="disabled || !!form" @click="open({kind:'edit-clause',trigger:ti,branch:bi,clause:ci,rows:[]},'编辑动作 / 设置','then')">编辑</NButton><NButton size="tiny" :disabled="disabled || !!form" @click="open({kind:'delete-clause',trigger:ti,branch:bi,clause:ci},'删除此动作 / 设置？','then')">删除</NButton></div><NTag v-else size="small">DSL 编辑</NTag></div>
              <div v-if="clause.name==='passive object' && rows('then',clause)" class="objects">
                <div v-for="(values,ri) in rows('then',clause)" :key="ri" class="object-row"><span>对象 #{{values[0]}} · 坐标 {{values.slice(1,4).join(', ')}} · 等级 {{values[4]}}<small>粒子：{{values[6] || '无'}}</small></span><div class="actions"><NButton size="tiny" :disabled="!active" @click="previewRow(values)">预览</NButton><NButton size="tiny" :disabled="disabled || !!form" @click="open({kind:'delete-row',trigger:ti,branch:bi,clause:ci,row:ri},'删除此被动对象记录？','then')">删除记录</NButton></div></div>
              </div>
            </div>
          </div>
          <div class="actions"><NButton size="small" :disabled="disabled || !!form" @click="open({kind:'add-branch',trigger:ti},'添加随机分支？')">添加随机分支</NButton><span class="hint">新增分支概率为 0，请按需要配置「分支概率」。</span></div>
        </div>
      </NCollapseItem>
    </NCollapse>
    <NAlert v-if="error && !form" type="error" :show-icon="false">{{error}}</NAlert>
    <NModal :show="!!form && active" preset="card" :title="form?.title" style="width:min(760px,94vw)" :mask-closable="false" :close-on-esc="false" @update:show="value=>{if(!value)form=null;}">
      <template v-if="form">
        <NAlert v-if="error" type="error">{{error}}</NAlert>
        <p v-if="confirmation" class="hint">{{form.action.kind==='add-branch' ? '普通触发器将转换为多分支结构，保留已有条件和动作；新分支概率为 0。' : '应用后修改当前物品草稿，可通过文本编辑器撤销。'}}</p>
        <div v-else class="form">
          <NSelect v-if="form.action.kind==='add-trigger' || form.action.kind==='add-clause'" :value="form.name" :options="selection" filterable aria-label="触发器字段类型" @update:value="choose" />
          <NCheckbox v-if="form.action.kind==='add-trigger'" v-model:checked="form.multiple">使用随机多分支结构</NCheckbox>
          <template v-if="specification">
            <p class="hint">参数使用脚本原始单位。职业、目标、状态等字符串保留当前客户端使用的 token。</p>
            <NSelect v-if="specification.counts && form.action.kind!=='edit-clause'" :value="form.rows[0]?.length" :options="specification.counts.map(value=>({value,label:`${value} 个参数`}))" aria-label="动作参数数量" @update:value="resize" />
            <div v-for="(row,ri) in form.rows" :key="ri" class="parameter-row"><strong v-if="specification.repeat">记录 {{ri+1}}</strong><div class="parameters">
              <label v-for="(value,pi) in row" :key="pi" :class="{wide: specification.params[pi]?.resource}"><span>{{specification.params[pi]?.label}}</span><EffectResourcePicker v-if="specification.params[pi]?.resource" v-model="row[pi]!" :kind="specification.params[pi]!.resource!" :disabled="!!disabled" /><NInput v-else v-model:value="row[pi]!" :disabled="!!disabled" :input-props="{'aria-label':specification.params[pi]?.label}" /></label>
            </div><div v-if="specification.name==='passive object'" class="actions"><NButton size="small" :disabled="!row[0]" @click="previewRow(row)">预览对象与粒子</NButton><NButton size="small" :disabled="!row[6]" @click="previewParticle(row[6]!)">单独预览粒子</NButton></div></div>
            <NButton v-if="specification.repeat" size="small" :disabled="!!disabled" @click="form.rows.push(specification.params.map(param=>param.default))">追加一条记录</NButton>
          </template>
        </div>
      </template>
      <template #footer><div class="actions"><NButton @click="form=null;error=''">取消</NButton><NButton :type="form?.action.kind.startsWith('delete')?'error':'primary'" :disabled="!!disabled" @click="apply">{{confirmation?'确认':'应用修改'}}</NButton></div></template>
    </NModal>
    <ItemEffectPreview v-if="preview" :show="true" :active="active" :file-index="file.index" :object-i-d="preview.objectID" :particle="preview.particle" @update:show="value=>{if(!value)preview=null;}" />
  </section>
</template>
<style scoped>
.item-triggers{margin-top:24px;border-top:1px solid var(--pvf-border-subtle);padding-top:18px;display:grid;gap:12px;color:var(--pvf-text-primary)}header,.section-heading,.clause,.object-row{display:flex;align-items:center;justify-content:space-between;gap:12px}header{flex-wrap:wrap}h3{margin:0;font-size:14px}.actions{display:flex;gap:6px;flex-wrap:wrap;align-items:center}.trigger-content{display:grid;gap:12px}.section-heading strong{font-size:13px}.branch{border-left:2px solid var(--pvf-border-normal);padding:10px 0 10px 14px;display:grid;gap:8px}.clause{padding:8px 0;border-bottom:1px solid var(--pvf-border-subtle)}.clause-info{min-width:0;display:grid;gap:4px;font-size:13px}.clause-info small,code,.hint,.object-row small{font-size:12px;color:var(--pvf-text-muted);overflow-wrap:anywhere}.objects{padding:6px 0;display:grid;gap:8px}.object-row{font-size:12px}.object-row small{display:block;margin-top:4px}.form{display:grid;gap:14px}.parameters{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}.parameters label{display:grid;gap:6px;font-size:12px;color:var(--pvf-text-secondary)}.wide{grid-column:1/-1}.parameter-row{display:grid;gap:12px;padding-bottom:12px;border-bottom:1px solid var(--pvf-border-subtle)}.hint{margin:0;line-height:1.7}@media(max-width:600px){.parameters{grid-template-columns:1fr}.section-heading,.clause,.object-row{align-items:flex-start;flex-direction:column}}
</style>
