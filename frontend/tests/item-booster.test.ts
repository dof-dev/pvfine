import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { addBoosterCategory, boosterGroupLabel, boosterRewardName, editBooster, isFlatBoosterSelection, itemTemplate, newBoosterRow, readBooster } from '../src/gui/itemBooster';
const booster='#PVF_File\r\n[stackable type]\r\n`[booster]` 0\r\n[booster info]\r\n[etc]\r\n1 100 500 2 # 第一项\r\n[/etc]\r\n[stackable]\r\n1 200 500 3 # 第二项\r\n[/stackable]\r\n[/booster info]\r\n[unknown]\r\n{5=1234}\r\n';
// Keep fixture baselines stable under core.autocrlf; CRLF cases are constructed explicitly.
const fixture=(name:string)=>readFileSync(new URL(`./fixtures/${name}`,import.meta.url),'utf8').replace(/\r\n?/g,'\n');
const uncategorizedSelection=fixture('uncategorized-selection.stk');
const epicSelection=fixture('epic-fragment-selection.stk');
const emptyPromptSelection=fixture('empty-prompt-selection.stk');
const clone=(text:string)=>JSON.parse(JSON.stringify(readBooster(text))) as ReturnType<typeof readBooster>;
describe('礼包编辑',()=>{
 it('模板可解析，普通材料不出现礼包表单',()=>{
  expect(readBooster(itemTemplate('booster'))).toMatchObject({mode:'booster',issue:''});
  expect(readBooster(itemTemplate('selection'))).toMatchObject({mode:'selection',issue:''});
  expect(readBooster(itemTemplate('material')).mode).toBeNull();
 });
 it('精确修改奖励数量与权重，保留未知段落、注释、换行及行类型',()=>{
  const draft=clone(booster);expect(draft.issue).toBe('');
  draft.groups[0]!.rows[0]!.quantity='5';draft.groups[0]!.rows[1]!.weight='0';
  expect(editBooster(booster,draft)).toBe(booster.replace('500 2','500 5').replace('500 3','0 3'));
 });
 it('删除指定奖励保留其他奖励原文，追加奖励位于容器内',()=>{
  const draft=clone(booster);draft.groups[0]!.rows.splice(0,1);draft.groups[0]!.rows.push({poolIndex:1,id:'300',quantity:'4',weight:'100'});
  const text=editBooster(booster,draft);
  expect(text).not.toContain('# 第一项');expect(text).toContain('[stackable]\r\n1 200 500 3 # 第二项\r\n');
  expect(readBooster(text).groups[0]!.rows.map(r=>r.id)).toEqual(['200','300']);expect(text).toContain('[unknown]\r\n{5=1234}');
 });
 it('自选礼包支持说明、可选数量及奖励增删，保留原分类坐标',()=>{
  const original=itemTemplate('selection').replace('1 1\n[/etc]','100 2 # 第一项\n200 3 # 第二项\n[/etc]');
  const draft=clone(original);expect(draft.issue).toBe('');
  draft.names[1]='材料礼包';draft.selection='2';draft.groups[0]!.rows.splice(0,1);draft.groups[0]!.rows.push({id:'300',quantity:'5',weight:''});
  const text=editBooster(original,draft),next=readBooster(text);
  expect(next.issue).toBe('');expect(next.selection).toBe('2');expect(boosterGroupLabel(next,next.groups[0]!)).toBe('材料礼包');
  expect(next.groups[0]!.rows.map(r=>[r.id,r.quantity])).toEqual([['200','3'],['300','5']]);
  expect(text).toContain('200 3 # 第二项');expect(text).toContain('# 第一项');expect(text).toContain('[booster select category]\n0 0');
 });
 it('无分类徽章自选礼包读取八项奖励，仅有一条选择提示',()=>{
  const doc=readBooster(uncategorizedSelection);
  expect(doc.issue).toBe('');expect(doc.dimensions).toEqual(['0']);expect(doc.selection).toBe('1');
  expect(doc.names).toEqual(['请选择徽章。']);expect(doc.groups).toHaveLength(1);
  expect(doc.groups[0]!.coordinates).toEqual(['0','0']);expect(boosterGroupLabel(doc,doc.groups[0]!)).toBe('可选物品');
  expect(doc.groups[0]!.rows.map(row=>[row.id,row.quantity])).toEqual(['2500033','2500036','2510046','2510037','2520037','2520043','2530040','2530037'].map(id=>[id,'4']));
  expect(editBooster(uncategorizedSelection,clone(uncategorizedSelection))).toBe(uncategorizedSelection);
 });
 it('无分类礼包编辑保留 0 维度、stackable 标签及其他字段，支持 CRLF',()=>{
  const text=uncategorizedSelection.replaceAll('\n','\r\n'),draft=clone(text);
  draft.groups[0]!.rows[1]!.quantity='8';draft.names[0]='请选择一种徽章。';
  expect(editBooster(text,draft)).toBe(text.replace('2500036\t4','2500036\t8').replace('请选择徽章。','请选择一种徽章。'));
 });
 it('无分类礼包可增删候选奖励，保留奖励容器和提示结构',()=>{
  const draft=clone(uncategorizedSelection);draft.groups[0]!.rows.splice(0,1);
  draft.groups[0]!.rows.push({id:'2500034',quantity:'2',weight:''});
  const text=editBooster(uncategorizedSelection,draft),next=readBooster(text);
  expect(next.issue).toBe('');expect(next.dimensions).toEqual(['0']);expect(next.names).toEqual(['请选择徽章。']);
  expect(next.groups[0]!.rows.map(row=>row.id)).toEqual(['2500036','2510046','2510037','2520037','2520043','2530040','2530037','2500034']);
  expect(text).toContain('[stackable]');expect(text).toContain('[/stackable]');expect(text).not.toContain('[etc]');
  expect(()=>addBoosterCategory(draft)).toThrow('仅单层分类');
 });
 it('无分类也兼容 etc 奖励容器，但拒绝多余维度、分类坐标、提示和内容段',()=>{
  const etc=uncategorizedSelection.replace('[stackable]','[etc]').replace('[/stackable]','[/etc]');
  expect(readBooster(etc).issue).toBe('');
  for(const text of [
   uncategorizedSelection.replace('[booster category num]\n\t0','[booster category num]\n\t0 1'),
   uncategorizedSelection.replace('[booster select category]\n\t0\t0','[booster select category]\n\t1\t0'),
   uncategorizedSelection.replace('[booster select category]\n\t0\t0','[booster select category]\n\t0\t1'),
   uncategorizedSelection.replace('`请选择徽章。`','`请选择徽章。` `不应存在的分类`'),
   uncategorizedSelection.replace('[/booster select category]','[/booster select category]\n[booster select category]\n0 0\n[etc]\n1 1\n[/etc]\n[/booster select category]'),
  ])expect(readBooster(text).issue).not.toBe('');
 });
 it('二维史诗碎片礼包保留空分类和省略的尾部名称，未修改时原文往返',()=>{
  const doc=readBooster(epicSelection);
  expect(doc.issue).toBe('');expect(doc.dimensions).toEqual(['2','8','5']);expect(doc.groups).toHaveLength(40);
  expect(doc.groups.filter(group=>!group.rows.length)).toHaveLength(7);expect(doc.names).toHaveLength(47);
  expect(boosterGroupLabel(doc,doc.groups[35]!)).toBe('魔枪士 / 长枪');
  expect(boosterGroupLabel(doc,doc.groups[36]!)).toBe('魔枪士 / 战戟');
  expect(editBooster(epicSelection,clone(epicSelection))).toBe(epicSelection);
 });
 it('修改非空分类奖励不补写空分类容器或名称，保留 CRLF 和其他字段',()=>{
  const text=epicSelection.replaceAll('\n','\r\n'),draft=clone(text);
  draft.groups[0]!.rows[0]!.quantity='60';
  expect(editBooster(text,draft)).toBe(text.replace('10300537\t50','10300537\t60'));
 });
 it('已有名称的空分类可以添加奖励，沿用 stackable 容器',()=>{
  const draft=clone(epicSelection),empty=draft.groups.find(group=>group.coordinates.join(':')==='5:4')!;
  empty.rows.push({id:'10300665',quantity:'50',weight:''});
  const text=editBooster(epicSelection,draft),next=readBooster(text);
  expect(next.issue).toBe('');expect(next.groups.filter(group=>!group.rows.length)).toHaveLength(6);
  expect(next.groups.find(group=>group.coordinates.join(':')==='5:4')!.rows[0]!.id).toBe('10300665');
  expect(text).toContain('[booster select category]\n\t5\t4\n\t[stackable]');expect(text).not.toContain('[etc]');
  expect(next.names).toHaveLength(47);
 });
 it('空分类可以保留空奖励容器，但仍拒绝未知内容、多个奖励段和缺失有效名称',()=>{
  const emptyContainer=epicSelection.replace('\t5\t4\n[/booster select category]','\t5\t4\n[stackable]\n[/stackable]\n[/booster select category]');
  expect(readBooster(emptyContainer).issue).toBe('');expect(editBooster(emptyContainer,clone(emptyContainer))).toBe(emptyContainer);
  const extra=epicSelection.replace('\t5\t4\n[/booster select category]','\t5\t4\n[etc]\n1 1\n[/etc]\n[stackable]\n2 1\n[/stackable]\n[/booster select category]');
  expect(readBooster(extra).issue).toContain('特殊自选礼包');
  expect(readBooster(epicSelection.replace('`长枪`\t`战戟`','`长枪`')).issue).toContain('分类名称');
  const missing=clone(epicSelection);missing.groups[39]!.rows.push({id:'10300665',quantity:'50',weight:''});
  expect(()=>editBooster(epicSelection,missing)).toThrow('没有名称');
 });
 it('equipment 二维自选分类按 ID、数量配对读取，保留 7 5 坐标',()=>{
  const blocks=Array.from({length:48},(_,i)=>`[booster select category]\n${Math.floor(i/6)} ${i%6}\n${i===47?'[equipment]\n109010320 1 109000332 1 109030219 1 109040220 1\n[/equipment]\n':''}[/booster select category]`).join('\n');
  const names=['请选择职业',...Array.from({length:8},(_,i)=>`职业${i+1}`),'请选择装备',...Array.from({length:48},(_,i)=>`装备分类${i+1}`)];
  const text=`[stackable type]\n\`[booster selection]\` 0\n[booster category num]\n2 8 6\n[booster selection num]\n1\n${blocks}\n[booster category name]\n${names.map(name=>'`'+name+'`').join(' ')}\n[/booster category name]\n`;
  const doc=readBooster(text);expect(doc.issue).toBe('');expect(doc.groups[47]!.coordinates).toEqual(['7','5']);
  expect(doc.groups[47]!.rows.map(row=>[row.id,row.quantity])).toEqual(['109010320','109000332','109030219','109040220'].map(id=>[id,'1']));
  expect(editBooster(text,clone(text))).toBe(text);
  const draft=clone(text);draft.groups[47]!.rows[0]!.quantity='2';
  expect(editBooster(text,draft)).toBe(text.replace('109010320 1','109010320 2'));
 });
 it('equipment 自选奖励可增删，保留容器、注释与 CRLF',()=>{
  const text=itemTemplate('selection').replace('[etc]\n1 1\n[/etc]','[equipment]\n109010320 1 # 第一件\n109000332 1 # 第二件\n[/equipment]').replaceAll('\n','\r\n');
  const draft=clone(text);expect(draft.issue).toBe('');draft.groups[0]!.rows.splice(0,1);draft.groups[0]!.rows.push({id:'109030219',quantity:'1',weight:''});
  const result=editBooster(text,draft),doc=readBooster(result);
  expect(doc.issue).toBe('');expect(doc.groups[0]!.rows.map(row=>row.id)).toEqual(['109000332','109030219']);
  expect(result).toContain('109000332 1 # 第二件');expect(result).toContain('[equipment]');expect(result).toContain('[/equipment]');expect(result).not.toContain('[etc]');
 });
 it('equipment 自选礼包新增分类和填充空分类时沿用 equipment 容器',()=>{
  const text=itemTemplate('selection').replaceAll('[etc]','[equipment]').replaceAll('[/etc]','[/equipment]');
  const draft=clone(text);addBoosterCategory(draft);draft.groups[1]!.rows[0]!.id='109040220';
  const result=editBooster(text,draft);expect(readBooster(result).issue).toBe('');
  expect(result.match(/\[equipment\]/g)).toHaveLength(2);expect(result).not.toContain('[etc]');
  const empty=result.replace(/\t\[equipment\]\n\t\t109040220\t1\n\t\[\/equipment\]\n/,'');
  const filled=clone(empty);expect(filled.issue).toBe('');filled.groups[1]!.rows.push({id:'109040220',quantity:'1',weight:''});
  expect(editBooster(empty,filled)).toContain('[equipment]\n\t\t109040220\t1');
 });
 it('1 0 无分类宝珠礼包允许单个空提示并读取十四项奖励，原文往返不补写名称',()=>{
  const doc=readBooster(emptyPromptSelection);
  expect(doc.issue).toBe('');expect(doc.dimensions).toEqual(['1','0']);expect(doc.names).toEqual(['']);
  expect(doc.groups).toHaveLength(1);expect(doc.groups[0]!.rows).toHaveLength(14);
  expect(doc.groups[0]!.rows.map(row=>row.id)).toEqual(['490700960',...Array.from({length:13},(_,i)=>String(490700964+i))]);
  expect(isFlatBoosterSelection(doc)).toBe(true);expect(boosterGroupLabel(doc,doc.groups[0]!)).toBe('可选物品');
  expect(editBooster(emptyPromptSelection,clone(emptyPromptSelection))).toBe(emptyPromptSelection);
 });
 it('修改宝珠奖励保留 1 0、空提示、原容器及多行说明，支持 CRLF',()=>{
  const text=emptyPromptSelection.replaceAll('\n','\r\n'),draft=clone(text);
  draft.groups[0]!.rows[0]!.quantity='2';
  expect(editBooster(text,draft)).toBe(text.replace('490700960\t1','490700960\t2'));
 });
 it('1 0 无分类礼包可增删奖励和填写或清空选择提示',()=>{
  const draft=clone(emptyPromptSelection);draft.groups[0]!.rows.splice(0,1);draft.groups[0]!.rows.push({id:'490700977',quantity:'1',weight:''});
  let text=editBooster(emptyPromptSelection,draft),doc=readBooster(text);
  expect(doc.issue).toBe('');expect(doc.names).toEqual(['']);expect(doc.groups[0]!.rows).toHaveLength(14);
  expect(doc.groups[0]!.rows.at(-1)!.id).toBe('490700977');
  doc.names[0]='请选择宝珠。';text=editBooster(text,doc);expect(readBooster(text).names).toEqual(['请选择宝珠。']);
  doc=clone(text);doc.names[0]='';expect(readBooster(editBooster(text,doc)).names).toEqual(['']);
 });
 it('1 0 兼容两条旧式名称，仍拒绝多余名称和错误分类坐标',()=>{
  const legacy=emptyPromptSelection.replace('\t``','\t`请选择。` `全部物品`');
  expect(readBooster(legacy).issue).toBe('');expect(editBooster(legacy,clone(legacy))).toBe(legacy);
  expect(readBooster(emptyPromptSelection.replace('\t``','\t`` `多余` `名称`')).issue).toContain('分类名称');
  expect(readBooster(emptyPromptSelection.replace('[booster select category]\n\t0\t0','[booster select category]\n\t1\t0')).issue).toContain('坐标');
  expect(isFlatBoosterSelection(readBooster(itemTemplate('selection')))).toBe(false);
  expect(()=>addBoosterCategory(clone(emptyPromptSelection))).toThrow('仅单层分类');
 });
 it('新增单层分类同步名称、数量和坐标，不改动原分类',()=>{
  const text=itemTemplate('selection'),draft=clone(text);
  addBoosterCategory(draft);draft.groups[1]!.rows[0]!.id='200';
  addBoosterCategory(draft);draft.groups[2]!.rows[0]!.id='300';
  const result=editBooster(text,draft),next=readBooster(result);
  expect(next.issue).toBe('');expect(next.dimensions).toEqual(['1','3']);
  expect(next.groups.map(g=>g.coordinates)).toEqual([['0','0'],['1','0'],['2','0']]);
  expect(next.groups.map(g=>g.rows[0]!.id)).toEqual(['1','200','300']);expect(next.names).toEqual(['请选择。','类别 1','类别 2','类别 3']);
 });
 it('二维分类按坐标显示名称，重复坐标与未知结构只读',()=>{
  const base=itemTemplate('selection');
  const text=base.replace('[booster category num]\n1 1','[booster category num]\n2 1 2').replace('[booster category name]', '[booster select category]\n0 1\n[etc]\n2 1\n[/etc]\n[/booster select category]\n[booster category name]').replace('`请选择。` `类别 1`','`请选择。` `类别 1` `请选择物品` `物品甲` `物品乙`');
  const doc=readBooster(text);expect(doc.issue).toBe('');expect(boosterGroupLabel(doc,doc.groups[1]!)).toBe('类别 1 / 物品乙');
  expect(readBooster(text.replace('0 1\n[etc]','0 0\n[etc]')).issue).toContain('坐标');
  expect(readBooster(booster.replace('[stackable]','[custom]').replace('[/stackable]','[/custom]')).issue).toContain('特殊');
 });
 it('拒绝缺失闭合、重复主段落、非法数值和过期奖励定位',()=>{
  expect(readBooster(booster.replace('[/booster info]','')).issue).toContain('闭合');
  expect(readBooster(booster+'[booster info]\n[/booster info]').issue).toContain('重复');
  const draft=clone(booster);draft.groups[0]!.rows[0]!.id='2147483648';expect(()=>editBooster(booster,draft)).toThrow('整数');
  draft.groups[0]!.rows[0]!.id='100';draft.groups[0]!.rows[1]!.sourceIndex=0;expect(()=>editBooster(booster,draft)).toThrow('奖励位置');
 });
 it('保留分类名称字符串引用的类型标记，禁止写成字面译文',()=>{
  const text=itemTemplate('selection').replace('`类别 1`','{8=`<31::category_name>`}');
  const draft=clone(text);expect(editBooster(text,draft)).toBe(text);
  draft.names[1]='译文';expect(()=>editBooster(text,draft)).toThrow('字符串表引用');
 });
});

describe('avatar 自选奖励',()=>{
 const ids=['2685210','2685338','2685470','2685595','2685730','2685860','2685989','2686119'];
 const avatar=itemTemplate('selection').replace('[etc]\n1 1\n[/etc]',`[avatar]\n${ids.map(id=>`${id} 1 3 0`).join('\n')}\n[/avatar]`);
 it('八件时装按四参数读取，原文往返，修改数量和附加参数仅替换对应 token',()=>{
  const text=avatar.replaceAll('\n','\r\n'),draft=clone(text);
  expect(draft.issue).toBe('');expect(draft.groups[0]!.rows).toHaveLength(8);
  expect(draft.groups[0]!.rows.map(r=>r.id)).toEqual(ids);
  expect(draft.groups[0]!.rows.every(r=>r.avatarParams?.join()==='3,0')).toBe(true);
  expect(editBooster(text,draft)).toBe(text);
  draft.groups[0]!.rows[1]!.quantity='2';draft.groups[0]!.rows[2]!.avatarParams=['4','-1'];
  expect(editBooster(text,draft)).toBe(text.replace('2685338 1 3 0','2685338 2 3 0').replace('2685470 1 3 0','2685470 1 4 -1'));
 });
 it('增删按四参数定位并保留注释，新奖励继承附加参数且独立编辑',()=>{
  const text=avatar.replace('2685338 1 3 0','2685338 1 3 0 # 保留注释');
  const draft=clone(text),group=draft.groups[0]!;
  group.rows.splice(0,1);const row=newBoosterRow(draft,group);row.id='2686120';group.rows.push(row);
  row.avatarParams![0]='5';expect(group.rows[0]!.avatarParams).toEqual(['3','0']);
  const result=editBooster(text,draft),doc=readBooster(result);
  expect(doc.issue).toBe('');expect(doc.groups[0]!.rows.map(r=>r.id)).toEqual([...ids.slice(1),'2686120']);
  expect(doc.groups[0]!.rows.at(-1)!.avatarParams).toEqual(['5','0']);
  expect(result).toContain('2685338 1 3 0 # 保留注释');
 });
 it('新增分类和填充空分类使用 avatar 四参数容器',()=>{
  const draft=clone(avatar);addBoosterCategory(draft);draft.groups[1]!.rows[0]!.id='2686120';
  const result=editBooster(avatar,draft);expect(readBooster(result).issue).toBe('');
  expect(result).toContain('[avatar]\n\t\t2686120\t1\t3\t0');
  const empty=result.replace(/\t\[avatar\]\n\t\t2686120\t1\t3\t0\n\t\[\/avatar\]\n/,'');
  const filled=clone(empty),group=filled.groups[1]!,row=newBoosterRow(filled,group);row.id='2686121';group.rows.push(row);
  const next=readBooster(editBooster(empty,filled));expect(next.issue).toBe('');
  expect(next.groups[1]!.rows[0]!.avatarParams).toEqual(['3','0']);
 });
 it('二维 1 2 坐标兼容混合奖励类型，各容器使用自己的参数宽度',()=>{
  const blocks=Array.from({length:6},(_,i)=>`[booster select category]\n${Math.floor(i/3)} ${i%3}\n${i===5?'[avatar]\n'+ids.map(id=>`${id} 1 3 0`).join(' ' )+'\n[/avatar]':i===0?'[equipment]\n109010320 1\n[/equipment]':i===1?'[stackable]\n100 2\n[/stackable]':''}\n[/booster select category]`).join('\n');
  const text=`[stackable type]\n\`[booster selection]\` 0\n[booster category num]\n2 2 3\n[booster selection num]\n1\n${blocks}\n[booster category name]\n${Array.from({length:10},(_,i)=>'\`分类'+i+'\`').join(' ')}\n[/booster category name]`;
  const draft=clone(text);expect(draft.issue).toBe('');expect(draft.groups[5]!.coordinates).toEqual(['1','2']);
  expect(editBooster(text,draft)).toBe(text);
  draft.groups[5]!.rows[7]!.quantity='4';draft.groups[0]!.rows[0]!.quantity='2';
  expect(editBooster(text,draft)).toBe(text.replace('2686119 1 3 0','2686119 4 3 0').replace('109010320 1','109010320 2'));
 });
 it('缺少附加参数或参数非法时拒绝写入，空 avatar 容器新增时要求填写',()=>{
  expect(readBooster(avatar.replace('2685210 1 3 0','2685210 1 3')).issue).toContain('两个附加参数');
  for(const params of [undefined,['3'],['3',''],['3','2147483648'],['3','1.5']]){
   const draft=clone(avatar);draft.groups[0]!.rows[0]!.avatarParams=params as [string,string]|undefined;
   expect(()=>editBooster(avatar,draft)).toThrow('附加参数');
  }
  const text=itemTemplate('selection').replace('[etc]\n1 1\n[/etc]','[avatar]\n[/avatar]'),draft=clone(text);
  const row=newBoosterRow(draft,draft.groups[0]);row.id='2685210';draft.groups[0]!.rows.push(row);
  expect(row.avatarParams).toEqual(['','']);expect(()=>editBooster(text,draft)).toThrow('附加参数');
  row.avatarParams=['3','0'];expect(readBooster(editBooster(text,draft)).issue).toBe('');
 });
});

describe('普通礼包段内连续奖励',()=>{
 const thanksgiving=fixture('thanksgiving-booster.stk');
 const packed=booster.replace('1 100 500 2 # 第一项','1 100 500 2 # 第一项\r\n101 300 4 # 同段第二项\r\n102 200 6 # 同段第三项');
 it('感恩礼盒读取 508 项奖励，随机获得项数仅出现一次，原文往返',()=>{
  const draft=clone(thanksgiving);expect(draft.issue).toBe('');expect(draft.groups[0]!.rows).toHaveLength(508);
  expect(draft.groups[0]!.rows[0]).toMatchObject({sourceIndex:0,id:'15646',weight:'68',quantity:'1'});
  expect(draft.groups[0]!.rows[1]).toMatchObject({sourceIndex:1,id:'18913',weight:'68',quantity:'1'});
  expect(draft.groups[0]!.rows.at(-1)).toMatchObject({sourceIndex:507,id:'10102',weight:'67',quantity:'1'});
  expect(draft.groups[0]!.rows.reduce((sum,r)=>sum+Number(r.weight),0)).toBe(100000);
  expect(editBooster(thanksgiving,draft)).toBe(thanksgiving);
 });
 it('大列表按记录精确修改 ID、数量及权重，不拆分原容器',()=>{
  const text=thanksgiving.replaceAll('\n','\r\n'),draft=clone(text),last=draft.groups[0]!.rows.at(-1)!;
  last.id='10104';last.weight='70';last.quantity='2';
  expect(editBooster(text,draft)).toBe(text.replace('10102\t67\t1','10104\t70\t2'));
 });
 it('混合单项和多项段时定位正确，保留注释、标记、CRLF 和其他字段',()=>{
  const text=packed,draft=clone(text);
  expect(draft.issue).toBe('');expect(draft.groups[0]!.rows.map(r=>r.sourceIndex)).toEqual([0,1,2,3]);
  draft.groups[0]!.rows[1]!.id='103';draft.groups[0]!.rows[3]!.quantity='9';
  expect(editBooster(text,draft)).toBe(text.replace('101 300 4','103 300 4').replace('500 3','500 9'));
 });
 it('删除段内首项、中项或末项仅移除对应三个参数，其他记录保持正确',()=>{
  for(const index of [0,1,2]) {
   const draft=clone(packed);draft.groups[0]!.rows.splice(index,1);
   const result=editBooster(packed,draft),next=readBooster(result);
   expect(next.issue).toBe('');expect(next.groups[0]!.rows.map(r=>r.id)).toEqual(['100','101','102','200'].filter((_,i)=>i!==index));
   expect(result).toContain('[etc]');expect(result).toContain('# 第一项');expect(result).toContain('# 同段第二项');expect(result).toContain('[unknown]\r\n{5=1234}');
  }
 });
 it('删除整个多项段后仍能编辑后续段并新增奖励',()=>{
  const draft=clone(packed);draft.groups[0]!.rows.splice(0,3);draft.groups[0]!.rows[0]!.weight='700';
  draft.groups[0]!.rows.push({poolIndex:1,id:'300',quantity:'2',weight:'300'});
  const result=editBooster(packed,draft),next=readBooster(result);
  expect(next.issue).toBe('');expect(next.groups[0]!.rows.map(r=>[r.id,r.weight,r.quantity])).toEqual([['200','700','3'],['300','300','2']]);
  expect(result).not.toContain('# 第一项');expect(result).toContain('[stackable]');
 });
 it('随机获得项数按奖励池修改，不改变候选数量与每项数量；拒绝不完整记录',()=>{
  const draft=clone(packed);draft.groups[0]!.pools![0]!.drawCount='2';
  const result=editBooster(packed,draft),next=readBooster(result);
  expect(result).toBe(packed.replace('1 100 500 2','2 100 500 2'));
  expect(next.groups[0]!.pools!.map(p=>p.drawCount)).toEqual(['2','1']);
  expect(next.groups[0]!.rows.map(r=>r.quantity)).toEqual(['2','4','6','3']);
  for(const values of ['1','1 100 2','1 100 500 2 101 300'])expect(readBooster(booster.replace('1 100 500 2',values)).issue).toContain('成组');
 });
});


describe('无分类礼包可省略分类名称段',()=>{
 const withoutNames=(text:string)=>text.replace(/\[booster category name\][\s\S]*?\[\/booster category name\]\r?\n?/,'');
 for(const [label,source] of [['0',uncategorizedSelection],['1 0',emptyPromptSelection]]) {
  it(`${label} 缺少名称段可解析，未修改时原文往返，编辑数量不补写名称`,()=>{
   const text=withoutNames(source!).replaceAll('\n','\r\n'),draft=clone(text);
   expect(draft.issue).toBe('');expect(draft.names).toEqual([]);expect(isFlatBoosterSelection(draft)).toBe(true);
   expect(editBooster(text,draft)).toBe(text);
   const first=draft.groups[0]!.rows[0]!;
   first.quantity='9';draft.selection='2';
   const result=editBooster(text,draft),next=readBooster(result);
   expect(next.issue).toBe('');expect(next.selection).toBe('2');expect(next.groups[0]!.rows[0]!.quantity).toBe('9');
   expect(result).not.toContain('[booster category name]');expect(next.names).toEqual([]);
  });
  it(`${label} 缺少名称段可增删奖励，仍禁止改动分类结构或凭空写入名称`,()=>{
   const text=withoutNames(source!),draft=clone(text),group=draft.groups[0]!;
   group.rows.splice(0,1);group.rows.push({id:'999',quantity:'2',weight:''});
   const result=editBooster(text,draft),next=readBooster(result);
   expect(next.issue).toBe('');expect(next.groups[0]!.rows).toHaveLength(group.rows.length);
   expect(next.groups[0]!.rows.at(-1)!.id).toBe('999');expect(result).not.toContain('[booster category name]');
   draft.names=['新增名称'];expect(()=>editBooster(text,draft)).toThrow('分类名称');
   draft.names=[];draft.dimensions=['1','2'];expect(()=>editBooster(text,draft)).toThrow('分类结构');
  });
 }
 it('有分类仍要求名称段，无分类现存名称段的重复和非法内容仍报错',()=>{
  expect(readBooster(withoutNames(itemTemplate('selection'))).issue).toContain('缺失');
  expect(readBooster(withoutNames(epicSelection)).issue).toContain('缺失');
  expect(readBooster(uncategorizedSelection+'\n[booster category name]\n`重复`\n[/booster category name]').issue).toContain('重复');
  expect(readBooster(emptyPromptSelection.replace('``','123')).issue).toContain('名称结构');
 });
});

describe('随机礼盒抽取语义',()=>{
 const text=itemTemplate('booster').replace('1 1 1000 1','2 100 300 4\n200 700 6');
 it('抽取项数属于池，每项发放数量单独保留',()=>{
  const doc=readBooster(text),group=doc.groups[0]!;
  expect(doc.issue).toBe('');expect(group.pools).toHaveLength(1);expect(group.pools![0]!.drawCount).toBe('2');
  expect(group.rows.map(r=>[r.poolIndex,r.id,r.quantity])).toEqual([[0,'100','4'],[0,'200','6']]);
  expect(editBooster(text,clone(text))).toBe(text);
 });
 it('新增候选追加到指定池，保留抽取项数，不额外创建抽取池',()=>{
  const draft=clone(text),group=draft.groups[0]!,row=newBoosterRow(draft,group,0);row.id='300';row.quantity='8';group.rows.push(row);
  const result=editBooster(text,draft),next=readBooster(result);
  expect(next.issue).toBe('');expect(next.groups[0]!.pools).toHaveLength(1);expect(next.groups[0]!.pools![0]!.drawCount).toBe('2');
  expect(next.groups[0]!.rows.map(r=>r.id)).toEqual(['100','200','300']);
  expect(result.match(/\[etc\]/g)).toHaveLength(1);
 });
 it('各池抽取项数独立，修改第二池并新增候选不影响第一池',()=>{
  const original=text.replace('[/booster info]','[etc]\n1 400 1000 3\n[/etc]\n[/booster info]'),draft=clone(original),group=draft.groups[0]!;
  group.pools![1]!.drawCount='3';const row=newBoosterRow(draft,group,1);row.id='500';group.rows.push(row);
  const result=editBooster(original,draft),next=readBooster(result);
  expect(next.groups[0]!.pools!.map(p=>p.drawCount)).toEqual(['2','3']);
  expect(next.groups[0]!.rows.map(r=>[r.poolIndex,r.id])).toEqual([[0,'100'],[0,'200'],[1,'400'],[1,'500']]);
  expect(result).toContain('[etc]\n2 100 300 4\n200 700 6\n[/etc]');
 });
 it('抽取项数必须为正整数，拒绝不存在的池和跨池移动',()=>{
  for(const count of ['0','-1','1.5','2147483648','']){
   const draft=clone(text);draft.groups[0]!.pools![0]!.drawCount=count;
   expect(()=>editBooster(text,draft)).toThrow('随机获得项数');
  }
  const draft=clone(booster);draft.groups[0]!.rows[0]!.poolIndex=1;expect(()=>editBooster(booster,draft)).toThrow('跨随机池');
  const unknown=clone(text),row=newBoosterRow(unknown,unknown.groups[0],9);row.id='300';unknown.groups[0]!.rows.push(row);
  expect(()=>editBooster(text,unknown)).toThrow('随机池无效');
 });
});

describe('自选装备强化等级',()=>{
 const text=itemTemplate('selection').replace('[etc]\n1 1\n[/etc]','[booster equipment upgrade]\n\t10\n[equipment]\n401040027 1 401030028 1 401010027 1 401020031 1 401000031 1\n[/equipment]');
 it('读取强化等级与五件装备，名称带前缀，未修改时原文往返',()=>{
  const doc=readBooster(text),group=doc.groups[0]!;
  expect(doc.issue).toBe('');expect(group.equipmentUpgrade).toBe('10');expect(group.rows).toHaveLength(5);
  expect(group.rows.map(r=>r.id)).toEqual(['401040027','401030028','401010027','401020031','401000031']);
  expect(boosterRewardName(group,'装备名')).toBe('+10 装备名');
  expect(editBooster(text,clone(text))).toBe(text);
 });
 it('修改数量和增删奖励不改强化段、注释或换行，仍使用 equipment 容器',()=>{
  const original=text.replace('\t10','\t10 # 保留强化注释').replaceAll('\n','\r\n'),draft=clone(original),group=draft.groups[0]!;
  group.rows[1]!.quantity='3';
  expect(editBooster(original,draft)).toBe(original.replace('401030028 1','401030028 3'));
  group.rows.splice(0,1);const row=newBoosterRow(draft,group);row.id='401040028';group.rows.push(row);
  const result=editBooster(original,draft),next=readBooster(result);
  expect(next.issue).toBe('');expect(next.groups[0]!.equipmentUpgrade).toBe('10');
  expect(next.groups[0]!.rows.map(r=>r.id)).toEqual(['401030028','401010027','401020031','401000031','401040028']);
  expect(result).toContain('[booster equipment upgrade]\r\n\t10 # 保留强化注释\r\n[equipment]');
 });
 it('强化等级按分类隔离，新分类不自动继承强化等级，零强化明确显示',()=>{
  const draft=clone(text);addBoosterCategory(draft);draft.groups[1]!.rows[0]!.id='401040028';
  const next=readBooster(editBooster(text,draft));expect(next.issue).toBe('');
  expect(boosterRewardName(next.groups[0],'装备甲')).toBe('+10 装备甲');
  expect(boosterRewardName(next.groups[1],'装备乙')).toBe('装备乙');
  expect(boosterRewardName(readBooster(text.replace('\t10','\t0')).groups[0],'装备')).toBe('+0 装备');
 });
 it('空强化分类可添加装备，并沿用 equipment 容器而非强化字段标签',()=>{
  const empty=text.replace(/401040027 1[^\n]*/,''),draft=clone(empty),group=draft.groups[0]!;
  expect(draft.issue).toBe('');const row=newBoosterRow(draft,group);row.id='401040027';group.rows.push(row);
  const next=readBooster(editBooster(empty,draft));expect(next.issue).toBe('');expect(next.groups[0]!.equipmentUpgrade).toBe('10');expect(next.groups[0]!.rows[0]!.id).toBe('401040027');
 });
 it('拒绝重复、非整数、多参数和负数强化等级，编辑草稿不能悄悄改写等级',()=>{
  for(const value of ['','-1','1.5','`10`','10 11','2147483648'])expect(readBooster(text.replace('\t10',value)).issue).not.toBe('');
  expect(readBooster(text.replace('[equipment]','[booster equipment upgrade]\n5\n[equipment]')).issue).toContain('重复强化');
  const draft=clone(text);draft.groups[0]!.equipmentUpgrade='12';expect(()=>editBooster(text,draft)).toThrow('强化等级已变化');
 });
});

describe('商城道具随机奖励',()=>{
 const text=itemTemplate('booster').replace('[etc]\n1 1 1000 1\n[/etc]','[cera]\n\t1\t42\t1000\t50 # 商城道具\n[/cera]');
 it('按抽取项数及三参数记录读取，原文往返',()=>{
  const draft=clone(text),group=draft.groups[0]!;
  expect(draft.issue).toBe('');expect(group.pools![0]!.source.name).toBe('cera');expect(group.pools![0]!.drawCount).toBe('1');
  expect(group.rows).toEqual([{sourceIndex:0,poolIndex:0,id:'42',weight:'1000',quantity:'50'}]);expect(editBooster(text,draft)).toBe(text);
 });
 it('修改抽取项数、ID、权重及数量，保留标签、注释和 CRLF',()=>{
  const source=text.replaceAll('\n','\r\n'),draft=clone(source),group=draft.groups[0]!;
  group.pools![0]!.drawCount='2';Object.assign(group.rows[0]!,{id:'43',weight:'500',quantity:'100'});
  expect(editBooster(source,draft)).toBe(source.replace('1\t42\t1000\t50','2\t43\t500\t100'));
 });
 it('混合奖励池增删商城候选，保留其他奖励池',()=>{
  const source=text.replace('[/booster info]','[etc]\n2 100 300 4\n200 700 6\n[/etc]\n[/booster info]');
  const draft=clone(source),group=draft.groups[0]!,row=newBoosterRow(draft,group,0);row.id='43';row.quantity='60';group.rows.push(row);
  const added=editBooster(source,draft),next=clone(added);
  expect(next.issue).toBe('');expect(next.groups[0]!.pools!.map(p=>p.source.name)).toEqual(['cera','etc']);
  expect(next.groups[0]!.rows.filter(r=>r.poolIndex===0).map(r=>r.id)).toEqual(['42','43']);
  expect(added).toContain('[etc]\n2 100 300 4\n200 700 6\n[/etc]');
  next.groups[0]!.rows=next.groups[0]!.rows.filter(r=>r.id!=='42');
  const result=editBooster(added,next),parsed=readBooster(result);
  expect(parsed.issue).toBe('');expect(parsed.groups[0]!.rows.map(r=>r.id)).toEqual(['43','100','200']);expect(result).toContain('[cera]');
 });
 it('拒绝缺少记录参数的商城奖励',()=>{
  for(const value of ['1','1 42 1000','1 42 1000 50 43'])expect(readBooster(text.replace('1\t42\t1000\t50',value)).issue).toContain('成组');
 });
});
