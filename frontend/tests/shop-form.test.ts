import { expect, test } from "vitest";
import { costForm, materialRow, validateCosts, materialInputs } from "../src/gui/shopForm";
import type { ShopItem } from "../bindings/pvfine/services/models";

test("载入新商品成本保留金币和所有材料，空值与零价格不同", () => {
  const item = { costs: [{ kind: "gold", quantity: "0" }, { kind: "material", itemId: "2", quantity: "10", name: "材料" }] } as ShopItem;
  const form = costForm(item);
  expect(form.gold).toBe("0");
  expect(materialInputs(form)).toEqual([{ itemId: "2", quantity: "10" }]);
  expect(costForm().gold).toBe("");
  expect(costForm().materials).toEqual([]);
  expect(form.materials[0].key).not.toBe(costForm(item).materials[0].key);
});
test("批量只验证选中成本；拦截非法数量但允许明确清除成本", () => {
  const form = { gold: "-1", materials: [materialRow("", "abc")] };
  expect(validateCosts(form, false, false)).toBe("");
  expect(validateCosts(form, true, false)).toContain("金币");
  expect(validateCosts(form, false, true)).toContain("兑换道具");
  expect(validateCosts({ gold: "", materials: [] }, true, true)).toBe("");
  expect(validateCosts({ gold: "2147483648", materials: [] }, true, true)).not.toBe("");
});
