import type { WorldDropItem, WorldDropLevel } from "../../bindings/pvfine/services/models";

export const MAX_INT32 = 2147483647;
export const MIN_INT32 = -2147483648;

export interface EditableWorldDropItem {
  id: number;
  weight: number;
  name: string;
  key: number;
}

export interface EditableWorldDropLevel {
  level: number;
  items: EditableWorldDropItem[];
  key: number;
}

let keySequence = 0;
export function nextFormKey(): number {
  return ++keySequence;
}

export function isPositiveInt32(value: unknown): boolean {
  return typeof value === "number" && Number.isInteger(value) && value > 0 && value <= MAX_INT32;
}

export function isNonNegativeInt32(value: unknown): boolean {
  return typeof value === "number" && Number.isInteger(value) && value >= 0 && value <= MAX_INT32;
}

export function isStoredItemId(value: unknown): boolean {
  return typeof value === "number" && Number.isInteger(value) && value >= MIN_INT32 && value <= MAX_INT32 && value !== -1;
}

export function validateLevelNumber(
  level: number,
  existingLevels: number[],
  currentLevel?: number,
): string {
  if (!isPositiveInt32(level)) {
    return "等级必须是正整数 (1 ~ 2147483647)";
  }
  const duplicates = existingLevels.filter((l) => l !== currentLevel);
  if (duplicates.includes(level)) {
    return `等级 ${level} 已存在，不可重复`;
  }
  return "";
}

export function validateItem(id: number, weight: number, allowExistingId = false): string {
  if (!(allowExistingId ? isStoredItemId(id) : isPositiveInt32(id))) {
    return "道具 ID 必须是正整数 (1 ~ 2147483647)";
  }
  if (!isNonNegativeInt32(weight)) {
    return "道具权重必须是非负整数 (0 ~ 2147483647)";
  }
  return "";
}

export function validateAllLevels(levels: WorldDropLevel[]): string {
  const seen = new Set<number>();
  for (const level of levels) {
    if (!isPositiveInt32(level.level)) {
      return "等级必须是正整数 (1 ~ 2147483647)";
    }
    if (seen.has(level.level)) {
      return `等级 ${level.level} 重复`;
    }
    seen.add(level.level);
    for (const item of level.items ?? []) {
      const itemErr = validateItem(item.id, item.weight, true);
      if (itemErr) {
        return `等级 ${level.level} 中的道具错误：${itemErr}`;
      }
    }
  }
  return "";
}

export function calcLevelTotalWeight(items: { weight: number }[] | null | undefined): number {
  if (!items || items.length === 0) return 0;
  return items.reduce((sum, item) => sum + (Number.isFinite(item.weight) && item.weight > 0 ? item.weight : 0), 0);
}

/**
 * 计算权重占比。总权重为 0 时返回中立的 "0.00%" 而不是 NaN。
 */
export function calcItemPercentage(weight: number, totalWeight: number): string {
  if (!Number.isFinite(weight) || weight <= 0 || !Number.isFinite(totalWeight) || totalWeight <= 0) {
    return "0.00%";
  }
  const ratio = (weight / totalWeight) * 100;
  return `${ratio.toFixed(2)}%`;
}

export function toEditableLevels(levels: WorldDropLevel[] | null): EditableWorldDropLevel[] {
  if (!levels) return [];
  return levels.map((lvl) => ({
    level: lvl.level,
    key: nextFormKey(),
    items: (lvl.items ?? []).map((item) => ({
      id: item.id,
      weight: item.weight,
      name: item.name || `未知物品 #${item.id}`,
      key: nextFormKey(),
    })),
  }));
}

export function toWorldDropLevels(editable: EditableWorldDropLevel[]): WorldDropLevel[] {
  return editable.map((lvl) => ({
    level: lvl.level,
    items: lvl.items.map((item) => ({
      id: item.id,
      weight: item.weight,
      name: item.name || `未知物品 #${item.id}`,
    })),
  }));
}

export function areLevelsEqual(a: WorldDropLevel[] | null, b: WorldDropLevel[] | null): boolean {
  const listA = a ?? [];
  const listB = b ?? [];
  if (listA.length !== listB.length) return false;
  for (let i = 0; i < listA.length; i++) {
    const la = listA[i];
    const lb = listB[i];
    if (la.level !== lb.level) return false;
    const itemsA = la.items ?? [];
    const itemsB = lb.items ?? [];
    if (itemsA.length !== itemsB.length) return false;
    for (let j = 0; j < itemsA.length; j++) {
      if (itemsA[j].id !== itemsB[j].id || itemsA[j].weight !== itemsB[j].weight) {
        return false;
      }
    }
  }
  return true;
}

export function moveItem<T>(list: T[], fromIndex: number, toIndex: number): boolean {
  if (fromIndex < 0 || fromIndex >= list.length || toIndex < 0 || toIndex >= list.length) {
    return false;
  }
  if (fromIndex === toIndex) return false;
  const [item] = list.splice(fromIndex, 1);
  list.splice(toIndex, 0, item);
  return true;
}

/**
 * 校验等级区间合法性：起始与结束等级必须存在于已有等级列表中，且 startLevel <= endLevel。
 */
export function validateLevelInterval(
  startLevel: number | null,
  endLevel: number | null,
  existingLevels: number[],
): string {
  if (startLevel === null || endLevel === null) {
    return "请选择起始与结束等级";
  }
  if (!existingLevels.includes(startLevel)) {
    return `起始等级 Lv. ${startLevel} 不存在`;
  }
  if (!existingLevels.includes(endLevel)) {
    return `结束等级 Lv. ${endLevel} 不存在`;
  }
  if (startLevel > endLevel) {
    return "起始等级不能大于结束等级";
  }
  return "";
}

/**
 * 获取闭区间 [startLevel, endLevel] 内已配置的已有等级记录。
 * 不创建不存在的中间等级，保留原有顺序。
 */
export function getLevelsInInterval(
  levels: EditableWorldDropLevel[],
  startLevel: number | null,
  endLevel: number | null,
): EditableWorldDropLevel[] {
  if (startLevel === null || endLevel === null || startLevel > endLevel) {
    return [];
  }
  return levels.filter((lvl) => lvl.level >= startLevel && lvl.level <= endLevel);
}

/**
 * 批量添加道具至指定等级区间内已存在的各个等级。
 * 若等级内已存在相同道具 ID，则直接追加新行（不跳过也不覆盖旧行）。
 */
export function batchAddItems(
  levels: EditableWorldDropLevel[],
  startLevel: number,
  endLevel: number,
  item: { id: number; weight: number; name: string },
): { affectedLevels: number[]; addedCount: number } {
  const targets = getLevelsInInterval(levels, startLevel, endLevel);
  for (const lvl of targets) {
    lvl.items.push({
      id: item.id,
      weight: item.weight,
      name: item.name || `未知物品 #${item.id}`,
      key: nextFormKey(),
    });
  }
  return {
    affectedLevels: targets.map((lvl) => lvl.level),
    addedCount: targets.length,
  };
}

/**
 * 统计指定等级区间内匹配指定道具 ID 的等级与行数。
 */
export function countMatchingItems(
  levels: EditableWorldDropLevel[],
  startLevel: number | null,
  endLevel: number | null,
  itemId: number,
): { matchingLevels: number[]; matchingRows: number } {
  const targets = getLevelsInInterval(levels, startLevel, endLevel);
  let matchingRows = 0;
  const matchingLevels: number[] = [];
  for (const lvl of targets) {
    const matches = lvl.items.filter((item) => item.id === itemId);
    if (matches.length > 0) {
      matchingRows += matches.length;
      matchingLevels.push(lvl.level);
    }
  }
  return {
    matchingLevels,
    matchingRows,
  };
}

/**
 * 批量删除指定等级区间内的指定道具（删除每个匹配等级中的全部匹配项，保留空等级）。
 */
export function batchDeleteItems(
  levels: EditableWorldDropLevel[],
  startLevel: number,
  endLevel: number,
  itemId: number,
): { affectedLevels: number[]; removedCount: number } {
  const targets = getLevelsInInterval(levels, startLevel, endLevel);
  let removedCount = 0;
  const affectedLevels: number[] = [];
  for (const lvl of targets) {
    const originalCount = lvl.items.length;
    lvl.items = lvl.items.filter((item) => item.id !== itemId);
    const deleted = originalCount - lvl.items.length;
    if (deleted > 0) {
      removedCount += deleted;
      affectedLevels.push(lvl.level);
    }
  }
  return {
    affectedLevels,
    removedCount,
  };
}

export interface WorldDropFilterCriteria {
  exactItemId?: number | null;
  query?: string;
}

export function isFilterCriteriaActive(criteria: string | WorldDropFilterCriteria | null | undefined): boolean {
  if (!criteria) return false;
  if (typeof criteria === "object") {
    if (criteria.exactItemId !== undefined && criteria.exactItemId !== null) return true;
    return !!criteria.query && criteria.query.trim().length > 0;
  }
  return typeof criteria === "string" && criteria.trim().length > 0;
}

/**
 * 道具筛选（纯视图逻辑，不影响底层保存快照）：
 * 1. 若指定 exactItemId（来自从物品库精准选取），则严格全等匹配 item.id，同名异 ID 或数字子串冲突均不出现；
 * 2. 若仅提供 query（自由文本），支持根据道具 ID（完全或前缀匹配）或名称（不区分大小写包含匹配）筛选。
 */
export function filterItems(
  items: EditableWorldDropItem[],
  criteria: string | WorldDropFilterCriteria,
): EditableWorldDropItem[] {
  if (typeof criteria === "object" && criteria !== null) {
    if (criteria.exactItemId !== undefined && criteria.exactItemId !== null) {
      const targetId = criteria.exactItemId;
      return items.filter((item) => item.id === targetId);
    }
    const q = (criteria.query ?? "").trim().toLowerCase();
    if (!q) return items;
    return items.filter((item) => {
      if (String(item.id) === q || String(item.id).includes(q)) return true;
      if (item.name && item.name.toLowerCase().includes(q)) return true;
      return false;
    });
  }

  const q = (criteria ?? "").trim().toLowerCase();
  if (!q) return items;
  return items.filter((item) => {
    if (String(item.id) === q || String(item.id).includes(q)) return true;
    if (item.name && item.name.toLowerCase().includes(q)) return true;
    return false;
  });
}

/**
 * 统计某等级下匹配筛选条件的道具数量。
 */
export function countLevelMatches(
  level: EditableWorldDropLevel | null | undefined,
  criteria: string | WorldDropFilterCriteria,
): number {
  if (!level) return 0;
  if (typeof criteria === "object" && criteria !== null) {
    if (criteria.exactItemId !== undefined && criteria.exactItemId !== null) {
      return filterItems(level.items, criteria).length;
    }
    if (!criteria.query || !criteria.query.trim()) {
      return level.items.length;
    }
    return filterItems(level.items, criteria).length;
  }
  const q = (criteria ?? "").trim();
  if (!q) return level.items.length;
  return filterItems(level.items, q).length;
}

/**
 * 在所有等级中找到第一个有匹配项的等级数值；若均无匹配则返回 null。
 */
export function findFirstLevelWithMatch(
  levels: EditableWorldDropLevel[],
  criteria: string | WorldDropFilterCriteria,
): number | null {
  for (const lvl of levels) {
    if (countLevelMatches(lvl, criteria) > 0) {
      return lvl.level;
    }
  }
  return null;
}
