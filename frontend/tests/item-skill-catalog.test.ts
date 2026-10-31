import { describe,expect,it,vi } from 'vitest';
import { createItemSkillCatalog } from '../src/gui/itemSkillCatalog';
import type { SkillTreeSkill } from '../bindings/pvfine/services/models';
const entry=(id:number,name:string):SkillTreeSkill=>({id,name,path:`skill/test/${id}.skl`,fileIndex:id});
function deferred<T>(){let resolve!:(value:T)=>void;const promise=new Promise<T>(r=>resolve=r);return{resolve,promise};}
describe('物品技能选择归档会话',()=>{
 it('技能 ID 按职业隔离，合并重复职业查询，同一归档复用缓存',async()=>{
  const read=vi.fn(async(job:string)=>[entry(12,job==='[swordman]'?'鬼斩':'魔法弹')]),session=createItemSkillCatalog(read);
  await session.load(['[swordman]','[mage]','[swordman]','[all]','invalid']);
  expect(read).toHaveBeenCalledTimes(2);expect(session.skills.value['[swordman]']![0]!.name).toBe('鬼斩');expect(session.skills.value['[mage]']![0]!.name).toBe('魔法弹');
  await session.load(['[swordman]']);expect(read).toHaveBeenCalledTimes(2);
 });
 it('切换职业时延迟返回不会覆盖新请求，旧职业仍可重新读取',async()=>{
  const old=deferred<SkillTreeSkill[]>(),next=deferred<SkillTreeSkill[]>();const session=createItemSkillCatalog(job=>job==='[swordman]'?old.promise:next.promise);
  const first=session.load(['[swordman]']),second=session.load(['[mage]']);next.resolve([entry(12,'新技能')]);await second;old.resolve([entry(12,'旧技能')]);await first;
  expect(session.skills.value).toEqual({'[mage]':[entry(12,'新技能')]});
  await session.load(['[swordman]']);expect(session.skills.value['[swordman]']![0]!.name).toBe('旧技能');
 });
 it('切换归档清空缓存并忽略旧归档响应',async()=>{
  const pending=deferred<SkillTreeSkill[]>(),session=createItemSkillCatalog(()=>pending.promise);const request=session.load(['[swordman]']);
  session.invalidate(true);pending.resolve([entry(1,'旧归档')]);await request;
  expect(session.skills.value).toEqual({});expect(session.loading.value).toBe(false);
 });
 it('部分职业失败不丢失成功结果，可以重试失败职业',async()=>{
  let retry=false;const read=vi.fn(async(job:string)=>{if(job==='[mage]'&&!retry)throw new Error('failed');return[entry(1,job)];});
  const session=createItemSkillCatalog(read);await session.load(['[mage]','[swordman]']);expect(session.errors.value['[mage]']).toContain('failed');expect(session.skills.value['[swordman]']).toHaveLength(1);
  retry=true;await session.load(['[mage]','[swordman]']);expect(read).toHaveBeenCalledTimes(3);expect(session.errors.value).toEqual({});
 });
 it('空列表和非职业标签不触发查询，空职业列表也会缓存',async()=>{
  const read=vi.fn(async()=>null),session=createItemSkillCatalog(read);await session.load(['[all]','','[bad]\n[tag]']);expect(read).not.toHaveBeenCalled();
  await session.load(['[mage]']);await session.load(['[mage]']);expect(read).toHaveBeenCalledTimes(1);expect(session.skills.value['[mage]']).toEqual([]);
 });
});
