import { itemFieldIssue, itemSection, parseItem, type ItemSection } from './item';
export interface BoosterRow { sourceIndex?: number; poolIndex?: number; id: string; quantity: string; weight: string; avatarParams?: [string, string] }
export interface BoosterPool { drawCount: string; source: ItemSection }
export interface BoosterGroup { coordinates: string[]; rows: BoosterRow[]; pools?: BoosterPool[]; equipmentUpgrade?: string; source?: ItemSection }
export interface BoosterDocument {
 mode: 'booster' | 'selection' | null; issue: string; groups: BoosterGroup[];
 selection: string; dimensions: string[]; names: string[]; sections: ItemSection[];
}
const integer = (value: string, minimum = 0) => /^\d+$/.test(value) && Number(value) >= minimum && Number(value) <= 2147483647;
function numbers(section: ItemSection, count?: number) {
 if (section.nested || section.tokens.some(t => t.kind !== 'integer') || count !== undefined && section.tokens.length !== count) throw new Error(`[${section.name}] 参数结构无法识别，请在 DSL 中编辑`);
 return section.tokens.map(t => t.value);
}
function single(sections: ItemSection[], name: string): ItemSection {
 const matches = sections.filter(s => s.name === name);
 if (matches.length !== 1) throw new Error(`[${name}] 缺失或重复，请在 DSL 中编辑`);
 return matches[0]!;
}
function selectionContents(group?: BoosterGroup): ItemSection | undefined {
 return group?.source?.children.find(child=>child.name!=='booster equipment upgrade');
}
function selectionTag(doc: BoosterDocument, group?: BoosterGroup): string {
 return selectionContents(group)?.name ?? doc.groups.map(selectionContents).find(Boolean)?.name ?? 'etc';
}
export function boosterRewardName(group: BoosterGroup | undefined, name: string): string {
 return group?.equipmentUpgrade===undefined ? name : `+${group.equipmentUpgrade} ${name}`;
}
function selectionValues(row: BoosterRow, tag: string): string[] {
 return tag==='avatar' ? [row.id,row.quantity,...row.avatarParams!] : [row.id,row.quantity];
}
export function newBoosterRow(doc: BoosterDocument, group?: BoosterGroup, poolIndex=0): BoosterRow {
 const row: BoosterRow = {id:'',quantity:'1',weight:'1000'};
 if(doc.mode==='booster')row.poolIndex=poolIndex;
 if (doc.mode==='selection' && selectionTag(doc,group)==='avatar') {
  const reference=group?.rows.find(r=>r.avatarParams)?.avatarParams ?? doc.groups.flatMap(g=>g.rows).find(r=>r.avatarParams)?.avatarParams;
  row.avatarParams=reference ? [...reference] : ['',''];
 }
 return row;
}
export function readBooster(text: string): BoosterDocument {
 const doc: BoosterDocument = { mode: null, issue: '', groups: [], selection: '1', dimensions: [], names: [], sections: [] };
 try {
  const sections = parseItem(text); doc.sections = sections;
  const type = itemSection(sections, 'stackable type');
  const mode = type?.tokens[0]?.value;
  if (mode !== '[booster]' && mode !== '[booster selection]') return doc;
  doc.mode = mode === '[booster]' ? 'booster' : 'selection';
  const typeIssue = itemFieldIssue(sections, { name:'stackable type',label:'',kinds:['string','integer'],counts:[1,2] });
  if (typeIssue) throw new Error(typeIssue);
  if (doc.mode === 'booster') {
   const info = single(sections, 'booster info');
   if (!info.paired || info.tokens.length) throw new Error('礼包内容结构无法识别，请在 DSL 中编辑');
   const rows: BoosterRow[] = [];
   const pools:BoosterPool[]=[];
   for (const child of info.children) {
    if (!['etc','stackable','cera'].includes(child.name) || !child.paired) throw new Error('包含特殊礼包奖励，请在 DSL 中编辑');
    const values = numbers(child);
    if (values.length<4 || (values.length-1)%3) throw new Error(`[${child.name}] 需包含随机获得项数及成组的物品 ID、权重、数量，请在 DSL 中编辑`);
    const poolIndex=pools.length;
    pools.push({drawCount:values[0]!,source:child});
    for(let i=1;i<values.length;i+=3) rows.push({sourceIndex:rows.length,poolIndex,id:values[i]!,weight:values[i+1]!,quantity:values[i+2]!});
   }
   doc.groups = [{ coordinates: [], rows, pools, source: info }];
  } else {
   doc.selection = numbers(single(sections,'booster selection num'),1)[0]!;
   doc.dimensions = numbers(single(sections,'booster category num'));
   const [dims,n,m] = doc.dimensions.map(Number);
   if (!(dims === 0 && doc.dimensions.length === 1 || dims === 1 && doc.dimensions.length === 2 && n! >= 0 || dims === 2 && doc.dimensions.length === 3 && n! > 0 && m! > 0)) throw new Error('自选礼包分类维度无法识别，请在 DSL 中编辑');
   const blocks = sections.filter(s => s.name === 'booster select category');
   if (blocks.length !== (dims === 0 ? 1 : dims === 1 ? Math.max(n!,1) : n!*m!)) throw new Error('分类数量与礼包内容不匹配，请在 DSL 中编辑');
   const used = new Set<string>();
   doc.groups = blocks.map(block => {
    if (!block.paired || block.tokens.length !== 2 || block.tokens.some(t => t.kind !== 'integer')) throw new Error('包含特殊自选礼包配置，请在 DSL 中编辑');
    const coordinates = block.tokens.map(t => t.value), [x,y] = coordinates.map(Number);
    const key = coordinates.join(':');
    if (used.has(key) || x! < 0 || y! < 0 || x! >= (dims === 0 ? 1 : Math.max(n!,1)) || y! >= (dims === 2 ? m! : 1)) throw new Error('分类坐标重复或超出范围，请在 DSL 中编辑');
    used.add(key);
    const upgrades=block.children.filter(child=>child.name==='booster equipment upgrade');
    const rewards=block.children.filter(child=>child.name!=='booster equipment upgrade');
    if(upgrades.length>1 || rewards.length>1)throw new Error('包含重复强化等级或特殊自选礼包配置，请在 DSL 中编辑');
    const upgrade=upgrades[0];
    const equipmentUpgrade=upgrade?numbers(upgrade,1)[0]:undefined;
    if(equipmentUpgrade!==undefined && !integer(equipmentUpgrade))throw new Error('装备强化等级必须为有效的非负整数');
    const contents = rewards[0];
    if (contents && (!['etc','stackable','equipment','avatar'].includes(contents.name) || !contents.paired)) throw new Error('包含特殊自选奖励，请在 DSL 中编辑');
    const values = contents ? numbers(contents) : [];
    const stride=contents?.name==='avatar'?4:2;
    if (values.length % stride) throw new Error(stride===4?'avatar 奖励必须包含 ID、数量及两个附加参数，请在 DSL 中编辑':'奖励 ID 和数量未配对，请在 DSL 中编辑');
    const rows: BoosterRow[] = [];
    for (let i=0;i<values.length;i+=stride) {
     const row:BoosterRow={sourceIndex:i/stride,id:values[i]!,quantity:values[i+1]!,weight:''};
     if(stride===4)row.avatarParams=[values[i+2]!,values[i+3]!];
     rows.push(row);
    }
    return {coordinates, rows, equipmentUpgrade, source:block};
   });
   if (isFlatBoosterSelection(doc) && !sections.some(s=>s.name==='booster category name')) return doc;
   const names = single(sections,'booster category name');
   if (names.nested || names.tokens.some(t=>t.kind !== 'string')) throw new Error('分类名称结构无法识别，请在 DSL 中编辑');
   doc.names = names.tokens.map(t=>t.value);
   const expected = dims === 0 ? 1 : dims === 1 ? 1 + Math.max(n!,1) : 2 + n! + n!*m!;
   // A rectangular two-level menu may end with empty slots whose names are omitted.
   let minimum = dims === 1 && n === 0 ? 1 : expected;
   if (dims === 2) {
    minimum = 2+n!;
    for (const group of doc.groups) {
     if (!group.rows.length) continue;
     const [x,y] = group.coordinates.map(Number);
     minimum = Math.max(minimum,2+n!+x!*m!+y!+1);
    }
   }
   if (doc.names.length < minimum || doc.names.length > expected) throw new Error('分类名称数量与维度不匹配，请在 DSL 中编辑');
  }
 } catch(e) { doc.issue = String(e); }
 return doc;
}
export function isFlatBoosterSelection(doc: BoosterDocument): boolean {
 return doc.mode==='selection' && (doc.dimensions[0]==='0' || doc.dimensions[0]==='1' && Number(doc.dimensions[1])===0);
}
export function boosterGroupLabel(doc: BoosterDocument, group: BoosterGroup): string {
 if (doc.mode === 'booster') return '礼包奖励';
 if (isFlatBoosterSelection(doc)) return '可选物品';
 const [x,y] = group.coordinates.map(Number);
 if (doc.dimensions[0] === '1') return doc.names[1+x!] || `分类 ${x!+1}`;
 const n = Number(doc.dimensions[1]), m = Number(doc.dimensions[2]);
 return `${doc.names[1+x!] || `分类 ${x!+1}`} / ${doc.names[2+n+x!*m+y!] || `子分类 ${y!+1}`}`;
}
interface Patch { start:number;end:number;value:string }
function patchValues(section: ItemSection, values: string[], kind:'integer'|'string', newline:string): Patch[] {
 const formatted = values.map(v=>kind==='string'?'`'+v.replaceAll('`','``')+'`':v);
 const patches: Patch[] = [];
 section.tokens.forEach((t,i)=> {
  if (values[i]===t.value) return;
  const value = formatted[i];
  patches.push({start:t.start,end:t.end,value:value===undefined?'':t.marker?`{${t.marker}=${value}}`:value});
 });
 if (values.length>section.tokens.length) {
  const last=section.tokens.at(-1);
  patches.push({start:last?.end??section.body,end:last?.end??section.body,value:(last?'\t':newline+'\t')+formatted.slice(section.tokens.length).join('\t')});
 }
 return patches;
}
export function editBooster(text: string, draft: BoosterDocument): string {
 const original = readBooster(text);
 if (original.issue || !original.mode || draft.mode !== original.mode) throw new Error(original.issue || '礼包类型已变化');
 const extra=draft.groups.length-original.groups.length;
 const adding=extra>0 && original.mode==='selection' && original.dimensions[0]==='1' && Number(original.dimensions[1])>0;
 if (extra!==0 && !adding || !adding && draft.dimensions.join()!==original.dimensions.join()) throw new Error('基础礼包编辑保留原分类结构');
 if (adding && (draft.dimensions[0]!=='1' || Number(draft.dimensions[1])!==draft.groups.length || draft.dimensions.length!==2)) throw new Error('新增分类数量不匹配');
 const newline=text.includes('\r\n')?'\r\n':'\n', patches:Patch[]=[];
 const selectionRewardTag=selectionTag(original);
 let additions='';
 for (let g=0;g<draft.groups.length;g++) {
  const next=draft.groups[g]!, old=original.groups[g]!;
  const rewardTag=selectionTag(original,old);

  if (old && next.equipmentUpgrade!==old.equipmentUpgrade || !old && next.equipmentUpgrade!==undefined)throw new Error('装备强化等级已变化，请通过 DSL 修改');
  if (old && next.coordinates.join()!==old.coordinates.join()) throw new Error('分类坐标已变化');
  for (const row of next.rows) {
   if (draft.mode==='selection' && rewardTag==='avatar' && (row.avatarParams?.length!==2 || row.avatarParams.some(value=>! /^-?\d+$/.test(value) || Number(value)<-2147483648 || Number(value)>2147483647))) throw new Error('avatar 的两个附加参数必须是有效整数');
   if (!integer(row.id,1) || !integer(row.quantity,1) || draft.mode==='booster' && !integer(row.weight)) throw new Error('奖励 ID、数量必须是有效的正整数，权重需为非负整数');
  }
  if (!old) {
   if (next.coordinates.join(':')!==`${g}:0`) throw new Error('新增分类坐标不匹配');
   additions+=`[booster select category]${newline}\t${g}\t0${newline}\t[${selectionRewardTag}]${newline}\t\t${next.rows.flatMap(row=>selectionValues(row,rewardTag)).join('\t')}${newline}\t[/${selectionRewardTag}]${newline}[/booster select category]${newline}`;
   continue;
  }
  const seen = new Set<number>();
  for (const row of next.rows) {
   if (row.sourceIndex === undefined) continue;
   if (!Number.isInteger(row.sourceIndex) || row.sourceIndex < 0 || row.sourceIndex >= old.rows.length || seen.has(row.sourceIndex)) throw new Error('奖励位置已变化，请重新打开编辑');
   seen.add(row.sourceIndex);
  }
  if (draft.mode==='booster') {
   const pools=next.pools??[],oldPools=old.pools??[];
   if(pools.length!==oldPools.length)throw new Error('随机奖励池结构已变化，请重新打开编辑');
   if(pools.some(pool=>!integer(pool.drawCount,1)))throw new Error('随机获得项数必须为有效的正整数');
   for(const row of next.rows) {
    if(!Number.isInteger(row.poolIndex??0) || (row.poolIndex??0)<0 || (row.poolIndex??0)>=pools.length)throw new Error('奖励所属随机池无效');
    if(row.sourceIndex!==undefined && row.poolIndex!==old.rows[row.sourceIndex]!.poolIndex)throw new Error('已有奖励不能跨随机池移动');
   }
   let offset=0;
   for(let poolIndex=0;poolIndex<oldPools.length;poolIndex++) {
    const child=oldPools[poolIndex]!.source,count=(child.tokens.length-1)/3;
    const retained=next.rows.filter(row=>row.sourceIndex!==undefined && row.sourceIndex>=offset && row.sourceIndex<offset+count);
    const added=next.rows.filter(row=>row.sourceIndex===undefined && (row.poolIndex??0)===poolIndex);
    if(!retained.length&&!added.length)patches.push({start:child.start,end:child.end,value:''});
    else {
     patches.push(...patchValues({...child,tokens:child.tokens.slice(0,1)},[pools[poolIndex]!.drawCount],'integer',newline));
     for(let i=0;i<count;i++) {
      const row=retained.find(row=>row.sourceIndex===offset+i);
      patches.push(...patchValues({...child,tokens:child.tokens.slice(1+i*3,1+(i+1)*3)},row?[row.id,row.weight,row.quantity]:[],'integer',newline));
     }
     if(added.length)patches.push({start:child.closeStart,end:child.closeStart,value:newline+'\t\t'+added.flatMap(row=>[row.id,row.weight,row.quantity]).join('\t')+newline});
    }
    offset+=count;
   }
  } else {
   const contents=selectionContents(old);
   if (draft.mode==='selection' && draft.dimensions[0]==='2' && next.rows.length) {
    const [x,y]=next.coordinates.map(Number),n=Number(draft.dimensions[1]),m=Number(draft.dimensions[2]);
    if (!draft.names[2+n+x!*m+y!]?.trim()) throw new Error('该空分类没有名称，请先补充分类名称');
   }
   if (!contents) {
    if (next.rows.length) {
     const tag=selectionRewardTag;
     const position=old.source!.closeStart;
     patches.push({start:position,end:position,value:`\t[${tag}]${newline}\t\t${next.rows.flatMap(row=>selectionValues(row,rewardTag)).join('\t')}${newline}\t[/${tag}]${newline}`});
    }
    continue;
   }
   old.rows.forEach((_,i)=> {
    const row=next.rows.find(row=>row.sourceIndex===i);
    patches.push(...patchValues({...contents,tokens:contents.tokens.slice(i*(rewardTag==='avatar'?4:2),(i+1)*(rewardTag==='avatar'?4:2))},row?selectionValues(row,rewardTag):[],'integer',newline));
   });
   const added=next.rows.filter(row=>row.sourceIndex===undefined).flatMap(row=>selectionValues(row,rewardTag));
   if(added.length)patches.push({start:contents.closeStart,end:contents.closeStart,value:newline+'\t'+added.join('\t')+newline});
  }
 }
 if (draft.mode==='selection') {
  if (!integer(draft.selection,1)) throw new Error('可选数量必须为正整数');
  if (draft.names.length!==original.names.length+extra || draft.names.some((name,i)=>!name.trim() && name!==original.names[i] && !(i===0 && isFlatBoosterSelection(draft)))) throw new Error('分类名称不能为空，且需保留原分类数量');
  patches.push(...patchValues(single(original.sections,'booster selection num'),[draft.selection],'integer',newline));
  const names=itemSection(original.sections,'booster category name');
  if (names) {
   if (additions) {
    patches.push({start:names.start,end:names.start,value:additions});
    patches.push(...patchValues(single(original.sections,'booster category num'),draft.dimensions,'integer',newline));
   }
   if (names.tokens.some((t,i)=>/^<\s*\d+\s*::/.test(t.value) && draft.names[i]!==t.value)) throw new Error('分类名称为字符串表引用，请通过 DSL 修改译文');
   patches.push(...patchValues(names,draft.names,'string',newline));
  }
 }
 let result=text;
 for (const p of patches.sort((a,b)=>b.start-a.start || b.end-a.end)) result=result.slice(0,p.start)+p.value+result.slice(p.end);
 return result;
}
export function addBoosterCategory(draft: BoosterDocument): void {
 if (draft.mode!=='selection' || draft.dimensions[0]!=='1' || Number(draft.dimensions[1])<1) throw new Error('仅单层分类支持新增分类');
 const index=draft.groups.length;
 draft.groups.push({coordinates:[String(index),'0'],rows:[newBoosterRow(draft)]});
 draft.names.push(`类别 ${index+1}`);
 draft.dimensions[1]=String(index+1);
}
export type ItemTemplate = 'material'|'booster'|'selection'|'weapon'|'armor';
export function itemTemplate(kind: ItemTemplate): string {
 let text='#PVF_File\n[name]\n`新物品`\n[rarity]\n0\n[minimum level]\n1\n';
 if (kind==='weapon'||kind==='armor') return text+`[equipment type]\n\t\`[${kind}]\`\t0\n[attach type]\n\t\`[free]\`\n`;
 text+=`[stackable type]\n\t\`[${kind==='selection'?'booster selection':kind==='booster'?'booster':'material'}]\`\t0\n[stack limit]\n9999\n`;
 if (kind==='booster') text+='[booster info]\n[etc]\n1 1 1000 1\n[/etc]\n[/booster info]\n';
 if (kind==='selection') text+='[booster category num]\n1 1\n[booster selection num]\n1\n[booster select category]\n0 0\n[etc]\n1 1\n[/etc]\n[/booster select category]\n[booster category name]\n`请选择。` `类别 1`\n[/booster category name]\n';
 return text;
}
