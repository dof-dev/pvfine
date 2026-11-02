import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { addCraneItem, editCraneItem, editCraneMaterial, parseCraneMinigame, removeCraneItem } from '../src/gui/craneMinigame';
import { getGUIProvider } from '../src/gui/registry';

const values = { id: 10000006, count: 2, viewRatio: 43.96, pickRatio: 90 };
const block = '[item]\n\t10000006 // 物品 ID 😀\n\t[cnt]\n\t\t2 // 数量\n\t[viewRatio]\n\t\t43.96\n\t[pickRatio]\n\t\t90\n[/item]';
const tail = '[material]\n\t2660547\t1 // 启动材料\n[send mail]\n[inven]1\n[detail]0 `消息`[/detail]\n[/send mail]\n[need material]3332 5[/need material]\n';
const fixture = '# 注释\n[viewCnt]6\n' + block + '\n' + block + '\n' + tail;

describe('娃娃机 GUI', () => {
  for (const eol of ['\n', '\r\n', '\r']) {
    it(`按记录编辑并保留其他 section、注释和 ${JSON.stringify(eol)}`, () => {
      const text = fixture.replaceAll('\n', eol), doc = parseCraneMinigame(text);
      expect(doc.items).toHaveLength(2);
      expect(editCraneItem(text, 0, values)).toBe(text);
      const expected = block.replace('43.96', '12.5').replaceAll('\n', eol);
      expect(editCraneItem(text, 1, { ...values, viewRatio: 12.5 })).toBe(text.slice(0, doc.items[1]!.section.start) + expected + text.slice(doc.items[1]!.section.end));
      expect(editCraneMaterial(text, 42, 3)).toBe(text.replace('2660547\t1', '42\t3'));
      const added = addCraneItem(text, { ...values, id: 42 });
      expect(parseCraneMinigame(added).items.map(i => i.fields[0].value)).toEqual(['10000006', '10000006', '42']);
      expect(added).toContain(tail.replaceAll('\n', eol));
      expect(removeCraneItem(text, 1)).toBe(text.slice(0, doc.items[1]!.section.start) + text.slice(doc.items[1]!.section.end));
    });
  }
  it('整数值保留原浮点类型，浮点几率支持小数', () => {
    const edited = editCraneItem(fixture, 0, { ...values, viewRatio: 50, pickRatio: 50.5 });
    expect(parseCraneMinigame(edited).items[0]!.fields.map(t => t.value)).toEqual(['10000006', '2', '50.0', '50.5']);
  });
  it('空文件可新增奖品和材料', () => {
    expect(parseCraneMinigame(addCraneItem('', values)).items).toHaveLength(1);
    expect(parseCraneMinigame(editCraneMaterial('', 42, 1)).material.map(t => t.value)).toEqual(['42', '1']);
  });
  it('未知奖品字段编辑时保留，删除时要求 DSL 确认', () => {
    const text = fixture.replace('[/item]', '[extra]`保留`[/item]');
    expect(editCraneItem(text, 0, { ...values, count: 3 })).toContain('[extra]`保留`');
    expect(() => removeCraneItem(text, 0)).toThrow('其他字段');
  });
  it('拒绝损坏结构、特殊类型和越界值', () => {
    for (const text of [fixture.replace('[/item]', ''), fixture.replace('[cnt]', '[pickRatio]'), fixture.replace('43.96', '`43.96`'), fixture + '[material]1 2', fixture.replace('43.96', 'NaN'), fixture.replace('2 // 数量', '2.5')]) expect(() => parseCraneMinigame(text)).toThrow();
    for (const invalid of [{ count: 1.5 }, { id: -1 }, { id: 2147483648 }, { pickRatio: 101 }, { viewRatio: -1 }, { viewRatio: Infinity }]) expect(() => editCraneItem(fixture, 0, { ...values, ...invalid })).toThrow();
    expect(() => removeCraneItem(fixture, 9)).toThrow();
  });
  it('只匹配目标路径，入口追加在现有工具后', () => {
    const file = { index: 0, path: '\\ETC\\CRANEMINIGAMEITEM.ETC', text: '', editable: true };
    expect(getGUIProvider(file)?.id).toBe('crane-minigame');
    expect(getGUIProvider({ ...file, path: 'etc/other.etc' })).toBeNull();
  });
  it.skipIf(!process.env.PVF_GUI_CRANE_FIXTURE)('真实归档全部奖品可无损编辑、增删，保留尾部配置', () => {
    const text = readFileSync(process.env.PVF_GUI_CRANE_FIXTURE!, 'utf8'), doc = parseCraneMinigame(text);
    expect(doc.items.length).toBeGreaterThan(100);
    expect(doc.material.map(t => t.value)).toEqual(['2660547', '1']);
    for (const [index, item] of doc.items.entries()) {
      const [id, count, viewRatio, pickRatio] = item.fields.map(t => Number(t.value));
      expect(editCraneItem(text, index, { id: id!, count: count!, viewRatio: viewRatio!, pickRatio: pickRatio! })).toBe(text);
      const field = item.fields[1];
      expect(editCraneItem(text, index, { id: id!, count: count! + 1, viewRatio: viewRatio!, pickRatio: pickRatio! })).toBe(text.slice(0, field.start) + String(count! + 1) + text.slice(field.end));
    }
    const added = addCraneItem(text, values);
    expect(parseCraneMinigame(added).items).toHaveLength(doc.items.length + 1);
    expect(added.slice(added.indexOf('[material]'))).toBe(text.slice(text.indexOf('[material]')));
    expect(parseCraneMinigame(removeCraneItem(text, 0)).items).toHaveLength(doc.items.length - 1);
  });
});
