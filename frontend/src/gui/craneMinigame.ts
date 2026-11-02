import { parseItem, type ItemSection, type ItemToken } from './item';

export interface CraneItem {
  section: ItemSection;
  fields: [ItemToken, ItemToken, ItemToken, ItemToken];
}
export interface CraneDocument {
  items: CraneItem[];
  material: ItemToken[];
}
export interface CraneValues { id: number; count: number; viewRatio: number; pickRatio: number }

const numeric = /^[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:e[+-]?\d+)?$/i;
function numbers(section: ItemSection, count: number): ItemToken[] {
  if (section.paired || section.children.length || section.tokens.length !== count || section.tokens.some(t => t.kind === 'string' || !numeric.test(t.value) || !Number.isFinite(Number(t.value)))) {
    throw new Error(`[${section.name}] 参数无法识别，请先在 DSL 中修复`);
  }
  return section.tokens;
}
export function craneInteger(value: number | null): number {
  if (value === null || !Number.isInteger(value) || value < 0 || value > 2147483647) throw new Error('请输入 0～2147483647 的整数');
  return value;
}
export function validateCraneValues(values: CraneValues) {
  craneInteger(values.id); craneInteger(values.count);
  if (!Number.isFinite(values.viewRatio) || values.viewRatio < 0 || values.viewRatio > 3.4028234663852886e38) throw new Error('视野比率需要为非负有限数值');
  if (!Number.isFinite(values.pickRatio) || values.pickRatio < 0 || values.pickRatio > 100) throw new Error('抓取几率需要在 0～100 之间');
}
export function parseCraneMinigame(text: string): CraneDocument {
  const sections = parseItem(text);
  const items = sections.filter(s => s.name === 'item').map(section => {
    if (!section.paired || section.tokens.length !== 1 || section.tokens[0]!.kind !== 'integer') throw new Error('[item] 需要物品 ID 和闭合标签，请先在 DSL 中修复');
    const fields: ItemToken[] = [section.tokens[0]!];
    for (const name of ['cnt', 'viewratio', 'pickratio']) {
      const matches = section.children.filter(s => s.name === name);
      if (matches.length !== 1) throw new Error(`[item] 中 [${name}] 缺失或重复，请先在 DSL 中修复`);
      fields.push(numbers(matches[0]!, 1)[0]!);
    }
    if (fields[1]!.kind !== 'integer') throw new Error('[cnt] 需要整数数量');
    validateCraneValues({ id: Number(fields[0]!.value), count: Number(fields[1]!.value), viewRatio: Number(fields[2]!.value), pickRatio: Number(fields[3]!.value) });
    return { section, fields: fields as CraneItem['fields'] };
  });
  const materials = sections.filter(s => s.name === 'material');
  if (materials.length > 1) throw new Error('[material] 重复，请先在 DSL 中修复');
  const material = materials.length ? numbers(materials[0]!, 2) : [];
  material.forEach(t => { if (t.kind !== 'integer') throw new Error('[material] 需要整数物品 ID 和数量'); craneInteger(Number(t.value)); });
  return { items, material };
}
const floatText = (value: number) => /[.e]/i.test(String(value)) ? String(value) : `${value}.0`;
function patch(text: string, fields: ItemToken[], values: number[]): string {
  return fields.map((token, i) => ({ token, value: values[i]! })).sort((a, b) => b.token.start - a.token.start).reduce((source, { token, value }) => {
    if (Number(token.value) === value) return source;
    // Keep floating-point fields floating-point even when the new value is whole.
    const replacement = /[.e]/i.test(token.value) ? floatText(value) : String(value);
    return source.slice(0, token.start) + replacement + source.slice(token.end);
  }, text);
}
export function editCraneItem(text: string, index: number, values: CraneValues): string {
  validateCraneValues(values);
  const item = parseCraneMinigame(text).items[index];
  if (!item) throw new Error('奖品位置已失效');
  return patch(text, item.fields, [values.id, values.count, values.viewRatio, values.pickRatio]);
}
const newline = (text: string) => text.includes('\r\n') ? '\r\n' : text.includes('\r') ? '\r' : '\n';
export function addCraneItem(text: string, values: CraneValues): string {
  validateCraneValues(values);
  const doc = parseCraneMinigame(text), eol = newline(text);
  const block = ['[item]', `\t${values.id}`, '\t[cnt]', `\t\t${values.count}`, '\t[viewRatio]', `\t\t${floatText(values.viewRatio)}`, '\t[pickRatio]', `\t\t${values.pickRatio}`, '[/item]'].join(eol);
  const position = doc.items.at(-1)?.section.end ?? parseItem(text).find(s => s.name === 'material')?.start ?? text.length;
  return text.slice(0, position) + eol + block + eol + text.slice(position);
}
export function removeCraneItem(text: string, index: number): string {
  const item = parseCraneMinigame(text).items[index];
  if (!item) throw new Error('奖品位置已失效');
  if (item.section.children.some(s => !['cnt', 'viewratio', 'pickratio'].includes(s.name))) throw new Error('该奖品包含其他字段，请在 DSL 中删除，以确认处理这些字段');
  return text.slice(0, item.section.start) + text.slice(item.section.end);
}
export function editCraneMaterial(text: string, id: number, count: number): string {
  craneInteger(id); craneInteger(count);
  const doc = parseCraneMinigame(text);
  if (doc.material.length) return patch(text, doc.material, [id, count]);
  const eol = newline(text);
  return text + eol + `[material]${eol}\t${id}\t${count}${eol}`;
}
