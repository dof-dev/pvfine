import type { SkillMode, SkillNumber, SkillProperty } from "../../bindings/pvfine/services/models";

export interface SkillPreviewPart {
  text: string;
  binding?: { dynamic: boolean; index: number; multiplier: number };
  token?: SkillNumber;
}

export function skillPreview(property: SkillProperty, mode: SkillMode, level: number): SkillPreviewPart[] {
  // ReadSkill resolves and normalizes template aliases / escaped line breaks.
  // Rendering therefore depends on template semantics, never PVF variants.
  const parts: SkillPreviewPart[] = [];
  const template = property.template.replaceAll("%%", "%");
  const placeholders = /<int>|<float(\d*)>/g;
  let end = 0;
  let index = 0;
  for (const match of template.matchAll(placeholders)) {
    const start = match.index!;
    if (start > end) parts.push({ text: template.slice(end, start) });
    const binding = property.bindings?.[index++];
    const token = binding?.dynamic ? mode.levels?.[level - 1]?.[binding.index] : mode.static?.[binding?.index ?? -1];
    const value = token && binding ? token.value * binding.multiplier : NaN;
    const precision = match[0] === "<int>" ? -1 : Number(match[1] || 0);
    const text = Number.isFinite(value)
      ? precision === -1 ? String(Math.trunc(value)) : value.toFixed(Math.min(precision, 20))
      : "（无对应数据）";
    parts.push({ text, binding, token });
    end = start + match[0].length;
  }
  if (end < template.length) parts.push({ text: template.slice(end) });
  return parts;
}

export type SkillOperation = "set" | "add" | "multiply";

/** Atomic source patches: validate every target and number before changing text. */
export function editSkillNumbers(text: string, tokens: SkillNumber[], operation: SkillOperation, operand: number): string {
  if (!Number.isFinite(operand) || !tokens.length) throw new Error("请输入有效数值");
  // ParseScriptView counts CRLF as one UTF-16 unit. Translate positions back
  // to the draft so its original line endings remain untouched.
  const offsets: number[] = [0];
  for (let i = 0; i < text.length; i++) {
    if (text[i] === "\r" && text[i + 1] === "\n") i++;
    offsets.push(i + 1);
  }
  const patches = tokens.map((token) => {
    const value = operation === "set" ? operand : operation === "add" ? token.value + operand : token.value * operand;
    if (!Number.isFinite(value)) throw new Error("运算结果超出数值范围");
    if (token.start < 0 || token.end <= token.start || token.end >= offsets.length) throw new Error("数据位置已失效，请重新加载");
    const start = offsets[token.start]!;
    const end = offsets[token.end]!;
    const original = Number(text.slice(start, end));
    if (token.tokenType !== 0 && token.tokenType !== 2) throw new Error("不支持的数值类型");
    if (token.tokenType === 0 ? original !== token.value : Math.fround(original) !== Math.fround(token.value)) throw new Error("技能草稿已变化，请重新打开表单");
    let formatted: string;
    if (token.tokenType === 0) {
      const integer = Math.round(value);
      if (integer < -2147483648 || integer > 2147483647) throw new Error("整数超出 32 位范围");
      formatted = String(integer);
    } else {
      const float = Math.fround(value);
      if (!Number.isFinite(float)) throw new Error("浮点数超出 32 位范围");
      formatted = String(float);
      if (!/[.eE]/.test(formatted)) formatted += ".0";
    }
    return { start, end, text: formatted };
  }).sort((a, b) => b.start - a.start);
  let result = text;
  let previous = text.length;
  for (const patch of patches) {
    if (patch.end > previous) throw new Error("数据位置重叠");
    result = result.slice(0, patch.start) + patch.text + result.slice(patch.end);
    previous = patch.start;
  }
  return result;
}

export const skillJobNames: Record<string, string> = {
  swordman: "鬼剑士（男）", atswordman: "鬼剑士（女）", fighter: "格斗家（女）", atfighter: "格斗家（男）",
  gunner: "神枪手（男）", atgunner: "神枪手（女）", mage: "魔法师（女）", atmage: "魔法师（男）",
  priest: "圣职者（男）", atpriest: "圣职者（女）", thief: "暗夜使者", knight: "守护者", demoniclancer: "魔枪士",
  creator: "缔造者", darkknight: "黑暗武士", common: "通用",
  creatormage: "缔造者", demonicswordman: "黑暗武士", gunblader: "枪剑士",
  archer: "弓箭手",
};
