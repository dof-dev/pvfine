<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue';
import { NAlert, NButton, NInput, NModal, NSelect, useMessage } from 'naive-ui';
import { FileGUIService } from '../../../bindings/pvfine/services';
import type { ItemCreationCheck, SearchHit } from '../../../bindings/pvfine/services/models';
import type { GUIFile } from '../../gui/types';
import { itemTemplate, type ItemTemplate } from '../../gui/itemBooster';
import { useEditorStore } from '../../stores/editor';
import { useFileGUIStore } from '../../stores/fileGUI';
import ItemPicker from '../ItemPicker.vue';
const props=defineProps<{ show:boolean; file:GUIFile; active:boolean; initialName:string }>();
const emit=defineEmits<{(e:'update:show',value:boolean):void;(e:'created',index:number):void}>();
const editor=useEditorStore(),gui=useFileGUIStore(),message=useMessage();
const form=reactive({mode:'current',template:'material' as ItemTemplate,name:'',id:'',path:'',referenceID:'',referencePath:'',referenceText:''});
const check=ref<ItemCreationCheck|null>(null), checking=ref(false),busy=ref(false),referenceLoading=ref(false),error=ref('');
let generation=0,referenceGeneration=0;
const disabled=computed(()=>!props.active||!props.file.editable||editor.saving||editor.guiApplying||busy.value);
const sourceText=computed(()=>form.mode==='current'?props.file.text:form.mode==='reference'?form.referenceText:itemTemplate(form.template));
const ready=computed(()=>!!check.value&&!(check.value.conflicts?.length ?? 0)&&!disabled.value&&!checking.value&&!referenceLoading.value&&(form.mode!=='reference'||!!form.referenceText));
const templateOptions=[{label:'材料',value:'material'},{label:'普通礼包',value:'booster'},{label:'自选礼包',value:'selection'},{label:'武器',value:'weapon'},{label:'防具',value:'armor'}];
function invalidate() { generation++;checking.value=false;check.value=null;error.value=''; }
watch(()=>[form.id,form.path,gui.epoch,gui.revision],invalidate);
watch(()=>props.show,show=>{referenceGeneration++;referenceLoading.value=false;if(show){Object.assign(form,{mode:'current',name:props.initialName==='未配置'?'新物品':props.initialName,id:'',path:'',referenceID:'',referenceText:'',referencePath:''});invalidate();}});
watch(()=>[form.mode,form.template],()=>{invalidate();if(form.mode==='template')form.path=(form.template==='weapon'||form.template==='armor')?'equipment/custom/new.equ':'stackable/custom/new.stk';});
watch(()=>[props.file.index,props.file.path,gui.epoch],()=>{ referenceGeneration++;emit('update:show',false); });
onBeforeUnmount(()=>{generation++;referenceGeneration++;});
async function reference(item:SearchHit|null){
 const request=++referenceGeneration,epoch=gui.epoch;
 form.referenceText='';form.referencePath='';invalidate();
 if(!item){referenceLoading.value=false;return;}
 referenceLoading.value=true;
 try{const draft=await editor.openGUIFile(item.fileIndex);if(request!==referenceGeneration||epoch!==gui.epoch)return;
  form.referenceText=draft.text;form.referencePath=draft.path;
  if(!form.path)form.path=draft.path.replace(/\.(equ|stk)$/i,'_copy.$1');
 }catch(e){if(request===referenceGeneration)error.value=String(e);}finally{if(request===referenceGeneration)referenceLoading.value=false;}
}
function request(){return{path:form.path.trim(),id:form.id.trim(),name:form.name,text:sourceText.value,revision:check.value?.revision??0,drafts:editor.itemCreationDrafts()};}
async function checkID(){
 if(disabled.value)return;
 const current=++generation,epoch=gui.epoch;checking.value=true;check.value=null;error.value='';
 try{const result=await FileGUIService.CheckItemCreation(request());if(current===generation&&epoch===gui.epoch)check.value=result;}
 catch(e){if(current===generation)error.value=String(e);}finally{if(current===generation)checking.value=false;}
}
async function create(){
 if(!ready.value)return;
 const epoch=gui.epoch;busy.value=true;error.value='';
 try{
  // Reference drafts are read again at submission; copying never saves the source.
  if(form.mode==='reference'){
   const draft=editor.getFileDraft(form.referencePath);if(!draft)throw new Error('参考物品草稿已关闭，请重新选择');
   form.referenceText=draft.text;
  }
  const targetEqu=/\.equ$/i.test(form.path);
  const sourceEqu=form.mode==='current'?/\.equ$/i.test(props.file.path):form.mode==='reference'?/\.equ$/i.test(form.referencePath):['weapon','armor'].includes(form.template);
  if(targetEqu!==sourceEqu)throw new Error('新文件与参考物品的类型必须一致');
  const result=await editor.createItem(request());
  if(epoch!==gui.epoch)return;
  emit('update:show',false);message.success(`已创建并登记物品 #${result.id}，保存 PVF 后写盘`);emit('created',result.fileIndex);
 }catch(e){if(epoch===gui.epoch)error.value=String(e);}finally{busy.value=false;}
}
</script>
<template>
 <NModal :show="show&&active" preset="card" title="新建物品" style="width:min(650px,94vw)" :mask-closable="false" :close-on-esc="false" :closable="!busy" @update:show="emit('update:show',$event)">
  <div class="create-form">
   <NSelect v-model:value="form.mode" :disabled="disabled" :options="[{label:'复制当前物品草稿',value:'current'},{label:'选择参考物品',value:'reference'},{label:'使用基础模板',value:'template'}]" aria-label="物品来源" />
   <NSelect v-if="form.mode==='template'" v-model:value="form.template" :disabled="disabled" :options="templateOptions" aria-label="物品模板" />
   <ItemPicker v-if="form.mode==='reference'" v-model="form.referenceID" :disabled="disabled" @select="reference" />
   <small v-if="form.mode==='reference'">{{referenceLoading?'正在读取参考物品…':form.referencePath||'选择参考物品后复制其当前草稿'}}</small>
   <label>新物品名称<NInput v-model:value="form.name" :disabled="disabled" :input-props="{'aria-label':'新物品名称'}" /></label>
   <label>物品 ID<NInput v-model:value="form.id" :disabled="disabled" placeholder="1–2147483647" :input-props="{'aria-label':'物品 ID'}" /></label>
   <label>归档内路径<NInput v-model:value="form.path" :disabled="disabled" placeholder="equipment/custom/new.equ 或 stackable/custom/new.stk" :input-props="{'aria-label':'归档内路径'}" /></label>
   <NAlert type="info" :show-icon="false">文件与列表登记一起保存到工作区。复制保留原物品属性和效果，新名称独立写入；原物品保持不变。</NAlert>
   <NAlert v-if="error" type="error">{{error}}</NAlert>
   <NAlert v-if="check" :type="(check.conflicts?.length ?? 0)?'warning':'success'" :title="(check.conflicts?.length ?? 0)?'发现冲突':'ID 与路径可用'">
    <div v-for="conflict in check.conflicts" :key="conflict">{{conflict}}</div>
    <template v-if="!(check.conflicts?.length ?? 0)">登记至 {{check.listPath}}<br>{{form.id}} → {{check.entryPath}}</template>
   </NAlert>
  </div>
  <template #footer><div class="create-actions"><NButton :disabled="busy" @click="emit('update:show',false)">取消</NButton><NButton :loading="checking" :disabled="disabled||!form.id.trim()||!form.path.trim()" @click="checkID">检查 ID 与路径</NButton><NButton type="primary" :loading="busy" :disabled="!ready||!form.name.trim()" @click="create">创建并登记</NButton></div></template>
 </NModal>
</template>
<style scoped>
.create-form { display:grid;gap:14px; } label { display:grid;gap:6px;font-size:12px;color:var(--pvf-text-secondary); } small{color:var(--pvf-text-muted);overflow-wrap:anywhere;}.create-actions{display:flex;justify-content:flex-end;gap:8px;flex-wrap:wrap;}
</style>
