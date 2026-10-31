import { describe, expect, it } from "vitest";
import { skillTreeEdge, skillTreeEdgeLayout, groupSkillTreeCells, skillTreeRowLevels, skillTreeCellNode, skillTreeDropTarget, moveSkillTreeCell, skillTreeCanvasHeight, addSkillTreeNode, removeSkillTreeNode, editSkillTreeLink } from "../src/gui/skillTree";
import type { SkillTreeNode } from "../bindings/pvfine/services/models";
const node = (x: number, y: number): SkillTreeNode => ({id: 1, sourceStart: 0, sourceClose: -1, nextTokens: [], positionTokens: [], x, y, name: "测试", explain: "", positionKind: "icon pos", position: [x, y], next: [], path: "", fileIndex: -1, icon: null, requiredLevel: null});
describe("技能树连线", () => {
  it("端点与实际路径一致，同位置关联绕开卡片，仍可选中编辑", () => {
    expect(skillTreeEdgeLayout(node(0, 0), node(94, 0)).end).toEqual({x: 90, y: 16});
    expect(skillTreeEdgeLayout(node(0, 0), node(0, 134)).end).toEqual({x: 16, y: 131});
    expect(skillTreeEdgeLayout(node(47, 67), node(47, 67))).toEqual({path: "M 81 83 H 91 V 57 H 63 V 64", end: {x: 63, y: 64}});
  });
  it("横向连线停在目标图标前，反向关系也保留箭头方向", () => {
    expect(skillTreeEdge(node(0, 0), node(94, 0))).toBe("M 34 16 H 62 V 16 H 90");
    expect(skillTreeEdge(node(94, 0), node(0, 0))).toBe("M 92 16 H 64 V 16 H 36");
  });
  it("纵向跨行连接保留像素偏移，并绕开图标中心", () => {
    expect(skillTreeEdge(node(23, 0), node(47, 134))).toBe("M 39 40 V 85.5 H 63 V 131");
  });
});

 describe("同位置技能", () => {
  it("保留坐标和文件顺序，同格技能可分别选择", () => {
    const a = {...node(235, 938), id: 319};
    const b = {...node(235, 938), id: 112};
    const c = {...node(329, 938), id: 113};
    const cells = groupSkillTreeCells([a, c, b]);
    expect(cells.map(cell => cell.nodes.map(n => n.id))).toEqual([[319, 112], [113]]);
    expect(cells[0]!.nodes[1]).toBe(b);
    expect([b.x, b.y]).toEqual([235, 938]);
  });
});

 describe("技能树行等级", () => {
  it("SP 按最左侧技能取学习等级，同坐标按文件顺序，不随所选技能变化", () => {
    const nodes = [
      {...node(94, 0), requiredLevel: 20},
      {...node(23, 0), requiredLevel: 5},
      {...node(23, 0), requiredLevel: 10},
      {...node(0, 134), requiredLevel: 0},
      {...node(0, 201), requiredLevel: null},
    ];
    const levels = skillTreeRowLevels(nodes, "sp");
    expect(levels.get(0)).toBe(5);
    expect(levels.has(1)).toBe(false);
    expect(levels.get(2)).toBe(0);
    expect(levels.get(3)).toBeNull();
  });
  it("TP 不产生等级标注", () => {
    expect(skillTreeRowLevels([{...node(0, 0), requiredLevel: 50}], "tp").size).toBe(0);
  });
});

 describe("同位置技能默认显示", () => {
  it("默认取文件中最后一个技能；记忆有效则恢复，技能已移除则回退到最后一个", () => {
    const cell = {key: "0:0", nodes: [{...node(0, 0), id: 0}, {...node(0, 0), id: 112}, {...node(0, 0), id: 319}]};
    expect(skillTreeCellNode(cell).id).toBe(319);
    expect(skillTreeCellNode(cell, 0).id).toBe(0);
    expect(skillTreeCellNode(cell, 112).id).toBe(112);
    expect(skillTreeCellNode(cell, 999).id).toBe(319);
  });
});

 describe("技能树拖拽", () => {
  it("吸附47×67网格，拒绝边界、已占用位置及半格图标的交叠", () => {
    const a = {...node(0, 0), sourceStart: 1};
    const b = {...node(94, 67), sourceStart: 2};
    const half = {...node(23, 134), sourceStart: 3};
    const cell = {key: "0:0", nodes: [a]};
    const nodes = [a, b, half];
    expect(skillTreeDropTarget(nodes, cell, 46, 66, 564, 268)).toMatchObject({x: 47, y: 67, valid: true});
    expect(skillTreeDropTarget(nodes, cell, 94, 67, 564, 268).valid).toBe(false);
    expect(skillTreeDropTarget(nodes, cell, 0, 134, 564, 268).valid).toBe(false);
    expect(skillTreeDropTarget(nodes, cell, 47, 134, 564, 268).valid).toBe(false);
    expect(skillTreeDropTarget(nodes, cell, -47, 0, 564, 268).valid).toBe(false);
    expect(skillTreeDropTarget(nodes, cell, 564, 0, 564, 268).valid).toBe(false);
    expect(skillTreeDropTarget(nodes, cell, 0, 268, 564, 268).valid).toBe(false);
  });
  it("整组移动混合坐标，仅改数字，保留关联、CRLF、emoji及其他分支", () => {
    const text = "// 😀\r\n[character job]\r\n`[swordman]` `none`\r\n[skill info]\r\n[index]\r\n1\r\n[cell pos]\r\n2\t3 // 坐标\r\n[next skill]\r\n9\r\n[/skill info]\r\n[skill info]\r\n[index]\r\n2\r\n[icon pos]\r\n94 201\r\n[/skill info]\r\n[/character job]\r\n[character job]\r\n`[mage]` `none`\r\n[skill info]\r\n[index]\r\n1\r\n[cell pos]\r\n2\t3\r\n[/skill info]\r\n[/character job]";
    const normalized = text.replaceAll("\r\n", "\n");
    const tokens = (raw: string, values: number[]) => {
      const start = normalized.indexOf(raw);
      return values.map((value, i) => ({value, start: start + (i ? String(values[0]).length + 1 : 0), end: start + (i ? String(values[0]).length + 1 : 0) + String(value).length, tokenType: 0}));
    };
    const a = {...node(94, 201), sourceStart: 1, positionKind: "cell pos", positionTokens: tokens("2\t3", [2, 3])};
    const b = {...node(94, 201), sourceStart: 2, positionKind: "icon pos", positionTokens: tokens("94 201", [94, 201])};
    const cell = {key: "94:201", nodes: [a, b]};
    const result = moveSkillTreeCell(text, cell.nodes, cell, 47, 67, 564, 268);
    expect(result).toBe(text.replace("2\t3 // 坐标", "1\t1 // 坐标").replace("94 201", "47 67"));
    expect(() => moveSkillTreeCell(text.replace("94 201", "95 201"), cell.nodes, cell, 47, 67, 564, 268)).toThrow("已变化");
    expect(() => moveSkillTreeCell(text, cell.nodes, cell, 23, 67, 564, 268)).toThrow("网格");
  });
});

 describe("技能树追加行", () => {
  it("末尾始终有空行，移入后继续扩展，画布外下一行仍不可放置", () => {
    const source = {...node(0, 201), sourceStart: 1};
    const cell = {key: "0:201", nodes: [source]};
    const height = skillTreeCanvasHeight([source]);
    expect(height).toBe(335);
    expect(skillTreeDropTarget([source], cell, 47, 268, 564, height).valid).toBe(true);
    expect(skillTreeDropTarget([source], cell, 47, 335, 564, height).valid).toBe(false);
    expect(skillTreeCanvasHeight([{...source, y: 268}])).toBe(402);
    expect(skillTreeCanvasHeight([])).toBe(268);
  });
  it("像素坐标有纵向偏移时，新增行仍完整留空", () => {
    const height = skillTreeCanvasHeight([node(23, 220)]);
    expect(height).toBe(402);
    expect(height - 67).toBeGreaterThan(220 + 56);
  });
});

 describe("点击空白格新增技能", () => {
  it("只在当前分支结束前插入，保留CRLF、emoji、其他分支及原有注释", () => {
    const text = "// 😀\r\n[character job]\r\n`[mage]` `none`\r\n// 保留\r\n[/character job]\r\n[character job]\r\n`[swordman]` `none`\r\n[/character job]";
    const branch = {job: "[mage]", name: "none", nodes: [], insertOffset: text.replaceAll("\r\n", "\n").indexOf("[/character job]")};
    const result = addSkillTreeNode(text, branch, 0, 94, 201, 564, 335);
    expect(result).toBe(text.replace("// 保留\r\n", "// 保留\r\n\t[skill info]\r\n\t\t[index]\r\n\t\t\t0\r\n\t\t[cell pos]\r\n\t\t\t2\t3\r\n\t[/skill info]\r\n"));
  });
  it("拒绝重复ID、占用位置、缺失结束标签和失效插入位置", () => {
    const text = "[character job]\n`[mage]` `none`\n[/character job]";
    const existing = {...node(0, 0), id: 5, sourceStart: 20};
    const branch = {job: "[mage]", name: "none", nodes: [existing], insertOffset: text.indexOf("[/character job]")};
    expect(() => addSkillTreeNode(text, branch, 5, 47, 0, 564, 268)).toThrow("已包含");
    expect(() => addSkillTreeNode(text, branch, 7, 0, 0, 564, 268)).toThrow("占用");
    expect(() => addSkillTreeNode(text, {...branch, insertOffset: -1}, 7, 47, 0, 564, 268)).toThrow("结束标签");
    expect(() => addSkillTreeNode("// 变化\n" + text, branch, 7, 47, 0, 564, 268)).toThrow("已变化");
  });
});

 describe("技能树删除", () => {
  const fixture = () => {
    const record = (id: number, next = "") => `[skill info]\n[index]\n${id}\n[cell pos]\n0 0\n${next}[/skill info]\n`;
    const first = record(7, "// [/skill info] 😀\n");
    const second = record(8, "[next skill]\n7 9 7 // 保留注释\n");
    const other = record(7);
    const text = `[character job]\n\`[swordman]\` \`none\`\n${first}${second}[/character job]\n[character job]\n\`[swordman]\` \`asura\`\n${other}[/character job]\n`;
    const a = {...node(0, 0), id: 7, sourceStart: text.indexOf(first), sourceClose: text.indexOf("[/skill info]", text.indexOf("😀"))};
    const linkStart = text.indexOf("7 9 7");
    const b = {...node(0, 0), id: 8, sourceStart: text.indexOf(second), sourceClose: text.indexOf("[/skill info]", linkStart), next: [7, 9, 7], nextTokens: [0, 2, 4].map((offset, i) => ({value: i === 1 ? 9 : 7, start: linkStart + offset, end: linkStart + offset + 1, tokenType: 2}))};
    return {text, first, second, a, b, branch: {job: "[swordman]", name: "none", insertOffset: -1, nodes: [a, b]}};
  };
  it("只移除选中记录及当前分支所有入边，保留重叠技能、注释、另一分支和CRLF", () => {
    const {text, first, a, branch} = fixture();
    const expected = text.replace(first, "").replace("7 9 7 //", " 9  //");
    expect(removeSkillTreeNode(text, branch, a)).toBe(expected);
    expect(removeSkillTreeNode(text.replaceAll("\n", "\r\n"), branch, a)).toBe(expected.replaceAll("\n", "\r\n"));
  });
  it("保留同行的其他文本、删除仅剩技能后允许空分支", () => {
    const text = "[character job]\n`[swordman]` `none`\n[skill info][index]7[cell pos]0 0[/skill info] // 留下\n[/character job]";
    const a = {...node(0, 0), id: 7, sourceStart: text.indexOf("[skill info]"), sourceClose: text.indexOf("[/skill info]")};
    const branch = {job: "[swordman]", name: "none", insertOffset: -1, nodes: [a]};
    expect(removeSkillTreeNode(text, branch, a)).toBe("[character job]\n`[swordman]` `none`\n // 留下\n[/character job]");
  });
  it("拒绝缺失关闭标签、过期关联及不属于分支的技能", () => {
    const {text, a, branch} = fixture();
    expect(() => removeSkillTreeNode(text, branch, {...a})).toThrow("技能已变化");
    a.sourceClose = -1;
    expect(() => removeSkillTreeNode(text, branch, a)).toThrow("不完整");
    const fresh = fixture();
    expect(() => removeSkillTreeNode(fresh.text.replace("7 9 7", "6 9 7"), fresh.branch, fresh.a)).toThrow("关联记录已变化");
    fresh.b.nextTokens = [];
    expect(() => removeSkillTreeNode(fresh.text, fresh.branch, fresh.a)).toThrow("关联记录不完整");
  });
});

 describe("技能树关联编辑", () => {
  const fixture = () => {
    const record = "[skill info]\n[index]\n1\n[cell pos]\n0 0\n[next skill]\n2 99 // 😀 顺序与注释保留\n[next skill]\n2\n[/skill info]";
    const text = "// 😀\n[character job]\n`[swordman]` `none`\n" + record + "\n[/character job]\n";
    const first = text.indexOf("2 99"), last = text.lastIndexOf("2\n");
    const source = {...node(0, 0), sourceStart: text.indexOf("[skill info]"), sourceClose: text.indexOf("[/skill info]"), next: [2, 99, 2], nextTokens: [
      {value: 2, start: first, end: first + 1, tokenType: 0},
      {value: 99, start: first + 2, end: first + 4, tokenType: 0},
      {value: 2, start: last, end: last + 1, tokenType: 0},
    ]};
    const branch = {job: "[swordman]", name: "none", insertOffset: -1, nodes: [source, {...node(47, 0), id: 2}, {...node(94, 0), id: 3}]};
    return {text, source, branch};
  };
  it("修改和删除按条目索引定位，保留重复关联、失效目标、注释与CRLF", () => {
    const {text, source, branch} = fixture();
    expect(editSkillTreeLink(text, branch, source, 2, 3)).toBe(text.replace("[next skill]\n2\n", "[next skill]\n3\n"));
    expect(editSkillTreeLink(text, branch, source, 1, null)).toBe(text.replace("2 99 //", "2  //"));
    expect(editSkillTreeLink(text.replaceAll("\n", "\r\n"), branch, source, 0, 3)).toBe(text.replace("2 99 //", "3 99 //").replaceAll("\n", "\r\n"));
  });
  it("新增追加到技能记录末尾，保留现有关联的顺序", () => {
    const {text, source, branch} = fixture();
    expect(editSkillTreeLink(text, branch, source, -1, 3)).toBe(text.replace("[/skill info]", "\t[next skill]\n\t\t3\n[/skill info]"));
    const empty = {...source, next: [], nextTokens: []};
    const emptyBranch = {...branch, nodes: [empty, ...branch.nodes.slice(1)]};
    const blankText = text.replace(/2 99/g, "    ").replace(/2\n/g, " \n");
    expect(editSkillTreeLink(blankText, emptyBranch, empty, -1, 3)).toContain("\t[next skill]\n\t\t3\n[/skill info]");
  });

  it("没有 next skill 标签时可新增首条关联，且不改其他记录", () => {
    const text = "[skill info][index]1[cell pos]0 0[/skill info]\n[skill info][index]2[cell pos]1 0[/skill info]";
    const source = {...node(0, 0), sourceClose: text.indexOf("[/skill info]")};
    const branch = {job: "[swordman]", name: "none", insertOffset: -1, nodes: [source, {...node(47, 0), id: 2}]};
    expect(editSkillTreeLink(text, branch, source, -1, 2)).toBe(text.replace("0 0[/skill info]", "0 0\n\t[next skill]\n\t\t2\n[/skill info]"));
  });
  it("拒绝跨分支目标、自关联、重复新增、过期索引与过期文本", () => {
    const {text, source, branch} = fixture();
    expect(() => editSkillTreeLink(text, branch, source, -1, 2)).toThrow("已存在");
    expect(() => editSkillTreeLink(text, branch, source, -1, 1)).toThrow("自身");
    expect(() => editSkillTreeLink(text, branch, source, -1, 99)).toThrow("当前分支");
    expect(() => editSkillTreeLink(text, branch, source, 3, null)).toThrow("位置已变化");
    expect(() => editSkillTreeLink(text.replace("2 99", "3 99"), branch, source, 0, null)).toThrow("记录已变化");
    source.sourceClose = -1;
    expect(() => editSkillTreeLink(text, branch, source, -1, 3)).toThrow("不完整");
  });
});
