import { expect, test } from "vitest";
import { RARITY_COLORS, RARITY_NAMES, RARITY_UNKNOWN, rarityColor, rarityName } from "../src/rarity";

test("已配置的稀有度返回名称与颜色", () => {
  expect(rarityName(0)).toBe("normal");
  expect(rarityName(4)).toBe("epic");
  expect(rarityName(RARITY_NAMES.length - 1)).toBe("legendary");
  expect(rarityColor(4)).toBe(RARITY_COLORS.epic);
  expect(rarityColor(0)).toBe(RARITY_COLORS.normal);
});

test("缺少 [rarity] 或越界时回退为未知，调用方自行决定默认色", () => {
  for (const unknown of [RARITY_UNKNOWN, -2, 7, 4.5, Number.NaN, undefined, null]) {
    expect(rarityName(unknown as number)).toBeNull();
    expect(rarityColor(unknown as number)).toBeUndefined();
  }
});

test("每个稀有度都有独立配色", () => {
  const colors = RARITY_NAMES.map((name) => RARITY_COLORS[name]);
  expect(new Set(colors).size).toBe(RARITY_NAMES.length);
});
