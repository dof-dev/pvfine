import type { SkillNumber, VendingGroup, VendingReward } from "../../bindings/pvfine/services/models";
import { editSkillNumbers } from "./skill";

export function vendingInteger(value: number | null): number {
  if (value === null || !Number.isInteger(value) || value < 0 || value > 2147483647) throw new Error("请输入 0～2147483647 的整数");
  return value;
}
export function vendingProbability(weight: number, total: number): string {
  return total > 0 ? `${Number((weight / total * 100).toPrecision(6))}%` : "—";
}
export function vendingTotal(rewards: VendingReward[]): number {
  return rewards.reduce((sum, row) => sum + (row.fields?.[1]?.value ?? 0), 0);
}
function offsets(text: string): number[] {
  const result = [0];
  for (let i = 0; i < text.length; i++) {
    if (text[i] === "\r" && text[i + 1] === "\n") i++;
    result.push(i + 1);
  }
  return result;
}
function splice(text: string, start: number, end: number, replacement: string): string {
  const map = offsets(text);
  if (!Number.isInteger(start) || !Number.isInteger(end) || start < 0 || end < start || end >= map.length) throw new Error("数据位置已失效，请重新加载");
  return text.slice(0, map[start]) + replacement + text.slice(map[end]);
}
export function editVendingNumbers(text: string, fields: SkillNumber[], values: number[]): string {
  if (fields.length !== values.length || !fields.length) throw new Error("字段数量不匹配");
  values.forEach(vendingInteger);
  return fields.map((field, i) => ({ field, value: values[i]! })).sort((a,b) => b.field.start-a.field.start)
    .reduce((source, { field, value }) => editSkillNumbers(source, [field], "set", value), text);
}
function rewardText(values: number[]): string {
  if (values.length !== 4) throw new Error("奖品需要四个字段");
  values.forEach(vendingInteger);
  if (values[3] !== 0 && values[3] !== 1) throw new Error("是否公告必须为 0 或 1");
  return values.join("\t");
}
export function addVendingReward(text: string, group: VendingGroup, values: number[]): string {
  const map = offsets(text), raw = map[group.outputClose];
  if (raw === undefined || !/^\[\/output\]/i.test(text.slice(raw))) throw new Error("奖品列表位置已失效");
  const eol = text.includes("\r\n") ? "\r\n" : text.includes("\r") ? "\r" : "\n";
  // Inserting at the tag keeps all existing rows and their comments untouched.
  return splice(text, group.outputClose, group.outputClose, `${eol}\t\t${rewardText(values)}${eol}\t`);
}
export function removeVendingReward(text: string, reward: VendingReward): string {
  const fields = reward.fields ?? [];
  if (fields.length !== 4) throw new Error("奖品位置已失效");
  editVendingNumbers(text, fields, fields.map(f => f.value)); // Validate coordinates before deletion.
  return splice(text, fields[0]!.start, fields[3]!.end, "");
}
export function addVendingGroup(text: string, number: number, material: number, quantity: number): string {
  [number, material, quantity].forEach(vendingInteger);
  const eol = text.includes("\r\n") ? "\r\n" : text.includes("\r") ? "\r" : "\n";
  return text + (text && !/[\r\n]$/.test(text) ? eol : "") + ["[item group]", "\t[group num]", `\t\t${number}`, "\t[material]", `\t\t${material}\t${quantity}`, "\t[output]", "\t[/output]", "[/item group]", ""].join(eol);
}
export function removeVendingGroup(text: string, group: VendingGroup): string {
  const map = offsets(text);
  if (!/^\[item group\]/i.test(text.slice(map[group.start])) || !/\[\/item group\]$/i.test(text.slice(map[group.start], map[group.end]))) throw new Error("分组位置已失效");
  return splice(text, group.start, group.end, "");
}
export function filterVendingMachines<T extends { name: string; id: string; path: string }>(entries: T[], query: string): T[] {
  const pattern = query.trim().split(/([*?])/).map(part => part === "*" ? ".*" : part === "?" ? "." : part.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")).join("");
  const re = new RegExp(pattern, "i");
  return entries.filter(entry => re.test(`${entry.name} ${entry.id} ${entry.path}`));
}
