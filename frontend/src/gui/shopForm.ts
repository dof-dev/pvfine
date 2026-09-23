import type { ShopItem, ShopMaterialInput } from "../../bindings/pvfine/services/models";
export interface MaterialRow { key: number; itemId: string; quantity: string; name: string }
export interface ShopCostForm { gold: string; materials: MaterialRow[] }
let sequence = 0;
export function materialRow(itemId = "", quantity = "1", name = ""): MaterialRow { return { key: ++sequence, itemId, quantity, name }; }
export function costForm(item?: ShopItem | null): ShopCostForm {
  return {
    gold: item?.costs?.find((cost) => cost.kind === "gold")?.quantity ?? "",
    materials: (item?.costs ?? []).filter((cost) => cost.kind === "material").map((cost) => materialRow(cost.itemId, cost.quantity, cost.name)),
  };
}
export function integerError(value: string): boolean { return !/^\d+$/.test(value) || Number(value) > 2147483647; }
export function validateCosts(form: ShopCostForm, gold: boolean, materials: boolean): string {
  if (gold && form.gold !== "" && integerError(form.gold)) return "金币价格必须为 0–2147483647 的整数，或留空取消金币价格";
  if (materials && form.materials.some((row) => integerError(row.itemId) || integerError(row.quantity))) return "每条兑换道具必须选择物品，并填写 0–2147483647 的整数数量";
  return "";
}
export function materialInputs(form: ShopCostForm): ShopMaterialInput[] { return form.materials.map(({ itemId, quantity }) => ({ itemId, quantity })); }
