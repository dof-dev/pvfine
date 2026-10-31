<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue';
import { NAlert, NButton, NInput, NModal, useMessage } from 'naive-ui';
import { FileGUIService } from '../../../bindings/pvfine/services';
import { applyBoosterInlineAction, type BoosterInlineAction } from '../../gui/itemBoosterActions';
import { createItemNameSession } from '../../gui/itemNames';
import type { GUIFile } from '../../gui/types';
import { addBoosterCategory, boosterGroupLabel, boosterRewardName, editBooster, isFlatBoosterSelection, newBoosterRow, readBooster, type BoosterDocument, type BoosterGroup } from '../../gui/itemBooster';
import { useEditorStore } from '../../stores/editor';
import { useFileGUIStore } from '../../stores/fileGUI';
import ItemPicker from '../ItemPicker.vue';
import BoosterContents from './BoosterContents.vue';
const props=defineProps<{file:GUIFile;active:boolean}>();
const emit=defineEmits<{(e:'pending',pending:boolean):void}>();
const editor=useEditorStore(),gui=useFileGUIStore(),message=useMessage();
const document=computed(()=>readBooster(props.file.text));
const inlinePending=ref(false);
const draft=ref<BoosterDocument|null>(null),show=ref(false),source=ref(''),epoch=ref(0);
const disabled=computed(()=>!props.active||!props.file.editable||editor.saving||editor.guiApplying);
const nameSession=createItemNameSession(FileGUIService.ReadItemNames);
const {names,loading:namesLoading,error:namesError}=nameSession;
const rewardIDs=computed(()=>[...new Set([
 ...document.value.groups.flatMap(group=>group.rows.map(row=>row.id)),
 ...(show.value?draft.value?.groups.flatMap(group=>group.rows.map(row=>row.id))??[]:[]),
])]);
let nameTimer:ReturnType<typeof setTimeout>|undefined;
let nameEpoch=gui.epoch;
watch(()=>[props.active,props.file.index,props.file.path,gui.epoch,gui.revision,rewardIDs.value.join('\u0000')],()=>{
 clearTimeout(nameTimer);
 nameSession.invalidate(nameEpoch!==gui.epoch);
 nameEpoch=gui.epoch;
 if(props.active)nameTimer=setTimeout(()=>void nameSession.load(rewardIDs.value),120);
},{immediate:true,flush:'sync'});
onBeforeUnmount(()=>{clearTimeout(nameTimer);nameSession.invalidate(true);});
function rewardName(id:string){
 const key=id.trim();
 if(!key)return '请选择物品';
 if(names.value[key])return names.value[key];
 if(namesLoading.value&&!Object.hasOwn(names.value,key))return '正在读取名称…';
 if(namesError.value)return '名称读取失败';
 return Object.hasOwn(names.value,key)?'未知或未命名物品':'物品';
}
let unregister:(()=>void)|undefined;
watch(()=>props.file.index,index=>{unregister?.();unregister=editor.registerGUIDirtyChecker(index,()=>show.value||inlinePending.value);},{immediate:true});
onBeforeUnmount(()=>{unregister?.();emit('pending',false);});
watch(()=>show.value||inlinePending.value,value=>emit('pending',value));
watch(()=>JSON.stringify([gui.epoch,props.file.index,props.file.path]),()=>{show.value=false;});
function canAddReward(group:BoosterGroup){
 if(!draft.value||draft.value.mode!=='selection'||draft.value.dimensions[0]!=='2')return true;
 const [x,y]=group.coordinates.map(Number),n=Number(draft.value.dimensions[1]),m=Number(draft.value.dimensions[2]);
 return !!draft.value.names[2+n+x!*m+y!]?.trim();
}
function open(){if(disabled.value||inlinePending.value||document.value.issue)return;draft.value=JSON.parse(JSON.stringify(document.value));source.value=props.file.text;epoch.value=gui.epoch;show.value=true;}
function applyInline(baseline:string,action:BoosterInlineAction):string {
 if(disabled.value||show.value)throw new Error('当前无法编辑礼包');
 const tab=editor.getFileDraft(props.file.index);
 if(!tab||tab.path!==props.file.path||tab.text!==baseline||props.file.text!==baseline)throw new Error('礼包草稿已变化，请重新编辑');
 const text=applyBoosterInlineAction(baseline,action);
 editor.updateContent(props.file.index,text);
 return text;
}
function apply(){
 if(disabled.value||!draft.value)return;
 try{
  const tab=editor.getFileDraft(props.file.index);
  if(epoch.value!==gui.epoch||!tab||tab.path!==props.file.path||tab.text!==source.value||props.file.text!==source.value)throw new Error('礼包草稿已变化，请重新打开编辑');
  editor.updateContent(props.file.index,editBooster(source.value,draft.value));show.value=false;
 }catch(e){message.error(String(e));}
}
</script>
<template>
 <section v-if="document.mode" class="booster-section">
  <div class="booster-heading"><h3>{{document.mode==='booster'?'随机礼包内容':'自选礼包内容'}}</h3><NButton size="small" secondary :disabled="disabled||inlinePending||!!document.issue" @click="open">编辑礼包</NButton></div>
  <NAlert v-if="document.issue" type="warning">{{document.issue}}</NAlert>
  <BoosterContents v-else :document="document" :active="active" :reward-name="rewardName" :editable="!disabled&&!show" :source-text="file.text" :identity="`${gui.epoch}:${file.index}:${file.path}`" :apply-change="applyInline" @pending="inlinePending=$event" />
  <NModal :show="show&&active" preset="card" title="编辑礼包内容" style="width:min(850px,95vw)" :mask-closable="false" :close-on-esc="false" @update:show="show=$event">
   <div v-if="draft" class="booster-form">
    <NAlert type="info" :show-icon="false">选择物品并填写数量。随机礼盒按奖励池设置随机获得项数，奖励权重保留原始单位；avatar 每项含两个附加参数，新增时沿用现有奖励的值；支持新增单层分类，已有分类的层级与坐标保持不变。</NAlert>
    <template v-if="draft.mode==='selection'">
     <label>可选数量<NInput v-model:value="draft.selection" :input-props="{'aria-label':'礼包可选数量'}" /></label>
     <NButton v-if="draft.dimensions[0]==='1' && Number(draft.dimensions[1])>0" size="small" @click="addBoosterCategory(draft)">添加分类</NButton>
     <div class="name-grid"><label v-for="(_,i) in draft.names" :key="i">{{isFlatBoosterSelection(draft)&&i===0?'选择提示':`分类文本 ${i+1}`}}<NInput v-model:value="draft.names[i]" :disabled="/^<\s*\d+\s*::/.test(draft.names[i]??'')" :input-props="{'aria-label':`分类文本${i+1}`}" /></label></div>
    </template>
    <section v-for="(group,i) in draft.groups" :key="i" class="reward-group">
     <div class="booster-heading"><strong>{{boosterGroupLabel(draft,group)}}<span v-if="group.equipmentUpgrade!==undefined"> · 装备强化 +{{group.equipmentUpgrade}}</span></strong><NButton v-if="draft.mode!=='booster'" size="small" :disabled="!canAddReward(group)" :title="canAddReward(group)?undefined:'此空分类未配置名称，请先在 DSL 中补充名称'" @click="group.rows.push(newBoosterRow(draft,group))">添加奖励</NButton></div>
     <div v-if="draft.mode==='booster'" class="pool-settings"><div v-for="(pool,p) in group.pools" :key="p" class="pool-setting"><label>奖励池 {{p+1}} · {{pool.source.name==='cera'?'商城道具 · ':''}}随机获得项数<NInput v-model:value="pool.drawCount" :input-props="{'aria-label':`奖励池${p+1}随机获得项数`}" /></label><NButton size="small" @click="group.rows.push(newBoosterRow(draft,group,p))">添加候选物品</NButton></div></div>
     <div v-for="(row,j) in group.rows" :key="j" class="reward-inputs">
      <span v-if="draft.mode==='booster' && (group.pools?.length??0)>1" class="hint">奖励池 {{(row.poolIndex??0)+1}}</span>
      <div class="reward-item"><NInput v-model:value="row.id" placeholder="物品 ID" :input-props="{'aria-label':`分类${i+1}奖励${j+1}物品 ID`}"/><ItemPicker v-model="row.id" :label="names[row.id.trim()]?boosterRewardName(group,names[row.id.trim()]!):undefined" :disabled="disabled" /></div>
      <label>数量<NInput v-model:value="row.quantity" :input-props="{'aria-label':`分类${i+1}奖励${j+1}数量`}" /></label>
      <label v-if="draft.mode==='booster'">权重<NInput v-model:value="row.weight" :input-props="{'aria-label':`奖励${j+1}权重`}" /></label>
      <template v-if="row.avatarParams"><label v-for="(_,k) in row.avatarParams" :key="k">附加参数 {{k+1}}<NInput v-model:value="row.avatarParams[k]" :input-props="{'aria-label':`分类${i+1}奖励${j+1}附加参数${k+1}`}" /></label></template>
      <NButton size="small" :disabled="group.rows.length<=1" @click="group.rows.splice(j,1)">删除</NButton>
     </div>
    </section>
   </div>
   <template #footer><div class="booster-actions"><NButton @click="show=false">取消</NButton><NButton type="primary" :disabled="disabled" @click="apply">应用礼包修改</NButton></div></template>
  </NModal>
 </section>
</template>
<style scoped>
.booster-section{margin:24px 0;border-top:1px solid var(--pvf-border-subtle);padding-top:18px}.booster-heading{display:flex;justify-content:space-between;align-items:center;gap:12px;margin-bottom:12px}h3{margin:0;font-size:14px}.reward-group{padding:12px 0;border-bottom:1px solid var(--pvf-border-subtle)}.hint,small{color:var(--pvf-text-muted);font-size:12px}.booster-form{display:grid;gap:14px;max-height:68vh;overflow-y:auto;padding-right:8px}.reward-inputs{display:flex;flex-wrap:wrap;gap:10px;align-items:flex-end;margin-top:12px}.reward-item{flex:1;min-width:0;display:grid;gap:6px}.reward-inputs>label{width:90px}.pool-settings{display:grid;gap:12px}.pool-setting{display:flex;align-items:flex-end;gap:12px}.pool-setting>label{width:220px}.booster-actions{display:flex;justify-content:flex-end;gap:8px}label{display:grid;gap:6px;font-size:12px;color:var(--pvf-text-secondary)}.name-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px}@media(max-width:600px){.reward-inputs{flex-wrap:wrap}.reward-item{flex-basis:100%}.name-grid{grid-template-columns:1fr}}
</style>
