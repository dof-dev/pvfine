import { describe,expect,it } from 'vitest';
import { applySkillDataSelection, defaultItemSkillEffect, editItemSkillEffect, itemSkillEffectSummary, readItemSkillEffects, type ItemSkillEffect } from '../src/gui/itemSkillEffects';
const base='#PVF_File\r\n[name]\r\n`装备😀`\r\n[if]\r\n[cooltime]\r\n3000\r\n[/if]\r\n[then]\r\n[skill levelup]\r\n`[swordman]` 99 5\r\n[/skill levelup]\r\n[/then]\r\n[unknown]\r\n{5=1234} # 未知字段\r\n';
const level:ItemSkillEffect={kind:'level',job:'[swordman]',skill:'12',value:'2'};
const data:ItemSkillEffect={kind:'data',job:'[at fighter]',skill:'233',scope:'[all]',data:'[cooltime]',index:'0',operator:'%',value:'-6'};
const range=defaultItemSkillEffect('range');
const parsed=(text:string)=>{const result=readItemSkillEffects(text);expect(result.issues).toEqual([]);return result;};
describe('装备技能增强精确编辑',()=>{
 it('三个格式可新增解析，合并同类段落，忽略触发器中的嵌套效果',()=>{
  let text=editItemSkillEffect(base,{kind:'add',effect:level});text=editItemSkillEffect(text,{kind:'add',effect:data});text=editItemSkillEffect(text,{kind:'add',effect:range});
  expect(parsed(text).entries.map(e=>e.effect)).toEqual([level,data,range]);expect(text).toContain(base);
  text=editItemSkillEffect(text,{kind:'add',effect:{...data,index:'1',value:'1.5'}});
  text=editItemSkillEffect(text,{kind:'add',effect:{...level,skill:'14',value:'-1'}});
  text=editItemSkillEffect(text,{kind:'add',effect:{...range,lower:'20',upper:'30'}});
  expect(parsed(text).entries).toHaveLength(6);expect(text.match(/\[skill data up\]/g)).toHaveLength(1);expect(text.match(/\[all skill item container\]/g)).toHaveLength(1);
 });
 it('等级加成支持连续记录、重复段落及字符串类型标记，未改时原文往返',()=>{
  const text=base+'[skill levelup]\r\n{7=`[swordman]`} 12 2 # 第一项\r\n`[mage]` 20 1\r\n[/skill levelup]\r\n[skill levelup]\r\n`[swordman]` 14 3\r\n[/skill levelup]\r\n';
  const doc=parsed(text);expect(doc.entries).toHaveLength(3);
  expect(editItemSkillEffect(text,{kind:'edit',entry:0,effect:doc.entries[0]!.effect})).toBe(text);
  expect(editItemSkillEffect(text,{kind:'edit',entry:0,effect:{...level,job:'[mage]',value:'4'}})).toBe(text.replace('{7=`[swordman]`} 12 2','{7=`[mage]`} 12 4'));
 });
 it('数据增强精确修改序号、数值、作用范围，保留未知字段和 CRLF',()=>{
  const text=editItemSkillEffect(base,{kind:'add',effect:data}),next={...data,scope:'[dungeon type]',index:'1',value:'-1.5'};
  const output=editItemSkillEffect(text,{kind:'edit',entry:0,effect:next});
  expect(parsed(output).entries[0]!.effect).toEqual(next);expect(output).toContain(base);
  expect(output.replaceAll('\r\n','')).not.toContain('\n');
 });
 it('删除连续记录只删除目标参数，删除最后记录移除整段',()=>{
  let text=editItemSkillEffect('',{kind:'add',effect:level});text=editItemSkillEffect(text,{kind:'add',effect:{...level,skill:'14'}});
  text=editItemSkillEffect(text,{kind:'delete',entry:0});expect(parsed(text).entries[0]!.effect).toMatchObject({skill:'14'});
  text=editItemSkillEffect(text,{kind:'delete',entry:0});expect(parsed(text).entries).toEqual([]);expect(text).not.toContain('[skill levelup]');
 });
 it('区间修改按字段定位，保留注释及已有额外条件，未改原文往返',()=>{
  const text=editItemSkillEffect(base,{kind:'add',effect:range}).replace('85\r\n','85 # 等级上限\r\n');
  const doc=parsed(text);expect(editItemSkillEffect(text,{kind:'edit',entry:0,effect:doc.entries[0]!.effect})).toBe(text);
  const next={...range,job:'[swordman]',upper:'90',value:'2'};
  const result=editItemSkillEffect(text,{kind:'edit',entry:0,effect:next});
  expect(parsed(result).entries[0]!.effect).toEqual(next);expect(result).toContain('90 # 等级上限');expect(result).toContain(base);
 });
 it('区间额外条件可增删改，支持多个区间独立修改与删除',()=>{
  let text=editItemSkillEffect('',{kind:'add',effect:{...range,extra:[]}});
  text=editItemSkillEffect(text,{kind:'edit',entry:0,effect:{...range,extra:['[normal skill]','[ex skill]']}});
  expect(parsed(text).entries[0]!.effect).toMatchObject({extra:['[normal skill]','[ex skill]']});
  text=editItemSkillEffect(text,{kind:'add',effect:{...range,lower:'30'}});
  text=editItemSkillEffect(text,{kind:'edit',entry:0,effect:{...range,extra:[]}});
  expect(parsed(text).entries[0]!.effect).toMatchObject({extra:[]});expect(parsed(text).entries).toHaveLength(2);
  text=editItemSkillEffect(text,{kind:'delete',entry:0});expect(parsed(text).entries[0]!.effect).toMatchObject({lower:'30'});
  text=editItemSkillEffect(text,{kind:'delete',entry:0});expect(text).not.toContain('[all skill item container]');
 });
 it('特殊结构分段只读，不影响其他有效增强；不修改未知条件字段',()=>{
  const text='[skill data up]\n`[mage]` 1 `custom`\n[/skill data up]\n'+editItemSkillEffect(base,{kind:'add',effect:level});
  const doc=readItemSkillEffects(text);expect(doc.issues.length).toBeGreaterThan(0);expect(doc.entries).toHaveLength(1);
  const next=editItemSkillEffect(text,{kind:'edit',entry:0,effect:{...level,value:'4'}});expect(next).toContain(text.slice(0,text.indexOf('#PVF_File')));
  const special=editItemSkillEffect('',{kind:'add',effect:range}).replace('[skill phase]','[special field]\n1\n[skill phase]');
  expect(readItemSkillEffects(special).issues.length).toBeGreaterThan(0);
 });
 it('校验职业、技能 ID、数值、等级范围和操作符，拒绝代码注入及过期定位',()=>{
  for(const effect of [
   {...level,job:'swordman'},{...level,job:'[mage]\n[name]'},{...level,skill:'-1'},{...level,value:'1.5'},
   {...data,index:'-1'},{...data,value:'NaN'},{...data,value:'3.5e38'},{...data,operator:'eval'},
   {...range,lower:'90',upper:'1'},{...range,extra:['[/all skill item]']},
  ])expect(()=>editItemSkillEffect(base,{kind:'add',effect})).toThrow();
  const text=editItemSkillEffect(base,{kind:'add',effect:level});
  expect(()=>editItemSkillEffect(text,{kind:'edit',entry:0,effect:data})).toThrow('类型');
  expect(()=>editItemSkillEffect(text,{kind:'delete',entry:9})).toThrow('条目已变化');
 });
 it('新增效果不追加到参数损坏的同名段落，坏记录保留且新记录可见',()=>{
  const text='[skill data up]\n`mage` 1 `[all]` `[cooltime]` 0 `%` 2\n[/skill data up]\n';
  const next=editItemSkillEffect(text,{kind:'add',effect:data}),doc=readItemSkillEffects(next);
  expect(next).toContain(text);expect(doc.issues.length).toBeGreaterThan(0);expect(doc.entries.map(e=>e.effect)).toEqual([data]);
  expect(next.match(/\[skill data up\]/g)).toHaveLength(2);
 });
 it('区间标签大小写与空格不因无修改应用而重写',()=>{
  const text=editItemSkillEffect('',{kind:'add',effect:range}).replace('[all]','[ ALL ]');
  const entry=parsed(text).entries[0]!;expect(editItemSkillEffect(text,{kind:'edit',entry:0,effect:entry.effect})).toBe(text);
 });
 it('缺失闭合或损坏的文本不能通过新增增强绕过解析检查',()=>{
  expect(()=>editItemSkillEffect('[if]\n[probability]\n10',{kind:'add',effect:level})).toThrow();
  expect(()=>editItemSkillEffect('[name]\n`未闭合',{kind:'add',effect:range})).toThrow();
 });
 it('摘要区分技能等级、数据修改和等级区间，不替数据序号猜测语义',()=>{
  expect(itemSkillEffectSummary(level)).toBe('技能等级 +2');
  expect(itemSkillEffectSummary(data)).toContain('冷却时间 [0] % -6');
  expect(itemSkillEffectSummary(range)).toContain('Lv1–85');
 });
});

describe('从技能 GUI 选择增强数据', () => {
 it('使用原始动态索引并保留装备作用范围、运算符与小数操作数', () => {
  const source = { ...data, value: '-7.5' } as Extract<ItemSkillEffect, {kind:'data'}>;
  const result = applySkillDataSelection(source, { job: '[mage]', skill: '14', dynamic: true, index: 2, mode: 'dungeon' });
  expect(result).toEqual({ ...source, job: '[mage]', skill: '14', data: '[level]', index: '2' });
  expect(source).toMatchObject({ job: '[at fighter]', data: '[cooltime]', index: '0' });
  expect(parsed(editItemSkillEffect('', {kind:'add',effect:result})).entries[0]!.effect).toEqual(result);
 });
 it('静态数据对应 static，索引零有效，决斗场选择不猜测装备作用范围', () => {
  expect(applySkillDataSelection(data as Extract<ItemSkillEffect,{kind:'data'}>, {job:'[mage]',skill:'14',dynamic:false,index:0,mode:'pvp'})).toMatchObject({data:'[static]',index:'0',scope:'[all]'});
 });
 it('拒绝失效索引和非法技能身份', () => {
  const selection={job:'[mage]',skill:'14',dynamic:true,index:0,mode:'dungeon'};
  for(const index of [-1, 1.5, NaN, Infinity, 2147483648]) expect(()=>applySkillDataSelection(data as Extract<ItemSkillEffect,{kind:'data'}>, {...selection,index})).toThrow();
  expect(()=>applySkillDataSelection(data as Extract<ItemSkillEffect,{kind:'data'}>, {...selection,skill:'bad'})).toThrow();
 });
});
