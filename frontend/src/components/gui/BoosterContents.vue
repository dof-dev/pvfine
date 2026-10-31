<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue';
import { NAlert, NButton, NCheckbox, NIcon, NInput, NSelect } from 'naive-ui';
import { Add20Regular, Delete20Regular, Edit20Regular } from '@vicons/fluent';
import { FileGUIService } from '../../../bindings/pvfine/services';
import type { ItemAppearance } from '../../../bindings/pvfine/services/models';
import { boosterRewardName, isFlatBoosterSelection, newBoosterRow, type BoosterDocument, type BoosterRow } from '../../gui/itemBooster';
import type { BoosterCategoryAction, BoosterInlineAction } from '../../gui/itemBoosterActions';
import { useFileGUIStore } from '../../stores/fileGUI';
import ImageThumbnail from '../ImageThumbnail.vue';
import BoosterRewardFields from './BoosterRewardFields.vue';
import { rarityTextStyle } from '../../rarity';

type Form = {source:string;epoch:number} & (
 {kind:'item';action:'add-item'|'edit-item';group:number;index?:number;row:BoosterRow;label?:string;originalID:string} |
 {kind:'category';action:BoosterCategoryAction;name:string} |
 {kind:'settings';pool?:number;value:string} |
 {kind:'delete';action:BoosterInlineAction;title:string;description:string}
);
const props=defineProps<{
 document:BoosterDocument;active:boolean;rewardName:(id:string)=>string;
 editable?:boolean;sourceText?:string;identity?:string;
 applyChange?:(source:string,action:BoosterInlineAction)=>string;
}>();
const emit=defineEmits<{(e:'pending',value:boolean):void}>();
const gui=useFileGUIStore();
const contentsRoot=ref<HTMLDivElement|null>(null);
const category=ref(0),subcategory=ref(0),checked=ref<number[]>([]);
const form=ref<Form|null>(null),error=ref('');
const appearances=ref<Record<string,ItemAppearance|null|undefined>>({});
const categorized=computed(()=>props.document.mode==='selection'&&!isFlatBoosterSelection(props.document));
const twoLevels=computed(()=>categorized.value&&props.document.dimensions[0]==='2');
const categories=computed(()=>Array.from({length:Number(props.document.dimensions[1])||0},(_,i)=>({value:i,label:props.document.names[1+i]||`分类 ${i+1}`})));
const subcategories=computed(()=>Array.from({length:Number(props.document.dimensions[2])||0},(_,i)=>({value:i,label:props.document.names[2+Number(props.document.dimensions[1])+category.value*Number(props.document.dimensions[2])+i]||`子分类 ${i+1}`})).filter(option=>{
 const group=props.document.groups.find(g=>Number(g.coordinates[0])===category.value&&Number(g.coordinates[1])===option.value);
 return !!props.document.names[2+Number(props.document.dimensions[1])+category.value*Number(props.document.dimensions[2])+option.value]?.trim() || !!group?.rows.length || group?.equipmentUpgrade!==undefined;
}));
const group=computed(()=>categorized.value?props.document.groups.find(g=>Number(g.coordinates[0])===category.value&&Number(g.coordinates[1])===(twoLevels.value?subcategory.value:0)):props.document.groups[0]);
const groupIndex=computed(()=>group.value?props.document.groups.indexOf(group.value):-1);
const rows=computed(()=>props.document.mode==='booster'?[...(group.value?.rows??[])].sort((a,b)=>(a.poolIndex??0)-(b.poolIndex??0)):group.value?.rows??[]);
const limit=computed(()=>Math.max(1,Number(props.document.selection)||1));
const canEdit=computed(()=>!!props.editable&&props.active&&!!props.applyChange&&props.sourceText!==undefined);
const toolsDisabled=computed(()=>!canEdit.value||!!form.value);
const canAddItem=computed(()=>!!group.value&&(!twoLevels.value||!!props.document.names[2+Number(props.document.dimensions[1])+category.value*Number(props.document.dimensions[2])+subcategory.value]?.trim()));
function clampSelection(){
 if(!categories.value.some(option=>option.value===category.value))category.value=categories.value[0]?.value??0;
 if(!subcategories.value.some(option=>option.value===subcategory.value))subcategory.value=subcategories.value[0]?.value??0;
 checked.value=[];
}
watch(()=>props.document,clampSelection,{immediate:true});
watch(category,()=>{subcategory.value=subcategories.value[0]?.value??0;checked.value=[];});
watch(subcategory,()=>{checked.value=[];});
watch(()=>[props.identity,gui.epoch],()=>{category.value=0;subcategory.value=0;form.value=null;error.value='';},{flush:'sync'});
watch(()=>props.sourceText,()=>{if(form.value){form.value=null;error.value='文件草稿已变化，请重新编辑。';}});
watch(()=>!!form.value,value=>emit('pending',value));
watch(form,async value=>{if(!value)return;await nextTick();if(props.active)contentsRoot.value?.querySelector<HTMLElement>('.inline-form')?.scrollIntoView({block:'nearest'});});
function toggle(index:number,value:boolean){checked.value=value?[...checked.value,index]:checked.value.filter(i=>i!==index);}
function base(){error.value='';return{source:props.sourceText!,epoch:gui.epoch};}
function editItem(row?:BoosterRow,pool=0){
 if(toolsDisabled.value||!group.value)return;
 form.value={...base(),kind:'item',action:row?'edit-item':'add-item',group:groupIndex.value,index:row?group.value.rows.indexOf(row):undefined,row:row?JSON.parse(JSON.stringify(row)):newBoosterRow(props.document,group.value,pool),label:row?boosterRewardName(group.value,props.rewardName(row.id)):undefined,originalID:row?.id??''};
}
function editCategory(kind:BoosterCategoryAction['kind'],level:0|1){
 if(toolsDisabled.value)return;
 const index=level===0?category.value:subcategory.value;
 const option=(level===0?categories.value:subcategories.value).find(option=>option.value===index);
 const action:BoosterCategoryAction={kind,level,parent:category.value,index};
 if(kind==='delete-category'){
  const count=props.document.groups.filter(g=>Number(g.coordinates[0])===category.value&&(level===0||Number(g.coordinates[1])===subcategory.value)).reduce((total,g)=>total+g.rows.length,0);
  form.value={...base(),kind:'delete',action,title:`删除${level?'子分类':'分类'}“${option?.label}”`,description:`将移除该分类及其中的 ${count} 项物品。可通过撤销恢复。`};
 }else form.value={...base(),kind:'category',action,name:kind==='rename-category'?option?.label??'':''};
}
function deleteItem(row:BoosterRow){
 if(toolsDisabled.value||!group.value)return;
 form.value={...base(),kind:'delete',action:{kind:'delete-item',group:groupIndex.value,index:group.value.rows.indexOf(row)},title:`删除“${boosterRewardName(group.value,props.rewardName(row.id))}”`,description:`从当前礼包移除 #${row.id} 的这条记录。`};
}
function settings(pool?:number){
 if(toolsDisabled.value)return;
 form.value={...base(),kind:'settings',pool,value:pool===undefined?props.document.selection:group.value?.pools?.[pool]?.drawCount??'1'};
}
function save(){
 const current=form.value;
 if(!current||!canEdit.value||!props.applyChange)return;
 try{
  if(current.source!==props.sourceText||current.epoch!==gui.epoch)throw new Error('文件草稿已变化，请重新编辑');
  let action:BoosterInlineAction;
  if(current.kind==='item')action={kind:current.action,group:current.group,index:current.index,row:current.row};
  else if(current.kind==='category')action={...current.action,name:current.name};
  else if(current.kind==='settings')action={kind:'settings',...(current.pool===undefined?{selection:current.value}:{pool:current.pool,drawCount:current.value})};
  else action=current.action;
  let nextCategory=category.value,nextSubcategory=subcategory.value;
  if('level' in action&&action.kind==='add-category'){
   if(action.level===0)nextCategory=categorized.value?categories.value.length:rows.value.length?1:0;
   else nextSubcategory=Array.from({length:Number(props.document.dimensions[2])},(_,i)=>i).find(i=>!subcategories.value.some(option=>option.value===i))??Number(props.document.dimensions[2]);
  }
  props.applyChange(current.source,action);
  form.value=null;error.value='';checked.value=[];
  category.value=nextCategory;subcategory.value=nextSubcategory;
 }catch(cause){error.value=String(cause);}
}
let generation=0;
watch(()=>[props.active,gui.epoch,gui.revision,rows.value.map(r=>r.id).join(',')],async()=>{
 const request=++generation;appearances.value={};
 if(!props.active||!rows.value.length)return;
 try{
  const result=await FileGUIService.ReadItemAppearances([...new Set(rows.value.map(r=>r.id))]);
  if(request===generation)appearances.value=result??{};
 }catch{ /* Missing artwork uses the same fallback as other item views. */ }
},{immediate:true});
onBeforeUnmount(()=>{generation++;emit('pending',false);});
</script>

<template>
 <div ref="contentsRoot" class="booster-contents">
  <div v-if="categorized" class="category-selectors">
   <div class="category-line">
    <NSelect v-model:value="category" :disabled="!!form" :options="categories" aria-label="礼包分类" />
    <div v-if="editable" class="inline-tools" role="toolbar" aria-label="分类操作">
     <NButton quaternary circle size="small" :disabled="toolsDisabled" title="添加分类" aria-label="添加分类" @click="editCategory('add-category',0)"><NIcon><Add20Regular /></NIcon></NButton>
     <NButton quaternary circle size="small" :disabled="toolsDisabled" title="重命名分类" aria-label="重命名分类" @click="editCategory('rename-category',0)"><NIcon><Edit20Regular /></NIcon></NButton>
     <NButton quaternary circle size="small" :disabled="toolsDisabled" title="删除分类" aria-label="删除分类" @click="editCategory('delete-category',0)"><NIcon><Delete20Regular /></NIcon></NButton>
    </div>
   </div>
   <div v-if="twoLevels" class="category-line">
    <NSelect :value="subcategories.length?subcategory:null" :disabled="!!form||!subcategories.length" :options="subcategories" placeholder="请添加子分类" aria-label="礼包子分类" @update:value="subcategory=$event" />
    <div v-if="editable" class="inline-tools" role="toolbar" aria-label="子分类操作">
     <NButton quaternary circle size="small" :disabled="toolsDisabled" title="添加子分类" aria-label="添加子分类" @click="editCategory('add-category',1)"><NIcon><Add20Regular /></NIcon></NButton>
     <NButton quaternary circle size="small" :disabled="toolsDisabled||!subcategories.length" title="重命名子分类" aria-label="重命名子分类" @click="editCategory('rename-category',1)"><NIcon><Edit20Regular /></NIcon></NButton>
     <NButton quaternary circle size="small" :disabled="toolsDisabled||!subcategories.length" title="删除子分类" aria-label="删除子分类" @click="editCategory('delete-category',1)"><NIcon><Delete20Regular /></NIcon></NButton>
    </div>
   </div>
  </div>
  <div v-if="editable" class="contents-toolbar">
   <NButton v-if="document.mode==='selection'&&!categorized" size="small" :disabled="toolsDisabled" @click="editCategory('add-category',0)">添加分类</NButton>
   <NButton v-if="document.mode==='selection'||!group?.pools?.length" size="small" secondary :disabled="toolsDisabled||!canAddItem" @click="editItem()"><template #icon><NIcon><Add20Regular /></NIcon></template>添加物品</NButton>
   <NButton v-if="document.mode==='selection'" size="small" quaternary :disabled="toolsDisabled" @click="settings()">可选数量：{{document.selection}}</NButton>
  </div>
  <NAlert v-if="error" class="inline-error" type="error" :show-icon="false">{{error}}</NAlert>
  <div v-if="form&&form.kind!=='item'" class="inline-form" @keydown.esc="form=null">
   <template v-if="form.kind==='category'"><label>{{form.action.kind==='add-category'?'新增':'重命名'}}{{form.action.level?'子分类':'分类'}}<NInput v-model:value="form.name" autofocus aria-label="分类名称" @keydown.enter.prevent="save" /></label></template>
   <template v-else-if="form.kind==='settings'"><label>{{form.pool===undefined?'可选物品项数':`奖励池 ${form.pool+1} 随机获得项数`}}<NInput v-model:value="form.value" autofocus aria-label="礼包获得项数" @keydown.enter.prevent="save" /></label></template>
   <template v-else><strong>{{form.title}}</strong><p>{{form.description}}</p></template>
   <div class="inline-actions"><NButton size="small" @click="form=null;error=''">取消</NButton><NButton size="small" :type="form.kind==='delete'?'error':'primary'" :disabled="!canEdit" @click="save">{{form.kind==='delete'?'确认删除':'应用修改'}}</NButton></div>
  </div>
  <div class="contents-caption"><span>{{document.mode==='booster'?'随机获得以下物品':'获得以下物品'}}</span><span v-if="document.mode==='selection'" class="selection-count">可选 {{document.selection}} 项 · 已选 {{checked.length}} 项</span></div>
  <div class="contents-list" role="list" aria-label="礼包物品">
   <template v-for="(row,i) in rows" :key="`${category}:${subcategory}:${i}:${row.id}`">
    <div v-if="document.mode==='booster'&&(i===0||row.poolIndex!==rows[i-1]?.poolIndex)" class="pool-caption">
     <span>{{(group?.pools?.length??0)>1?`奖励池 ${(row.poolIndex??0)+1} · `:''}}{{group?.pools?.[row.poolIndex??0]?.source.name==='cera'?'商城道具 · ':''}}随机获得 {{group?.pools?.[row.poolIndex??0]?.drawCount}} 项</span>
     <div v-if="editable" class="inline-tools"><NButton size="tiny" quaternary :disabled="toolsDisabled" :aria-label="`编辑奖励池${(row.poolIndex??0)+1}抽取数量`" @click="settings(row.poolIndex??0)">修改项数</NButton><NButton size="tiny" secondary :disabled="toolsDisabled" :aria-label="`奖励池${(row.poolIndex??0)+1}添加物品`" @click="editItem(undefined,row.poolIndex??0)">添加物品</NButton></div>
    </div>
    <div class="contents-row" :class="{'is-checked':checked.includes(i)}" role="listitem">
     <div class="contents-icon"><ImageThumbnail :reference="appearances[row.id]?.icon" :size="36" show-fallback animated /></div>
     <div class="contents-item">
      <div class="contents-name"><span class="contents-item-name" :style="rarityTextStyle(appearances[row.id]?.rarity)">{{boosterRewardName(group,rewardName(row.id))}}</span> <span class="contents-quantity">({{row.quantity}}个)</span></div>
      <div class="contents-details"><span>#{{row.id}}</span><span v-if="document.mode==='booster'">权重 {{row.weight}}</span><span v-if="row.avatarParams">附加参数 {{row.avatarParams.join(' / ')}}</span></div>
     </div>
     <div v-if="editable" class="inline-tools" role="toolbar" :aria-label="`物品 ${row.id} 操作`">
      <NButton quaternary circle size="small" :disabled="toolsDisabled" title="编辑物品" :aria-label="`编辑物品 ${row.id}`" @click="editItem(row)"><NIcon><Edit20Regular /></NIcon></NButton>
      <NButton quaternary circle size="small" :disabled="toolsDisabled" title="删除物品" :aria-label="`删除物品 ${row.id}`" @click="deleteItem(row)"><NIcon><Delete20Regular /></NIcon></NButton>
     </div>
     <NCheckbox v-if="document.mode==='selection'" :checked="checked.includes(i)" :disabled="!active||!!form||(!checked.includes(i)&&checked.length>=limit)" :aria-label="`选择 ${boosterRewardName(group,rewardName(row.id))} #${row.id}`" @update:checked="toggle(i,$event)" />
    </div>
    <div v-if="form?.kind==='item'&&form.action==='edit-item'&&form.index===group?.rows.indexOf(row)" class="inline-form row-form" @keydown.esc="form=null">
     <BoosterRewardFields :row="form.row" :random="document.mode==='booster'" :label="form.row.id===form.originalID?form.label:undefined" :disabled="!canEdit" />
     <div class="inline-actions"><NButton size="small" @click="form=null;error=''">取消</NButton><NButton size="small" type="primary" :disabled="!canEdit" @click="save">应用物品修改</NButton></div>
    </div>
   </template>
   <div v-if="form?.kind==='item'&&form.action==='add-item'" class="inline-form row-form" @keydown.esc="form=null">
    <strong>添加物品</strong><BoosterRewardFields :row="form.row" :random="document.mode==='booster'" :disabled="!canEdit" />
    <div class="inline-actions"><NButton size="small" @click="form=null;error=''">取消</NButton><NButton size="small" type="primary" :disabled="!canEdit" @click="save">添加到当前{{document.mode==='booster'?'奖励池':'分类'}}</NButton></div>
   </div>
   <p v-if="!rows.length&&form?.kind!=='item'" class="contents-empty">{{twoLevels&&!subcategories.length?'请先添加子分类，再添加物品。':'暂无物品，可在此添加。'}}</p>
  </div>
 </div>
</template>

<style scoped>
.booster-contents{max-width:820px}.category-selectors{display:grid;gap:10px;margin-bottom:16px}.category-line{display:flex;align-items:center;gap:8px;min-width:0}.category-line>.n-select{flex:1;min-width:0}.contents-toolbar{display:flex;flex-wrap:wrap;align-items:center;gap:8px;margin-bottom:14px}.inline-tools{display:flex;align-items:center;gap:2px;flex-shrink:0}.inline-tools :deep(.n-button){color:var(--pvf-text-muted)}.inline-tools :deep(.n-button:hover){color:var(--pvf-text-primary)}.inline-form{display:grid;gap:12px;padding:16px;margin-bottom:14px;border:1px solid var(--pvf-border-normal);border-radius:6px;background:var(--pvf-surface-card)}.inline-form label{display:grid;gap:8px;font-size:12px;color:var(--pvf-text-secondary)}.inline-form p{margin:0;color:var(--pvf-text-secondary);font-size:13px}.inline-actions{display:flex;justify-content:flex-end;gap:8px}.inline-error{margin-bottom:12px}.row-form{margin:0;border-radius:0;border-width:1px 0;background:var(--pvf-surface-panel)}.contents-caption{display:flex;align-items:baseline;justify-content:space-between;gap:12px;margin-bottom:10px;font-size:13px;color:var(--pvf-text-secondary)}.selection-count{font-size:12px;color:var(--pvf-text-muted)}.pool-caption{display:flex;flex-wrap:wrap;align-items:center;justify-content:space-between;gap:8px;padding:10px 16px;font-size:12px;color:var(--pvf-text-secondary);border-bottom:1px solid var(--pvf-border-subtle)}.contents-list{max-height:520px;overflow-y:auto;border:1px solid var(--pvf-border-subtle);border-radius:6px;background:var(--pvf-surface-inset)}.contents-row{display:flex;align-items:center;gap:14px;min-height:76px;padding:12px 16px;box-sizing:border-box}.contents-row+.contents-row{border-top:1px solid var(--pvf-border-subtle)}.contents-row.is-checked{background:var(--pvf-surface-selected)}.contents-icon{display:flex;align-items:center;justify-content:center;width:46px;height:46px;flex-shrink:0;border:1px solid var(--pvf-border-subtle);border-radius:4px}.contents-item{min-width:0;flex:1}.contents-name{font-size:14px;line-height:1.5;overflow-wrap:anywhere}.contents-item-name{color:#7ed8ec;text-shadow:0 1px 2px #000,0 0 2px #000}.contents-quantity{white-space:nowrap;color:var(--pvf-text-secondary)}.contents-details{display:flex;flex-wrap:wrap;gap:12px;margin-top:4px;font-size:12px;color:var(--pvf-text-muted)}.contents-empty{padding:28px 16px;text-align:center;color:var(--pvf-text-muted);font-size:13px}@media(max-width:600px){.contents-row{gap:8px;padding:10px}.contents-icon{width:38px;height:38px}.contents-caption{flex-wrap:wrap}.inline-form{padding:12px}.pool-caption{padding:10px}}
</style>
