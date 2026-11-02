import { describe, expect, it } from 'vitest';
import { existsSync, readFileSync } from 'node:fs';
import { ceraDate, ceraSchemas, ceraTabProducts, changeCeraProduct, newCeraValues, nextCeraID, readCeraShop } from '../src/gui/ceraShop';
import { getGUIProvider } from '../src/gui/registry';

const item = '3000109 1 1 0 0 15 0 0 `Life Token` 0 0 -1 `` -1';
const avatar = '3107733 515540507 4 0 0 120 0 0 0 0 -1 -1 -1 ``';
const pkg = '3400245 590713421 0 0 599 0 0 `Summer Box` 90 0 -1 -1 `20241105`';
function fixture(eol = '\n') {
  return ['# 😀 注释保留', '[item]', item, '[/item]', '[dont trade avatar]', avatar, '[/dont trade avatar]', '[package]', pkg, '[/package]',
    '[shop button list and sequence]', '`event` `mileage` `hotandnew` `package` `avatar` `item` `exit`', '[/shop button list and sequence]',
    '[purchasing limit]', '3000109 1 0 2 0 0 0 0', '[/purchasing limit]', '[specific product mileage]', '3000109 5', '[/specific product mileage]',
    '[hotnew sell ipg]', '3000109 6', '[/hotnew sell ipg]', '[storage]', '[cargo 1]', '3000109 1', '[cargo 1]', '3400245 590713421', '[/storage]',
    '[visible storage]', '[cargo 1]', '1 3000109', '[/visible storage]', '[unknown]', '`3000109` 3000109', '[/unknown]', ''].join(eol);
}
describe('商城结构和编辑', () => {
  it('识别三种布局、导航顺序及聚合页签', () => {
    const doc = readCeraShop(fixture());
    expect(doc.issues).toEqual([]); expect(doc.products).toHaveLength(3);
    expect(doc.tabs).toEqual(['event','mileage','hotandnew','package','avatar','item','exit']);
    expect(doc.products.find(p => p.section === 'package')).toMatchObject({ name: 'Summer Box', cash: '599', date: '20241105' });
    expect(doc.products.find(p => p.section === 'dont trade avatar')).toMatchObject({ name: '', cash: '120' });
    expect(ceraTabProducts(doc, 'hotandnew').map(p => p.id)).toEqual(['3000109']);
    expect(ceraTabProducts(doc, 'mileage').map(p => p.id)).toEqual(['3000109']);
    expect(ceraDate('20241105')).toBe('2024-11-05');
  });
  for (const eol of ['\n', '\r\n', '\r']) {
    it(`逐字段编辑保留未知字段、注释和换行 ${JSON.stringify(eol)}`, () => {
      const source = fixture(eol).replace('1 1 0 0 15', '1 1 # 价格注释'+eol+' 0 0 15');
      const row = readCeraShop(source).products.find(p => p.section === 'item')!, values = row.fields.map(t => t.value);
      values[5] = '30'; values[8] = '中文 `名称`';
      const result = changeCeraProduct(source, 'edit', 'item', values, row);
      expect(result).toBe(source.replace('0 0 15', '0 0 30').replace('`Life Token`', '`中文 ``名称```'));
      expect(readCeraShop(result).products.find(p => p.section === 'item')?.name).toBe('中文 `名称`');
    });
    it(`改号和删除同步商品引用，但保留物品编号 ${JSON.stringify(eol)}`, () => {
      const source = fixture(eol), row = readCeraShop(source).products.find(p => p.section === 'item')!, values = row.fields.map(t => t.value);
      values[0] = '3000999';
      const result = changeCeraProduct(source, 'edit', 'item', values, row), doc = readCeraShop(result);
      expect(doc.products.find(p => p.id === '3000999')?.itemId).toBe('1');
      expect(ceraTabProducts(doc, 'hotandnew')[0]?.id).toBe('3000999');
      expect(result).toContain('[unknown]'+eol+'`3000109` 3000109');
      expect(result).toContain('3000999 1'); expect(result).toContain('1 3000999');
      const removed = changeCeraProduct(source, 'delete', 'item', [], row), after = readCeraShop(removed);
      expect(after.products).toHaveLength(2); expect(after.sections.find(s => s.name === 'purchasing limit')?.tokens).toEqual([]);
      expect(after.sections.find(s => s.name === 'storage')?.children).toHaveLength(1);
      expect(after.sections.find(s => s.name === 'visible storage')?.children).toHaveLength(0);
      expect(removed).toContain('3400245 590713421'); expect(removed).toContain('[unknown]'+eol+'`3000109` 3000109');
    });
  }
  it('新增自动递增、允许手动编号，缺少分类时追加分类', () => {
    const source = fixture(), doc = readCeraShop(source);
    expect(nextCeraID(doc)).toBe('3400246');
    const values = newCeraValues(doc, 'item event'); values[1] = '42';
    const result = changeCeraProduct(source, 'add', 'item event', values);
    expect(result.startsWith(source)).toBe(true);
    expect(readCeraShop(result).products.find(p => p.section === 'item event')).toMatchObject({ id: '3400246', itemId: '42', cash: '0' });
    values[0] = '3500000'; expect(readCeraShop(changeCeraProduct(source, 'add', 'item', values)).products).toHaveLength(4);
  });
  it('保留特殊类型字符串标记', () => {
    const source = fixture().replace('`Life Token`', '{7=`Life Token`}');
    const target = readCeraShop(source).products.find(p => p.section === 'item')!, next = target.fields.map(t => t.value); next[8] = '新名称';
    expect(changeCeraProduct(source, 'edit', 'item', next, target)).toContain('{7=`新名称`}');
  });
  it('拒绝重复编号、无效数值、失效目标和不完整记录', () => {
    const source = fixture(), doc = readCeraShop(source), row = doc.products.find(p => p.section === 'item')!, values = row.fields.map(t => t.value);
    for (const value of ['3400245', '0', '-1', '2147483648', '1.5', '']) { values[0] = value; expect(() => changeCeraProduct(source,'edit','item',values,row)).toThrow(); }
    values[0] = row.id; values[5] = '-1'; expect(() => changeCeraProduct(source,'edit','item',values,row)).toThrow();
    expect(() => changeCeraProduct('# shift\n'+source,'delete','item',[],row)).toThrow('已变化');
    const broken = source.replace(item, item + ' 9'), parsed = readCeraShop(broken);
    expect(parsed.blocked.has('item')).toBe(true); expect(() => newCeraValues(parsed,'item')).toThrow(); expect(() => nextCeraID(parsed)).toThrow();
    expect(() => changeCeraProduct(source.replace('3000109 1 0 2 0 0 0 0', '3000109 1'), 'delete', 'item', [], row)).toThrow();
  });
  it('可交易时装独立使用 13 字段布局', () => {
    const source = '[avatar]\n4000114 39000 1 0 0 700 0 0 90 0 -1 -1 ``\n[/avatar]';
    const doc = readCeraShop(source); expect(doc.issues).toEqual([]);
    expect(doc.products[0]).toMatchObject({ id: '4000114', cash: '700', name: '' });
    const values = newCeraValues(doc, 'avatar'); values[1] = '39003';
    expect(values).toHaveLength(13); expect(values[12]).toBe('');
    expect(readCeraShop(changeCeraProduct(source, 'add', 'avatar', values)).products).toHaveLength(2);
  });
  it('入口准确识别商城文件', () => {
    const file = (path: string) => ({ index: 1, path, text: '', editable: true });
    expect(getGUIProvider(file('etc/(r)cerashop.etc'))?.id).toBe('cera-shop');
    expect(getGUIProvider(file('ETC\\(R)CERASHOP.ETC'))?.id).toBe('cera-shop');
    expect(getGUIProvider(file('etc/cerashopoption.cos'))).toBeNull();
  });
  const realPath = process.env.PVF_CERA_TEXT;
  it.skipIf(!realPath || !existsSync(realPath!))('110US 真实样本分组、改价、增删与引用更新', () => {
    const source = readFileSync(realPath!, 'utf8'), doc = readCeraShop(source);
    expect(doc.products).toHaveLength(17091); expect(doc.issues).toEqual([]);
    expect(doc.products.filter(p => p.section === 'dont trade avatar')).toHaveLength(10755);
    expect(doc.products.filter(p => p.section === 'package')).toHaveLength(5120);
    for (const section of ['item','dont trade avatar','package']) {
      const row = doc.products.find(p => p.section === section)!, values = row.fields.map(t => t.value); values[ceraSchemas[section]!.cash] = '123';
      expect(readCeraShop(changeCeraProduct(source,'edit',section,values,row)).products.find(p => p.id === row.id)?.cash).toBe('123');
      expect(readCeraShop(changeCeraProduct(source,'delete',section,[],row)).products).toHaveLength(17090);
    }
    const values = newCeraValues(doc,'item'); values[1]='1';
    expect(readCeraShop(changeCeraProduct(source,'add','item',values)).products).toHaveLength(17092);
  });
});
