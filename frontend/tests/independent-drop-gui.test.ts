import { describe, it, expect } from "vitest";
import { applyDropPatches, convertDropRecord, dropListText, dropRecordText, editDropFields, replaceDropSpan, sourceOffset } from "../src/gui/independentDrop";
import type { IndependentDropRow, SkillNumber } from "../bindings/pvfine/services/models";
function fixture() {
 const line="0 20 3176 1000000 20 30 40 50 1 2 3 4 5 0 99 -1 1";
 const normalized="# 😀\n[independent drop]\n"+line+"\n[list]\n3176 20 3030 80\n[/list]\n[replace list]\n[dungeon condition]\n1 20 2\n[/replace list]\n[/independent drop]\n";
 const start=normalized.indexOf(line);
 const fields=[...line.matchAll(/-?\d+/g)].map(m=>({value:Number(m[0]),start:start+m.index!,end:start+m.index!+m[0].length,tokenType:0}) as SkillNumber);
 return {text:normalized.replaceAll("\n","\r\n"),row:{fields,start,end:normalized.indexOf("[/independent drop]"),hasExtensions:true} as IndependentDropRow};
}
describe("独立掉落源位置编辑",()=>{
 it("概率只修改目标字段，保留扩展条件、注释、emoji 和 CRLF",()=>{
  const {text,row}=fixture();
  expect(editDropFields(text,row,[{column:3,value:752500},{column:8,value:7}])).toBe(text.replace("1000000","752500").replace("1 2 3 4 5 0 99","7 2 3 4 5 0 99"));
 });
 it("处理列表增删、零权重和重复 ID",()=>{
  const content=dropListText([{id:3176,weight:0},{id:3176,weight:2}]);
  expect(content).toContain("3176\t0");
  const {text}=fixture();const normalized=text.replaceAll("\r\n","\n");
  const from=normalized.indexOf("[list]")+6,to=normalized.indexOf("[/list]");
  const edited=replaceDropSpan(text,from,to,"\n"+content+"\n");
  expect(edited).toContain("[replace list]\r\n[dungeon condition]");
  expect(edited).toContain("3176\t2");
  expect(sourceOffset(text,3)).toBe(3);
 });
 it("三种模式生成完整记录",()=>{
  const {row}=fixture(),values=row.fields!.map(f=>f.value);
  for(const mode of [0,1,2]){
   const record=dropRecordText([...values.slice(0,16),mode],[{id:3176,weight:1}],17);
   expect(record.includes("[list]")).toBe(mode!==0);
   if(mode===2)expect(record).toContain("\t\t17\n");
  }
 });
 it("转换掉落模式时保留相邻注释和额外 section",()=>{
  const {text,row}=fixture();
  const clean=text.replace(/\[replace list\][\s\S]*?\[\/replace list\]\r\n/,"");
  const normalized=clean.replaceAll("\r\n","\n");
  const source={...row,hasExtensions:false,listTagStart:normalized.indexOf("[list]"),listStart:normalized.indexOf("[list]")+6,listEnd:normalized.indexOf("[/list]")};
  const values=row.fields!.map(f=>f.value);values[16]=2;
  const edited=convertDropRecord(clean,source,values,[],17);
  expect(edited).toContain("[list]\n\t\t17\n\t[/list]");
  expect(edited).toContain("# 😀\r\n[independent drop]\r\n");
  expect(edited.match(/\[list\]/g)).toHaveLength(1);
  expect(edited).toContain("[/independent drop]\r\n");
  expect(()=>convertDropRecord(text,row,values,[],17)).toThrow("扩展");
 });
 it("拒绝过期、重叠、未知模式、溢出和非法概率",()=>{
  const {text,row}=fixture();
  expect(()=>editDropFields(text.replace("1000000","900000"),row,[{column:3,value:1}])).toThrow("草稿已变化");
  for(const value of [-1,1000001,NaN,1.5])expect(()=>editDropFields(text,row,[{column:3,value}])).toThrow();
  expect(()=>editDropFields(text,row,[{column:16,value:0}])).toThrow();
  expect(()=>editDropFields(text,row,[{column:3,value:1},{column:3,value:2}])).toThrow("重叠");
  expect(()=>applyDropPatches(text,[{token:row.fields![1]!,value:2147483648}])).toThrow();
  expect(()=>dropListText([{id:1,weight:-1}])).toThrow();
  expect(()=>sourceOffset(text,99999)).toThrow();
  expect(()=>dropRecordText([...row.fields!.slice(0,16).map(f=>f.value),5],[],0)).toThrow("未知");
 });
});
