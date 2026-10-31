import { editSkillNumbers } from "./skill";
import type { SkillTreeNode, SkillTreeBranch } from "../../bindings/pvfine/services/models";

export const skillBranchNames: Record<string, string> = {
  none: "未转职", weaponmaster: "剑魂", soulbringer: "鬼泣", berserker: "狂战士", asura: "阿修罗", swordghost: "剑影",
  ranger: "漫游枪手", launcher: "枪炮师", mechanic: "机械师", spitfire: "弹药专家", assault: "合金战士",
  nenmaster: "气功师", striker: "散打", streetfighter: "街霸", grappler: "柔道家",
  elementalmaster: "元素师", summoner: "召唤师", battlemage: "战斗法师", witch: "魔道学者", enchantress: "小魔女",
  crusader: "圣骑士", infighter: "蓝拳圣使", exorcist: "驱魔师", avenger: "复仇者", inquisitor: "异端审判者", shaman: "巫女", mistress: "诱魔者",
  rogue: "刺客", necromancer: "死灵术士", kunoichi: "忍者", shadowdancer: "影舞者",
  muse: "缪斯", traveler: "旅人", hunter: "猎人", vigilante: "妖护使",
  elementalbomber: "元素爆破师", glacialmaster: "冰结师", bloodmage: "血法师", swiftmaster: "逐风者", dimensionwalker: "次元行者",
  miko: "巫女", temptress: "诱魔者", demonslayer: "契魔者", vegabond: "流浪武士", blade: "刃影",
  duelist: "决战者", vanguard: "征战者", dragonianlancer: "狩猎者", darklancer: "暗枪士",
  hitman: "暗刃", agent: "特工", troubleshooter: "战线佣兵", specialist: "源能专家",
  elvenknight: "精灵骑士", chaos: "混沌魔灵", paladin: "帕拉丁", dragonknight: "龙骑士",
  swordmaster: "驭剑士", darktempler: "暗殿骑士", demonicslayer: "契魔者", vagabond: "流浪武士", spectre: "刃影",
};

// Use the edge facing the destination, keeping arrows outside the 32px icon.
export function skillTreeEdgeLayout(source: SkillTreeNode, target: SkillTreeNode): {path: string; end: {x: number; y: number}} {
  const sx = source.x + 16, sy = source.y + 16;
  const tx = target.x + 16, ty = target.y + 16;
  if (source.x === target.x && source.y === target.y) {
    return {path: `M ${source.x + 34} ${sy} H ${source.x + 44} V ${source.y - 10} H ${tx} V ${target.y - 3}`, end: {x: tx, y: target.y - 3}};
  }
  if (Math.abs(tx - sx) > Math.abs(ty - sy)) {
    const direction = Math.sign(tx - sx);
    const start = sx + direction * 18, end = tx - direction * 20;
    const mid = (start + end) / 2;
    return {path: `M ${start} ${sy} H ${mid} V ${ty} H ${end}`, end: {x: end, y: ty}};
  }
  const direction = Math.sign(ty - sy) || 1;
  const start = sy + direction * 24, end = ty - direction * 19;
  const mid = (start + end) / 2;
  return {path: `M ${sx} ${start} V ${mid} H ${tx} V ${end}`, end: {x: tx, y: end}};
}

export function skillTreeEdge(source: SkillTreeNode, target: SkillTreeNode): string {
  return skillTreeEdgeLayout(source, target).path;
}

export interface SkillTreeCell {
  key: string;
  nodes: SkillTreeNode[];
}

/** Keep source order and coordinates; overlapping skills remain individually selectable. */
export function groupSkillTreeCells(nodes: SkillTreeNode[]): SkillTreeCell[] {
  const cells = new Map<string, SkillTreeCell>();
  for (const node of nodes) {
    const key = `${node.x}:${node.y}`;
    let cell = cells.get(key);
    if (!cell) { cell = {key, nodes: []}; cells.set(key, cell); }
    cell.nodes.push(node);
  }
  return [...cells.values()];
}

/** Row labels use the leftmost skill, with source order breaking position ties. */
export function skillTreeRowLevels(nodes: SkillTreeNode[], kind: string): Map<number, number | null> {
  const first = new Map<number, SkillTreeNode>();
  if (kind !== "sp") return new Map();
  for (const node of nodes) {
    const row = Math.floor(node.y / 67);
    const previous = first.get(row);
    if (!previous || node.x < previous.x) first.set(row, node);
  }
  return new Map([...first].map(([row, node]) => [row, node.requiredLevel]));
}

/** Prefer the last source entry unless this cell has a remembered selection. */
export function skillTreeCellNode(cell: SkillTreeCell, rememberedID?: number): SkillTreeNode {
  return cell.nodes.find(node => node.id === rememberedID) ?? cell.nodes[cell.nodes.length - 1]!;
}

export function skillTreeCellSelectionKey(path: string, job: string | null, kind: string, branch: string | null, cellKey: string): string {
  return JSON.stringify([path.toLowerCase(), job, kind, branch, cellKey]);
}

export interface SkillTreeDropTarget {
  x: number;
  y: number;
  valid: boolean;
  reason: string;
}

/** Snap the whole source cell; half-cell icons also participate in collisions. */
export function skillTreeDropTarget(nodes: SkillTreeNode[], cell: SkillTreeCell, x: number, y: number, width: number, height: number): SkillTreeDropTarget {
  const target = {x: Math.round(x / 47) * 47, y: Math.round(y / 67) * 67, valid: false, reason: ""};
  if (![x, y, width, height].every(Number.isFinite) || target.x < 0 || target.y < 0 || target.x + 34 > width || target.y + 56 > height) {
    return {...target, reason: "超出当前画布"};
  }
  const source = new Set(cell.nodes.map(node => node.sourceStart));
  if (nodes.some(node => !source.has(node.sourceStart) && node.x < target.x + 34 && node.x + 34 > target.x && node.y < target.y + 56 && node.y + 56 > target.y)) {
    return {...target, reason: "目标位置已被其他技能占用"};
  }
  return {...target, valid: true};
}

export function moveSkillTreeCell(text: string, nodes: SkillTreeNode[], cell: SkillTreeCell, x: number, y: number, width: number, height: number): string {
  const target = skillTreeDropTarget(nodes, cell, x, y, width, height);
  if (!target.valid || target.x !== x || target.y !== y) throw new Error(target.reason || "目标必须位于47×67网格上");
  const patches = cell.nodes.flatMap(node => {
    if (node.positionTokens?.length !== 2 || !["cell pos", "icon pos"].includes(node.positionKind)) throw new Error("坐标记录不完整，请先在文本中修正");
    const values = node.positionKind === "cell pos" ? [x / 47, y / 67] : [x, y];
    return node.positionTokens.map((token, i) => ({token, value: values[i]!}));
  }).sort((a, b) => b.token.start - a.token.start);
  // Patch right to left so the original UTF-16 offsets stay valid. The shared
  // number editor checks every old value and preserves CRLF and other fields.
  let result = text;
  for (const patch of patches) result = editSkillNumbers(result, [patch.token], "set", patch.value);
  return result;
}

/** Reserve a blank grid row after all occupied rows, including pixel offsets. */
export function skillTreeCanvasHeight(nodes: SkillTreeNode[]): number {
  const occupiedRows = Math.ceil(Math.max(0, ...nodes.map(node => node.y + 67)) / 67);
  return Math.max(4, occupiedRows + 1) * 67;
}

/** Insert only one skill record before the lexer's matching branch close tag. */
export function addSkillTreeNode(text: string, branch: SkillTreeBranch, id: number, x: number, y: number, width: number, height: number): string {
  if (!Number.isInteger(id) || id < 0 || id > 2147483647) throw new Error("技能ID无效");
  if (branch.nodes?.some(node => node.id === id)) throw new Error("当前分支已包含此技能");
  const target = skillTreeDropTarget(branch.nodes ?? [], {key: "", nodes: []}, x, y, width, height);
  if (!target.valid || x !== target.x || y !== target.y) throw new Error(target.reason || "目标必须位于47×67网格上");
  if (!Number.isInteger(branch.insertOffset) || branch.insertOffset < 0) throw new Error("分支缺少结束标签，请先修正文本");
  let offset = 0;
  for (let unit = 0; unit < branch.insertOffset && offset < text.length; unit++, offset++) {
    if (text[offset] === "\r" && text[offset + 1] === "\n") offset++;
  }
  if (!/^\[\s*\/\s*character job\s*\]/i.test(text.slice(offset))) throw new Error("分支位置已变化，请重新读取");
  const newline = text.includes("\r\n") ? "\r\n" : text.includes("\r") ? "\r" : "\n";
  const lineStart = Math.max(text.lastIndexOf("\n", offset - 1), text.lastIndexOf("\r", offset - 1)) + 1;
  const prefix = text.slice(lineStart, offset);
  const ownLine = /^[ \t]*$/.test(prefix);
  const indent = ownLine ? prefix : "";
  const insertion = ownLine ? lineStart : offset;
  const record = ["[skill info]", "\t[index]", `\t\t${id}`, "\t[cell pos]", `\t\t${x / 47}\t${y / 67}`, "[/skill info]"].map(line => `${indent}\t${line}`).join(newline) + newline;
  return text.slice(0, insertion) + (ownLine ? "" : newline) + record + text.slice(insertion);
}

/** Remove one source record and incoming links in this branch, preserving other text. */
export function removeSkillTreeNode(text: string, branch: SkillTreeBranch, node: SkillTreeNode): string {
  if (!branch.nodes?.includes(node)) throw new Error("技能已变化，请重新选择");
  const rawOffset = (offset: number) => skillTreeRawOffset(text, offset);
  let start = rawOffset(node.sourceStart), end = rawOffset(node.sourceClose);
  const closing = /^\[\s*\/\s*skill info\s*\]/i.exec(text.slice(end));
  if (end <= start || !/^\[\s*skill info\s*\]/i.test(text.slice(start)) || !closing) throw new Error("技能位置已变化，请重新读取");
  end += closing[0].length;
  const lineStart = Math.max(text.lastIndexOf("\n", start - 1), text.lastIndexOf("\r", start - 1)) + 1;
  const lineSuffix = /^[ \t]*(?:\r\n|\r|\n|$)/.exec(text.slice(end));
  if (/^[ \t]*$/.test(text.slice(lineStart, start)) && lineSuffix) {
    start = lineStart;
    end += lineSuffix[0].length;
  }
  const patches = [{start, end}];
  for (const other of branch.nodes) {
    if (other === node) continue;
    if ((other.next ?? []).length !== (other.nextTokens ?? []).length) throw new Error("关联记录不完整，请重新读取");
    for (const token of other.nextTokens ?? []) {
      if (token.value !== node.id) continue;
      const from = rawOffset(token.start), to = rawOffset(token.end);
      if (Number(text.slice(from, to)) !== node.id || to <= from) throw new Error("关联记录已变化，请重新读取");
      patches.push({start: from, end: to});
    }
  }
  patches.sort((a, b) => b.start - a.start);
  let result = text, boundary = text.length;
  for (const patch of patches) {
    if (patch.end > boundary) throw new Error("技能记录重叠，请先修正文本");
    result = result.slice(0, patch.start) + result.slice(patch.end);
    boundary = patch.start;
  }
  return result;
}

function skillTreeRawOffset(text: string, offset: number): number {
  if (!Number.isInteger(offset) || offset < 0) throw new Error("技能记录不完整，请先修正文本");
  let raw = 0, unit = 0;
  for (; unit < offset && raw < text.length; unit++, raw++) {
    if (text[raw] === "\r" && text[raw + 1] === "\n") raw++;
  }
  if (unit !== offset) throw new Error("技能位置已变化，请重新读取");
  return raw;
}

/** Edit a single ordered successor. Index -1 appends; a null target removes it. */
export function editSkillTreeLink(text: string, branch: SkillTreeBranch, node: SkillTreeNode, index: number, target: number | null): string {
  if (!branch.nodes?.includes(node)) throw new Error("技能已变化，请重新选择");
  const next = node.next ?? [], tokens = node.nextTokens ?? [];
  if (next.length !== tokens.length) throw new Error("关联记录不完整，请重新读取");
  if (!Number.isInteger(index) || index < -1 || index >= next.length || (index === -1 && target === null)) throw new Error("关联位置已变化，请重新读取");
  const start = skillTreeRawOffset(text, node.sourceStart), close = skillTreeRawOffset(text, node.sourceClose);
  if (close <= start || !/^\[\s*skill info\s*\]/i.test(text.slice(start)) || !/^\[\s*\/\s*skill info\s*\]/i.test(text.slice(close))) throw new Error("技能记录不完整，请先修正文本");
  const ranges = tokens.map((token, i) => {
    const from = skillTreeRawOffset(text, token.start), to = skillTreeRawOffset(text, token.end);
    if (from <= start || to > close || to <= from || token.value !== next[i] || Number(text.slice(from, to)) !== next[i]) throw new Error("关联记录已变化，请重新读取");
    return {from, to};
  });
  if (target !== null) {
    if (!Number.isInteger(target) || target < 0 || target > 2147483647 || !branch.nodes.some(n => n.id === target)) throw new Error("请选择当前分支的目标技能");
    if (target === node.id) throw new Error("不能关联技能自身");
    if (index >= 0 && next[index] === target) return text;
    if (next.some((id, i) => id === target && i !== index)) throw new Error("已存在该目标的关联");
  }
  if (index >= 0) {
    const range = ranges[index]!;
    return text.slice(0, range.from) + (target === null ? "" : String(target)) + text.slice(range.to);
  }
  const newline = text.includes("\r\n") ? "\r\n" : text.includes("\r") ? "\r" : "\n";
  const lineStart = Math.max(text.lastIndexOf("\n", close - 1), text.lastIndexOf("\r", close - 1)) + 1;
  const prefix = text.slice(lineStart, close);
  const ownLine = /^[ \t]*$/.test(prefix), indent = ownLine ? prefix : "";
  const insertion = ownLine ? lineStart : close;
  // A repeated section appends without rewriting earlier sections or their comments.
  const record = `${indent}\t[next skill]${newline}${indent}\t\t${target}${newline}`;
  return text.slice(0, insertion) + (ownLine ? "" : newline) + record + text.slice(insertion);
}
