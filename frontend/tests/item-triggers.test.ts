import { describe, expect, it } from 'vitest';
import { editItemTrigger, readItemTriggers, triggerClauseRows, triggerClauseSpec, triggerClauses, formatItemTriggerBlock } from '../src/gui/itemTriggers';
const base = '#PVF_File\r\n[name]\r\n`装备😀`\r\n[if]\r\n[attack success]\r\n1 # 命中\r\n[cooltime]\r\n3000\r\n[/if]\r\n# 条件与动作间注释\r\n[then]\r\n[target]\r\n{7=`myself`} -1\r\n[custom]\r\n{5=1234} # 未知动作\r\n[passive object]\r\n48030 1 2 3 80 0 {8=`a.ptl`} # 对象一\r\n48045 4 5 6 90 2 `` # 对象二\r\n[/passive object]\r\n[/then]\r\n[unknown]\r\n42 # 后续字段\r\n';
function doc(text: string) { const result = readItemTriggers(text); expect(result.issues).toEqual([]); return result; }
function objectRows(text: string) { const section = doc(text).triggers[0]!.branches[0]!.children.find(c => c.name === 'passive object')!; return triggerClauseRows(section, triggerClauseSpec('then', section)!)!; }
describe('触发器精确编辑', () => {
  it('识别配对、嵌套动作、重复对象行和类型标记', () => {
    const trigger = doc(base).triggers[0]!;
    expect(trigger.condition.children.map(c=>c.name)).toEqual(['attack success','cooltime']);
    expect(objectRows(base)).toEqual([['48030','1','2','3','80','0','a.ptl'],['48045','4','5','6','90','2','']]);
    expect(doc(base).triggers).toHaveLength(1);
  });
  it('修改单个 token 保留 CRLF、未知段、注释、转义与 marker', () => {
    const rows = objectRows(base); rows[0]![6] = 'new`😀.ptl'; rows[1]![4] = '99';
    const output = editItemTrigger(base,{kind:'edit-clause',trigger:0,branch:0,clause:2,rows});
    const expected = base.replace('{8=`a.ptl`}','{8=`new``😀.ptl`}').replace('6 90 2','6 99 2');
    const start = expected.indexOf('[if]'), end = expected.indexOf('[/then]') + '[/then]'.length;
    expect(output).toBe(expected.slice(0, start) + formatItemTriggerBlock(expected.slice(start, end)) + expected.slice(end));
    expect(editItemTrigger(base,{kind:'edit-clause',trigger:0,branch:0,clause:0,rows:[['myself','-1']]})).toBe(base);
  });
  it('追加条件与动作不修改现有结构，不读取 then 内同名条件', () => {
    let text = editItemTrigger(base,{kind:'add-clause',trigger:0,name:'time',rows:[['1000','1','0']]});
    text = editItemTrigger(text,{kind:'add-clause',trigger:0,branch:0,name:'stat',rows:[['attack speed','+','10']]});
    expect(doc(text).triggers[0]!.condition.children.at(-1)?.name).toBe('time');
    expect(doc(text).triggers[0]!.branches[0]!.children.at(-1)?.name).toBe('stat');
    expect(text).toContain('# 条件与动作间注释'); expect(text).toContain('[unknown]\r\n42');
  });
  it('重复记录可增加及单独删除，保留原记录注释', () => {
    const rows = objectRows(base), extra = ['5','0','0','0','1','0',''];
    let text = editItemTrigger(base,{kind:'edit-clause',trigger:0,branch:0,clause:2,rows:[...rows,extra]});
    expect(objectRows(text)).toEqual([...rows,extra]); expect(text).toContain('# 对象一');
    text = editItemTrigger(text,{kind:'delete-row',trigger:0,branch:0,clause:2,row:0});
    expect(objectRows(text)).toEqual([rows[1],extra]); expect(text).toContain('# 对象一');
  });
  it('非闭合多行字段追加记录不吞掉后续字段，删除只删除目标字段', () => {
    const text = '[if]\n[hit]\n1\n[/if]\n[then]\n[stat]\n`attack speed` `+` 10 # old\n[duration]\n5000\n[/then]\n';
    const next=editItemTrigger(text,{kind:'edit-clause',trigger:0,branch:0,clause:0,rows:[['attack speed','+','10'],['cast speed','+','20']]});
    expect(doc(next).triggers[0]!.branches[0]!.children[1]!.tokens[0]!.value).toBe('5000');
    expect(next).toContain('# old');
    const deleted=editItemTrigger(next,{kind:'delete-clause',trigger:0,branch:0,clause:0});
    expect(deleted).toContain('[duration]\n\t\t5000'); expect(deleted).not.toContain('[stat]');
  });
  it('添加随机分支包裹现有 then，保留原分支并以零概率新增', () => {
    const result = editItemTrigger(base,{kind:'add-branch',trigger:0}), trigger=doc(result).triggers[0]!;
    expect(trigger.multiple).toBe(true); expect(trigger.branches).toHaveLength(2); expect(result).toContain('\t\t[custom]\r\n\t\t\t{5=1234} # 未知动作');
    expect(trigger.branches.map(b=>b.children.find(c=>c.name==='then probability')?.tokens[0]?.value)).toEqual(['100.0','0.0']);
    const again=editItemTrigger(result,{kind:'add-branch',trigger:0}); expect(doc(again).triggers[0]!.branches).toHaveLength(3);
    const removed=editItemTrigger(again,{kind:'delete-branch',trigger:0,branch:1}); expect(doc(removed).triggers[0]!.branches).toHaveLength(2);
    const one=editItemTrigger(removed,{kind:'delete-branch',trigger:0,branch:1}); expect(()=>editItemTrigger(one,{kind:'delete-branch',trigger:0,branch:0})).toThrow('至少保留');
  });
  it('分支概率支持浮点，动作内字符串标签和换行不改变嵌套边界', () => {
    let text=editItemTrigger('',{kind:'add-trigger',condition:'use skill',rows:[['swordman','5']],multiple:true});
    text=editItemTrigger(text,{kind:'edit-clause',trigger:0,branch:0,clause:1,rows:[['33.3']]});
    text=editItemTrigger(text,{kind:'add-clause',trigger:0,branch:0,name:'speech on',rows:[['0','2000','你好`\n[/then]']]});
    expect(doc(text).triggers[0]!.branches).toHaveLength(1); expect(text).toContain('33.3');
    expect(doc(text).triggers[0]!.condition.children[0]!.paired).toBe(true);
  });
  it('整组删除保留组间注释及后续未知字段', () => {
    const text=editItemTrigger(base,{kind:'delete-trigger',trigger:0});
    expect(doc(text).triggers).toHaveLength(0); expect(text).toContain('# 条件与动作间注释'); expect(text).toContain('[unknown]\r\n42 # 后续字段');
  });
  it('拒绝坏结构、过期位置、整数溢出、未支持字段和错误参数形状', () => {
    for (const text of ['[if]\n[time]\n1','[if]\n[/if]\n[if]\n[/if]\n[then]\n[/then]']) expect(()=>editItemTrigger(text,{kind:'add-trigger',condition:'hit',rows:[['1']],multiple:false})).toThrow();
    for(const value of ['NaN','2147483648','1\n[name]','1.2']) expect(()=>editItemTrigger(base,{kind:'add-clause',trigger:0,name:'cooltime',rows:[[value]]})).toThrow();
    expect(()=>editItemTrigger(base,{kind:'delete-trigger',trigger:9})).toThrow('已变化');
    expect(()=>editItemTrigger(base,{kind:'delete-clause',trigger:0,branch:0,clause:1})).toThrow('特殊');
    expect(()=>editItemTrigger(base,{kind:'add-clause',trigger:0,branch:0,name:'passive object',rows:[['1','2']]})).toThrow('参数');
    expect(()=>editItemTrigger(base,{kind:'add-trigger',condition:'use command',rows:[['1']],multiple:false})).toThrow('command');
  });
  it('每个预设可生成有效配对结构（资源 ID 需提供合法值）', () => {
    for(const spec of triggerClauses.filter(c=>c.name!=='use command')) {
      const row=spec.params.map(p=>p.resource&&p.kind==='integer'?'1':p.default);
      const text=editItemTrigger('',{kind:'add-trigger',condition:'hit',rows:[['1']],multiple:false});
      expect(()=>editItemTrigger(text,{kind:'add-clause',trigger:0,branch:spec.scope==='then'?0:undefined,name:spec.name,rows:[row]})).not.toThrow();
    }
  });
});

describe('操作键兼容已有 command', () => {
 it('只复用当前文件已有的 command，不生成或更改二进制 token', () => {
  const text='[command]\n{5=138}\n'+base;
  const result=editItemTrigger(text,{kind:'add-trigger',condition:'use command',rows:[['1']],multiple:false});
  expect(result).toContain(text); expect(doc(result).triggers).toHaveLength(2);
  expect(result.match(/\[command\]/g)).toHaveLength(1);
  expect(editItemTrigger(text,{kind:'add-clause',trigger:0,name:'use command',rows:[['1']]})).toContain('{5=138}');
 });
});

describe('GUI 写入 DSL 的嵌套缩进', () => {
 const expected = '[if]\n\t[attack success]\n\t\t1\n[/if]\n[multiple then]\n\t[then]\n\t\t[target]\n\t\t\t`myself`\t-1\n\t\t[then probability]\n\t\t\t100.0\n\t[/then]\n[/multiple then]\n';
 it('新建多分支按容器层级缩进，前面的普通物品字段保持原样', () => {
  const prefix='[anti evil]\n\t340\n';
  expect(editItemTrigger(prefix,{kind:'add-trigger',condition:'attack success',rows:[['1']],multiple:true})).toBe(prefix+expected);
 });
 it('新增恢复动作和随机分支正确缩进，不在动作前额外插入空行', () => {
  let text=editItemTrigger('',{kind:'add-trigger',condition:'attack success',rows:[['1']],multiple:true});
  text=editItemTrigger(text,{kind:'add-clause',trigger:0,branch:0,name:'restore',rows:[['hp','%','10','0']]});
  text=editItemTrigger(text,{kind:'add-branch',trigger:0});
  expect(text).toContain('\t\t[restore]\n\t\t\t`hp`\t`%`\t10\t0\n\t[/then]');
  expect(text).not.toContain('\n\n[restore]');
  expect(text).toContain('\t[then]\n\t\t[target]\n\t\t\t`myself`\t-1\n\t\t[then probability]\n\t\t\t0.0\n\t[/then]\n[/multiple then]');
 });
 it('修改旧版无缩进草稿时修正当前触发器层级，保留其他段落、换行与 token 文本', () => {
  const prefix='[anti evil]\r\n\t340\r\n',suffix='\r\n[unknown]\r\n {5=9} # 不触碰\r\n';
  const body='[if]\r\n[attack success]\r\n1\r\n[/if]\r\n[multiple then]\r\n[then]\r\n[target]\r\n{7=`myself`} -1\r\n[restore]\r\n`hp` `%` 10 0 # 恢复\r\n[then probability]\r\n100.0\r\n[/then]\r\n[/multiple then]';
  const text=editItemTrigger(prefix+body+suffix,{kind:'edit-clause',trigger:0,branch:0,clause:1,rows:[['hp','%','20','0']]});
  expect(text.startsWith(prefix)).toBe(true);expect(text.endsWith(suffix)).toBe(true);
  expect(text).toContain('\t\t[restore]\r\n\t\t\t`hp` `%` 20 0 # 恢复');
  expect(text).toContain('\t\t\t{7=`myself`} -1');
  expect(text.replaceAll('\r\n','')).not.toContain('\n');
 });
 it('嵌套闭合动作逐层缩进，多行字符串和注释的内容不变，重复格式化幂等', () => {
  const raw='[if]\n[use skill]\n`swordman` 5\n[/use skill]\n[/if]\n[then]\n[speech on]\n0 2000 `第一行\n  [then]\n\t原有缩进😀` # 注释\n[/speech on]\n/* 保留\n    [unknown]\n*/\n[/then]';
  const formatted=formatItemTriggerBlock(raw);
  expect(formatted).toContain('\t[use skill]\n\t\t`swordman` 5\n\t[/use skill]');
  expect(formatted).toContain('`第一行\n  [then]\n\t原有缩进😀` # 注释');
  expect(formatted).toContain('/* 保留\n    [unknown]\n*/');
  expect(formatted).toContain('\t[/speech on]\n');
  expect(formatItemTriggerBlock(formatted)).toBe(formatted);
 });
 it('不修改没有变化的表单，闭合标签同一行时不会截断或改写 token', () => {
  expect(editItemTrigger(base,{kind:'edit-clause',trigger:0,branch:0,clause:2,rows:objectRows(base)})).toBe(base);
  let text='[if] [hit] 1 [/if]\n[then] [target] `myself` -1 [/then]';
  text=editItemTrigger(text,{kind:'add-clause',trigger:0,branch:0,name:'restore',rows:[['hp','%','10','0']]});
  expect(doc(text).triggers[0]!.branches[0]!.children.map(s=>s.name)).toEqual(['target','restore']);
 });
});
