import type { SkillDataSelection } from './skill';
import { parseItem, type ItemSection, type ItemToken } from './item';

export type ItemSkillEffect =
  { kind:'level';job:string;skill:string;value:string } |
  { kind:'data';job:string;skill:string;scope:string;data:string;index:string;operator:string;value:string } |
  { kind:'range';job:string;growType:string;group:string;lower:string;upper:string;value:string;phase:string;extra:string[] };
export interface ItemSkillEntry { effect:ItemSkillEffect; section:ItemSection; tokens:ItemToken[]; container?:ItemSection; range?:RangeSource }
interface RangeSource { job:ItemSection;group:ItemSection;phase:ItemSection;lower:ItemToken;upper:ItemToken;value:ItemToken;growType:ItemToken;condition:ItemSection;extra?:ItemSection }
export interface ItemSkillEffects { entries:ItemSkillEntry[];issues:string[];sections:ItemSection[] }
export const itemSkillDataKinds=[['[level]','等级数据'],['[static]','静态数据'],['[cooltime]','冷却时间'],['[mp]','MP 消耗'],['[skill consume item]','技能道具消耗'],['[casting time]','施放时间']].map(([value,label])=>({value:value!,label:label!}));
const tag=(value:string)=>/^\[[a-z][a-z0-9 _-]*\]$/i.test(value);
const integer=(value:string,minimum=-2147483648)=>/^[+-]?\d+$/.test(value)&&Number(value)>=minimum&&Number(value)<=2147483647;
const number=(value:string)=>/^[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:e[+-]?\d+)?$/i.test(value)&&Number.isFinite(Number(value))&&Math.abs(Number(value))<=3.4028234663852886e38;
const quote=(value:string)=>'`'+value.replaceAll('`','``')+'`';
const signed=(value:string)=>Number(value)>0?'+'+value:value;
export function itemSkillEffectSummary(effect:ItemSkillEffect):string {
 if(effect.kind==='level')return `技能等级 ${signed(effect.value)}`;
 if(effect.kind==='range')return `Lv${effect.lower}–${effect.upper} · 技能等级 ${signed(effect.value)}`;
 return `${itemSkillDataKinds.find(k=>k.value===effect.data)?.label??effect.data} [${effect.index}] ${effect.operator} ${effect.value} · ${effect.scope}`;
}
export function defaultItemSkillEffect(kind:ItemSkillEffect['kind'],job=''):ItemSkillEffect {
 if(kind==='level')return{kind,job,skill:'',value:'1'};
 if(kind==='data')return{kind,job,skill:'',scope:'[dungeon type]',data:'[level]',index:'0',operator:'%',value:'0'};
 return{kind,job:'[all]',growType:'0',group:'[all]',lower:'1',upper:'85',value:'1',phase:'[all]',extra:['[except for ex skill]']};
}
function validate(effect:ItemSkillEffect){
 if(!tag(effect.job))throw new Error('职业必须为有效标签，例如 [swordman]');
 if(effect.kind==='data'?!number(effect.value):!integer(effect.value))throw new Error('技能数值无效：等级加成需为整数，数据修改支持整数或小数');
 if(effect.kind==='range'){
  if(!integer(effect.growType,0)||!integer(effect.lower,0)||!integer(effect.upper,0)||Number(effect.lower)>Number(effect.upper))throw new Error('成长类型和等级范围无效，等级下限不能大于上限');
  if(!tag(effect.group)||!tag(effect.phase)||effect.extra.some(value=>!tag(value)))throw new Error('技能组、阶段和额外条件必须为有效标签');
 }else{
  if(!integer(effect.skill,0))throw new Error('技能 ID 必须为有效的非负整数');
  if(effect.kind==='data'&&(!tag(effect.scope)||!tag(effect.data)||!integer(effect.index,0)||!['+','-','*','%'].includes(effect.operator)))throw new Error('技能数据作用范围、数据类型、序号或操作符无效');
 }
}
function leaf(section:ItemSection|undefined,name:string,count:number):ItemSection {
 if(!section||section.name!==name||section.paired||section.nested||section.tokens.length!==count)throw new Error('区间技能结构无法识别');
 return section;
}
function rangeEntry(section:ItemSection,container:ItemSection):ItemSkillEntry {
 if(!section.paired||section.tokens.length||section.children.length!==5)throw new Error('区间技能包含特殊字段，请在 DSL 中编辑');
 const [grow,job,condition,phase,phaseValue]=section.children;
 leaf(grow,'item growtype',0);leaf(job,job!.name,1);leaf(phase,'skill phase',0);leaf(phaseValue,phaseValue!.name,0);
 if(!condition?.paired||condition.name!=='skill apply condition'||condition.tokens.length||![5,6].includes(condition.children.length))throw new Error('区间技能条件结构无法识别');
 const [group,groupValue,lower,upper,value,extra]=condition.children;
 leaf(group,'skill group',0);leaf(groupValue,groupValue!.name,0);leaf(lower,'lower bound level',1);leaf(upper,'upper bound level',1);leaf(value,'value',1);
 if(extra&&(!extra.paired||extra.name!=='extra condition'||extra.tokens.length||extra.children.some(child=>child.paired||child.nested||child.tokens.length)))throw new Error('区间技能额外条件结构无法识别');
 const numeric=[job!.tokens[0]!,lower!.tokens[0]!,upper!.tokens[0]!,value!.tokens[0]!];
 if(numeric.some(token=>token.kind!=='integer'))throw new Error('区间技能参数需为整数');
 const effect:ItemSkillEffect={kind:'range',job:`[${job!.name}]`,growType:numeric[0]!.value,group:`[${groupValue!.name}]`,lower:numeric[1]!.value,upper:numeric[2]!.value,value:numeric[3]!.value,phase:`[${phaseValue!.name}]`,extra:extra?.children.map(child=>`[${child.name}]`)??[]};
 validate(effect);
 return{effect,section,container,tokens:[],range:{job:job!,group:groupValue!,phase:phaseValue!,lower:numeric[1]!,upper:numeric[2]!,value:numeric[3]!,growType:numeric[0]!,condition,extra}};
}
export function readItemSkillEffects(text:string):ItemSkillEffects {
 const doc:ItemSkillEffects={entries:[],issues:[],sections:[]};
 try{
  doc.sections=parseItem(text);
  for(const section of doc.sections){
   if(section.name==='all skill item container'){
    if(!section.paired||section.tokens.length){doc.issues.push('[all skill item container] 结构无法识别');continue;}
    for(const child of section.children){
     if(child.name!=='all skill item'){doc.issues.push(`技能区间包含 [${child.name}]，保留原文`);continue;}
     try{doc.entries.push(rangeEntry(child,section));}catch(cause){doc.issues.push(String(cause));}
    }
   }else if(section.name==='skill levelup'||section.name==='skill data up'){
    try{
     const width=section.name==='skill levelup'?3:7;
     if(!section.paired||section.nested||section.tokens.length%width)throw new Error(`[${section.name}] 参数未完整分组，请在 DSL 中编辑`);
     const entries:ItemSkillEntry[]=[];
     for(let i=0;i<section.tokens.length;i+=width){
      const tokens=section.tokens.slice(i,i+width),values=tokens.map(token=>token.value);
      const strings=width===3?[0]:[0,2,3,5];
      if(strings.some(index=>tokens[index]!.kind!=='string')||tokens.some((token,index)=>!strings.includes(index)&&token.kind!=='integer'&&!(index===6&&number(token.value))))throw new Error(`[${section.name}] 参数类型无法识别，请在 DSL 中编辑`);
      const effect:ItemSkillEffect=width===3?{kind:'level',job:values[0]!,skill:values[1]!,value:values[2]!}:{kind:'data',job:values[0]!,skill:values[1]!,scope:values[2]!,data:values[3]!,index:values[4]!,operator:values[5]!,value:values[6]!};
      validate(effect);entries.push({effect,section,tokens});
     }
     doc.entries.push(...entries);
    }catch(cause){doc.issues.push(String(cause));}
   }
  }
 }catch(cause){doc.issues.push(String(cause));}
 return doc;
}
interface Patch {start:number;end:number;value:string}
function tokenPatch(token:ItemToken,value:string,string=false):Patch|undefined {
 if(token.value===value)return;
 const formatted=string?quote(value):value;
 return{start:token.start,end:token.end,value:token.marker?`{${token.marker}=${formatted}}`:formatted};
}
function values(effect:Exclude<ItemSkillEffect,{kind:'range'}>):string[]{
 return effect.kind==='level'?[effect.job,effect.skill,effect.value]:[effect.job,effect.skill,effect.scope,effect.data,effect.index,effect.operator,effect.value];
}
function rangeText(effect:Extract<ItemSkillEffect,{kind:'range'}>,newline:string):string {
 return ['[all skill item]','\t[item growtype]',`\t\t${effect.job}`,`\t\t${effect.growType}`,'\t[skill apply condition]','\t\t[skill group]',`\t\t\t${effect.group}`,'\t\t[lower bound level]',`\t\t\t${effect.lower}`,'\t\t[upper bound level]',`\t\t\t${effect.upper}`,'\t\t[value]',`\t\t\t${effect.value}`,...(effect.extra.length?['\t\t[extra condition]',...effect.extra.map(value=>'\t\t\t'+value),'\t\t[/extra condition]']:[]),'\t[/skill apply condition]','\t[skill phase]',`\t\t${effect.phase}`,'[/all skill item]'].join(newline)+newline;
}
export function editItemSkillEffect(text:string,action:{kind:'add';effect:ItemSkillEffect}|{kind:'edit';entry:number;effect:ItemSkillEffect}|{kind:'delete';entry:number}):string {
 const doc=readItemSkillEffects(text),patches:Patch[]=[],newline=text.includes('\r\n')?'\r\n':'\n';
 // A broken root parse has no trustworthy insertion positions.
 if(!doc.sections.length&&doc.issues.length)throw new Error(doc.issues[0]);
 if(action.kind==='add'){
  validate(action.effect);
  const effect=action.effect;
  if(effect.kind==='range'){
   const container=doc.sections.filter(s=>s.name==='all skill item container'&&s.paired&&!s.tokens.length).at(-1);
   const block=rangeText(effect,newline);
   if(container)patches.push({start:container.closeStart,end:container.closeStart,value:newline+block});
   else patches.push({start:text.length,end:text.length,value:(text.endsWith(newline)?'':newline)+`[all skill item container]${newline}${block}[/all skill item container]${newline}`});
  }else{
   const name=effect.kind==='level'?'skill levelup':'skill data up';
   const host=doc.sections.filter(s=>s.name===name&&s.paired&&!s.nested&&(s.tokens.length===0||doc.entries.some(entry=>entry.section===s))).at(-1);
   const strings=effect.kind==='level'?[0]:[0,2,3,5];
   const row='\t'+values(effect).map((v,i)=>strings.includes(i)?quote(v):v).join('\t')+newline;
   if(host)patches.push({start:host.closeStart,end:host.closeStart,value:newline+row});
   else patches.push({start:text.length,end:text.length,value:(text.endsWith(newline)?'':newline)+`[${name}]${newline}${row}[/${name}]${newline}`});
  }
 }else{
  const entry=doc.entries[action.entry];
  if(!entry)throw new Error('技能增强条目已变化，请重新编辑');
  if(action.kind==='delete'){
   if(entry.effect.kind==='range'){
    const target=entry.container!.children.length===1?entry.container!:entry.section;
    patches.push({start:target.start,end:target.end,value:''});
   }else if(entry.section.tokens.length===entry.tokens.length)patches.push({start:entry.section.start,end:entry.section.end,value:''});
   else entry.tokens.forEach(token=>patches.push({start:token.start,end:token.end,value:''}));
  }else{
   const effect=action.effect;validate(effect);
   if(effect.kind!==entry.effect.kind)throw new Error('编辑时不能改变技能增强类型，请新增对应条目');
   if(effect.kind==='range'&&entry.effect.kind==='range'){
    const source=entry.range!;
    for(const [section,value] of [[source.job,effect.job],[source.group,effect.group],[source.phase,effect.phase]] as [ItemSection,string][]){
     if(`[${section.name}]`!==value)patches.push({start:section.start,end:section.body,value});
    }
    for(const [token,value] of [[source.lower,effect.lower],[source.upper,effect.upper],[source.value,effect.value],[source.growType,effect.growType]] as [ItemToken,string][]){const patch=tokenPatch(token,value);if(patch)patches.push(patch);}
    if(effect.extra.join()!==entry.effect.extra.join()){
     const raw=effect.extra.length?`[extra condition]${newline}${effect.extra.map(value=>'\t\t'+value).join(newline)}${newline}[/extra condition]${newline}`:'';
     patches.push({start:source.extra?.start??source.condition.closeStart,end:source.extra?.end??source.condition.closeStart,value:raw});
    }
   }else if(effect.kind!=='range'){
    const strings=effect.kind==='level'?[0]:[0,2,3,5];
    values(effect).forEach((value,i)=>{const patch=tokenPatch(entry.tokens[i]!,value,strings.includes(i));if(patch)patches.push(patch);});
   }
  }
 }
 for(const patch of patches.sort((a,b)=>b.start-a.start||b.end-a.end))text=text.slice(0,patch.start)+patch.value+text.slice(patch.end);
 // Parse again so insertion can never introduce an unclosed or mismatched block.
 parseItem(text);
 return text;
}

/** Copy source identity only; preview multipliers and levels do not change equipment operands. */
export function applySkillDataSelection(effect: Extract<ItemSkillEffect, { kind: 'data' }>, selection: SkillDataSelection & { job: string; skill: string }): Extract<ItemSkillEffect, { kind: 'data' }> {
 if (!Number.isInteger(selection.index) || selection.index < 0) throw new Error('技能数据索引无效');
 const next = { ...effect, job: selection.job, skill: selection.skill, data: selection.dynamic ? '[level]' : '[static]', index: String(selection.index) };
 validate(next);
 return next;
}
