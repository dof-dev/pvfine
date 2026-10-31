/** Item forms patch token ranges; nested effects and unknown sections stay intact. */
import { questReference } from './quest';
export type ItemKind = 'string' | 'integer';
export interface ItemToken { start: number; end: number; value: string; kind: ItemKind | 'unsupported'; marker?: string }
export interface ItemSection { name: string; start: number; body: number; end: number; closeStart: number; paired: boolean; children: ItemSection[]; tokens: ItemToken[]; nested: boolean }
export interface ItemField { name: string; label: string; kinds: ItemKind[]; counts?: number[]; repeat?: ItemKind; multiline?: boolean; equ?: boolean; stk?: boolean }
export const itemGroups: { label: string; fields: ItemField[] }[] = [
  { label: '基础信息', fields: [
    { name: 'name', label: '名称', kinds: ['string'] },
    { name: 'name2', label: '附加名称', kinds: ['string'] },
    { name: 'rarity', label: '稀有度', kinds: ['integer'] },
    { name: 'minimum level', label: '最低使用等级', kinds: ['integer'] },
    { name: 'attach type', label: '绑定类型', kinds: ['string'] },
    { name: 'equipment type', label: '装备类型与编号', kinds: ['string', 'integer'], counts: [1, 2], equ: true },
    { name: 'stackable type', label: '道具类型与编号', kinds: ['string', 'integer'], counts: [1, 2], stk: true },
    { name: 'usable job', label: '可用职业', kinds: ['string'], repeat: 'string' },
    { name: 'price', label: '出售价格', kinds: ['integer'] },
    { name: 'weight', label: '重量（原始值）', kinds: ['integer'] },
    { name: 'durability', label: '耐久度', kinds: ['integer'], equ: true },
    { name: 'stack limit', label: '堆叠上限', kinds: ['integer'], stk: true },
  ] },
  { label: '装备属性', fields: [
    ...[['physical attack', '力量'], ['magical attack', '智力'], ['physical defense', '体力'], ['magical defense', '精神'],
      ['equipment physical attack', '物理攻击力'], ['equipment magical attack', '魔法攻击力'], ['separate attack', '独立攻击力'],
      ['equipment physical defense', '物理防御力'], ['equipment magical defense', '魔法防御力'],
      ['HP MAX', 'HP 上限'], ['MP MAX', 'MP 上限']].map(([name, label]) => ({ name: name!, label: label!, kinds: ['integer'] as ItemKind[], repeat: 'integer' as const, counts: [1, 2, 4], equ: true })),
    ...[['attack speed', '攻击速度'], ['cast speed', '施放速度'], ['move speed', '移动速度'],
      ['physical critical hit', '物理暴击'], ['magical critical hit', '魔法暴击'], ['anti evil', '抗魔值']].map(([name, label]) => ({ name: name!, label: label!, kinds: ['integer'] as ItemKind[], equ: true })),
  ] },
  { label: '说明文本', fields: [
    ...[['basic explain', '基础说明'], ['detail explain', '详细说明'], ['flavor text', '背景描述'], ['explain', '道具说明']].map(([name, label]) => ({ name: name!, label: label!, kinds: ['string'] as ItemKind[], multiline: true })),
  ] },
  { label: '图像引用', fields: [
    { name: 'icon', label: '物品图标', kinds: ['string', 'integer'] },
    { name: 'field image', label: '地面图像', kinds: ['string', 'integer'] },
  ] },
];
export function parseItem(text: string): ItemSection[] {
  const lex: (ItemToken & { tag?: string })[] = [];
  let i = 0;
  const quoted = () => {
    const start = ++i;
    while (i < text.length) {
      if (text[i] !== '`') { i++; continue; }
      if (text[i + 1] === '`') { i += 2; continue; }
      return text.slice(start, i++).replaceAll('``', '`');
    }
    throw new Error('文本未闭合，请先在 DSL 中修复');
  };
  while (i < text.length) {
    if (/\s/.test(text[i]!)) { i++; continue; }
    if (text[i] === '#' || text.startsWith('//', i)) {
      while (i < text.length && !/[\r\n]/.test(text[i]!)) i++;
      continue;
    }
    if (text.startsWith('/*', i)) {
      const end = text.indexOf('*/', i + 2);
      if (end < 0) throw new Error('注释未闭合，请先在 DSL 中修复');
      i = end + 2;
      continue;
    }
    const start = i;
    let value = '', kind: ItemToken['kind'] = 'unsupported', tag: string | undefined, marker: string | undefined;
    if (text[i] === '`') { value = quoted(); kind = 'string'; }
    else if (text[i] === '[') {
      const end = text.indexOf(']', i);
      if (end < 0) throw new Error('标签未闭合，请先在 DSL 中修复');
      tag = text.slice(++i, end).trim().toLowerCase(); value = tag; i = end + 1;
    } else if (text[i] === '{') {
      i++;
      while (i < text.length && text[i] !== '}') { if (text[i] === '`') quoted(); else i++; }
      if (i >= text.length) throw new Error('类型标记未闭合，请先在 DSL 中修复');
      i++; value = text.slice(start, i);
      const match = /^\{(5|7|8|10)=\s*`((?:[^`]|``)*)`\s*\}$/.exec(value);
      if (match) { marker = match[1]; value = match[2]!.replaceAll('``', '`'); kind = 'string'; }
    } else {
      while (i < text.length && !/[\s`\[#{]/.test(text[i]!) && !text.startsWith('//', i) && !text.startsWith('/*', i)) i++;
      value = text.slice(start, i);
      if (/^[+-]?\d+$/.test(value)) kind = 'integer';
    }
    lex.push({ start, end: i, value, kind, marker, tag });
  }
  // Preserve container boundaries even when a required closing tag is missing.
  const paired = new Set(['if', 'then', 'multiple then', 'booster info', 'booster select category',
    ...lex.filter(t => t.tag?.startsWith('/')).map(t => t.tag!.slice(1).trim())]);
  const sections: ItemSection[] = [], stack: ItemSection[] = [];
  let current: ItemSection | undefined;
  for (const token of lex) {
    if (token.tag !== undefined) {
      if (current && current !== stack.at(-1)) current.end = token.start;
      if (token.tag.startsWith('/')) {
        const parent = stack.pop();
        if (parent?.name !== token.tag.slice(1).trim()) throw new Error('闭合标签不匹配，请先在 DSL 中修复');
        parent.closeStart = token.start;
        parent.end = token.end;
        current = stack.at(-1);
      } else {
        const node: ItemSection = { name: token.tag, start: token.start, body: token.end, end: text.length,
          closeStart: text.length, paired: paired.has(token.tag), children: [], tokens: [], nested: false };
        const parent = stack.at(-1);
        if (parent) { parent.children.push(node); parent.nested = true; }
        else sections.push(node);
        if (node.paired) stack.push(node);
        current = node;
      }
    } else current?.tokens.push(token);
  }
  if (stack.length) throw new Error('容器标签未闭合，请先在 DSL 中修复');
  return sections;
}
export function itemSection(sections: ItemSection[], name: string) { return sections.find(s => s.name === name.toLowerCase()); }
export function itemFieldIssue(sections: ItemSection[], field: ItemField): string {
  const matches = sections.filter(s => s.name === field.name.toLowerCase()), section = matches[0];
  if (matches.length > 1) return '存在重复字段，请在 DSL 中编辑';
  if (!section) return '';
  if (section.nested) return '包含嵌套结构，请在 DSL 中编辑';
  if (!(field.counts ?? [field.kinds.length]).includes(section.tokens.length) && !(!field.counts && field.repeat && section.tokens.length >= field.kinds.length)) return '参数数量无法识别，请在 DSL 中编辑';
  if (section.tokens.some((t, i) => t.kind !== (field.kinds[i] ?? field.repeat))) return '包含特殊类型，请在 DSL 中编辑';
  if (section.tokens.some(t => t.kind === 'string' && questReference(t.value))) return '字符串表引用，请在 DSL 中使用修改译文';
  return '';
}
export function editItem(text: string, field: ItemField, values: string[]): string {
  const sections = parseItem(text), issue = itemFieldIssue(sections, field);
  if (issue) throw new Error(issue);
  if (!(field.counts ?? [field.kinds.length]).includes(values.length) && !(!field.counts && field.repeat && values.length >= field.kinds.length)) throw new Error('参数数量不匹配');
  const formatted = values.map((value, i) => {
    if ((field.kinds[i] ?? field.repeat) === 'string') return '`' + value.replaceAll('`', '``') + '`';
    if (!/^[+-]?\d+$/.test(value) || Number(value) < -2147483648 || Number(value) > 2147483647) throw new Error('请输入 32 位范围内的整数');
    return value;
  });
  const section = itemSection(sections, field.name);
  const newline = text.includes('\r\n') ? '\r\n' : '\n';
  if (!section) return text + (text && !text.endsWith('\n') ? newline : '') + `[${field.name}]${newline}\t${formatted.join('\t')}${newline}`;
  if (section.tokens.length !== values.length) throw new Error('已有字段的参数数量不能在基础编辑中改变');
  let result = text;
  for (let i = section.tokens.length - 1; i >= 0; i--) {
    const token = section.tokens[i]!;
    if (token.value === values[i]) continue;
    const replacement = token.marker ? `{${token.marker}=${formatted[i]}}` : formatted[i]!;
    result = result.slice(0, token.start) + replacement + result.slice(token.end);
  }
  return result;
}
