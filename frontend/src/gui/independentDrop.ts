import type { IndependentDropItem, IndependentDropRow, SkillNumber } from "../../bindings/pvfine/services/models";
import { editSkillNumbers } from "./skill";

export const independentDropModes = [{ label: "直接物品", value: 0 }, { label: "内嵌列表", value: 1 }, { label: "外部列表", value: 2 }];
export function dropModeLabel(mode: number): string { return independentDropModes.find(option => option.value === mode)?.label ?? `扩展模式 ${mode}`; }
export function sourceOffset(text: string, offset: number): number {
  if (!Number.isInteger(offset) || offset < 0) throw new Error("数据位置无效");
  let count = 0;
  for (let i = 0; i <= text.length; i++) {
    if (count === offset) return i;
    if (text[i] === "\r" && text[i + 1] === "\n") i++;
    count++;
  }
  throw new Error("数据位置已失效");
}
export function replaceDropSpan(text: string, start: number, end: number, value: string): string {
  if (end < start) throw new Error("数据范围无效");
  const a = sourceOffset(text, start), b = sourceOffset(text, end);
  return text.slice(0, a) + value + text.slice(b);
}
export function dropListText(items: { id: number; weight: number }[]): string {
  return items.map(item => {
    if (!Number.isInteger(item.id) || item.id < 0 || item.id > 2147483647 || !Number.isInteger(item.weight) || item.weight < 0 || item.weight > 2147483647) throw new Error("物品 ID 和权重必须为非负的 32 位整数");
    return `\t\t${item.id}\t${item.weight}`;
  }).join("\n");
}
export function dropRecordText(values: number[], items: { id: number; weight: number }[], listID: number): string {
  if (values.length !== 17 || values.some(n => !Number.isInteger(n) || n < -2147483648 || n > 2147483647)) throw new Error("记录需要 17 个 32 位整数");
  if (![0, 1].includes(values[0]!)) throw new Error("请选择怪物或 APC");
  if (!values.slice(3,8).every(n => n >= 0 && n <= 1000000) || values.slice(8,15).some(n => n < 0)) throw new Error("概率、数量或等级超出范围");
  if (values[14]! > 0 && values[13]! > values[14]!) throw new Error("最低等级不能超过最高等级");
  const mode = values[16];
  if (![0,1,2].includes(mode!)) throw new Error("不能新建未知掉落模式");
  const fields = [...values]; if (mode !== 0) fields[2] = 0;
  let text = fields.join("\t");
  if (mode === 1) text += `\n\t[list]\n${dropListText(items)}\n\t[/list]`;
  if (mode === 2) {
    if (!Number.isInteger(listID) || listID < 0 || listID > 2147483647) throw new Error("请输入有效列表 ID");
    text += `\n\t[list]\n\t\t${listID}\n\t[/list]`;
  }
  return text + "\n\t";
}
export function editDropFields(text: string, row: IndependentDropRow, changes: { column: number; value: number }[]): string {
  const fields = row.fields ?? [];
  const patches = changes.map(change => {
    const token = fields[change.column];
    if (!token || !Number.isInteger(change.column) || change.column < 0 || change.column > 15 || !Number.isInteger(change.value)) throw new Error("字段无效");
    if (change.value < -2147483648 || change.value > 2147483647) throw new Error("整数超出范围");
    if (change.column === 0 && ![0,1].includes(change.value)) throw new Error("请选择怪物或 APC");
    if (change.column >= 3 && change.column <= 7 && (fields[16]?.value ?? 0) > 2) throw new Error("扩展模式的概率含义未确认，请使用文本模式编辑");
    if (change.column >= 3 && change.column <= 7 && (change.value < 0 || change.value > 1000000)) throw new Error("概率必须为 0～100%");
    if (change.column >= 8 && change.column <= 14 && change.value < 0) throw new Error("数量和等级不能为负数");
    return {token,value:change.value};
  }).sort((a,b) => b.token.start - a.token.start);
  const min = changes.find(c=>c.column===13)?.value ?? fields[13]?.value ?? 0;
  const max = changes.find(c=>c.column===14)?.value ?? fields[14]?.value ?? 0;
  if (changes.some(c=>c.column===13 || c.column===14) && max > 0 && min > max) throw new Error("最低等级不能超过最高等级");
  return applyDropPatches(text, patches);
}
export function applyDropPatches(text: string, patches: {token: SkillNumber; value: number}[]): string {
  let previous = Infinity, result = text;
  for (const patch of [...patches].sort((a,b)=>b.token.start-a.token.start)) {
    if (patch.token.end > previous) throw new Error("数据位置重叠");
    result = editSkillNumbers(result,[patch.token],"set",patch.value); previous=patch.token.start;
  }
  return result;
}
export function editableDropItems(items: IndependentDropItem[]): { id: number; weight: number }[] {
  return items.filter(item=>item.fields?.length===2).map(item=>({id:item.fields![0]!.value,weight:item.fields![1]!.value}));
}

export function convertDropRecord(text: string, row: IndependentDropRow, values: number[], items: {id:number;weight:number}[], listID: number): string {
  if (row.hasExtensions || ![0,1,2].includes(row.fields?.[16]?.value ?? -1)) throw new Error("扩展记录不能转换掉落模式");
  const content=dropRecordText(values,items,listID).trimEnd();
  let result=text;
  if(row.listStart>=0) result=replaceDropSpan(result,row.listTagStart,row.listEnd+7,"");
  return replaceDropSpan(result,row.start,row.fields![16]!.end,content);
}
