import { editBooster, isFlatBoosterSelection, readBooster, type BoosterDocument, type BoosterGroup, type BoosterRow } from './itemBooster';
import type { ItemSection, ItemToken } from './item';

export interface BoosterCategoryAction {
  kind: 'add-category' | 'rename-category' | 'delete-category';
  level: 0 | 1;
  parent?: number;
  index?: number;
  name?: string;
}
export type BoosterInlineAction = BoosterCategoryAction |
  { kind: 'add-item' | 'edit-item' | 'delete-item'; group: number; index?: number; row?: BoosterRow } |
  { kind: 'settings'; selection?: string; pool?: number; drawCount?: string };
interface Patch { start: number; end: number; value: string }
interface Name { value: string; raw: string; token?: ItemToken }
interface Cell { name: Name; group: BoosterGroup }
interface Category { name: Name; cells: Cell[] }
const quoted = (value: string) => '`' + value.replaceAll('`', '``') + '`';
const name = (value: string): Name => ({ value, raw: quoted(value) });
const emptyGroup = (): BoosterGroup => ({ coordinates: [], rows: [] });
const occupied = (cell: Cell) => !!cell.name.value.trim() || !!cell.group.rows.length || cell.group.equipmentUpgrade !== undefined;
function apply(text: string, patches: Patch[]): string {
  for (const patch of patches.sort((a,b)=>b.start-a.start || b.end-a.end)) text=text.slice(0,patch.start)+patch.value+text.slice(patch.end);
  const result=readBooster(text);
  if(result.issue)throw new Error(result.issue);
  return text;
}
function snapshot(text: string): BoosterDocument {
  const doc=readBooster(text);
  if(doc.issue || !doc.mode)throw new Error(doc.issue || '当前文件不是礼包');
  return doc;
}
function numbers(section: ItemSection, values: string[], newline: string): Patch[] {
  const patches: Patch[]=[];
  section.tokens.forEach((token,i)=>{if(token.value!==values[i])patches.push({start:token.start,end:token.end,value:values[i]??''});});
  if(values.length>section.tokens.length){
    const position=section.tokens.at(-1)?.end??section.body;
    patches.push({start:position,end:position,value:(section.tokens.length?'\t':newline+'\t')+values.slice(section.tokens.length).join('\t')});
  }
  return patches;
}

/** Category changes move only coordinates and name tokens; retained reward blocks stay verbatim. */
export function changeBoosterCategory(text: string, action: BoosterCategoryAction): string {
  const doc=snapshot(text);
  if(doc.mode!=='selection')throw new Error('随机礼盒没有自选分类');
  const flat=isFlatBoosterSelection(doc),two=doc.dimensions[0]==='2';
  if(action.level===1&&!two)throw new Error('当前礼包没有二级分类');
  if(action.kind!=='add-category'&&flat)throw new Error('当前礼包没有分类');
  if(action.kind!=='delete-category'&&!action.name?.trim())throw new Error('分类名称不能为空');
  const newline=text.includes('\r\n')?'\r\n':'\n';
  const namesSection=doc.sections.find(section=>section.name==='booster category name');
  const atom=(index:number):Name=>{
    const token=namesSection?.tokens[index];
    return token?{value:token.value,raw:text.slice(token.start,token.end),token}:name('');
  };
  const prompt=namesSection?atom(0):name('请选择。');
  const n=flat?1:Number(doc.dimensions[1]),width=two?Number(doc.dimensions[2]):1;
  const secondPrompt=two?atom(1+n):name('请选择物品。');
  const parents:Category[]=Array.from({length:n},(_,x)=>({
    name:flat?(doc.names[1]?atom(1):name('默认分类')):atom(1+x),
    cells:Array.from({length:width},(_,y)=>({name:two?atom(2+n+x*width+y):name(''),group:doc.groups.find(g=>Number(g.coordinates[0])===x&&Number(g.coordinates[1])===y)!})),
  }));
  const blankCell=():Cell=>({name:name(''),group:emptyGroup()});
  const rename=(old:Name,value:string):Name=>{
    if(old.value===value)return old;
    if(/^<\s*\d+\s*::/.test(old.value))throw new Error('分类名称为字符串表引用，请通过 DSL 修改译文');
    return name(value);
  };
  if(action.level===0){
    if(action.kind==='add-category'){
      if(flat&&!parents[0]!.cells[0]!.group.rows.length){parents[0]!.name=name(action.name!);}
      else parents.push({name:name(action.name!),cells:Array.from({length:width},blankCell)});
    }else{
      const index=action.index;
      if(index===undefined||!Number.isInteger(index)||!parents[index])throw new Error('分类已变化，请重新操作');
      if(action.kind==='delete-category')parents.splice(index,1);
      else parents[index]!.name=rename(parents[index]!.name,action.name!);
    }
  }else{
    const parent=parents[action.parent??-1];
    if(!parent)throw new Error('父分类已变化，请重新操作');
    if(action.kind==='add-category'){
      let index=parent.cells.findIndex(cell=>!occupied(cell));
      if(index<0){index=parent.cells.length;parents.forEach(p=>p.cells.push(blankCell()));}
      parent.cells[index]={name:name(action.name!),group:parent.cells[index]!.group};
    }else{
      const index=action.index;
      if(index===undefined||!Number.isInteger(index)||!parent.cells[index])throw new Error('子分类已变化，请重新操作');
      if(action.kind==='rename-category')parent.cells[index]!.name=rename(parent.cells[index]!.name,action.name!);
      else {parent.cells.splice(index,1);parent.cells.push(blankCell());}
    }
  }
  let dims:string[],outputNames:Name[],groups:BoosterGroup[]=[];
  if(!parents.length){
    dims=['0'];outputNames=[prompt];groups=[{coordinates:['0','0'],rows:[]}];
  }else if(two){
    // Each parent has its own logical children; the file stores a rectangular grid.
    const m=action.kind==='delete-category'?Math.max(1,...parents.map(p=>p.cells.reduce((last,cell,i)=>occupied(cell)?i+1:last,0))):parents[0]!.cells.length;
    dims=['2',String(parents.length),String(m)];
    outputNames=[prompt,...parents.map(p=>p.name),secondPrompt];
    parents.forEach((p,x)=>{
      for(let y=0;y<m;y++){
        const cell=p.cells[y]??blankCell();
        groups.push({...cell.group,coordinates:[String(x),String(y)]});outputNames.push(cell.name);
      }
    });
    const minimum=2+parents.length;
    while(outputNames.length>minimum&&!outputNames.at(-1)!.token&&!outputNames.at(-1)!.value)outputNames.pop();
  }else{
    dims=['1',String(parents.length)];outputNames=[prompt,...parents.map(p=>p.name)];
    groups=parents.map((p,x)=>({...p.cells[0]!.group,coordinates:[String(x),'0']}));
  }
  const patches=numbers(doc.sections.find(s=>s.name==='booster category num')!,dims,newline);
  const retained=new Set(groups.flatMap(group=>group.source?[group.source.start]:[]));
  for(const group of doc.groups)if(!retained.has(group.source!.start))patches.push({start:group.source!.start,end:group.source!.end,value:''});
  let additions='';
  for(const group of groups){
    if(group.source)patches.push(...numbers(group.source,group.coordinates,newline));
    else additions+=`[booster select category]${newline}\t${group.coordinates.join('\t')}${newline}[/booster select category]${newline}`;
  }
  if(namesSection){
    namesSection.tokens.forEach((token,i)=>{
      const next=outputNames[i],raw=next?.raw??'';
      if(raw!==text.slice(token.start,token.end))patches.push({start:token.start,end:token.end,value:raw});
    });
    if(outputNames.length>namesSection.tokens.length)patches.push({start:namesSection.closeStart,end:namesSection.closeStart,value:newline+'\t'+outputNames.slice(namesSection.tokens.length).map(n=>n.raw).join('\t')+newline});
    if(additions)patches.push({start:namesSection.start,end:namesSection.start,value:additions});
  }else{
    additions+=`[booster category name]${newline}\t${outputNames.map(n=>n.raw).join('\t')}${newline}[/booster category name]${newline}`;
    const position=doc.groups.at(-1)!.source!.end;
    patches.push({start:position,end:position,value:newline+additions});
  }
  return apply(text,patches);
}

export function applyBoosterInlineAction(text: string, action: BoosterInlineAction): string {
  if('level' in action)return changeBoosterCategory(text,action);
  const draft=snapshot(text);
  if(action.kind==='settings'){
    if(action.selection!==undefined){if(draft.mode!=='selection')throw new Error('当前礼包不是自选礼包');draft.selection=action.selection;}
    if(action.drawCount!==undefined){
      const pool=draft.groups[0]?.pools?.[action.pool??-1];
      if(!pool)throw new Error('奖励池已变化，请重新操作');
      pool.drawCount=action.drawCount;
    }
  }else{
    const group=draft.groups[action.group];
    if(!group)throw new Error('分类已变化，请重新操作');
    if(action.kind==='add-item'){
      if(!action.row)throw new Error('请选择物品');
      const row={...action.row,sourceIndex:undefined};
      // An empty random box has no pool yet. The first candidate creates one draw.
      if(draft.mode==='booster'&&!group.pools?.length){
        if(!/^\d+$/.test(row.id)||Number(row.id)<1||Number(row.id)>2147483647||!/^\d+$/.test(row.quantity)||Number(row.quantity)<1||Number(row.quantity)>2147483647||!/^\d+$/.test(row.weight)||Number(row.weight)>2147483647)throw new Error('物品 ID、数量及权重无效');
        const newline=text.includes('\r\n')?'\r\n':'\n',position=group.source!.closeStart;
        return apply(text,[{start:position,end:position,value:`\t[etc]${newline}\t\t1\t${row.id}\t${row.weight}\t${row.quantity}${newline}\t[/etc]${newline}`}]);
      }
      group.rows.push(row);
    }else{
      const row=group.rows[action.index??-1];
      if(!row)throw new Error('物品已变化，请重新操作');
      if(action.kind==='delete-item')group.rows.splice(action.index!,1);
      else {if(!action.row)throw new Error('请选择物品');group.rows[action.index!]={...action.row,sourceIndex:row.sourceIndex,poolIndex:row.poolIndex};}
    }
  }
  return editBooster(text,draft);
}
