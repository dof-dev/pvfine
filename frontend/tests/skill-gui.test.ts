import { describe, expect, it } from "vitest";
import { editSkillNumbers, skillPreview } from "../src/gui/skill";
import type { SkillMode, SkillNumber, SkillProperty } from "../bindings/pvfine/services/models";
const number = (value: number, start = 0, end = 1, tokenType = 0): SkillNumber => ({ value, start, end, tokenType });
const mode: SkillMode = { id: "dungeon", width: 2, static: [number(10000)], levels: [[number(50), number(123)], [number(100), number(456)]], properties: [], issues: [] };
const property: SkillProperty = { template: "时间<int>秒，伤害<int>\n减速<float2>%% / <float1>%%", bindings: [
  { dynamic: false, index: 0, multiplier: .001 }, { dynamic: true, index: 0, multiplier: 2 },
  { dynamic: true, index: 1, multiplier: .1 }, { dynamic: true, index: 1, multiplier: .01 },
] };
describe("技能描述与数据编辑", () => {
  it("渲染多行、多个占位符、倍率、百分号和等级", () => {
    expect(skillPreview(property, mode, 1).map((p) => p.text).join("")).toBe("时间10秒，伤害100\n减速12.30% / 1.2%");
    expect(skillPreview(property, mode, 2).map((p) => p.text).join("")).toContain("伤害200\n减速45.60%");
    expect(skillPreview(property, mode, 2).filter((p) => p.token)).toHaveLength(4);
  });
  it("缺失绑定或索引时保留文本并禁用占位符", () => {
    const preview = skillPreview({ template: "伤害<int>", bindings: [{ dynamic: true, index: 9, multiplier: 1 }] }, mode, 1);
    expect(preview[1]?.token).toBeUndefined();
    expect(preview[1]?.text).toBe("（无对应数据）");
  });
  it("批量替换同一列，仅修改目标数值并保留格式", () => {
    const text = "[level info]\n2\n10 20 # first\n30 40\n[/level info]";
    const tokens = [number(10, text.indexOf("10"), text.indexOf("10") + 2), number(30, text.indexOf("30"), text.indexOf("30") + 2)];
    expect(editSkillNumbers(text, tokens, "set", 99)).toBe(text.replace("10", "99").replace("30", "99"));
    expect(editSkillNumbers(text, tokens, "add", 5)).toBe(text.replace("10", "15").replace("30", "35"));
    expect(editSkillNumbers(text, tokens, "multiply", 1.25)).toBe(text.replace("10", "13").replace("30", "38"));
  });
  it("保留浮点类型、UTF-16 定位和 CRLF", () => {
    const text = "`😀`\r\n1.5\r\n2";
    expect(editSkillNumbers(text, [number(1.5, 5, 8, 2)], "set", 2)).toBe("`😀`\r\n2.0\r\n2");
  });
  it("拒绝过期定位、溢出和重叠，避免部分写入", () => {
    expect(() => editSkillNumbers("99", [number(10, 0, 2)], "set", 5)).toThrow("草稿已变化");
    expect(() => editSkillNumbers("1", [number(1)], "set", 2147483648)).toThrow("32 位范围");
    expect(() => editSkillNumbers("1", [number(1, 0, 1, 2)], "multiply", 1e40)).toThrow("32 位范围");
    expect(() => editSkillNumbers("1", [number(1), number(1)], "add", 1)).toThrow("重叠");
    expect(() => editSkillNumbers("1", [number(1)], "set", Infinity)).toThrow();
  });
});
