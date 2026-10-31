import { describe, expect, it } from 'vitest';
import { editItem, itemFieldIssue, itemGroups, itemSection, parseItem, type ItemField } from '../src/gui/item';
import { getGUIProvider } from '../src/gui/registry';
const field = (name: string) => itemGroups.flatMap(g => g.fields).find(f => f.name === name)!;
const source = '#PVF_File\r\n[name]\r\n`装备😀` # 名称\r\n[equipment physical attack]\r\n100 200\r\n[if]\r\n[cooltime]\r\n3000\r\n[/if]\r\n[then]\r\n[physical attack]\r\n10\r\n[/then]\r\n[custom]\r\n{5=1234} # 保留\r\n';
describe('物品 GUI 精确编辑', () => {
  it('追加 GUI provider，识别大小写扩展名', () => {
    for (const path of ['equipment/A.EQU', 'stackable/a.stk']) expect(getGUIProvider({ index: 0, path, text: '', editable: true })?.id).toBe('item');
    expect(getGUIProvider({ index: 0, path: 'a.equ.txt', text: '', editable: true })).toBeNull();
  });
  it('修改多值属性保留触发器、未知类型、注释和 CRLF', () => {
    expect(editItem(source, field('equipment physical attack'), ['150', '250'])).toBe(source.replace('100 200', '150 250'));
    expect(itemSection(parseItem(source), 'physical attack')).toBeUndefined();
    expect(editItem(source, field('physical attack'), ['30'])).toBe(source + '[physical attack]\r\n\t30\r\n');
  });
  it('同名顶层和嵌套字段互不覆盖，保留闭合段', () => {
    const text = '[physical attack]\n20\n[then]\n[physical attack]\n50\n[/then]\n';
    expect(editItem(text, field('physical attack'), ['40'])).toBe(text.replace('20', '40'));
    const job = '[usable job]\n`[swordman]` # 职业\n`[mage]`\n[/usable job]\n';
    expect(editItem(job, field('usable job'), ['[fighter]', '[mage]'])).toBe(job.replace('[swordman]', '[fighter]'));
  });
  it('保留多行字符串、转义反引号和类型标记', () => {
    const text = '[basic explain]\n{8=`原文``😀\n[rarity]\n5`} # 注释\n[rarity]\n3';
    expect(editItem(text, field('basic explain'), ['新`说明\n第二行'])).toBe(text.replace('{8=`原文``😀\n[rarity]\n5`}', '{8=`新``说明\n第二行`}'));
    expect(editItem(text, field('rarity'), ['4'])).toBe(text.replace(/3$/, '4'));
    expect(editItem(text, field('basic explain'), ['原文`😀\n[rarity]\n5'])).toBe(text);
  });
  it('保护字符串引用、重复字段、嵌套字段和特殊类型', () => {
    for (const text of ['[name]\n`a`\n[NAME]\n`b`', '[name]\n{10=`<31::key>`}', '[name]\n`<31::key>`', '[name]\n{5=42}', '[name]\n[inner]\n`x`\n[/inner]\n[/name]']) {
      expect(itemFieldIssue(parseItem(text), field('name'))).not.toBe('');
      expect(() => editItem(text, field('name'), ['新名称'])).toThrow();
    }
  });
  it('拒绝破损结构、整数溢出和形状改变', () => {
    for (const text of ['[if]\n[physical attack]\n10', '[then]\n[physical attack]\n10', '[name]\n`x', '[name', '[name]\n{8=`x`', '[/then]', '[if]\n[name]\n`x`\n[/then]']) expect(() => parseItem(text)).toThrow();
    for (const value of ['', '1.5', '2147483648', '-2147483649', 'NaN', '1e4']) expect(() => editItem(source, field('rarity'), [value])).toThrow();
    expect(() => editItem(source, field('equipment physical attack'), ['3'])).toThrow('参数数量');
    expect(editItem(source, field('rarity'), ['0'])).toContain('[rarity]\r\n\t0\r\n');
  });
  it('图像与类型的混合参数分别校验，新增字段放在文件末尾', () => {
    const icon = field('icon');
    expect(editItem('[icon]\n`a.img` 1 # 图标\n', icon, ['b.img', '9'])).toBe('[icon]\n`b.img` 9 # 图标\n');
    expect(() => editItem(source, icon, ['a.img', '1.2'])).toThrow();
    expect(editItem('[name]\n`a`', field('equipment type'), ['[weapon]', '3'])).toBe('[name]\n`a`\n[equipment type]\n\t`[weapon]`\t3\n');
  });
  it('忽略行注释与块注释中的标签和反引号，精确保留注释', () => {
    const text = '// [name] `oops\n[name]\n`名称` /* [name] */\n[price]\n10// [then]\n/* `未闭合字符串\n[price] */\n';
    expect(editItem(text, field('price'), ['20'])).toBe(text.replace('10//', '20//'));
    expect(() => parseItem('[name]\n`x` /* 未闭合')).toThrow('注释未闭合');
  });
  it('忽略注释中标签，嵌套效果之后仍能编辑普通字段', () => {
    const text = source + '# [name] [/bad]\r\n[minimum level]\r\n90\r\n';
    expect(editItem(text, field('minimum level'), ['110'])).toBe(text.replace('90\r\n', '110\r\n'));
    const custom: ItemField = { name: 'custom', label: '', kinds: ['integer'] };
    expect(() => editItem(text, custom, ['2'])).toThrow('特殊类型');
  });
});
