import { parseItem, type ItemSection, type ItemToken } from './item';

export type CeraLayout = 'item' | 'avatar' | 'package';
export interface CeraSchema { label: string; tab: string; layout: CeraLayout; width: number; name: number; gold: number; cash: number; date: number }
const item = (label: string, tab: string): CeraSchema => ({ label, tab, layout: 'item', width: 14, name: 8, gold: 3, cash: 5, date: 12 });
export const ceraSchemas: Record<string, CeraSchema> = {
  'avatar': { label: '可交易时装', tab: 'avatar', layout: 'avatar', width: 13, name: -1, gold: 3, cash: 5, date: 12 },
  'dont trade avatar': { label: '不可交易时装', tab: 'avatar', layout: 'avatar', width: 14, name: -1, gold: 3, cash: 5, date: 13 },
  'avatar dye': item('染色道具', 'avatar'),
  'item event': item('活动商品', 'event'),
  'item second': item('消耗道具', 'item'),
  'item': item('常规道具', 'item'),
  'item mod or ext': item('仓库与扩展', 'item'),
  'item period or contract': item('期限与契约', 'item'),
  'creature': item('宠物', 'item'),
  'item etc': item('其他道具', 'item'),
  'premium': item('高级商品', 'item'),
  'package related': item('关联礼包', 'package'),
  'package': { label: '礼包', tab: 'package', layout: 'package', width: 13, name: 7, gold: 2, cash: 4, date: 12 },
};
export const ceraTabLabels: Record<string, string> = { event: '活动', mileage: '积分', hotandnew: '热卖与新品', package: '礼包', avatar: '时装', item: '道具', exit: '退出' };
export interface CeraProduct { section: string; row: number; fields: ItemToken[]; id: string; itemId: string; name: string; gold: string; cash: string; date: string }
export interface CeraDocument { sections: ItemSection[]; products: CeraProduct[]; tabs: string[]; hot: Set<string>; mileage: Set<string>; issues: string[]; blocked: Set<string> }
const refWidths: Record<string, number> = {
  'purchasing limit': 8, 'push cart limit': 1, 'gift disallowance': 1, 'gift only': 1,
  'not stackable buy': 1, 'immediately adaptive product': 1, 'hotnew sell ipg': 2,
  'default promotion page': 4, 'specific product mileage': 2, 'hide purchased limit item': 1,
};
const integer = (value: string, min = 0) => /^[+-]?\d+$/.test(value) && Number(value) >= min && Number(value) <= 2147483647;
const canonical = (value: string) => String(Number(value));
function schemaKinds(schema: CeraSchema) {
  return Array.from({ length: schema.width }, (_, i) => i === schema.name || i === schema.date ? 'string' : 'integer');
}
export function readCeraShop(text: string): CeraDocument {
  const sections = parseItem(text);
  const doc: CeraDocument = { sections, products: [], tabs: [], hot: new Set(), mileage: new Set(), issues: [], blocked: new Set() };
  for (const [name, schema] of Object.entries(ceraSchemas)) {
    const matches = sections.filter(s => s.name === name);
    if (!matches.length) continue;
    const section = matches[0]!;
    const kinds = schemaKinds(schema);
    if (matches.length > 1 || !section.paired || section.nested || section.tokens.length % schema.width || section.tokens.some((t, i) => t.kind !== kinds[i % schema.width] || t.kind === 'integer' && !integer(t.value, -2147483648))) {
      doc.blocked.add(name); doc.issues.push(`[${name}] 的记录布局无法识别，请在文本模式编辑；该分类已锁定。`); continue;
    }
    for (let i = 0; i < section.tokens.length; i += schema.width) {
      const fields = section.tokens.slice(i, i + schema.width);
      doc.products.push({ section: name, row: i / schema.width, fields, id: fields[0]!.value, itemId: fields[1]!.value,
        name: schema.name < 0 ? '' : fields[schema.name]!.value, gold: fields[schema.gold]!.value,
        cash: fields[schema.cash]!.value, date: fields[schema.date]!.value });
    }
  }
  const seen = new Set<string>();
  for (const product of doc.products) {
    if (seen.has(canonical(product.id))) doc.issues.push(`商品编号 ${product.id} 重复，请先在文本模式修正。`);
    seen.add(canonical(product.id));
  }
  const sequence = sections.filter(s => s.name === 'shop button list and sequence');
  const order = sequence[0]?.tokens.map(t => t.value) ?? ['event', 'mileage', 'hotandnew', 'package', 'avatar', 'item', 'exit'];
  if (sequence.length > 1) doc.issues.push('商城按钮顺序重复，使用第一组配置。');
  doc.tabs = [...new Set(order)];
  // Keep every product accessible when a script omits its category button.
  for (const tab of ['event', 'package', 'avatar', 'item']) if (doc.products.some(p => ceraSchemas[p.section]!.tab === tab) && !doc.tabs.includes(tab)) doc.tabs.push(tab);
  for (const [name, target] of [['hotnew sell ipg', doc.hot], ['specific product mileage', doc.mileage]] as const) {
    for (const section of sections.filter(s => s.name === name)) {
      if (!section.nested && section.tokens.length % 2 === 0) for (let i = 0; i < section.tokens.length; i += 2) target.add(canonical(section.tokens[i]!.value));
    }
  }
  return doc;
}
export function ceraTabProducts(doc: CeraDocument, tab: string): CeraProduct[] {
  if (tab === 'hotandnew' || tab === 'mileage') {
    const index = new Map(doc.products.map(p => [canonical(p.id), p]));
    return [...(tab === 'hotandnew' ? doc.hot : doc.mileage)].flatMap(id => index.has(id) ? [index.get(id)!] : []);
  }
  return doc.products.filter(p => ceraSchemas[p.section]!.tab === tab);
}
export function nextCeraID(doc: CeraDocument): string {
  if (doc.blocked.size) throw new Error('存在未识别的商品分类，无法保证自动编号不重复，请先修正分类布局');
  let next = doc.products.reduce((max, p) => Math.max(max, Number(p.id)), 2999999) + 1;
  const reserved = new Set<string>();
  for (const section of doc.sections) {
    const width = refWidths[section.name];
    if (width) for (let i = 0; i < section.tokens.length; i += width) reserved.add(canonical(section.tokens[i]!.value));
  }
  while (reserved.has(String(next))) next++;
  if (next > 2147483647) throw new Error('商品编号已达到 32 位整数上限');
  return String(next);
}
export function newCeraValues(doc: CeraDocument, name: string): string[] {
  const schema = ceraSchemas[name];
  if (!schema || doc.blocked.has(name)) throw new Error('该商品分类不能编辑');
  const values: string[] = schemaKinds(schema).map(kind => kind === 'string' ? '' : '0');
  values[0] = nextCeraID(doc); values[1] = '';
  if (schema.layout === 'item') { values[2] = '1'; values[11] = '-1'; values[13] = '-1'; }
  else if (schema.layout === 'avatar') { values[2] = '4'; for (let i = 10; i < schema.width - 1; i++) values[i] = '-1'; }
  else { values[10] = '-1'; values[11] = '-1'; }
  return values;
}
export function ceraFieldLabel(schema: CeraSchema, index: number): string {
  if (index === 0) return '商品编号';
  if (index === 1) return '物品编号';
  if (index === schema.name) return '商品名称';
  if (index === schema.gold) return '金币价格';
  if (index === schema.cash) return '点券价格';
  if (index === schema.date) return '日期参数';
  if (schema.layout === 'item' && index === 2) return '购买数量';
  return `参数 ${index + 1}`;
}
export function ceraDate(value: string): string {
  if (!/^\d{8}$/.test(value)) return value;
  return `${value.slice(0, 4)}-${value.slice(4, 6)}-${value.slice(6, 8)}`;
}
interface Patch { start: number; end: number; value: string }
function formatValue(value: string, token: Pick<ItemToken, 'kind' | 'marker'>): string {
  if (token.kind === 'string') {
    const quoted = '`' + value.replaceAll('`', '``') + '`';
    return token.marker ? `{${token.marker}=${quoted}}` : quoted;
  }
  if (token.kind !== 'integer' || !integer(value, -2147483648)) throw new Error('参数必须为 32 位整数');
  return value;
}
function patches(text: string, changes: Patch[]): string {
  let result = text, last = text.length;
  for (const change of changes.sort((a, b) => b.start - a.start)) {
    if (change.start < 0 || change.end < change.start || change.end > last) throw new Error('编辑位置重叠，请重新打开表单');
    result = result.slice(0, change.start) + change.value + result.slice(change.end); last = change.start;
  }
  return result;
}
// Only known product-ID references are rewritten. Item IDs and unknown sections remain untouched.
function referencePatches(doc: CeraDocument, oldID: string, nextID: string | null): Patch[] {
  const result: Patch[] = [];
  for (const section of doc.sections) {
    const width = refWidths[section.name];
    if (width) {
      if (section.nested || section.tokens.length % width || section.tokens.some(t => t.kind !== 'integer')) throw new Error(`[${section.name}] 结构异常，无法同步商品编号`);
      for (let i = 0; i < section.tokens.length; i += width) {
        const token = section.tokens[i]!;
        if (canonical(token.value) !== canonical(oldID)) continue;
        result.push({ start: token.start, end: nextID === null ? section.tokens[i + width - 1]!.end : token.end, value: nextID ?? '' });
      }
    }
    if (section.name === 'storage' || section.name === 'visible storage') {
      for (const child of section.children) {
        if (!['cargo 1', 'cargo 2', 'cargo account'].includes(child.name) || child.tokens.length !== 2 || child.tokens.some(t => t.kind !== 'integer')) throw new Error(`[${section.name}] 仓库映射异常，无法同步商品编号`);
        const token = child.tokens[section.name === 'storage' ? 0 : 1]!;
        if (canonical(token.value) !== canonical(oldID)) continue;
        result.push(nextID === null ? { start: child.start, end: child.end, value: '' } : { start: token.start, end: token.end, value: nextID });
      }
    }
  }
  return result;
}
export function changeCeraProduct(text: string, action: 'add' | 'edit' | 'delete', sectionName: string, values: string[], target?: CeraProduct): string {
  const doc = readCeraShop(text), schema = ceraSchemas[sectionName];
  if (!schema || doc.blocked.has(sectionName)) throw new Error('商品分类结构异常，不能编辑');
  const section = doc.sections.find(s => s.name === sectionName);
  const current = target && doc.products.find(p => p.section === target.section && p.row === target.row);
  if (action !== 'add' && (!target || !current || current.fields.some((t, i) => t.start !== target.fields[i]?.start || t.end !== target.fields[i]?.end || t.value !== target.fields[i]?.value))) throw new Error('商品数据已变化，请重新打开表单');
  if (current && current.section !== sectionName) throw new Error('不能直接改变已有商品的记录布局');
  if (current && doc.products.filter(p => canonical(p.id) === canonical(current.id)).length > 1) throw new Error('商品编号重复，请先在文本模式修正');
  const changes: Patch[] = [];
  if (action === 'delete') {
    if (doc.blocked.size) throw new Error('存在未识别的商品分类，无法安全删除商品及其引用');
    changes.push({ start: current!.fields[0]!.start, end: current!.fields.at(-1)!.end, value: '' }, ...referencePatches(doc, current!.id, null));
  } else {
    if (values.length !== schema.width) throw new Error('商品字段数量不匹配');
    for (const i of [0, 1, schema.gold, schema.cash]) if (!integer(values[i]!, i < 2 ? 1 : 0)) throw new Error('商品和物品编号必须为正整数，价格必须为非负整数');
    if (schema.layout === 'item' && !integer(values[2]!, 1)) throw new Error('购买数量必须为正整数');
    if (doc.products.some(p => p !== current && canonical(p.id) === canonical(values[0]!))) throw new Error('商品编号已存在');
    if (action === 'add' && doc.blocked.size) throw new Error('存在未识别的商品分类，无法确认编号唯一性');
    if (current && canonical(current.id) !== canonical(values[0]!) && doc.blocked.size) throw new Error('存在未识别的商品分类，无法安全修改商品编号');
    const kinds = schemaKinds(schema);
    const formatted = values.map((v, i) => formatValue(v, current?.fields[i] ?? { kind: kinds[i]! as 'string' | 'integer' }));
    if (current) {
      for (let i = 0; i < values.length; i++) if (values[i] !== current.fields[i]!.value) changes.push({ start: current.fields[i]!.start, end: current.fields[i]!.end, value: formatted[i]! });
      if (canonical(current.id) !== canonical(values[0]!)) changes.push(...referencePatches(doc, current.id, values[0]!));
    } else {
      const eol = text.includes('\r\n') ? '\r\n' : text.includes('\r') ? '\r' : '\n';
      const row = `${eol}\t${formatted.join('\t')}${eol}`;
      changes.push(section ? { start: section.closeStart, end: section.closeStart, value: row } : { start: text.length, end: text.length, value: `${eol}[${sectionName}]${row}[/${sectionName}]${eol}` });
    }
  }
  return patches(text, changes);
}
