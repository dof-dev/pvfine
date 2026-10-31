<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from "vue";
import { NAlert, NButton, NCheckbox, NEmpty, NInput, NInputNumber, NModal, NPagination, NSelect, NSpin, NTag, useDialog, useMessage } from "naive-ui";
import { FileGUIService } from "../../../bindings/pvfine/services";
import type { IndependentDropDocument, IndependentDropRow } from "../../../bindings/pvfine/services/models";
import type { GUIFile } from "../../gui/types";
import { applyDropPatches, convertDropRecord, dropListText, dropModeLabel, dropRecordText, editableDropItems, editDropFields, independentDropModes, replaceDropSpan, sourceOffset } from "../../gui/independentDrop";
import { useEditorStore } from "../../stores/editor";
import { useFileGUIStore } from "../../stores/fileGUI";
import ItemPicker from "../ItemPicker.vue";

const props = defineProps<{ file: GUIFile; active: boolean }>();
const editor = useEditorStore(), gui = useFileGUIStore(), message = useMessage(), dialog = useDialog();
const document = ref<IndependentDropDocument | null>(null), error = ref(""), loading = ref(false), sourceText = ref("");
const query = ref(""), mode = ref<number | null>(null), selected = ref(0), page = ref(1), itemPage = ref(1);
const rows = computed(() => document.value?.rows ?? []);
const filtered = computed(() => rows.value.map((row,index)=>({row,index})).filter(({row}) => {
  if (mode.value !== null && row.fields?.[16]?.value !== mode.value) return false;
  const key = query.value.trim().toLowerCase();
  return !key || [row.monsterName,row.itemName,...(row.fields ?? []).slice(0,3).map(f=>String(f.value)),...(row.items ?? []).flatMap(item=>[item.name,...(item.fields ?? []).slice(0,1).map(f=>String(f.value))])].some(v=>v.toLowerCase().includes(key));
}));
const visibleRows = computed(() => filtered.value.slice((page.value-1)*60,page.value*60));
const row = computed(() => rows.value[selected.value]);
const modeOptions = computed(() => [...independentDropModes,...[...new Set(rows.value.map(r=>r.fields?.[16]?.value ?? 0))].filter(n=>![0,1,2].includes(n)).map(n=>({label:dropModeLabel(n),value:n}))]);
const ready = computed(() => !loading.value && !error.value && sourceText.value===props.file.text);
const canEdit = computed(() => ready.value && props.file.editable && !editor.saving && !editor.guiApplying);
const dirty = computed(() => {const tab=editor.getFileDraft(props.file.index);return !!tab && editor.isTextDirty(tab);});
const listItems = computed(() => (row.value?.items ?? []).filter(item=>item.fields?.length===2));
const visibleItems = computed(() => listItems.value.slice((itemPage.value-1)*50,itemPage.value*50));
const external = computed(() => document.value?.lists?.find(list=>list.id===String(row.value?.items?.[0]?.fields?.[0]?.value)));
const form = reactive({ show:false, kind:"record" as "record"|"new"|"list"|"bulk"|"balance", text:"", epoch:0, index:-1, target:0, values:[] as (number|null)[], items:"", listID:0 as number|null, itemID:"", columns:[] as number[], targets:[] as IndependentDropRow[] });
const fieldLabels = ["来源类型","怪物 / APC ID","物品 ID","难度 1 概率（%）","难度 2 概率（%）","难度 3 概率（%）","难度 4 概率（%）","难度 5 概率（%）","难度 1 数量","难度 2 数量","难度 3 数量","难度 4 数量","难度 5 数量","最低等级","最高等级","职业 ID（-1 为不限）"];
const formTitle = computed(() => ({record:"编辑掉落记录",new:"新增掉落记录",list:"编辑掉落列表",bulk:`批量设置 · ${form.targets.length} 条筛选结果`,balance:"副本掉率平衡参数"})[form.kind]);
let generation=0, timer:ReturnType<typeof setTimeout>|undefined;
async function load() {
  const request=++generation, epoch=gui.epoch, text=props.file.text, index=props.file.index;
  loading.value=true;error.value="";
  try {
    const result=await FileGUIService.ReadIndependentDrop(index,text);
    if (request!==generation || epoch!==gui.epoch || text!==props.file.text || index!==props.file.index) return;
    if (!result) throw new Error("未返回独立掉落数据");
    document.value=result;sourceText.value=text;selected.value=Math.min(Math.max(0,selected.value),Math.max(0,(result.rows?.length ?? 0)-1));
  } catch(cause) {if(request===generation && epoch===gui.epoch){error.value=String(cause);document.value=null;}}
  finally {if(request===generation)loading.value=false;}
}
watch(()=>[props.file.index,props.file.text,gui.epoch,gui.revision,props.active],()=>{generation++;clearTimeout(timer);form.show=false;if(props.active)timer=setTimeout(load,80);},{immediate:true});
watch(()=>gui.epoch,()=>{document.value=null;});
watch([query,mode],()=>{page.value=1;});
watch(selected,()=>{itemPage.value=1;});
watch(filtered, entries=>{
  page.value=Math.min(page.value,Math.max(1,Math.ceil(entries.length/60)));
  if(!entries.some(entry=>entry.index===selected.value))selected.value=entries[0]?.index ?? -1;
});
watch(()=>props.file.index,()=>{selected.value=0;page.value=1;query.value="";mode.value=null;});
onBeforeUnmount(()=>{generation++;clearTimeout(timer);});
function value(r:IndependentDropRow|undefined,column:number){return r?.fields?.[column]?.value ?? 0;}
function itemsText(r:IndependentDropRow){return editableDropItems(r.items ?? []).map(item=>`${item.id}\t${item.weight}`).join("\n");}
function openForm(kind:typeof form.kind) {
  if (!canEdit.value || !document.value) return;
  const r=row.value;
  Object.assign(form,{show:true,kind,text:props.file.text,epoch:gui.epoch,index:props.file.index,target:selected.value,columns:[],targets:filtered.value.map(entry=>entry.row),itemID:"",
    values:kind==="balance" ? (document.value.balance ?? []).map(f=>f.value) : kind==="new" ? [0,-1,0,1000000,1000000,1000000,1000000,1000000,1,1,1,1,1,0,0,-1,0] : (r?.fields ?? []).map(f=>f.value),
    items:kind!=="new" && r ? itemsText(r):"",listID:r?.items?.[0]?.fields?.[0]?.value ?? 0});
}
function parseItems(){
  return form.items.split(/\r?\n/).filter(line=>line.trim()).map(line=>{const parts=line.trim().split(/[\s,，]+/);if(parts.length!==2)throw new Error("每行请输入物品 ID 和权重，用空格或制表符分隔");return {id:Number(parts[0]),weight:Number(parts[1])};});
}
function checkedText(){
  if (!canEdit.value || form.text!==props.file.text || form.epoch!==gui.epoch || form.index!==props.file.index) throw new Error("文件已变化，请重新打开表单");
  return form.text;
}
function apply() {
  try {
    let text=checkedText();const r=rows.value[form.target];
    if(form.kind==="new") {
      const values=form.values.map(n=>n??NaN);if(values[16]===2 && !document.value?.lists?.some(list=>list.id===String(form.listID) && list.fileIndex>=0))throw new Error("外部列表 ID 不存在");
      const at=document.value!.bodyEnd;text=replaceDropSpan(text,at,at,"\n\t"+dropRecordText(values,parseItems(),form.listID??NaN));
    } else if(form.kind==="list") {
      if(!r || r.listStart<0)throw new Error("列表结构已变化");
      if(!document.value?.isList && value(r,16)===2) {
        if(!document.value?.lists?.some(list=>list.id===String(form.listID) && list.fileIndex>=0))throw new Error("外部列表 ID 不存在");
        text=applyDropPatches(text,[{token:r.items![0]!.fields![0]!,value:form.listID??NaN}]);
      } else {const content=dropListText(parseItems());text=replaceDropSpan(text,r.listStart,r.listEnd,"\n"+content+"\n\t");}
    } else if(form.kind==="balance") {
      text=applyDropPatches(text,(document.value?.balance ?? []).map((token,i)=>{const n=form.values[i];if(n===null||n===undefined||!Number.isInteger(n)||n<0||n>2147483647)throw new Error("平衡参数必须是非负整数");return {token,value:n};}));
    } else if(form.kind==="record" && r && form.values[16]!==value(r,16)) {
      if(r.hasExtensions || value(r,16)>2)throw new Error("扩展记录不能转换掉落模式");
      const values=form.values.map(n=>n??NaN);
      if(values[16]===2 && !document.value?.lists?.some(list=>list.id===String(form.listID) && list.fileIndex>=0))throw new Error("外部列表 ID 不存在");
      text=convertDropRecord(text,r,values,parseItems(),form.listID??NaN);
    } else {
      const columns=form.kind==="bulk" ? form.columns : fieldLabels.map((_,i)=>i);
      if(!columns.length)throw new Error("请选择修改项");
      const targets=form.kind==="bulk" ? form.targets : r ? [r] : [];
      // Collect each field edit in descending source order so earlier lengths cannot invalidate later offsets.
      for (const target of [...targets].sort((a,b)=>b.start-a.start)) {
        const changes=columns.filter(c=>form.kind==="bulk" || form.values[c]!==target.fields?.[c]?.value).map(column=>({column,value:form.values[column]??NaN}));
        if(changes.length)text=editDropFields(text,target,changes);
      }
    }
    editor.updateContent(props.file.index,text);form.show=false;message.success("已更新草稿");
  }catch(cause){message.error(String(cause));}
}
function structural(action:"copy"|"delete") {
  if(!canEdit.value||!row.value||!document.value)return;
  const r=row.value,text=props.file.text,epoch=gui.epoch;
  if(r.hasExtensions || ![0,1,2].includes(value(r,16))){message.warning("扩展记录请在文本模式中增删，避免丢失条件关联");return;}
  const perform=()=>{if(!canEdit.value||text!==props.file.text||epoch!==gui.epoch)return;
    const result=action==="delete" ? replaceDropSpan(text,r.start,r.end,"") : replaceDropSpan(text,r.end,r.end,text.slice(sourceOffset(text,r.start),sourceOffset(text,r.end)));
    editor.updateContent(props.file.index,result);
  };
  if(action==="delete")dialog.warning({title:"删除掉落记录？",content:"此操作将更新当前文件草稿，可通过文本编辑器撤销。",positiveText:"删除",negativeText:"取消",onPositiveClick:perform});else perform();
}
async function save(){if(!canEdit.value||form.show||!dirty.value)return;const epoch=gui.epoch;try{if(await editor.saveFileTab(props.file.index) && epoch===gui.epoch)message.success("已保存到工作区；保存 PVF 后写入归档");}catch(cause){if(epoch===gui.epoch)message.error(String(cause));}}
async function openExternal(){if(!external.value||external.value.fileIndex<0)return;await editor.openFile(external.value.fileIndex);editor.setGUIMode(external.value.fileIndex,"gui");}
function displayInput(column:number){const n=form.values[column];return n===null||n===undefined ? null : column>=3&&column<=7 && (form.values[16] ?? 0)<=2 ? n/10000 : n;}
function updateInput(column:number,n:number|null){form.values[column]=n===null?null:column>=3&&column<=7 ? Math.round(n*10000):n;}
function addItem(){const id=Number(form.itemID);if(!form.itemID||!Number.isInteger(id)||id<=0)return;form.items+=(form.items.trim()?"\n":"")+`${id}\t1`;form.itemID="";}
</script>

<template>
  <div class="independent-view">
    <header class="drop-heading"><div><h2>独立掉落</h2><span class="muted">{{ document?.isList ? '共享权重列表' : `${rows.length} 条记录` }}</span><NTag v-if="document?.version" size="small" :bordered="false">{{ document.version }}</NTag></div><div class="actions">
      <NButton v-if="document?.balance?.length" size="small" :disabled="!canEdit || form.show" @click="openForm('balance')">副本平衡参数</NButton>
      <NButton v-if="!document?.isList" size="small" :disabled="!canEdit || form.show" @click="openForm('new')">新增记录</NButton>
      <NButton size="small" type="primary" :loading="editor.saving" :disabled="!canEdit || !dirty || form.show" @click="save">保存独立掉落</NButton>
    </div></header>
    <NAlert v-if="error" type="error">{{ error }} <NButton size="tiny" @click="load">重新加载</NButton></NAlert>
    <NSpin :show="loading" class="drop-content">
      <div v-if="document" class="drop-layout" :class="{'list-only':document.isList}">
        <aside v-if="!document.isList" class="record-sidebar">
          <div class="search-controls"><NInput v-model:value="query" clearable placeholder="搜索怪物、物品名称或 ID" aria-label="搜索独立掉落" /><NSelect v-model:value="mode" :options="modeOptions" clearable placeholder="全部掉落模式" aria-label="筛选掉落模式" /><div class="result-count"><span>{{ filtered.length }} 条结果</span><NButton text size="tiny" :disabled="!canEdit || !filtered.length || form.show" @click="openForm('bulk')">批量设置</NButton></div></div>
          <div class="record-list"><button v-for="entry in visibleRows" :key="entry.index" class="record" :class="{selected:selected===entry.index}" @click="selected=entry.index"><strong>{{ entry.row.monsterName || (value(entry.row,0)===1 ? 'APC' : '怪物') }} <small>#{{ value(entry.row,1) }}</small></strong><span>{{ dropModeLabel(value(entry.row,16)) }}<template v-if="value(entry.row,16)===0"> · {{ entry.row.itemName || `物品 #${value(entry.row,2)}` }}</template><template v-else> · {{ entry.row.items?.length ?? 0 }} 项</template></span><small>等级 {{ value(entry.row,13) }}～{{ value(entry.row,14) }} · 第 {{ entry.index+1 }} 条</small></button><NEmpty v-if="!visibleRows.length" description="没有匹配记录" /></div>
          <NPagination v-model:page="page" :item-count="filtered.length" :page-size="60" :page-slot="3" size="small" simple />
        </aside>
        <main class="record-detail">
          <template v-if="row">
            <div class="detail-heading"><div><h3>{{ document.isList ? '掉落列表' : row.monsterName || `来源 #${value(row,1)}` }}</h3><span v-if="!document.isList" class="muted">{{ value(row,0)===1 ? 'APC' : '怪物' }} #{{ value(row,1) }} · {{ dropModeLabel(value(row,16)) }}</span></div><div v-if="!document.isList" class="actions"><NButton size="small" :disabled="!canEdit || form.show" @click="openForm('record')">编辑记录</NButton><NButton size="small" :disabled="!canEdit || form.show || row.hasExtensions || value(row,16)>2" @click="structural('copy')">复制</NButton><NButton size="small" type="error" quaternary :disabled="!canEdit || form.show || row.hasExtensions || value(row,16)>2" @click="structural('delete')">删除</NButton></div></div>
            <NAlert v-if="!document.isList && (row.hasExtensions || value(row,16)>2)" type="info" :show-icon="false">此记录包含扩展掉落行为，模式与条件保持原样。概率字段的扩展含义尚未确认，以下显示原始数值。</NAlert>
            <template v-if="!document.isList">
              <div class="record-summary"><div><small>掉落物品</small><strong>{{ value(row,16)===0 ? row.itemName || `物品 #${value(row,2)}` : dropModeLabel(value(row,16)) }}</strong></div><div><small>等级范围</small><strong>{{ value(row,13) }}～{{ value(row,14) }}</strong></div><div><small>职业限制</small><strong>{{ value(row,15)===-1 ? '不限' : value(row,15) }}</strong></div></div>
              <table class="drop-table"><thead><tr><th>难度档位</th><th>{{ value(row,16)>2 ? '原始概率参数' : '掉落概率' }}</th><th>掉落数量</th></tr></thead><tbody><tr v-for="i in 5" :key="i"><th>难度 {{ i }}</th><td>{{ value(row,16)>2 ? value(row,i+2) : `${Number((value(row,i+2)/10000).toFixed(4))}%` }}</td><td>{{ value(row,i+7) }}</td></tr></tbody></table>
              <p class="muted explanation">各难度独立生效，无需合计为 100%。档位名称可能随客户端变化，以原始顺序显示。</p>
            </template>
            <section v-if="row.listStart>=0" class="items-section"><div class="detail-heading"><h3>{{ !document.isList && value(row,16)===2 ? '外部列表引用' : '物品与权重' }}</h3><NButton size="small" :disabled="!canEdit || form.show || (!document.isList && value(row,16)>2)" @click="openForm('list')">{{ !document.isList && value(row,16)===2 ? '更换列表' : '编辑列表' }}</NButton></div>
              <template v-if="!document.isList && value(row,16)===2"><p>列表 ID：{{ row.items?.[0]?.fields?.[0]?.value }}</p><p class="muted">{{ external?.path || '列表未注册' }}</p><NButton :disabled="!external || external.fileIndex<0" @click="openExternal">打开共享列表</NButton><p class="muted">修改共享列表会影响引用它的所有记录。</p></template>
              <template v-else><table class="drop-table"><thead><tr><th>物品</th><th>ID</th><th>权重</th></tr></thead><tbody><tr v-for="item in visibleItems" :key="item.fields?.[0]?.start"><td>{{ item.name || '未知物品' }}</td><td>{{ item.fields?.[0]?.value }}</td><td>{{ item.fields?.[1]?.value }}</td></tr></tbody></table><NEmpty v-if="!listItems.length" description="列表为空" /><NPagination v-if="listItems.length>50" v-model:page="itemPage" :item-count="listItems.length" :page-size="50" size="small" /><p class="muted">权重表示相对比例，不等于最终掉落概率。</p></template>
            </section>
          </template>
          <NEmpty v-else description="暂无独立掉落记录，可点击新增记录" />
        </main>
      </div>
    </NSpin>
    <NModal v-model:show="form.show" preset="card" :title="formTitle" style="width: 760px; max-width: 95vw" :mask-closable="false">
      <div class="edit-form">
        <NAlert v-if="form.kind==='bulk'" type="info" :show-icon="false">只修改当前筛选结果的勾选项。五档概率与数量分别设置。</NAlert>
        <template v-if="form.kind==='record' || form.kind==='new' || form.kind==='bulk'">
          <div v-if="form.kind!=='bulk'" class="field"><label>掉落模式</label><NSelect v-model:value="form.values[16]" :options="form.kind==='new' ? independentDropModes : modeOptions" :disabled="form.kind==='record' && (!!row?.hasExtensions || value(row,16)>2)" /></div>
          <div class="field-grid"><template v-for="(label,column) in fieldLabels" :key="column"><div v-if="form.kind!=='bulk' || column>=3 && column<=14" class="field"><NCheckbox v-if="form.kind==='bulk'" :checked="form.columns.includes(column)" @update:checked="checked=>form.columns=checked ? [...form.columns,column] : form.columns.filter(c=>c!==column)">{{ label }}</NCheckbox><label v-else>{{ column>=3 && column<=7 && form.kind==='record' && (form.values[16] ?? 0)>2 ? `难度 ${column-2} 原始概率参数` : label }}</label>
            <NSelect v-if="column===0" v-model:value="form.values[column]" :options="[{label:'怪物',value:0},{label:'APC',value:1}]" />
            <div v-else-if="column===2 && form.values[16]===0" class="item-input"><NInputNumber v-model:value="form.values[column]" :min="0" :max="2147483647" :precision="0" :aria-label="label" /><ItemPicker :model-value="String(form.values[2] ?? '')" @update:model-value="form.values[2]=Number($event)" /></div>
            <NInputNumber v-else :value="displayInput(column)" :min="column>=3 ? column===15 ? -1 : 0 : -2147483648" :max="column>=3 && column<=7 && (form.values[16] ?? 0)<=2 ? 100 : 2147483647" :precision="column>=3 && column<=7 && (form.values[16] ?? 0)<=2 ? 4 : 0" :disabled="column===2 && form.values[16]!==0 || form.kind==='bulk' && !form.columns.includes(column) || form.kind==='record' && (form.values[16] ?? 0)>2 && column>=3 && column<=7" :aria-label="label" @update:value="n=>updateInput(column,n)" />
          </div></template></div>
        </template>
        <div v-if="form.kind==='balance'" class="field-grid"><div v-for="(_,i) in form.values" :key="i" class="field"><label>原始参数 {{ i+1 }}</label><NInputNumber v-model:value="form.values[i]" :min="0" :max="2147483647" :precision="0" /></div><p class="muted">保持原始字段顺序；不同变体的参数含义可能不同。</p></div>
        <template v-if="form.kind==='list' || (form.kind==='new' || form.kind==='record' && form.values[16]!==value(row,16)) && form.values[16]!==0">
          <template v-if="form.kind!=='list' ? form.values[16]===2 : !document?.isList && value(row,16)===2"><label>外部列表 ID</label><NSelect v-model:value="form.listID" filterable :options="document?.lists?.map(list=>({label:`${list.id} · ${list.path}`,value:Number(list.id),disabled:list.fileIndex<0}))" /></template>
          <template v-else><p class="muted">每行填写“物品 ID 权重”。保留重复项与零权重，按原顺序保存。</p><div class="actions"><ItemPicker v-model="form.itemID" /><NButton :disabled="!form.itemID" @click="addItem">追加物品</NButton></div><NInput v-model:value="form.items" type="textarea" :autosize="{minRows:8,maxRows:16}" placeholder="3176 20&#10;3030 80" aria-label="掉落列表内容" /></template>
        </template>
        <div class="dialog-actions"><NButton @click="form.show=false">取消</NButton><NButton type="primary" :disabled="!canEdit" @click="apply">应用到草稿</NButton></div>
      </div>
    </NModal>
  </div>
</template>

<style scoped>
.independent-view{height:100%;display:flex;flex-direction:column;overflow:hidden;color:var(--pvf-text-primary)}
.drop-heading,.detail-heading{display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap}.drop-heading{padding:16px 20px;border-bottom:1px solid var(--pvf-border-subtle)}.drop-heading>div,.actions{display:flex;align-items:center;gap:8px;flex-wrap:wrap}h2,h3,p{margin:0}h2{font-size:17px}h3{font-size:15px}.muted{color:var(--pvf-text-muted);font-size:12px;line-height:1.6}.drop-content{flex:1;min-height:0;overflow:auto}.drop-content :deep(.n-spin-content){height:100%}.drop-layout{display:grid;grid-template-columns:280px minmax(0,1fr);height:100%;min-height:360px}.list-only{grid-template-columns:minmax(0,1fr)}.record-sidebar{border-right:1px solid var(--pvf-border-subtle);display:flex;flex-direction:column;min-height:0;background:var(--pvf-surface-subtle)}.search-controls{padding:12px;display:grid;gap:8px}.result-count{display:flex;justify-content:space-between;align-items:center;font-size:12px;color:var(--pvf-text-muted)}.record-list{flex:1;overflow:auto;padding:0 6px}.record{border:0;border-left:3px solid transparent;background:transparent;color:inherit;text-align:left;display:grid;gap:5px;width:100%;padding:10px;cursor:pointer;border-bottom:1px solid var(--pvf-border-subtle)}.record:hover{background:var(--pvf-surface-hover)}.record.selected{background:var(--pvf-surface-card);border-left-color:var(--pvf-primary,#18a058)}.record strong{font-size:13px;overflow-wrap:anywhere}.record span,.record small{color:var(--pvf-text-muted);font-size:12px}.record-sidebar>.n-pagination{padding:12px}.record-detail{overflow:auto;padding:20px;display:flex;flex-direction:column;gap:18px}.record-summary{display:flex;gap:28px;flex-wrap:wrap;padding:14px 0;border-block:1px solid var(--pvf-border-subtle)}.record-summary>div{display:grid;gap:5px}.record-summary small{color:var(--pvf-text-muted)}.record-summary strong{font-size:14px;font-weight:500}.drop-table{width:100%;border-collapse:collapse;font-size:13px;font-variant-numeric:tabular-nums}.drop-table th{text-align:left;font-weight:500;color:var(--pvf-text-secondary)}.drop-table td,.drop-table th{padding:10px 12px;border-bottom:1px solid var(--pvf-border-subtle)}.drop-table thead{background:var(--pvf-surface-subtle)}.drop-table tbody tr:hover{background:var(--pvf-surface-hover)}.items-section{display:grid;gap:12px}.explanation{margin-top:-10px}.edit-form{display:grid;gap:16px;max-height:70vh;overflow:auto}.field-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:14px 20px}.field{display:grid;gap:6px;font-size:12px}.item-input{display:flex;gap:6px;min-width:0}.item-input>.n-input-number{flex:1;min-width:0}.dialog-actions{display:flex;justify-content:flex-end;gap:8px}.record:focus-visible{outline:2px solid var(--pvf-primary,#18a058);outline-offset:-2px}
@media(max-width:800px){.drop-layout{grid-template-columns:220px minmax(0,1fr)}.record-detail{padding:12px}.drop-heading{padding:12px}}@media(max-width:580px){.drop-layout{display:flex;flex-direction:column;height:auto}.record-sidebar{max-height:300px;border-right:0;border-bottom:1px solid var(--pvf-border-subtle)}.field-grid{grid-template-columns:1fr}}
</style>
