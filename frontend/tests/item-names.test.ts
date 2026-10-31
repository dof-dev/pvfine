import { describe, expect, it, vi } from 'vitest';
import { createItemNameSession } from '../src/gui/itemNames';
function deferred<T>() { let resolve!: (value: T) => void; const promise=new Promise<T>(r=>{resolve=r;});return{resolve,promise}; }
describe('礼包关联物品名称',()=>{
 it('合并普通与自选奖励 ID，只查询一次重复 ID，保留未知物品标记',async()=>{
  const read=vi.fn(async()=>({'1':'药剂','4':'装备😀'}));const session=createItemNameSession(read);
  await session.load(['1','4','1',' 1 ','999','','invalid']);
  expect(read).toHaveBeenCalledExactlyOnceWith(['1','4','999']);expect(session.names.value).toEqual({'1':'药剂','4':'装备😀','999':''});
 });
 it('修改奖励 ID 后淘汰旧名称，延迟结果不能覆盖新奖励',async()=>{
  const old=deferred<Record<string,string>>(),next=deferred<Record<string,string>>();let calls=0;
  const session=createItemNameSession(()=>++calls===1?old.promise:next.promise);
  const first=session.load(['1']);const second=session.load(['2']);next.resolve({'2':'新奖励'});await second;
  old.resolve({'1':'旧奖励'});await first;expect(session.names.value).toEqual({'2':'新奖励'});
 });
 it('切换归档清空名称并忽略旧归档响应，防止同 ID 串名',async()=>{
  const pending=deferred<Record<string,string>>();const session=createItemNameSession(()=>pending.promise);
  const first=session.load(['1']);session.invalidate(true);pending.resolve({'1':'旧归档'});await first;
  expect(session.names.value).toEqual({});expect(session.loading.value).toBe(false);
 });
 it('同归档刷新保留已显示名称，失败后可以重试，空列表不发请求',async()=>{
  const read=vi.fn().mockResolvedValueOnce({'1':'原名称'}).mockRejectedValueOnce(new Error('read failed')).mockResolvedValueOnce({'1':'更新名称'});
  const session=createItemNameSession(read);await session.load(['1']);session.invalidate();await session.load(['1']);
  expect(session.names.value['1']).toBe('原名称');expect(session.error.value).toContain('read failed');
  await session.load(['1']);expect(session.names.value['1']).toBe('更新名称');expect(session.error.value).toBe('');
  await session.load([]);expect(session.names.value).toEqual({});expect(read).toHaveBeenCalledTimes(3);
 });
});
