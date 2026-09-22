/**
 * 稀有度配色：与 config/annotations.json 中 `[rarity]` 枚举（0..6）的顺序一致。
 * 装备预览、资源管理器名称标签与商店商品名共用这里的颜色，避免各处再写一份。
 */
export const RARITY_NAMES = [
  "normal",
  "magic",
  "rare",
  "artifact",
  "epic",
  "brave",
  "legendary",
] as const;

export type RarityName = (typeof RARITY_NAMES)[number];

/** 文件没有 [rarity] 段时后端返回的哨兵值（pvf.RarityUnknown）。 */
export const RARITY_UNKNOWN = -1;

export const RARITY_COLORS: Record<RarityName, string> = {
  normal: "#ffffff",
  magic: "#5e9dff",
  rare: "#b46cff",
  artifact: "#dc57b7",
  epic: "#ffd438",
  brave: "#ff4a4a",
  legendary: "#ff7903",
};

/** 已知稀有度返回名称，未知（缺省或越界）返回 null。 */
export function rarityName(rarity: number | null | undefined): RarityName | null {
  if (typeof rarity !== "number" || !Number.isInteger(rarity)) return null;
  return RARITY_NAMES[rarity] ?? null;
}

/** 已知稀有度返回颜色，未知返回 undefined，由调用方决定回退配色。 */
export function rarityColor(rarity: number | null | undefined): string | undefined {
  const name = rarityName(rarity);
  return name ? RARITY_COLORS[name] : undefined;
}
