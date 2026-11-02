import { describe, expect, it } from "vitest";
import type { VendingGroup, VendingReward, SkillNumber } from "../bindings/pvfine/services/models";
import { addVendingGroup, addVendingReward, editVendingNumbers, filterVendingMachines, removeVendingGroup, removeVendingReward, vendingInteger, vendingProbability, vendingTotal } from "../src/gui/vendingMachine";

function fixture(eol = "\n", compact = false) {
  const text = ("# 😀 保留注释\n[name]`抽奖机`\n[item group]\n[group num]1\n[material]42 2\n[output]" + (compact ? "" : "\n\t") + "7740\t3000\t2\t0" + (compact ? "" : "\n\t") + "[/output]\n[other]`保留参数`\n[/item group]\n[tail]99\n").replaceAll("\n", eol);
  const n = text.replace(/\r\n|\r/g,"\n"), start = n.indexOf("7740");
  const fields = [[7740,0],[3000,5],[2,10],[0,12]].map(([value,offset]) => ({value:value!,start:start+offset!,end:start+offset!+String(value).length,tokenType:0} as SkillNumber));
  const reward: VendingReward = {fields,name:"测试奖品"};
  const group = {start:n.indexOf("[item group]"),end:n.indexOf("[/item group]")+13,rewards:[reward],outputClose:n.indexOf("[/output]")} as VendingGroup;
  return {text,group,reward};
}
describe("抽奖机编辑", () => {
  for (const eol of ["\n","\r\n","\r"]) {
    it(`保留 ${JSON.stringify(eol)}、emoji、注释和未知参数`, () => {
      const {text,group,reward}=fixture(eol);
      expect(editVendingNumbers(text,reward.fields!,[7740,20,4,1])).toBe(text.replace("7740\t3000\t2\t0","7740\t20\t4\t1"));
      const added=addVendingReward(text,group,[7740,10,1,0]);
      expect(added).toContain("7740\t3000\t2\t0"); expect(added).toContain("7740\t10\t1\t0"); expect(added).toContain("[other]`保留参数`");
      expect(removeVendingReward(text,reward)).toBe(text.replace("7740\t3000\t2\t0",""));
      expect(removeVendingGroup(text,group)).toBe(text.slice(0,text.indexOf("[item group]"))+text.slice(text.indexOf("[/item group]")+13));
      expect(addVendingGroup(text,2,42,1)).toContain(`[group num]${eol}\t\t2`);
      if(eol==="\r\n") expect(added.replaceAll("\r\n","")).not.toContain("\n");
    });
  }
  it("紧凑格式新增奖品不会拼接原数值", () => {
    const {text,group}=fixture("\n",true);
    expect(addVendingReward(text,group,[42,1,2,0])).toContain("7740\t3000\t2\t0\n\t\t42\t1\t2\t0\n\t[/output]");
  });
  it("拒绝失效位置和无效整数", () => {
    const {text,group,reward}=fixture();
    expect(()=>editVendingNumbers(text.replace("3000","2000"),reward.fields!,[1,2,3,0])).toThrow();
    expect(()=>addVendingReward(text,{...group,outputClose:0} as VendingGroup,[1,2,3,0])).toThrow();
    expect(()=>addVendingReward(text,group,[1,2,3,2])).toThrow();
    for(const value of [null,NaN,-1,0.1,2147483648]) expect(()=>vendingInteger(value)).toThrow();
  });
  it("独立计算重复物品记录，零权重不显示 NaN", () => {
    const {reward}=fixture();
    expect(vendingTotal([reward,reward])).toBe(6000);
    expect(vendingProbability(3000,6000)).toBe("50%"); expect(vendingProbability(0,0)).toBe("—"); expect(vendingProbability(20,100000000)).toBe("0.00002%");
  });
  it("按 LST ID、名称、路径和通配符筛选", () => {
    const entries=[{name:"抽奖机",id:"12",path:"etc/vending/a.vm"},{name:"特殊",id:"99",path:"etc/vending/b.vm"}];
    expect(filterVendingMachines(entries,"12")).toEqual([entries[0]]); expect(filterVendingMachines(entries,"*.vm")).toEqual(entries);
    expect(filterVendingMachines(entries,"特殊")).toEqual([entries[1]]); expect(filterVendingMachines(entries,"A.?m")).toEqual([entries[0]]); expect(filterVendingMachines(entries,"[")).toEqual([]);
  });
});
