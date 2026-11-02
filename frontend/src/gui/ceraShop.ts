import { parseItem, type ItemSection, type ItemToken } from './item';

export type CeraLayout = 'item' | 'avatar' | 'package' | 'external';
export interface CeraSchema { label: string; tab: string; layout: CeraLayout; width: number; name: number; gold: number; cash: number; date: number; quantity?: number; productId?: number; itemId?: number; readOnly?: boolean; stringFields?: number[]; defaults?: Record<number, string> }
const item = (label: string, tab: string): CeraSchema => ({ label, tab, layout: 'item', width: 14, name: 8, gold: 3, cash: 5, date: 12, defaults: { 11: '-1', 13: '-1' } });
export const ceraSchemas: Record<string, CeraSchema> = {
  'avatar': { label: '可交易时装', tab: 'avatar', layout: 'avatar', width: 13, name: -1, gold: 3, cash: 5, date: 12, defaults: { 2: '4', 10: '-1', 11: '-1' } },
  'dont trade avatar': { label: '不可交易时装', tab: 'avatar', layout: 'avatar', width: 14, name: -1, gold: 3, cash: 5, date: 13, defaults: { 2: '4', 10: '-1', 11: '-1', 12: '-1' } },
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
  'package': { label: '礼包', tab: 'package', layout: 'package', width: 13, name: 7, gold: 2, cash: 4, date: 12, defaults: { 10: '-1', 11: '-1' } },
};
const legacyItem = (label: string, tab = 'item'): CeraSchema => ({ label, tab, layout: 'item', width: 9, name: 6, gold: 3, cash: 5, date: -1 });
const legacyPackage = (label: string, tab = 'package'): CeraSchema => ({ label, tab, layout: 'package', width: 11, name: 5, gold: 2, cash: 4, date: -1, defaults: { 8: '-1', 9: '-1' } });
export const legacyCeraSchemas: Record<string, CeraSchema> = {
  // The six-field avatar table contains period/type flags, not a confirmed price column.
  avatar: { label: '时装', tab: 'avatar', layout: 'avatar', width: 6, name: -1, gold: -1, cash: -1, date: -1, defaults: { 2: '1', 5: '-1' } },
  coin: legacyItem('复活币'),
  item: legacyItem('常规道具'),
  material: legacyItem('材料'),
  recoveryitem: legacyItem('恢复道具'),
  premium: legacyItem('契约与服务'),
  'selectable character premium': { ...legacyItem('魔王之契约'), quantity: -1, gold: -1, defaults: { 2: '-1', 3: '7', 8: '-1' } },
  visual: { ...legacyItem('名称装饰'), width: 8, quantity: -1, gold: -1, defaults: { 2: '-1', 3: '7' } },
  creature: legacyItem('宠物'),
  'community package': legacyPackage('结婚礼包', 'community'),
  'community item': legacyItem('社区道具', 'community'),
  'regular package': legacyPackage('常规礼包'),
  package: legacyPackage('礼包'),
  'charac premium package': { label: '角色服务礼包', tab: 'package', layout: 'package', width: 9, name: 3, gold: -1, cash: 2, date: -1, defaults: { 6: '1' } },
  derivative: { label: '周边商品', tab: 'derivative', layout: 'external', width: 7, name: 1, gold: -1, cash: 2, date: -1, productId: -1, itemId: 3, readOnly: true, stringFields: [0, 1] },
};
const twItem = (label: string, tab = 'item'): CeraSchema => ({ label, tab, layout: 'item', width: 10, name: 6, gold: 3, cash: 5, date: -1, defaults: { 9: '-1' } });
export const twCeraSchemas: Record<string, CeraSchema> = {
  avatar: { label: '可交易时装', tab: 'avatar', layout: 'avatar', width: 7, name: -1, gold: -1, cash: -1, date: -1, defaults: { 2: '1', 5: '-1', 6: '-1' } },
  'dont trade avatar': { label: '不可交易时装', tab: 'avatar', layout: 'avatar', width: 8, name: -1, gold: -1, cash: -1, date: -1, defaults: { 2: '4', 5: '-1', 6: '-1', 7: '-1' } },
  coin: twItem('复活币'),
  item: twItem('常规道具'),
  creature: twItem('宠物', 'creature'),
  premium: twItem('契约与服务'),
  package: { label: '礼包', tab: 'package', layout: 'package', width: 10, name: 5, gold: 2, cash: 4, date: -1, defaults: { 8: '-1', 9: '-1' } },
};
export function ceraQuantityIndex(schema: CeraSchema): number { return schema.quantity ?? (schema.layout === 'item' ? 2 : -1); }
export const ceraTabLabels: Record<string, string> = { event: '活动', mileage: '积分', hotandnew: '热卖与新品', package: '礼包', avatar: '时装', item: '道具', community: '社区', derivative: '周边', creature: '宠物', exit: '退出' };
export interface CeraProduct { section: string; row: number; fields: ItemToken[]; id: string; itemId: string; name: string; gold: string; cash: string; date: string; quantity: string }
export interface CeraDocument { family: 'legacy' | 'modern' | 'tw'; schemas: Record<string, CeraSchema>; references: Record<string, { width: number; index: number }>; sections: ItemSection[]; products: CeraProduct[]; tabs: string[]; hot: Set<string>; mileage: Set<string>; issues: string[]; blocked: Set<string> }
const refWidths: Record<string, number> = {
  'purchasing limit': 8, 'push cart limit': 1, 'gift disallowance': 1, 'gift only': 1,
  'not stackable buy': 1, 'immediately adaptive product': 1, 'hotnew sell ipg': 2,
  'default promotion page': 4, 'specific product mileage': 2, 'hide purchased limit item': 1,
};
const integer = (value: string, min = 0) => /^[+-]?\d+$/.test(value) && Number(value) >= min && Number(value) <= 2147483647;
const canonical = (value: string) => String(Number(value));
function schemaKinds(schema: CeraSchema) {
  return Array.from({ length: schema.width }, (_, i) => (schema.stringFields ?? [schema.name, schema.date]).includes(i) ? 'string' : 'integer');
}
export function readCeraShop(text: string): CeraDocument {
  const sections = parseItem(text, {
    'shop button and sequence': 'shop button list and sequence',
  });
  const matchesSchema = (section: ItemSection, schema: CeraSchema | undefined): boolean => {
    if (!schema || !section.paired || section.nested || section.tokens.length % schema.width) return false;
    const kinds = schemaKinds(schema);
    return section.tokens.every((t, i) => t.kind === kinds[i % schema.width] && (t.kind !== 'integer' || integer(t.value, -2147483648)));
  };
  const isTW = sections.some(section => section.tokens.length > 0 && matchesSchema(section, twCeraSchemas[section.name]) && !matchesSchema(section, legacyCeraSchemas[section.name]) && !matchesSchema(section, ceraSchemas[section.name]));
  const family = isTW ? 'tw' : sections.some(section => section.tokens.length > 0 && matchesSchema(section, legacyCeraSchemas[section.name]) && !matchesSchema(section, ceraSchemas[section.name])) ? 'legacy' : 'modern';
  const preferred = family === 'tw' ? twCeraSchemas : family === 'legacy' ? legacyCeraSchemas : ceraSchemas;
  const candidates = [preferred, ceraSchemas, legacyCeraSchemas, twCeraSchemas];
  const references = Object.fromEntries(Object.entries(refWidths).map(([name, width]) => [name, { width, index: 0 }]));
  if (family === 'legacy') Object.assign(references, { 'default promotion page': { width: 2, index: 0 }, 'buy restrict item': { width: 4, index: 1 }, 'btn control': { width: 4, index: 0 } });
  const doc: CeraDocument = { family, schemas: { ...preferred }, references, sections, products: [], tabs: [], hot: new Set(), mileage: new Set(), issues: [], blocked: new Set() };
  for (const name of new Set(candidates.flatMap(schemas => Object.keys(schemas)))) {
    const matches = sections.filter(s => s.name === name);
    if (!matches.length) continue;
    const section = matches[0]!;
    if (!section.tokens.length && !preferred[name]) continue;
    const options = candidates.map(schemas => schemas[name]).filter((schema): schema is CeraSchema => !!schema);
    const schema = options.find(candidate => matchesSchema(section, candidate)) ?? options[0]!;
    doc.schemas[name] = schema;
    if (matches.length > 1 || !matchesSchema(section, schema)) {
      doc.blocked.add(name); doc.issues.push(`[${name}] 的记录布局无法识别，请在文本模式编辑；该分类已锁定。`); continue;
    }
    for (let i = 0; i < section.tokens.length; i += schema.width) {
      const fields = section.tokens.slice(i, i + schema.width);
      const value = (index: number) => fields[index]?.value ?? '';
      doc.products.push({ section: name, row: i / schema.width, fields, id: value(schema.productId ?? 0), itemId: value(schema.itemId ?? 1),
        name: value(schema.name), gold: value(schema.gold), cash: value(schema.cash), date: value(schema.date), quantity: value(ceraQuantityIndex(schema)) });
    }
  }
  const seen = new Set<string>();
  for (const product of doc.products) {
    if (Number(product.id) <= 0 || doc.schemas[product.section]!.readOnly) continue;
    if (seen.has(canonical(product.id))) doc.issues.push(`商品编号 ${product.id} 重复，请先在文本模式修正。`);
    seen.add(canonical(product.id));
  }
  const sequence = sections.filter(s => s.name === 'shop button list and sequence');
  const order = sequence[0]?.tokens.map(t => t.value) ?? ['event', 'mileage', 'hotandnew', 'package', 'avatar', 'item', 'exit'];
  if (sequence.length > 1) doc.issues.push('商城按钮顺序重复，使用第一组配置。');
  doc.tabs = [...new Set(order)];
  // Keep every product accessible when a script omits its category button.
  for (const tab of new Set(Object.values(doc.schemas).map(schema => schema.tab))) if (doc.products.some(p => doc.schemas[p.section]!.tab === tab) && !doc.tabs.includes(tab)) doc.tabs.push(tab);
  for (const [name, target] of [['hotnew sell ipg', doc.hot], ['specific product mileage', doc.mileage]] as const) {
    for (const section of sections.filter(s => s.name === name)) {
      if (!section.nested && section.tokens.length % 2 === 0) for (let i = 0; i < section.tokens.length; i += 2) target.add(canonical(section.tokens[i]!.value));
    }
  }
  return doc;
}
export function ceraTabProducts(doc: CeraDocument, tab: string): CeraProduct[] {
  if (tab === 'hotandnew' || tab === 'mileage') {
    const index = new Map(doc.products.filter(p => Number(p.id) > 0).map(p => [canonical(p.id), p]));
    return [...(tab === 'hotandnew' ? doc.hot : doc.mileage)].flatMap(id => index.has(id) ? [index.get(id)!] : []);
  }
  return doc.products.filter(p => doc.schemas[p.section]!.tab === tab);
}
export function nextCeraID(doc: CeraDocument): string {
  if (doc.blocked.size) throw new Error('存在未识别的商品分类，无法保证自动编号不重复，请先修正分类布局');
  let next = doc.products.reduce((max, p) => Math.max(max, Number(p.id) || 0), doc.family === 'tw' ? 8999 : doc.family === 'legacy' ? 99999 : 2999999) + 1;
  const reserved = new Set<string>();
  for (const section of doc.sections) {
    const spec = doc.references[section.name];
    if (spec) for (let i = 0; i < section.tokens.length; i += spec.width) if (section.tokens[i + spec.index]) reserved.add(canonical(section.tokens[i + spec.index]!.value));
  }
  while (reserved.has(String(next))) next++;
  if (next > 2147483647) throw new Error('商品编号已达到 32 位整数上限');
  return String(next);
}
export function newCeraValues(doc: CeraDocument, name: string): string[] {
  const schema = doc.schemas[name];
  if (!schema || schema.readOnly || doc.blocked.has(name)) throw new Error('该商品分类不能编辑');
  const values: string[] = schemaKinds(schema).map(kind => kind === 'string' ? '' : '0');
  values[0] = nextCeraID(doc); values[1] = '';
  const quantity = ceraQuantityIndex(schema);
  if (quantity >= 0) values[quantity] = '1';
  for (const [index, value] of Object.entries(schema.defaults ?? {})) values[Number(index)] = value;
  return values;
}
export function ceraFieldLabel(schema: CeraSchema, index: number): string {
  if (index === 0) return '商品编号';
  if (index === 1) return '物品编号';
  if (index === schema.name) return '商品名称';
  if (index === schema.gold) return '金币价格';
  if (index === schema.cash) return '点券价格';
  if (index === schema.date) return '日期参数';
  if (index === ceraQuantityIndex(schema)) return '购买数量';
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
    const spec = doc.references[section.name];
    if (spec) {
      const { width, index } = spec;
      if (section.nested || section.tokens.length % width || section.tokens.some(t => t.kind !== 'integer')) throw new Error(`[${section.name}] 结构异常，无法同步商品编号`);
      for (let i = 0; i < section.tokens.length; i += width) {
        const token = section.tokens[i + index]!;
        if (canonical(token.value) !== canonical(oldID)) continue;
        result.push({ start: nextID === null ? section.tokens[i]!.start : token.start, end: nextID === null ? section.tokens[i + width - 1]!.end : token.end, value: nextID ?? '' });
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
  const doc = readCeraShop(text), schema = doc.schemas[sectionName];
  if (!schema || schema.readOnly || doc.blocked.has(sectionName)) throw new Error('商品分类结构异常，不能编辑');
  const section = doc.sections.find(s => s.name === sectionName);
  const current = target && doc.products.find(p => p.section === target.section && p.row === target.row);
  if (action !== 'add' && (!target || !current || current.fields.some((t, i) => t.start !== target.fields[i]?.start || t.end !== target.fields[i]?.end || t.value !== target.fields[i]?.value))) throw new Error('商品数据已变化，请重新打开表单');
  if (current && current.section !== sectionName) throw new Error('不能直接改变已有商品的记录布局');
  if (current && Number(current.id) > 0 && doc.products.filter(p => canonical(p.id) === canonical(current.id)).length > 1) throw new Error('商品编号重复，请先在文本模式修正');
  const changes: Patch[] = [];
  if (action === 'delete') {
    if (doc.blocked.size) throw new Error('存在未识别的商品分类，无法安全删除商品及其引用');
    changes.push({ start: current!.fields[0]!.start, end: current!.fields.at(-1)!.end, value: '' }, ...(Number(current!.id) > 0 ? referencePatches(doc, current!.id, null) : []));
  } else {
    if (values.length !== schema.width) throw new Error('商品字段数量不匹配');
    for (const i of [0, 1]) if (!integer(values[i]!, 1) && !(current && values[i] === current.fields[i]!.value && Number(values[i]) === -1)) throw new Error('商品和物品编号必须为正整数');
    for (const i of [schema.gold, schema.cash]) if (i >= 0 && !integer(values[i]!, 0)) throw new Error('价格必须为非负整数');
    const quantity = ceraQuantityIndex(schema);
    if (quantity >= 0 && !integer(values[quantity]!, 1)) throw new Error('购买数量必须为正整数');
    if (Number(values[0]) > 0 && doc.products.some(p => p !== current && canonical(p.id) === canonical(values[0]!))) throw new Error('商品编号已存在');
    if (action === 'add' && doc.blocked.size) throw new Error('存在未识别的商品分类，无法确认编号唯一性');
    if (current && canonical(current.id) !== canonical(values[0]!) && doc.blocked.size) throw new Error('存在未识别的商品分类，无法安全修改商品编号');
    const kinds = schemaKinds(schema);
    const formatted = values.map((v, i) => formatValue(v, current?.fields[i] ?? { kind: kinds[i]! as 'string' | 'integer' }));
    if (current) {
      for (let i = 0; i < values.length; i++) if (values[i] !== current.fields[i]!.value) changes.push({ start: current.fields[i]!.start, end: current.fields[i]!.end, value: formatted[i]! });
      if (Number(current.id) > 0 && canonical(current.id) !== canonical(values[0]!)) changes.push(...referencePatches(doc, current.id, values[0]!));
    } else {
      const eol = text.includes('\r\n') ? '\r\n' : text.includes('\r') ? '\r' : '\n';
      const row = `${eol}\t${formatted.join('\t')}${eol}`;
      changes.push(section ? { start: section.closeStart, end: section.closeStart, value: row } : { start: text.length, end: text.length, value: `${eol}[${sectionName}]${row}[/${sectionName}]${eol}` });
    }
  }
  return patches(text, changes);
}
