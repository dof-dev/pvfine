import { describe, expect, it } from "vitest";
import { editForge } from "../src/gui/forge";
import { upgradeRange } from "../src/gui/upgrade";
import type { UpgradeRow } from "../bindings/pvfine/services/models";
function fixture() {
 const normalized = "# 😀\n[table]\n2 1 10000 1\n3 2 5000 1.11\n[/table]\n[separate upgrade max]\n2\n[item weights by grade]\n96 3326 51\n100 3326 51\n[/item weights by grade]";
 const field = (value: string, start: number) => ({ value: Number(value), start, end: start + value.length, tokenType: value.includes('.') ? 2 : 0 });
 const row = (line: string, level: number, grade = false): UpgradeRow => {
  const start = normalized.indexOf(line);
  const fields = [...line.matchAll(/\d+(?:\.\d+)?/g)].map(m => field(m[0], start + m.index!));
  return { level, fields: grade ? fields.slice(1) : fields, fieldsT2: [], costName: "强烈的气息" } as UpgradeRow;
 };
 return { text: normalized.replaceAll("\n", "\r\n"), rows: [row("2 1 10000 1",1), row("3 2 5000 1.11",2)], grades: [row("96 3326 51",96,true), row("100 3326 51",100,true)], max: [{ level: 1, fields: [field("2", normalized.indexOf("[separate upgrade max]\n") + "[separate upgrade max]\n".length)], fieldsT2: [], costName: "" } as UpgradeRow] };
}
describe("锻造 GUI 源码编辑", () => {
 it("六列布局的归一化字段保留附加系数，只更新成功率", () => {
  const text = "[table]\n2 1 2.6 1 10000 1\n[/table]";
  const start = text.indexOf("10000");
  const row = { level: 1, fields: [
   { value: 2, start: 8, end: 9, tokenType: 0 },
   { value: 1, start: 10, end: 11, tokenType: 0 },
   { value: 10000, start, end: start + 5, tokenType: 0 },
   { value: 1, start: start + 6, end: start + 7, tokenType: 0 },
  ], fieldsT2: [{ value: 2.6, start: 12, end: 15, tokenType: 2 }, { value: 1, start: 16, end: 17, tokenType: 0 }], costName: "" } as UpgradeRow;
  expect(editForge(text, [row], "table", [{ column: 2, value: 75.25 }], 1)).toBe(text.replace("10000", "7525"));
 });
 it("成功率使用万分比，批量保留属性系数和 CRLF", () => {
  const {text,rows}=fixture();
  expect(editForge(text,rows,"table",[{column:2,value:75.25}],2)).toBe(text.replace("10000 1","7525 1").replace("5000 1.11","7525 1.11"));
  expect(editForge(text,[rows[1]!],"table",[{column:3,value:1.5}],2)).toBe(text.replace("1.11","1.5"));
 });
 it("装备等级范围单独选择且仅替换材料和数量基数", () => {
  const {text,grades}=fixture();
  const chosen=upgradeRange(grades,[97,100]);
  expect(chosen).toHaveLength(1);
  expect(editForge(text,chosen,"grades",[{column:0,value:4444},{column:1,value:80}],2)).toBe(text.replace("100 3326 51","100 4444 80"));
 });
 it("最大等级限制及无效、过期和重叠数据拒绝", () => {
  const {text,rows,max,grades}=fixture();
  expect(editForge(text,max,"maximum",[{column:0,value:1}],2)).toBe(text.replace("[separate upgrade max]\r\n2","[separate upgrade max]\r\n1"));
  for (const value of [0,3,1.5]) expect(()=>editForge(text,max,"maximum",[{column:0,value}],2)).toThrow();
  expect(()=>editForge(text,rows,"table",[{column:2,value:101}],2)).toThrow();
  expect(()=>editForge(text,rows,"table",[{column:0,value:1}],2)).toThrow();
  expect(()=>editForge(text,grades,"grades",[{column:1,value:1.5}],2)).toThrow();
  expect(()=>editForge(text,rows,"table",[{column:3,value:1.5}],2)).toThrow("整数");
  expect(()=>editForge(text.replace("5000","6000"),rows,"table",[{column:2,value:1}],2)).toThrow("草稿已变化");
  expect(()=>editForge(text,rows,"table",[{column:2,value:1},{column:2,value:2}],2)).toThrow("重叠");
 });
});
