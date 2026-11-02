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
  it('兼容 90CN 按钮顺序的历史结束标签，编辑保留原始拼写', () => {
    const source = fixture().replace('[/shop button list and sequence]', '[/shop button and sequence]');
    const doc = readCeraShop(source);
    expect(doc.tabs).toEqual(['event','mileage','hotandnew','package','avatar','item','exit']);
    expect(doc.issues).toEqual([]);
    const row = doc.products.find(p => p.section === 'item')!, values = row.fields.map(t => t.value); values[5] = '42';
    const edited = changeCeraProduct(source, 'edit', 'item', values, row);
    expect(edited).toBe(source.replace('0 0 15', '0 0 42'));
    expect(edited).toContain('[/shop button and sequence]');
    expect(() => readCeraShop(fixture().replace('[/item]', '[/wrong item]'))).toThrow('闭合标签不匹配');
  });
  function legacyFixture(eol = '\n') {
    return ['# 旧版商城 😀', '[avatar]', '120000 39032 1 1 0 -1', '[/avatar]',
      '[coin]', '100000 1 10 0 0 880 `10个` 0 0', '[/coin]',
      '[item]', '100534 2681921 1 0 0 2000 `账号金库升级工具` 0 0', '[/item]',
      '[selectable character premium]', '-1 2681927 -1 7 0 2000 `魔王之契约` 0 -1', '[/selectable character premium]',
      '[visual]', '100615 2681313 -1 7 0 300 `名称装饰卡 (7天)` 0', '[/visual]',
      '[package]', '104763 490702435 0 0 19800 `夏日礼包` 1 0 -1 -1 15', '[/package]',
      '[community item]', '102401 2683401 1 0 0 14888 `1个` 101 0', '[/community item]',
      '[charac premium package]', '100625 2682006 3880 `60天` 0 0 1 60 100614', '[/charac premium package]',
      '[derivative]', '`https://example.invalid/item` `周边` 338 -1 -1 5 -1', '[/derivative]',
      '[shop button list and sequence]', '`mileage` `hotandnew` `package` `avatar` `item` `community` `derivative` `exit`', '[/shop button list and sequence]',
      '[buy restrict item]', '1 100534 7 1', '[/buy restrict item]',
      '[btn control]', '100534 0 1 0', '[/btn control]',
      '[default promotion page]', '100534 29 -1 7', '[/default promotion page]',
      '[specific product mileage]', '100534 3', '[/specific product mileage]',
      '[custom]', '100534 `保留`', '[/custom]', ''].join(eol);
  }
  it('按结构识别普通版商城，保留旧版页签及各分类', () => {
    const doc = readCeraShop(legacyFixture());
    expect(doc.family).toBe('legacy'); expect(doc.issues).toEqual([]); expect(doc.products).toHaveLength(9);
    expect(doc.tabs).toEqual(['mileage','hotandnew','package','avatar','item','community','derivative','exit']);
    expect(doc.schemas.item).toMatchObject({ width: 9, name: 6, date: -1 });
    expect(doc.products.find(p => p.section === 'avatar')).toMatchObject({ gold: '', cash: '', quantity: '' });
    expect(doc.products.find(p => p.section === 'selectable character premium')).toMatchObject({ gold: '', quantity: '', cash: '2000' });
    expect(doc.products.find(p => p.section === 'visual')).toMatchObject({ gold: '', quantity: '', cash: '300' });
    expect(doc.products.find(p => p.section === 'charac premium package')).toMatchObject({ cash: '3880', name: '60天' });
    expect(ceraTabProducts(doc, 'community')).toHaveLength(1);
    expect(ceraTabProducts(doc, 'derivative')[0]).toMatchObject({ id: '', itemId: '-1', name: '周边', cash: '338' });
    expect(() => newCeraValues(doc, 'derivative')).toThrow();
    expect(() => changeCeraProduct(legacyFixture(), 'delete', 'derivative', [], ceraTabProducts(doc, 'derivative')[0])).toThrow();
  });
  for (const eol of ['\n','\r\n','\r']) {
    it(`普通版增删改保留字段宽度、期限及引用 ${JSON.stringify(eol)}`, () => {
      const source = legacyFixture(eol), doc = readCeraShop(source), row = doc.products.find(p => p.section === 'item')!;
      const values = row.fields.map(t => t.value); values[0] = '100535'; values[5] = '1234';
      const changed = changeCeraProduct(source,'edit','item',values,row), after = readCeraShop(changed);
      expect(after.products.find(p => p.section === 'item')).toMatchObject({ id: '100535', cash: '1234' });
      expect(changed).toContain('1 100535 7 1'); expect(changed).toContain('100535 29 -1 7');
      expect(changed).toContain('[custom]'+eol+'100534 `保留`');
      expect(changed).toContain('100615 2681313 -1 7 0 300');
      const removed = readCeraShop(changeCeraProduct(source,'delete','item',[],row));
      expect(removed.sections.find(s => s.name === 'buy restrict item')?.tokens).toHaveLength(0);
      expect(removed.sections.find(s => s.name === 'btn control')?.tokens).toHaveLength(0);
      expect(removed.sections.find(s => s.name === 'default promotion page')?.tokens.map(t => t.value)).toEqual(['-1','7']);
      for (const name of ['avatar','item','package','visual','charac premium package']) {
        const fields = newCeraValues(doc,name); fields[1] = '42';
        const added = readCeraShop(changeCeraProduct(source,'add',name,fields));
        expect(added.products).toHaveLength(10); expect(fields).toHaveLength(doc.schemas[name]!.width);
      }
      expect(nextCeraID(doc)).toBe('120001');
    });
  }
  const legacyPath = process.env.PVF_CERA90_LEGACY_TEXT;
  it.skipIf(!legacyPath || !existsSync(legacyPath!))('90CN etc/cerashop.etc 真实样本显示所有商品并使用原布局增删改', () => {
    const source = readFileSync(legacyPath!, 'utf8'), doc = readCeraShop(source);
    expect(doc.family).toBe('legacy'); expect(doc.issues).toEqual([]); expect(doc.blocked.size).toBe(0);
    expect(doc.products).toHaveLength(37127);
    expect(ceraTabProducts(doc,'avatar')).toHaveLength(36394);
    expect(ceraTabProducts(doc,'item')).toHaveLength(239);
    expect(ceraTabProducts(doc,'package')).toHaveLength(453);
    expect(ceraTabProducts(doc,'community')).toHaveLength(34);
    expect(ceraTabProducts(doc,'derivative')).toHaveLength(7);
    for (const name of ['avatar','item','coin','material','recoveryitem','premium','selectable character premium','visual','creature','community package','community item','regular package','package','charac premium package']) {
      const row = doc.products.find(p => p.section === name && Number(p.id) > 0)!, schema = doc.schemas[name]!, values = row.fields.map(t => t.value);
      const column = schema.cash >= 0 ? schema.cash : 2; values[column] = schema.cash >= 0 ? '123' : '2';
      const changed = changeCeraProduct(source,'edit',name,values,row);
      const token = row.fields[column]!;
      expect(changed).toBe(source.slice(0,token.start)+values[column]+source.slice(token.end));
      const fields = newCeraValues(doc,name); fields[1] = row.itemId;
      expect(readCeraShop(changeCeraProduct(source,'add',name,fields)).products).toHaveLength(37128);
    }
    const target = doc.products.find(p => p.section === 'item')!, values = target.fields.map(t => t.value); values[0] = nextCeraID(doc);
    expect(readCeraShop(changeCeraProduct(source,'edit','item',values,target)).products.find(p => p.id === values[0])).toBeDefined();
    expect(readCeraShop(changeCeraProduct(source,'delete','item',[],target)).products).toHaveLength(37126);
  });
  function twFixture() {
    return ['# TW 商城 😀', '[npc info]', '`Map/Npc/NpcFace01.img` 12', '[/npc info]',
      '[avatar]', '[/avatar]', '[dont trade avatar]', '[/dont trade avatar]', '[avatar dye]', '[/avatar dye]',
      '[coin]', '9000 1 1 0 0 5 `复活币` 0 0 -1', '[/coin]',
      '[item]', '10000 50 1 0 0 10 `金库升级` 0 0 -1', '[/item]',
      '[premium]', '30001 31 1 0 0 50 `契约` 0 0 -1', '[/premium]',
      '[creature]', '20000 24 100 0 0 30 `宠物饲料` 0 0 -1', '[/creature]',
      '[package]', '100031 2660041 0 0 1080 `礼包` 2 0 -1 -1', '[/package]',
      '[gift disallowance]', '10000', '[/gift disallowance]',
      '[shop button list and sequence]', '`hotandnew` `avatar` `item` `creature` `package` `exit`', '[/shop button list and sequence]', ''].join('\n');
  }
  it('TW 使用十字段道具和礼包，宠物按配置显示独立页签', () => {
    const doc = readCeraShop(twFixture());
    expect(doc.family).toBe('tw'); expect(doc.issues).toEqual([]); expect(doc.products).toHaveLength(5);
    expect(doc.schemas.item).toMatchObject({ width:10, name:6, date:-1 });
    expect(doc.schemas.package).toMatchObject({ width:10, name:5, date:-1 });
    expect(doc.schemas.avatar.width).toBe(7); expect(doc.schemas['dont trade avatar'].width).toBe(8);
    expect(doc.schemas['avatar dye']).toBeUndefined();
    expect(doc.tabs).toEqual(['hotandnew','avatar','item','creature','package','exit']);
    expect(ceraTabProducts(doc,'item')).toHaveLength(3); expect(ceraTabProducts(doc,'creature')).toHaveLength(1);
    expect(doc.products.find(p => p.section === 'creature')).toMatchObject({ cash:'30', quantity:'100' });
    expect(nextCeraID(doc)).toBe('100032');
    const row = doc.products.find(p => p.section === 'item')!, values=row.fields.map(t=>t.value); values[0]='100032'; values[5]='20';
    const changed=changeCeraProduct(twFixture(),'edit','item',values,row);
    expect(changed).toContain('[gift disallowance]\n100032');
    expect(changed).toContain('`Map/Npc/NpcFace01.img` 12');
    expect(readCeraShop(changed).products.find(p=>p.id==='100032')).toMatchObject({cash:'20',name:'金库升级'});
    for(const name of ['avatar','dont trade avatar','coin','item','premium','creature','package']) {
      const fields=newCeraValues(doc,name); fields[1]='42';
      expect(readCeraShop(changeCeraProduct(twFixture(),'add',name,fields)).products).toHaveLength(6);
      expect(fields).toHaveLength(doc.schemas[name]!.width);
    }
  });
  it('TW 七／八字段时装保留原始期限与附加参数，不误作价格或数量', () => {
    const source='[avatar]\n144327 39000 1 90 0 -1 -1\n[/avatar]\n[dont trade avatar]\n55309429 101500454 4 0 0 -1 120164 -1\n[/dont trade avatar]';
    const doc=readCeraShop(source); expect(doc.family).toBe('tw'); expect(doc.issues).toEqual([]);
    expect(doc.products).toHaveLength(2);
    for(const row of doc.products) {
      expect(row).toMatchObject({gold:'',cash:'',quantity:''});
      const values=row.fields.map(t=>t.value); values[2]='2';
      const token=row.fields[2]!;
      expect(changeCeraProduct(source,'edit',row.section,values,row)).toBe(source.slice(0,token.start)+'2'+source.slice(token.end));
    }
  });
  for(const [variable,count] of [['PVF_CERA_TW_TEXT',133],['PVF_CERA_TW_FIRST_TEXT',54476]] as const) {
    const path=process.env[variable];
    it.skipIf(!path || !existsSync(path!))(`TW 真实商城回归 ${variable}`,()=>{
      const source=readFileSync(path!,'utf8'),doc=readCeraShop(source);
      expect(doc.family).toBe('tw'); expect(doc.blocked.size).toBe(0); expect(doc.products).toHaveLength(count);
      expect(doc.issues).toEqual([]);
      expect(doc.tabs).toContain('creature');
      expect(ceraTabProducts(doc,'creature')).toHaveLength(count===133?5:17);
      if(count===133)expect(ceraTabProducts(doc,'item')).toHaveLength(96);
      for(const name of ['coin','item','premium','creature','package']) {
        const row=doc.products.find(p=>p.section===name && Number(p.id)>0)!,schema=doc.schemas[name]!,values=row.fields.map(t=>t.value);
        values[schema.cash]='123'; const token=row.fields[schema.cash]!;
        expect(changeCeraProduct(source,'edit',name,values,row)).toBe(source.slice(0,token.start)+'123'+source.slice(token.end));
        const added=newCeraValues(doc,name);added[1]=row.itemId;
        expect(readCeraShop(changeCeraProduct(source,'add',name,added)).products).toHaveLength(count+1);
        expect(readCeraShop(changeCeraProduct(source,'delete',name,[],row)).products).toHaveLength(count-1);
      }
    });
  }
  const real90Path = process.env.PVF_CERA90_TEXT;
  it.skipIf(!real90Path || !existsSync(real90Path!))('90CN 真实商城可以解析并修改四种记录的价格', () => {
    const source = readFileSync(real90Path!, 'utf8'), doc = readCeraShop(source);
    expect(doc.blocked.size).toBe(0);
    expect(doc.tabs).toEqual(['event','mileage','hotandnew','package','avatar','item','exit']);
    expect(doc.products.length).toBeGreaterThan(60000);
    for (const section of ['item','avatar','dont trade avatar','package']) {
      const row = doc.products.find(p => p.section === section && Number(p.id) > 0)!, values = row.fields.map(t => t.value); values[ceraSchemas[section]!.cash] = '123';
      const changed = changeCeraProduct(source, 'edit', section, values, row);
      expect(changed).toContain('[/shop button and sequence]');
      expect(readCeraShop(changed).products.find(p => p.section === section && p.row === row.row)?.cash).toBe('123');
    }
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
