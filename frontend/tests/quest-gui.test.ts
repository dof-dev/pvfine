import { describe, expect, it } from 'vitest';
import { editQuest, parseQuest, questValues, questAnnotationText, questDisplayValue, questReference, questReferenceAnnotation } from '../src/gui/quest';
import type { EditorAnnotation } from '../bindings/pvfine/services/models';
import { getGUIProvider } from '../src/gui/registry';
const source = '#PVF_File\n[name]\n`任务😀` # 名称\n[type]\n`[seeking]`\n[sub type]\n-1\n[int data]\n100 2 # 第一组\n200 3\n[/int data]\n[custom]\n{8=`<1::text>`}\n';
describe('任务 GUI', () => {
  it('识别 qst 并读取草稿，忽略字符串内标签', () => {
    expect(getGUIProvider({ index: 1, path: 'quest/TEST.QST', text: source, editable: true })?.id).toBe('quest');
    const doc = parseQuest(source);
    expect(questValues(doc, 'name')).toEqual(['任务😀']);
    expect(questValues(doc, 'type')).toEqual(['[seeking]']);
    expect(questValues(doc, 'int data')).toEqual(['100', '2', '200', '3']);
    expect(doc.sections).toHaveLength(5);
  });
  it('仅修改目标 token，保留未知字段、注释、空白及 CRLF', () => {
    const text = source.replaceAll('\n', '\r\n');
    expect(editQuest(text, [{ name: 'int data', values: ['100', '9', '200', '3'], kind: 'integer' }])).toBe(text.replace('100 2', '100 9'));
    expect(editQuest(text, [{ name: 'name', values: ['新`任务😀\n对话'], kind: 'string' }])).toBe(text.replace('`任务😀`', '`新``任务😀\n对话`'));
  });
  it('增删列表参数保留闭合标签和注释，支持空字段', () => {
    const added = editQuest(source, [{ name: 'int data', values: ['100', '2', '200', '3', '300', '4'], kind: 'integer' }]);
    expect(added).toContain('200 3\t300\t4\n[/int data]');
    const removed = editQuest(source, [{ name: 'int data', values: [], kind: 'integer' }]);
    expect(removed).toContain('# 第一组');
    expect(removed).toContain('[/int data]');
    expect(questValues(parseQuest(removed), 'int data')).toEqual([]);
    expect(questValues(parseQuest(editQuest('[int data]\n# keep\n[/int data]', [{ name: 'int data', values: ['1', '2'], kind: 'integer' }])), 'int data')).toEqual(['1', '2']);
  });
  it('新增字段追加到末尾，切换类型保留原始参数', () => {
    const added = editQuest(source, [{ name: 'reward selection int data', values: ['100', '1'], kind: 'integer', paired: true }]);
    expect(added.startsWith(source)).toBe(true);
    expect(added).toContain('[reward selection int data]\n\t100\t1\n[/reward selection int data]');
    const changed = editQuest(source, [{ name: 'type', values: ['[condition under clear]'], kind: 'string' }, { name: 'sub type', values: ['6'], kind: 'integer' }]);
    expect(questValues(parseQuest(changed), 'sub type')).toEqual(['6']);
    expect(questValues(parseQuest(changed), 'int data')).toEqual(['100', '2', '200', '3']);
  });
  it('重复字段、字符串引用和特殊类型禁止覆盖', () => {
    expect(() => editQuest('[name]\n`a`\n[name]\n`b`', [{ name: 'name', values: ['c'], kind: 'string' }])).toThrow('重复字段');
    const ref = '[name]\n{8=`<1::name>`}\n[level]\n1 120';
    expect(() => editQuest(ref, [{ name: 'name', values: ['c'], kind: 'string' }])).toThrow('字符串引用');
    expect(editQuest(ref, [{ name: 'level', values: ['10', '120'], kind: 'integer' }])).toContain('{8=`<1::name>`}');
    expect(() => editQuest('[level]\n`1`', [{ name: 'level', values: ['2'], kind: 'integer' }])).toThrow('类型不匹配');
  });
  it('拒绝未闭合、交叉闭合、非整数和溢出', () => {
    for (const text of ['[name]\n`oops', '[name', '[outer]\n[inner]\n1\n[/outer]\n[/inner]', '[name]\n{8=`x`']) expect(() => parseQuest(text)).toThrow();
    for (const value of ['', '1.5', 'NaN', '2147483648', '-2147483649']) expect(() => editQuest(source, [{ name: 'sub type', values: [value], kind: 'integer' }])).toThrow();
  });
  it('嵌套区块只读保留，子字段不会覆盖顶层字段', () => {
    const text = '[name]\n`root`\n[change depend on grow job]\n1\n[name]\n`nested`\n[solve message]\n`dialogue`\n[/change depend on grow job]\n[level]\n1 115';
    const doc = parseQuest(text);
    expect(questValues(doc, 'name')).toEqual(['root']);
    expect(questValues(doc, 'solve message')).toEqual([]);
    expect(doc.sections.find(s => s.name === 'change depend on grow job')?.issue).toContain('嵌套');
    expect(() => editQuest(text, [{ name: 'change depend on grow job', kind: 'integer', values: ['2'] }])).toThrow('嵌套');
    expect(editQuest(text, [{ name: 'level', kind: 'integer', values: ['2', '115'] }])).toBe(text.replace('1 115', '2 115'));
    const paired = '[outer]\n[inner]\n1\n[/inner]\n[other]\n2\n[/outer]\n[level]\n3';
    expect(parseQuest(paired).sections.map(s => s.name)).toEqual(['outer', 'level']);
    expect(editQuest(paired, [{ name: 'level', kind: 'integer', values: ['4'] }])).toBe(paired.replace('[level]\n3', '[level]\n4'));
  });
  it('正确处理反引号转义、多行文本和注释中的标签', () => {
    const text = '# [name]\n[name]\n`a``b\n[other]`\n[level]\n1 120';
    expect(questValues(parseQuest(text), 'name')).toEqual(['a`b\n[other]']);
    expect(editQuest(text, [{ name: 'level', values: ['5', '120'], kind: 'integer' }])).toBe(text.replace('1 120', '5 120'));
  });
});

describe('110+ 任务字符串', () => {
  const annotation: EditorAnnotation = { start: 0, end: 1, title: '冒险任务', content: '', type: 'placeholder', targetFileIndex: -1, rarity: -1, placeholder: { tableIndex: 31, key: 'quest_name' } };
  it('解析 8/10 引用和字面文本，忽略引用内容中的标签及花括号', () => {
    const text = '[name]\n{8=`<31::quest_name>`}\n[depend message]\n{10=`<31::quest_dialogue>`}\n[grade]\n{10=`[daily]`}\n[solve message]\n{8=`文字``与}\n[标签]`}';
    const doc = parseQuest(text);
    expect(questValues(doc, 'grade')).toEqual(['[daily]']);
    expect(doc.sections[0]?.tokens[0]).toMatchObject({ markerType: 8, kind: 'string', reference: { tableIndex: 31, key: 'quest_name' } });
    expect(doc.sections[1]?.tokens[0]?.markerType).toBe(10);
    expect(questValues(doc, 'solve message')).toEqual(['文字`与}\n[标签]']);
    expect(questReference('< 31 :: quest_name >')).toEqual({ tableIndex: 31, key: 'quest_name' });
  });
  it('编辑字面文本保留 8/10 类型、转义、注释和其他引用', () => {
    const text = '[name]\r\n{8=`<31::quest_name>`}\r\n[grade]\r\n{10= `[daily]` } # keep\r\n[solve message]\r\n{8=`原文`}';
    expect(editQuest(text, [{ name: 'grade', kind: 'string', values: ['[epic]'] }])).toBe(text.replace('{10= `[daily]` }', '{10=`[epic]`}'));
    expect(editQuest(text, [{ name: 'grade', kind: 'string', values: ['[daily]'] }])).toBe(text);
    expect(editQuest(text, [{ name: 'solve message', kind: 'string', values: ['新`文本\n下一行'] }])).toBe(text.replace('{8=`原文`}', '{8=`新``文本\n下一行`}'));
  });
  it('引用禁止改写为内联文本或删除，数值编辑保留原始标记', () => {
    for (const marker of ['{8=`<31::quest_name>`}', '{10=`<31::quest_name>`}', '`<31::quest_name>`']) {
      const text = `[name]\n${marker}\n[level]\n1 120`;
      expect(() => editQuest(text, [{ name: 'name', kind: 'string', values: ['替换'] }])).toThrow('修改译文');
      expect(() => editQuest(text, [{ name: 'name', kind: 'string', values: [] }])).toThrow('修改译文');
      expect(editQuest(text, [{ name: 'level', kind: 'integer', values: ['5', '120'] }])).toBe(text.replace('1 120', '5 120'));
    }
  });
  it('按表号和键解析译文，摆脱 DSL 改动后的过期位置', () => {
    const doc = parseQuest('[name]\n{10=`<31::quest_name>`}');
    expect(questDisplayValue(doc, 'name', [annotation])).toBe('冒险任务');
    expect(questReferenceAnnotation({ tableIndex: 32, key: 'quest_name' }, [annotation])).toBeUndefined();
    expect(questDisplayValue(doc, 'name', [])).toBe('<31::quest_name>');
    const fallback = { ...annotation, title: '冒险任务（未翻译）', placeholder: { ...annotation.placeholder!, fallback: true } };
    expect(questAnnotationText(fallback)).toBe('冒险任务');
    expect(questAnnotationText({ ...annotation, title: '原文（未翻译）' })).toBe('原文（未翻译）');
    expect(questAnnotationText({ ...annotation, placeholder: { ...annotation.placeholder!, missing: true } })).toBe('');
  });
});
