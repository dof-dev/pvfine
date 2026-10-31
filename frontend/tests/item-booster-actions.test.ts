import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { applyBoosterInlineAction, changeBoosterCategory } from '../src/gui/itemBoosterActions';
import { itemTemplate, newBoosterRow, readBooster } from '../src/gui/itemBooster';
const one=itemTemplate('selection');
const block=(x:number,y:number,contents='')=>`[booster select category]\n${x} ${y}\n${contents}[/booster select category]\n`;
const two=`[stackable type]\n\`[booster selection]\` 0\n[booster category num]\n2 2 2\n[booster selection num]\n1\n${block(0,0,'[booster equipment upgrade]\n10 # 强化\n[equipment]\n100 1\n[/equipment]\n')}${block(0,1,'[avatar]\n200 1 3 0 # 时装\n[/avatar]\n')}${block(1,0,'[stackable]\n300 2\n[/stackable]\n')}${block(1,1)}[booster category name]\n\`请选择职业\` \`职业甲\` \`职业乙\` \`请选择物品\` \`武器\` \`时装\` \`材料\`\n[/booster category name]\n[unknown]\n{5=1234} # 其他字段\n`;
const checked=(text:string)=>{const doc=readBooster(text);expect(doc.issue).toBe('');return doc;};
const get=(text:string,x:number,y=0)=>checked(text).groups.find(g=>g.coordinates.join(':')===`${x}:${y}`)!;

describe('礼包行内分类管理',()=>{
 it('单层重命名仅修改名称 token，保留 CRLF 与其他字段',()=>{
  const text=(one+'[unknown]\n123\n').replaceAll('\n','\r\n');
  expect(changeBoosterCategory(text,{kind:'rename-category',level:0,index:0,name:'新分类'})).toBe(text.replace('类别 1','新分类'));
  expect(changeBoosterCategory(text,{kind:'rename-category',level:0,index:0,name:'类别 1'})).toBe(text);
 });
 it('添加空单层分类同步维度、坐标、名称，可立即添加物品',()=>{
  let text=changeBoosterCategory(one,{kind:'add-category',level:0,name:'新材料'}),doc=checked(text);
  expect(doc.dimensions).toEqual(['1','2']);expect(doc.names).toEqual(['请选择。','类别 1','新材料']);expect(doc.groups[1]!.rows).toEqual([]);
  text=applyBoosterInlineAction(text,{kind:'add-item',group:1,row:{id:'200',quantity:'3',weight:''}});
  expect(get(text,1).rows[0]).toMatchObject({id:'200',quantity:'3'});
 });
 it('删除单层分类只删除该类并重排坐标；删除最后一类变为无分类空列表',()=>{
  let text=changeBoosterCategory(one,{kind:'add-category',level:0,name:'第二类'});
  text=applyBoosterInlineAction(text,{kind:'add-item',group:1,row:{id:'200',quantity:'3',weight:''}});
  text=changeBoosterCategory(text,{kind:'delete-category',level:0,index:0});
  expect(checked(text).names).toEqual(['请选择。','第二类']);expect(get(text,0).rows[0]!.id).toBe('200');
  text=changeBoosterCategory(text,{kind:'delete-category',level:0,index:0});
  expect(checked(text).dimensions).toEqual(['0']);expect(get(text,0).rows).toEqual([]);expect(checked(text).names).toEqual(['请选择。']);
 });
 it('无分类列表添加分类时保留已有物品；缺省名称段可创建',()=>{
  const flat=one.replace('[booster category num]\n1 1','[booster category num]\n0').replace(/\[booster category name\][\s\S]*?\[\/booster category name\]/,'');
  const text=changeBoosterCategory(flat,{kind:'add-category',level:0,name:'新类'}),doc=checked(text);
  expect(doc.dimensions).toEqual(['1','2']);expect(doc.names).toEqual(['请选择。','默认分类','新类']);expect(get(text,0).rows[0]!.id).toBe('1');
 });
 it('无分类空列表添加首类不创建多余默认分类',()=>{
  const empty=one.replace('[booster category num]\n1 1','[booster category num]\n0').replace('`请选择。` `类别 1`','`请选择。`').replace('1 1\n[/etc]','\n[/etc]');
  const doc=checked(changeBoosterCategory(empty,{kind:'add-category',level:0,name:'首类'}));
  expect(doc.dimensions).toEqual(['1','1']);expect(doc.names).toEqual(['请选择。','首类']);
 });
 it('二维添加父分类保持其他奖励块、强化、avatar 参数及省略名称可解析',()=>{
  const text=changeBoosterCategory(two,{kind:'add-category',level:0,name:'职业丙'}),doc=checked(text);
  expect(doc.dimensions).toEqual(['2','3','2']);expect(doc.groups).toHaveLength(6);expect(doc.names.slice(0,5)).toEqual(['请选择职业','职业甲','职业乙','职业丙','请选择物品']);
  expect(text).toContain(block(0,0,'[booster equipment upgrade]\n10 # 强化\n[equipment]\n100 1\n[/equipment]\n'));
  expect(get(text,0,1).rows[0]!.avatarParams).toEqual(['3','0']);expect(text).toContain('[unknown]\n{5=1234} # 其他字段');
 });
 it('二维删除父分类同步名称与坐标，不删除其他父分类物品',()=>{
  const text=changeBoosterCategory(two,{kind:'delete-category',level:0,index:0}),doc=checked(text);
  expect(doc.dimensions).toEqual(['2','1','1']);expect(doc.names).toEqual(['请选择职业','职业乙','请选择物品','材料']);
  expect(get(text,0,0).rows[0]!.id).toBe('300');expect(text).not.toContain('100 1');expect(text).not.toContain('200 1');
 });
 it('增加子分类优先复用当前父分类空位，不改其他父分类',()=>{
  const text=changeBoosterCategory(two,{kind:'add-category',level:1,parent:1,name:'消耗品'}),doc=checked(text);
  expect(doc.dimensions).toEqual(['2','2','2']);expect(doc.names.at(-1)).toBe('消耗品');expect(get(text,1,1).rows).toEqual([]);
  expect(get(text,0,0).equipmentUpgrade).toBe('10');expect(get(text,0,1).rows[0]!.avatarParams).toEqual(['3','0']);
 });
 it('子分类已满时扩展矩形宽度，其他父分类补空位，原数据不串类',()=>{
  const text=changeBoosterCategory(two,{kind:'add-category',level:1,parent:0,name:'新武器'}),doc=checked(text);
  expect(doc.dimensions).toEqual(['2','2','3']);expect(doc.groups).toHaveLength(6);
  expect(doc.names).toEqual(['请选择职业','职业甲','职业乙','请选择物品','武器','时装','新武器','材料']);
  expect(get(text,0,2).rows).toEqual([]);expect(get(text,1,0).rows[0]!.id).toBe('300');expect(get(text,1,2).rows).toEqual([]);
 });
 it('仅删除选中父分类的子分类并收缩尾部空位，保留其余分类奖励',()=>{
  const text=changeBoosterCategory(two,{kind:'delete-category',level:1,parent:0,index:0}),doc=checked(text);
  expect(doc.dimensions).toEqual(['2','2','1']);expect(doc.names).toEqual(['请选择职业','职业甲','职业乙','请选择物品','时装','材料']);
  expect(get(text,0,0).rows[0]!.id).toBe('200');expect(get(text,0,0).rows[0]!.avatarParams).toEqual(['3','0']);expect(get(text,1,0).rows[0]!.id).toBe('300');
 });
 it('子分类重命名不改其他名称或强化字段',()=>{
  expect(changeBoosterCategory(two,{kind:'rename-category',level:1,parent:0,index:0,name:'强化装备'})).toBe(two.replace('`武器`','`强化装备`'));
 });
 it('新增和删除分类时完整保留移动后的字符串引用及类型标记；引用不可直接重命名',()=>{
  const text=two.replace('`职业乙`','{8=`<31::job_b>`}').replace('`材料`','{10=`<31::material>`}');
  const next=changeBoosterCategory(text,{kind:'delete-category',level:0,index:0});
  expect(checked(next).names).toEqual(['请选择职业','<31::job_b>','请选择物品','<31::material>']);
  expect(next).toContain('{8=`<31::job_b>`}');expect(next).toContain('{10=`<31::material>`}');
  expect(()=>changeBoosterCategory(text,{kind:'rename-category',level:0,index:1,name:'错误替换'})).toThrow('字符串表引用');
 });
 it('用户真实二维样例可增删父分类，未改奖励逐块保留',()=>{
  const text=readFileSync(new URL('./fixtures/epic-fragment-selection.stk',import.meta.url),'utf8');
  const next=changeBoosterCategory(text,{kind:'delete-category',level:0,index:0}),doc=checked(next);
  expect(doc.dimensions[1]).toBe('7');
  expect(doc.groups[0]!.rows.map(r=>r.id)).toEqual(readBooster(text).groups.find(g=>g.coordinates.join(':')==='1:0')!.rows.map(r=>r.id));
  expect(checked(changeBoosterCategory(next,{kind:'add-category',level:0,name:'新增职业'})).dimensions[1]).toBe('8');
 });
 it('重命名保留矩形尾部空位及其省略名称，不擅自收缩维度',()=>{
  const text=two.replace('2 2 2','2 2 3').replace('[booster category name]',block(0,2)+block(1,2)+'[booster category name]').replace('`时装` `材料`','`时装` `` `材料`');
  checked(text);
  expect(changeBoosterCategory(text,{kind:'rename-category',level:0,index:1,name:'职业丙'})).toBe(text.replace('`职业乙`','`职业丙`'));
 });
 it('分类名称中的反引号正确转义，新增分类支持 CRLF',()=>{
  const text=one.replaceAll('\n','\r\n');
  const next=changeBoosterCategory(text,{kind:'add-category',level:0,name:'测试`名称'});
  expect(checked(next).names.at(-1)).toBe('测试`名称');expect(next).toContain('`测试``名称`');
  expect(next.replaceAll('\r\n','')).not.toContain('\n');
 });
 it('删除最后一个二维父分类后，其他字段保留且可重新建立分类',()=>{
  let text=changeBoosterCategory(two,{kind:'delete-category',level:0,index:0});
  text=changeBoosterCategory(text,{kind:'delete-category',level:0,index:0});
  expect(checked(text).dimensions).toEqual(['0']);expect(get(text,0).rows).toEqual([]);
  expect(text).toContain('[unknown]\n{5=1234} # 其他字段');
  text=changeBoosterCategory(text,{kind:'add-category',level:0,name:'新分类'});
  expect(checked(text).names).toEqual(['请选择职业','新分类']);
 });
 it('无分类旧式第二条名称在建立分类时保留为默认分类名称',()=>{
  const text=one.replace('[booster category num]\n1 1','[booster category num]\n1 0');
  const next=checked(changeBoosterCategory(text,{kind:'add-category',level:0,name:'新类别'}));
  expect(next.names).toEqual(['请选择。','类别 1','新类别']);
 });
 it('拒绝空名称、错误索引及随机礼包分类操作',()=>{
  expect(()=>changeBoosterCategory(one,{kind:'add-category',level:0,name:' '})).toThrow('不能为空');
  expect(()=>changeBoosterCategory(one,{kind:'delete-category',level:0,index:9})).toThrow('分类已变化');
  expect(()=>changeBoosterCategory(one,{kind:'add-category',level:1,name:'子类'})).toThrow('没有二级分类');
  expect(()=>changeBoosterCategory(itemTemplate('booster'),{kind:'add-category',level:0,name:'子类'})).toThrow('没有自选分类');
 });
});

describe('礼包行内物品管理',()=>{
 it('编辑物品保留强化字段与精确 token 定位',()=>{
  const text=applyBoosterInlineAction(two,{kind:'edit-item',group:0,index:0,row:{id:'101',quantity:'4',weight:''}});
  expect(text).toBe(two.replace('100 1','101 4'));expect(get(text,0,0).equipmentUpgrade).toBe('10');
 });
 it('删除最后一个物品得到空分类，可重新添加；avatar 参数不丢失',()=>{
  const row={...get(two,0,1).rows[0]!,avatarParams:['4','0'] as [string,string]};
  let text=applyBoosterInlineAction(two,{kind:'delete-item',group:1,index:0});expect(get(text,0,1).rows).toEqual([]);
  text=applyBoosterInlineAction(text,{kind:'add-item',group:1,row});expect(get(text,0,1).rows[0]!.avatarParams).toEqual(['4','0']);expect(text).toContain('[avatar]');
 });
 it('随机池新增候选保留抽取项数；删除最后候选后可从空礼包重新添加',()=>{
  let text=itemTemplate('booster');const row=newBoosterRow(readBooster(text),readBooster(text).groups[0]);row.id='200';
  text=applyBoosterInlineAction(text,{kind:'add-item',group:0,row});expect(checked(text).groups[0]!.pools).toHaveLength(1);
  text=applyBoosterInlineAction(text,{kind:'delete-item',group:0,index:1});
  text=applyBoosterInlineAction(text,{kind:'delete-item',group:0,index:0});expect(checked(text).groups[0]!.rows).toEqual([]);
  text=applyBoosterInlineAction(text,{kind:'add-item',group:0,row});expect(checked(text).groups[0]!.rows[0]!.id).toBe('200');expect(checked(text).groups[0]!.pools![0]!.drawCount).toBe('1');
 });
 it('行内修改可选数量与随机项数，候选物品数据保持原样',()=>{
  expect(checked(applyBoosterInlineAction(one,{kind:'settings',selection:'2'})).selection).toBe('2');
  const text=itemTemplate('booster');expect(applyBoosterInlineAction(text,{kind:'settings',pool:0,drawCount:'3'})).toBe(text.replace('1 1 1000 1','3 1 1000 1'));
 });
 it('无分类列表可删除最后项且保持缺省名称段；非法数值不产生输出',()=>{
  const text=one.replace('[booster category num]\n1 1','[booster category num]\n0').replace(/\[booster category name\][\s\S]*?\[\/booster category name\]/,'');
  const next=applyBoosterInlineAction(text,{kind:'delete-item',group:0,index:0});expect(get(next,0).rows).toEqual([]);expect(next).not.toContain('[booster category name]');
  expect(()=>applyBoosterInlineAction(text,{kind:'add-item',group:0,row:{id:'bad',quantity:'1',weight:''}})).toThrow('正整数');
  expect(()=>applyBoosterInlineAction(text,{kind:'edit-item',group:0,index:9,row:{id:'100',quantity:'1',weight:''}})).toThrow('物品已变化');
 });
});

describe('商城道具奖励行内编辑',()=>{
 const text=itemTemplate('booster').replace('[etc]\n1 1 1000 1\n[/etc]','[cera]\n1 42 1000 50\n[/cera]');
 it('修改项数、编辑、添加及删除商城道具，保存仍使用 cera',()=>{
  let result=applyBoosterInlineAction(text,{kind:'settings',pool:0,drawCount:'2'});
  result=applyBoosterInlineAction(result,{kind:'edit-item',group:0,index:0,row:{id:'43',weight:'500',quantity:'100'}});
  result=applyBoosterInlineAction(result,{kind:'add-item',group:0,row:{poolIndex:0,id:'44',weight:'500',quantity:'200'}});
  let doc=checked(result);expect(doc.groups[0]!.pools![0]!.drawCount).toBe('2');
  expect(doc.groups[0]!.rows.map(r=>[r.id,r.weight,r.quantity])).toEqual([['43','500','100'],['44','500','200']]);
  result=applyBoosterInlineAction(result,{kind:'delete-item',group:0,index:0});doc=checked(result);
  expect(doc.groups[0]!.rows.map(r=>r.id)).toEqual(['44']);expect(result).toContain('[cera]');expect(result).not.toContain('[etc]');
 });
});
