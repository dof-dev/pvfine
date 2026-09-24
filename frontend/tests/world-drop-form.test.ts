import { describe, expect, it } from "vitest";
import {
  areLevelsEqual,
  batchAddItems,
  batchDeleteItems,
  calcItemPercentage,
  calcLevelTotalWeight,
  countLevelMatches,
  countMatchingItems,
  filterItems,
  findFirstLevelWithMatch,
  getLevelsInInterval,
  isFilterCriteriaActive,
  isNonNegativeInt32,
  isPositiveInt32,
  moveItem,
  toEditableLevels,
  toWorldDropLevels,
  validateAllLevels,
  validateItem,
  validateLevelInterval,
  validateLevelNumber,
  type WorldDropFilterCriteria,
} from "../src/gui/worldDropForm";
import type { WorldDropLevel } from "../bindings/pvfine/services/models";

describe("worldDropForm validation & calculation", () => {
  it("整数范围校验正整数与非负整数边界", () => {
    expect(isPositiveInt32(1)).toBe(true);
    expect(isPositiveInt32(2147483647)).toBe(true);
    expect(isPositiveInt32(0)).toBe(false);
    expect(isPositiveInt32(-1)).toBe(false);
    expect(isPositiveInt32(2147483648)).toBe(false);
    expect(isPositiveInt32(1.5)).toBe(false);
    expect(isPositiveInt32("10")).toBe(false);

    expect(isNonNegativeInt32(0)).toBe(true);
    expect(isNonNegativeInt32(100)).toBe(true);
    expect(isNonNegativeInt32(2147483647)).toBe(true);
    expect(isNonNegativeInt32(-1)).toBe(false);
    expect(isNonNegativeInt32(2147483648)).toBe(false);
    expect(isNonNegativeInt32(0.1)).toBe(false);
  });

  it("等级校验：正整数且禁止重复等级，但允许当前等级保留原数值", () => {
    const existing = [1, 10, 85];
    expect(validateLevelNumber(0, existing)).toContain("正整数");
    expect(validateLevelNumber(-5, existing)).toContain("正整数");
    expect(validateLevelNumber(2147483648, existing)).toContain("正整数");

    // 重复新增禁止
    expect(validateLevelNumber(10, existing)).toContain("已存在");
    // 编辑当前等级，数值保持 10 不算重复
    expect(validateLevelNumber(10, existing, 10)).toBe("");
    // 正常新等级
    expect(validateLevelNumber(20, existing)).toBe("");
  });

  it("道具校验：ID 必须为正整数，权重必须为非负整数", () => {
    expect(validateItem(1001, 50)).toBe("");
    expect(validateItem(1001, 0)).toBe(""); // 权重允许为 0
    expect(validateItem(0, 50)).toContain("道具 ID");
    expect(validateItem(-1, 50)).toContain("道具 ID");
    expect(validateItem(0, 50, true)).toBe("");
    expect(validateItem(-2, 50, true)).toBe("");
    expect(validateItem(-1, 50, true)).toContain("道具 ID");
    expect(validateItem(1001, -1)).toContain("权重");
    expect(validateItem(1001, 2147483648)).toContain("权重");
  });

  it("全量等级列表校验：允许空等级，禁止重复等级与非法数据", () => {
    const validWithEmpty: WorldDropLevel[] = [
      { level: 1, items: [{ id: 100, weight: 50, name: "物品A" }] },
      { level: 10, items: [] }, // 允许空等级
      { level: 85, items: [{ id: 200, weight: 0, name: "物品B" }] },
    ];
    expect(validateAllLevels(validWithEmpty)).toBe("");
    expect(validateAllLevels([{ level: 1, items: [{ id: 0, weight: 5, name: "未知物品 #0" }, { id: -2, weight: 0, name: "未知物品 #-2" }] }])).toBe("");

    const duplicateLevels: WorldDropLevel[] = [
      { level: 1, items: [] },
      { level: 1, items: [] },
    ];
    expect(validateAllLevels(duplicateLevels)).toContain("等级 1 重复");

    const invalidItemLevel: WorldDropLevel[] = [
      { level: 1, items: [{ id: -1, weight: 10, name: "" }] },
    ];
    expect(validateAllLevels(invalidItemLevel)).toContain("道具错误");
  });

  it("计算等级总权重与百分比占比：总权重为 0 时显示中立 0.00% 而不是 NaN", () => {
    const items = [
      { id: 1, weight: 100, name: "" },
      { id: 2, weight: 300, name: "" },
    ];
    const total = calcLevelTotalWeight(items);
    expect(total).toBe(400);
    expect(calcItemPercentage(100, total)).toBe("25.00%");
    expect(calcItemPercentage(300, total)).toBe("75.00%");

    // 总权重为 0 时：绝不能返回 NaN
    const zeroTotal = calcLevelTotalWeight([{ id: 1, weight: 0, name: "" }]);
    expect(zeroTotal).toBe(0);
    expect(calcItemPercentage(0, zeroTotal)).toBe("0.00%");
    expect(calcItemPercentage(0, 0)).toBe("0.00%");
    expect(calcItemPercentage(-10, 0)).toBe("0.00%");
  });

  it("数据转换与对比：保持顺序且正确判定脏状态", () => {
    const original: WorldDropLevel[] = [
      { level: 5, items: [{ id: 10, weight: 50, name: "药水" }] },
      { level: 1, items: [{ id: 20, weight: 80, name: "长剑" }] },
    ];
    const editable = toEditableLevels(original);
    expect(editable.length).toBe(2);
    expect(editable[0].level).toBe(5);
    expect(editable[1].level).toBe(1);

    const converted = toWorldDropLevels(editable);
    expect(areLevelsEqual(original, converted)).toBe(true);

    // 修改权重
    editable[0].items[0].weight = 99;
    expect(areLevelsEqual(original, toWorldDropLevels(editable))).toBe(false);

    // 恢复
    editable[0].items[0].weight = 50;
    expect(areLevelsEqual(original, toWorldDropLevels(editable))).toBe(true);

    // 调整顺序应视为不同（保留顺序不自动排序）
    const reordered: WorldDropLevel[] = [original[1], original[0]];
    expect(areLevelsEqual(original, reordered)).toBe(false);
  });

  it("道具排序移动操作", () => {
    const items = [
      { id: 1, weight: 10, name: "A", key: 1 },
      { id: 2, weight: 20, name: "B", key: 2 },
      { id: 3, weight: 30, name: "C", key: 3 },
    ];
    // 下移第 0 项
    expect(moveItem(items, 0, 1)).toBe(true);
    expect(items.map((i) => i.id)).toEqual([2, 1, 3]);

    // 越界保护
    expect(moveItem(items, -1, 1)).toBe(false);
    expect(moveItem(items, 0, 5)).toBe(false);
    expect(moveItem(items, 1, 1)).toBe(false);
  });

  describe("等级区间校验与筛选 (validateLevelInterval & getLevelsInInterval)", () => {
    const existing = [1, 10, 20, 85];

    it("空选择或不在已有等级范围内的区间校验失败", () => {
      expect(validateLevelInterval(null, 10, existing)).toContain("请选择");
      expect(validateLevelInterval(1, null, existing)).toContain("请选择");
      expect(validateLevelInterval(5, 10, existing)).toContain("Lv. 5 不存在");
      expect(validateLevelInterval(1, 99, existing)).toContain("Lv. 99 不存在");
    });

    it("起始等级大于结束等级校验失败", () => {
      expect(validateLevelInterval(85, 1, existing)).toContain("不能大于");
    });

    it("合法的已有等级闭区间校验成功", () => {
      expect(validateLevelInterval(1, 1, existing)).toBe("");
      expect(validateLevelInterval(1, 10, existing)).toBe("");
      expect(validateLevelInterval(10, 85, existing)).toBe("");
    });

    it("getLevelsInInterval 保留原有顺序且仅包含存在的配置等级", () => {
      const levels = toEditableLevels([
        { level: 50, items: [] },
        { level: 10, items: [] },
        { level: 85, items: [] },
        { level: 1, items: [] },
      ]);

      const sub = getLevelsInInterval(levels, 10, 50);
      expect(sub.map((l) => l.level)).toEqual([50, 10]);

      const single = getLevelsInInterval(levels, 85, 85);
      expect(single.map((l) => l.level)).toEqual([85]);

      expect(getLevelsInInterval(levels, 85, 10)).toEqual([]);
      expect(getLevelsInInterval(levels, null, 10)).toEqual([]);
    });
  });

  describe("批量添加道具 (batchAddItems)", () => {
    it("向区间内已有等级添加道具；若已存在相同道具则追加新行而不覆盖", () => {
      const levels = toEditableLevels([
        { level: 1, items: [{ id: 100, weight: 10, name: "药水" }] },
        { level: 10, items: [] }, // 空等级
        { level: 20, items: [{ id: 200, weight: 50, name: "金币" }] },
        { level: 85, items: [] },
      ]);

      // 向 [1, 20] 区间批量添加道具 100
      const res = batchAddItems(levels, 1, 20, {
        id: 100,
        weight: 99,
        name: "新药水",
      });

      expect(res.addedCount).toBe(3);
      expect(res.affectedLevels).toEqual([1, 10, 20]);

      // 等级 1 原有道具 100，现在应该有 2 条记录（追加而非覆盖）
      expect(levels[0].items.length).toBe(2);
      expect(levels[0].items[0]).toMatchObject({ id: 100, weight: 10, name: "药水" });
      expect(levels[0].items[1]).toMatchObject({ id: 100, weight: 99, name: "新药水" });

      // 等级 10 原为空，现在添加 1 项
      expect(levels[1].items.length).toBe(1);
      expect(levels[1].items[0]).toMatchObject({ id: 100, weight: 99 });

      // 等级 20 原有金币，现在追加新药水
      expect(levels[2].items.length).toBe(2);
      expect(levels[2].items[1]).toMatchObject({ id: 100, weight: 99 });

      // 等级 85 不在区间内，保持为空
      expect(levels[3].items.length).toBe(0);
    });
  });

  describe("批量删除道具 (countMatchingItems & batchDeleteItems)", () => {
    it("匹配统计准确反映区间内匹配等级数及全部重复行", () => {
      const levels = toEditableLevels([
        {
          level: 1,
          items: [
            { id: 100, weight: 10, name: "A" },
            { id: 100, weight: 20, name: "A duplicate" },
            { id: 200, weight: 5, name: "B" },
          ],
        },
        { level: 10, items: [{ id: 100, weight: 30, name: "A" }] },
        { level: 20, items: [{ id: 300, weight: 1, name: "C" }] },
        { level: 85, items: [{ id: 100, weight: 99, name: "A outside" }] },
      ]);

      // 统计 [1, 20] 区间内道具 100
      const match = countMatchingItems(levels, 1, 20, 100);
      expect(match.matchingLevels).toEqual([1, 10]);
      expect(match.matchingRows).toBe(3); // 等级 1 有 2 行 + 等级 10 有 1 行

      // 统计未出现的道具
      const noMatch = countMatchingItems(levels, 1, 20, 9999);
      expect(noMatch.matchingLevels).toEqual([]);
      expect(noMatch.matchingRows).toBe(0);
    });

    it("批量删除移除匹配等级内的全部该道具记录，保留空等级与其他道具", () => {
      const levels = toEditableLevels([
        {
          level: 1,
          items: [
            { id: 100, weight: 10, name: "A" },
            { id: 100, weight: 20, name: "A duplicate" },
            { id: 200, weight: 5, name: "B" },
          ],
        },
        { level: 10, items: [{ id: 100, weight: 30, name: "A" }] },
        { level: 20, items: [{ id: 300, weight: 1, name: "C" }] },
        { level: 85, items: [{ id: 100, weight: 99, name: "A outside" }] },
      ]);

      const res = batchDeleteItems(levels, 1, 20, 100);
      expect(res.removedCount).toBe(3);
      expect(res.affectedLevels).toEqual([1, 10]);

      // 等级 1: 道具 100 全部删除，保留道具 200
      expect(levels[0].items.length).toBe(1);
      expect(levels[0].items[0].id).toBe(200);

      // 等级 10: 道具 100 删除后变为空等级，但等级结构仍保留
      expect(levels[1].items.length).toBe(0);
      expect(levels[1].level).toBe(10);

      // 等级 20: 无变动
      expect(levels[2].items.length).toBe(1);
      expect(levels[2].items[0].id).toBe(300);

      // 等级 85: 在区间外，未受影响
      expect(levels[3].items.length).toBe(1);
      expect(levels[3].items[0].id).toBe(100);
    });

    it("0 项匹配时 batchDeleteItems 无影响并返回空结果", () => {
      const levels = toEditableLevels([
        { level: 1, items: [{ id: 50, weight: 10, name: "" }] },
      ]);
      const res = batchDeleteItems(levels, 1, 1, 999);
      expect(res.removedCount).toBe(0);
      expect(res.affectedLevels).toEqual([]);
      expect(levels[0].items.length).toBe(1);
    });
  });

  describe("道具视图筛选 (filterItems & countLevelMatches)", () => {
    const items = [
      { id: 1001, weight: 10, name: "泰拉石光剑", key: 1 },
      { id: 1002, weight: 20, name: "泰拉石巨剑", key: 2 },
      { id: 2001, weight: 30, name: "生命药水", key: 3 },
      { id: 3000, weight: 5, name: "无影剑", key: 4 },
    ];

    it("空查询返回完整列表", () => {
      expect(filterItems(items, "")).toEqual(items);
      expect(filterItems(items, "   ")).toEqual(items);
    });

    it("按道具名称（大小写不敏感包含）筛选", () => {
      const matched = filterItems(items, "泰拉");
      expect(matched.map((i) => i.id)).toEqual([1001, 1002]);

      const single = filterItems(items, "药水");
      expect(single.map((i) => i.id)).toEqual([2001]);
    });

    it("按道具 ID 完全或前缀筛选", () => {
      const byId = filterItems(items, "2001");
      expect(byId.map((i) => i.id)).toEqual([2001]);

      const byPrefix = filterItems(items, "100");
      expect(byPrefix.map((i) => i.id)).toEqual([1001, 1002]);
    });

    it("无匹配项返回空数组", () => {
      expect(filterItems(items, "不存在的武器")).toEqual([]);
    });

    it("countLevelMatches 准确统计等级内匹配数", () => {
      const lvl = {
        level: 1,
        key: 1,
        items,
      };
      expect(countLevelMatches(lvl, "泰拉")).toBe(2);
      expect(countLevelMatches(lvl, "药水")).toBe(1);
      expect(countLevelMatches(lvl, "9999")).toBe(0);
      expect(countLevelMatches(lvl, "")).toBe(4);
      expect(countLevelMatches(null, "泰拉")).toBe(0);
    });

    it("隐藏行在底层数据和 toWorldDropLevels 快照中完好保留", () => {
      const level = {
        level: 85,
        key: 1,
        items,
      };
      // 视图中只展示 "药水"
      const displayed = filterItems(level.items, "药水");
      expect(displayed.length).toBe(1);

      // 底层 level.items 依然是 4 项
      expect(level.items.length).toBe(4);

      // toWorldDropLevels 导出的全量快照包含全部 4 项
      const exported = toWorldDropLevels([level]);
      expect(exported[0].items.length).toBe(4);
      expect(exported[0].items.map((i) => i.id)).toEqual([1001, 1002, 2001, 3000]);
    });

    it("同名异 ID 精准过滤：选择指定物品仅匹配该 item.id，同名不同 ID 绝不出现", () => {
      const collisionItems = [
        { id: 1001, weight: 10, name: "神秘宝箱", key: 1 },
        { id: 2002, weight: 20, name: "神秘宝箱", key: 2 },
        { id: 3003, weight: 30, name: "金币", key: 3 },
      ];

      // 自由文本按名称检索时，两者均会命中
      const textMatches = filterItems(collisionItems, "神秘宝箱");
      expect(textMatches.length).toBe(2);

      // 精准选取 ID 为 1001 的物品：仅出现 1001，同名的 2002 绝不出现
      const exact1001 = filterItems(collisionItems, { exactItemId: 1001 });
      expect(exact1001.length).toBe(1);
      expect(exact1001[0].id).toBe(1001);

      // 精准选取 ID 为 2002 的物品：仅出现 2002，同名的 1001 绝不出现
      const exact2002 = filterItems(collisionItems, { exactItemId: 2002 });
      expect(exact2002.length).toBe(1);
      expect(exact2002[0].id).toBe(2002);
    });

    it("数字子串冲突精准过滤：选取 ID 10 仅匹配 10，不匹配 100、210、9105", () => {
      const numberCollisionItems = [
        { id: 10, weight: 10, name: "短剑", key: 1 },
        { id: 100, weight: 20, name: "长剑", key: 2 },
        { id: 210, weight: 30, name: "重剑", key: 3 },
        { id: 9105, weight: 40, name: "巨剑", key: 4 },
        { id: 777, weight: 50, name: "钝器", key: 5 },
      ];

      // 自由文本检索 "10" 时，包含子串 10 的所有 4 项均会命中
      const subStringMatches = filterItems(numberCollisionItems, "10");
      expect(subStringMatches.map((i) => i.id)).toEqual([10, 100, 210, 9105]);

      // 精准选取 ID: 10 时，严格只匹配 10
      const exactTen = filterItems(numberCollisionItems, { exactItemId: 10 });
      expect(exactTen.length).toBe(1);
      expect(exactTen[0].id).toBe(10);
    });

    it("精准筛选清除与重置行为：重置或清除后恢复全量列表", () => {
      const testItems = [
        { id: 10, weight: 10, name: "A", key: 1 },
        { id: 20, weight: 20, name: "B", key: 2 },
      ];

      // 激活精准筛选
      expect(isFilterCriteriaActive({ exactItemId: 10 })).toBe(true);
      expect(filterItems(testItems, { exactItemId: 10 }).length).toBe(1);

      // 清除筛选：exactItemId 为 null 且 query 为空
      expect(isFilterCriteriaActive({ exactItemId: null, query: "" })).toBe(false);
      expect(isFilterCriteriaActive("")).toBe(false);
      expect(filterItems(testItems, { exactItemId: null, query: "" })).toEqual(testItems);
      expect(filterItems(testItems, "")).toEqual(testItems);
    });

    it("findFirstLevelWithMatch 准确找到首个包含匹配掉落的等级", () => {
      const levels = toEditableLevels([
        { level: 1, items: [{ id: 100, weight: 10, name: "A" }] },
        { level: 10, items: [] }, // 空
        { level: 20, items: [{ id: 500, weight: 20, name: "目标物品" }] },
        { level: 85, items: [{ id: 500, weight: 30, name: "目标物品" }] },
      ]);

      // 查找 ID 500：首个匹配等级应为 20
      const firstLevel = findFirstLevelWithMatch(levels, { exactItemId: 500 });
      expect(firstLevel).toBe(20);

      // 查找不存在的物品 ID 9999：返回 null
      const nonExistent = findFirstLevelWithMatch(levels, { exactItemId: 9999 });
      expect(nonExistent).toBeNull();
    });
  });
});
