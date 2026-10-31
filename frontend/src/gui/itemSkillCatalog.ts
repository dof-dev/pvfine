import { ref } from 'vue';
import type { SkillTreeSkill } from '../../bindings/pvfine/services/models';

/** Skill IDs are job-local. Never reuse a result across archive epochs. */
export function createItemSkillCatalog(read:(job:string)=>PromiseLike<SkillTreeSkill[]|null>){
 const skills=ref<Record<string,SkillTreeSkill[]>>({}),errors=ref<Record<string,string>>({}),loading=ref(false);
 let generation=0;
 function invalidate(clear=false){generation++;loading.value=false;errors.value={};if(clear)skills.value={};}
 async function load(jobs:string[]){
  const wanted=[...new Set(jobs.filter(job=>/^\[[a-z][a-z0-9 _-]*\]$/i.test(job)&&job!=='[all]'))].filter(job=>!Object.hasOwn(skills.value,job));
  const request=++generation;errors.value={};loading.value=wanted.length>0;
  await Promise.allSettled(wanted.map(async job=>{
   try{const result=await read(job);if(request===generation)skills.value={...skills.value,[job]:result??[]};}
   catch(cause){if(request===generation)errors.value={...errors.value,[job]:String(cause)};}
  }));
  if(request===generation)loading.value=false;
 }
 return{skills,errors,loading,load,invalidate};
}
