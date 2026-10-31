<script setup lang="ts">
import { computed, defineAsyncComponent, nextTick, onBeforeUnmount, ref, watch } from 'vue';
import { NAlert,NButton,NIcon,NInput,NSelect } from 'naive-ui';
import { Add20Regular,Delete20Regular,Edit20Regular } from '@vicons/fluent';
import { FileGUIService } from '../../../bindings/pvfine/services';
import type { SkillTreeRegistration } from '../../../bindings/pvfine/services/models';
import type { GUIFile } from '../../gui/types';
import { defaultItemSkillEffect,editItemSkillEffect,itemSkillDataKinds,itemSkillEffectSummary,readItemSkillEffects,type ItemSkillEffect } from '../../gui/itemSkillEffects';
import { createItemSkillCatalog } from '../../gui/itemSkillCatalog';
import { applySkillDataSelection } from '../../gui/itemSkillEffects';
import type { SkillDataSelection } from '../../gui/skill';
import { skillJobNames } from '../../gui/skill';
import { useEditorStore } from '../../stores/editor';
import { useFileGUIStore } from '../../stores/fileGUI';
const SkillDataPicker=defineAsyncComponent(()=>import('./SkillDataPicker.vue'));
const pickerShow=ref(false);
const props=defineProps<{file:GUIFile;active:boolean;busy?:boolean}>();
const emit=defineEmits<{(e:'pending',value:boolean):void}>();
const editor=useEditorStore(),gui=useFileGUIStore();
const root=ref<HTMLElement|null>(null);
const document=computed(()=>readItemSkillEffects(props.file.text));
const disabled=computed(()=>!props.active||!props.file.editable||!!props.busy||editor.saving||editor.guiApplying);
const form=ref<{effect:ItemSkillEffect;entry?:number;remove:boolean;extraText:string;source:string;epoch:number;path:string}|null>(null);
const error=ref(''),registrations=ref<SkillTreeRegistration[]>([]),registrationError=ref('');
const catalog=createItemSkillCatalog(FileGUIService.ListSkillTreeSkills),{skills,errors,loading}=catalog;
const jobLabel=(job:string)=>skillJobNames[job.replace(/[\[\] ]/g,'')]||job;
const jobs=computed(()=>[...new Set([...registrations.value.map(r=>r.job),...document.value.entries.map(e=>e.effect.job),...(form.value?[form.value.effect.job]:[])])].filter(Boolean).map(value=>({value,label:`${jobLabel(value)} · ${value}`})));
const skillOptions=computed(()=>form.value?(skills.value[form.value.effect.job]??[]).map(skill=>({value:String(skill.id),label:`${skill.name} #${skill.id}`})):[]);
function skillName(effect:ItemSkillEffect){
 if(effect.kind==='range')return `${effect.job==='[all]'?'全部职业':jobLabel(effect.job)} · ${effect.group}`;
 return `${jobLabel(effect.job)} · ${skills.value[effect.job]?.find(skill=>String(skill.id)===effect.skill)?.name||`技能 #${effect.skill}`}`;
}
let registrationRequest=0;
async function loadRegistrations(){
 const request=++registrationRequest,epoch=gui.epoch;
 registrationError.value='';
 try{const result=await FileGUIService.ListSkillTrees();if(request===registrationRequest&&epoch===gui.epoch)registrations.value=result??[];}
 catch(cause){if(request===registrationRequest)registrationError.value=String(cause);}
}
let catalogEpoch=gui.epoch,catalogRevision=gui.revision;
watch(()=>JSON.stringify([props.active,gui.epoch,gui.revision,document.value.entries.filter(e=>e.effect.kind!=='range').map(e=>e.effect.job),form.value?.effect.kind!=='range'?form.value?.effect.job:'']),()=>{
 const changed=catalogEpoch!==gui.epoch||catalogRevision!==gui.revision;
 catalog.invalidate(changed);catalogEpoch=gui.epoch;catalogRevision=gui.revision;
 if(changed)registrations.value=[];
 if(!props.active){registrationRequest++;return;}
 if(changed||!registrations.value.length)void loadRegistrations();
 void catalog.load([...document.value.entries.filter(e=>e.effect.kind!=='range').map(e=>e.effect.job),...(form.value&&form.value.effect.kind!=='range'?[form.value.effect.job]:[])]);
},{immediate:true});
let unregister:(()=>void)|undefined;
watch(()=>props.file.index,index=>{unregister?.();unregister=editor.registerGUIDirtyChecker(index,()=>!!form.value);},{immediate:true});
watch(()=>!!form.value,value=>emit('pending',value));
watch(form,async value=>{if(value){await nextTick();if(props.active)root.value?.querySelector<HTMLElement>('.effect-form')?.scrollIntoView({block:'nearest'});}});
watch(()=>form.value?.effect.job,(job,previous)=>{if(previous&&job!==previous&&form.value&&!form.value.remove&&form.value.effect.kind!=='range')form.value.effect.skill='';},{flush:'sync'});
watch(()=>!!form.value,value=>{if(!value)pickerShow.value=false;});
watch(()=>props.active,value=>{if(!value)pickerShow.value=false;});
watch(()=>JSON.stringify([gui.epoch,props.file.index,props.file.path]),()=>{form.value=null;error.value='';},{flush:'sync'});
watch(()=>props.file.text,()=>{if(form.value){form.value=null;error.value='文件草稿已变化，请重新编辑技能增强。';}});
onBeforeUnmount(()=>{unregister?.();catalog.invalidate(true);registrationRequest++;emit('pending',false);});
function open(kind:ItemSkillEffect['kind'],entry?:number,remove=false){
 if(disabled.value||form.value)return;
 const effect=entry===undefined?defaultItemSkillEffect(kind,jobs.value.find(job=>job.value!=='[all]')?.value??''):document.value.entries[entry]?.effect;
 if(!effect)return;
 form.value={effect:JSON.parse(JSON.stringify(effect)),entry,remove,extraText:effect.kind==='range'?effect.extra.join('\n'):'',source:props.file.text,epoch:gui.epoch,path:props.file.path};error.value='';
}
function selectData(selection:SkillDataSelection & {job:string;skill:string}){
 if(!form.value||form.value.effect.kind!=='data'||disabled.value)return;
 const next=applySkillDataSelection(form.value.effect,selection);
 form.value.effect.job=next.job;
 Object.assign(form.value.effect,next);
 pickerShow.value=false;
}
function apply(){
 const current=form.value;if(!current||disabled.value)return;
 try{
  const tab=editor.getFileDraft(props.file.index);
  if(current.epoch!==gui.epoch||!tab||tab.path!==current.path||props.file.path!==current.path||tab.text!==current.source||props.file.text!==current.source)throw new Error('技能增强草稿已变化，请重新编辑');
  if(!current.remove&&current.effect.kind==='range')current.effect.extra=current.extraText.split('\n').map(value=>value.trim()).filter(Boolean);
  const action=current.remove?{kind:'delete' as const,entry:current.entry!}:current.entry===undefined?{kind:'add' as const,effect:current.effect}:{kind:'edit' as const,entry:current.entry,effect:current.effect};
  editor.updateContent(props.file.index,editItemSkillEffect(current.source,action));form.value=null;error.value='';
 }catch(cause){error.value=String(cause);}
}
const effectTypes={level:'技能等级加成',data:'技能数据修改',range:'等级区间加成'};
const scopeOptions=[{value:'[dungeon type]',label:'副本 [dungeon type]'},{value:'[all]',label:'全部场景 [all]'}];
</script>

<template>
 <section ref="root" class="item-skill-effects" aria-label="物品技能增强">
  <header><h3>技能增强</h3><div class="effect-actions"><NButton v-for="(label,kind) in effectTypes" :key="kind" size="small" secondary :disabled="disabled||!!form" @click="open(kind)"><template #icon><NIcon><Add20Regular /></NIcon></template>{{label}}</NButton></div></header>
  <NAlert v-for="(issue,i) in document.issues" :key="i" type="warning" :show-icon="false">{{issue}}</NAlert>
  <div v-for="(entry,i) in document.entries" :key="`${entry.section.start}:${i}`" class="effect-row">
   <div class="effect-content"><strong>{{skillName(entry.effect)}}</strong><div>{{itemSkillEffectSummary(entry.effect)}}</div><small v-if="entry.effect.kind!=='range'">技能 ID {{entry.effect.skill}}</small><small v-else>{{entry.effect.extra.join(' · ')}}</small></div>
   <div class="effect-actions"><NButton quaternary circle size="small" :disabled="disabled||!!form" :aria-label="`编辑技能增强${i+1}`" title="编辑技能增强" @click="open(entry.effect.kind,i)"><NIcon><Edit20Regular /></NIcon></NButton><NButton quaternary circle size="small" :disabled="disabled||!!form" :aria-label="`删除技能增强${i+1}`" title="删除技能增强" @click="open(entry.effect.kind,i,true)"><NIcon><Delete20Regular /></NIcon></NButton></div>
  </div>
  <p v-if="!document.entries.length&&!form" class="effect-empty">尚未配置技能增强，可添加单技能或等级区间加成。</p>
  <NAlert v-if="error" type="error" :show-icon="false">{{error}}</NAlert>
  <div v-if="form" class="effect-form" @keydown.esc="form=null;error=''">
   <template v-if="form.remove"><strong>删除 {{skillName(form.effect)}} 的{{effectTypes[form.effect.kind]}}？</strong><p>{{itemSkillEffectSummary(form.effect)}}</p></template>
   <template v-else>
    <strong>{{form.entry===undefined?'新增':'编辑'}}{{effectTypes[form.effect.kind]}}</strong>
    <div class="effect-fields">
     <label>职业<NSelect :value="form.effect.job||null" @update:value="form.effect.job=String($event??'')" filterable tag :options="form.effect.kind==='range'?[{value:'[all]',label:'全部职业 [all]'},...jobs.filter(j=>j.value!=='[all]')]:jobs.filter(j=>j.value!=='[all]')" placeholder="选择职业或输入 [swordman]" :disabled="disabled" aria-label="技能增强职业" /></label>
     <template v-if="form.effect.kind!=='range'">
      <label>技能<NSelect :value="form.effect.skill||null" @update:value="form.effect.skill=String($event??'')" filterable tag :loading="loading" :options="skillOptions" placeholder="选择技能或直接输入 ID" :disabled="disabled" aria-label="技能增强技能" /></label>
      <label v-if="form.effect.kind==='level'">等级加成<NInput v-model:value="form.effect.value" placeholder="例如 2 或 -1" :disabled="disabled" :input-props="{'aria-label':'技能等级加成'}" /></label>
      <template v-else>
       <div class="wide"><NButton size="small" :disabled="disabled" @click="pickerShow=true">选择技能数据</NButton></div>
       <label>作用范围<NSelect v-model:value="form.effect.scope" filterable tag :options="scopeOptions" :disabled="disabled" aria-label="技能作用范围" /></label>
       <label>数据类型<NSelect v-model:value="form.effect.data" filterable tag :options="itemSkillDataKinds" :disabled="disabled" aria-label="技能数据类型" /></label>
       <label>数据序号<NInput v-model:value="form.effect.index" :disabled="disabled" :input-props="{'aria-label':'技能数据序号'}" /></label>
       <label>操作符<NSelect v-model:value="form.effect.operator" :options="['+','-','*','%'].map(value=>({value,label:value}))" :disabled="disabled" aria-label="技能数据操作符" /></label>
       <label>数值<NInput v-model:value="form.effect.value" :disabled="disabled" :input-props="{'aria-label':'技能数据数值'}" /></label>
      </template>
     </template>
     <template v-else>
      <label>成长类型<NInput v-model:value="form.effect.growType" :disabled="disabled" :input-props="{'aria-label':'技能成长类型'}" /></label>
      <label>技能组<NSelect v-model:value="form.effect.group" filterable tag :options="[{value:'[all]',label:'全部技能 [all]'},{value:'[active]',label:'主动技能 [active]'}]" :disabled="disabled" aria-label="区间技能组" /></label>
      <label>等级下限<NInput v-model:value="form.effect.lower" :disabled="disabled" :input-props="{'aria-label':'技能等级下限'}" /></label>
      <label>等级上限<NInput v-model:value="form.effect.upper" :disabled="disabled" :input-props="{'aria-label':'技能等级上限'}" /></label>
      <label>等级加成<NInput v-model:value="form.effect.value" :disabled="disabled" :input-props="{'aria-label':'区间技能等级加成'}" /></label>
      <label>技能阶段<NSelect v-model:value="form.effect.phase" filterable tag :options="[{value:'[all]',label:'全部阶段 [all]'}]" :disabled="disabled" aria-label="技能阶段" /></label>
      <label class="wide">额外条件（每行一个标签）<NInput v-model:value="form.extraText" type="textarea" :autosize="{minRows:2,maxRows:5}" :disabled="disabled" :input-props="{'aria-label':'技能额外条件'}" /></label>
     </template>
    </div>
    <p v-if="form.effect.kind==='data'" class="effect-hint">数据序号对应所选技能自身的数据表；数值使用脚本原始单位。</p>
    <p v-if="registrationError||errors[form.effect.job]" class="effect-hint">技能列表暂不可用，可直接输入职业标签和技能 ID。<NButton text size="tiny" @click="loadRegistrations();catalog.load([form.effect.job])">重试读取</NButton></p>
   </template>
   <div class="effect-footer"><NButton size="small" @click="form=null;error=''">取消</NButton><NButton size="small" :type="form.remove?'error':'primary'" :disabled="disabled" @click="apply">{{form.remove?'确认删除':'应用技能增强'}}</NButton></div>
  </div>
  <SkillDataPicker v-if="form && form.effect.kind==='data' && pickerShow" v-model:show="pickerShow" :job="form.effect.job" :skill="form.effect.skill" @select-data="selectData" />
 </section>
</template>

<style scoped>
.item-skill-effects{margin-top:24px;border-top:1px solid var(--pvf-border-subtle);padding-top:18px;display:grid;gap:12px}.item-skill-effects header{display:flex;flex-wrap:wrap;align-items:center;justify-content:space-between;gap:10px}.item-skill-effects h3{margin:0;font-size:14px}.effect-actions{display:flex;flex-wrap:wrap;gap:6px;align-items:center}.effect-row{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:12px 0;border-bottom:1px solid var(--pvf-border-subtle)}.effect-content{min-width:0;font-size:13px;overflow-wrap:anywhere}.effect-content strong{font-size:13px;font-weight:500}.effect-content div{margin-top:4px;color:var(--pvf-text-secondary)}.effect-content small,.effect-hint,.effect-empty{font-size:12px;color:var(--pvf-text-muted)}.effect-content small{display:block;margin-top:4px}.effect-empty,.effect-hint{margin:0;line-height:1.6}.effect-form{display:grid;gap:14px;padding:16px;border:1px solid var(--pvf-border-normal);border-radius:6px;background:var(--pvf-surface-card)}.effect-fields{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}.effect-fields label{display:grid;gap:6px;font-size:12px;color:var(--pvf-text-secondary)}.effect-fields .wide{grid-column:1/-1}.effect-footer{display:flex;justify-content:flex-end;gap:8px}@media(max-width:600px){.effect-fields{grid-template-columns:1fr}.effect-form{padding:12px}}
</style>
