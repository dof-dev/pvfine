/** Quest fields use source ranges so unrelated sections and comments survive edits. */
import type { EditorAnnotation } from '../../bindings/pvfine/services/models';
export interface QuestReference { tableIndex: number; key: string }
export interface QuestToken { start: number; end: number; value: string; kind: 'string' | 'integer' | 'unsupported'; markerType?: number; reference?: QuestReference }
export function questReference(value: string): QuestReference | undefined {
  const match = /^<\s*(\d+)\s*::([^<>]+)>$/.exec(value);
  if (!match || Number(match[1]) > 2147483647 || !match[2]!.trim()) return;
  return { tableIndex: Number(match[1]), key: match[2]!.trim() };
}
export function questReferenceAnnotation(reference: QuestReference, annotations: EditorAnnotation[]): EditorAnnotation | undefined {
  return annotations.find((a) => a.placeholder?.tableIndex === reference.tableIndex && a.placeholder.key === reference.key);
}
export function questAnnotationText(annotation: EditorAnnotation): string {
  if (annotation.placeholder?.missing) return '';
  const suffix = '（未翻译）';
  return annotation.placeholder?.fallback && annotation.title.endsWith(suffix) ? annotation.title.slice(0, -suffix.length) : annotation.title;
}
export function questDisplayValue(doc: QuestDocument, name: string, annotations: EditorAnnotation[]): string {
  const token = questField(doc, name)?.tokens[0];
  if (!token) return '';
  const annotation = token.reference && questReferenceAnnotation(token.reference, annotations);
  return annotation ? questAnnotationText(annotation) : token.value;
}
export interface QuestSection { name: string; start: number; body: number; end: number; paired: boolean; tokens: QuestToken[]; issue: string }
export interface QuestDocument { sections: QuestSection[] }
export type QuestTargetKind = 'dungeon' | 'monster' | 'npc';
export interface QuestCondition { value: string; label: string; type: string; subType: number; fields: string[]; repeat?: boolean; queries?: Record<number, QuestTargetKind> }
export const questConditions: QuestCondition[] = [
  { value: 'hunt', label: '杀怪', type: '[hunt monster]', subType: -1, fields: ['副本 ID', '难度', '怪物 ID', '数量'], repeat: true, queries: { 0: 'dungeon', 2: 'monster' } },
  { value: 'map', label: '通关房间', type: '[clear map]', subType: -1, fields: ['地图 ID'] },
  { value: 'seeking', label: '收集物品', type: '[seeking]', subType: -1, fields: ['物品 ID', '数量'], repeat: true },
  { value: 'npc', label: '对话', type: '[meet npc]', subType: -1, fields: ['NPC ID'], queries: { 0: 'npc' } },
  { value: 'clear', label: '通关副本', type: '[condition under clear]', subType: 6, fields: ['副本 ID', '难度'], queries: { 0: 'dungeon' } },
  { value: 'rooms', label: '通关房间数量', type: '[condition under clear]', subType: 11, fields: ['副本 ID', '难度', '房间数量'] },
  { value: 'time', label: '限时通关', type: '[condition under clear]', subType: 0, fields: ['副本 ID', '难度', '时间（秒）'], queries: { 0: 'dungeon' } },
  { value: 'party', label: '组队通关', type: '[condition under clear]', subType: 5, fields: ['副本 ID', '难度', '人数'], queries: { 0: 'dungeon' } },
];
export const questRewards = [
  { value: '[item]', label: '道具', fields: ['物品 ID', '数量'], repeat: true },
  { value: '[title]', label: '称号', fields: ['称号 ID', '数量'], repeat: false },
  { value: '[grow type]', label: '转职', fields: ['转职 ID'], repeat: false },
  { value: '[awakening type]', label: '觉醒', fields: ['觉醒次数'], repeat: false },
];
export const questGrades = [
  { label: '每日任务', value: '[daily]' }, { label: '重复任务', value: '[normaly repeat]' },
  { label: '普通任务', value: '[common unique]' }, { label: '成就任务', value: '[achievement]' }, { label: '主线任务', value: '[epic]' },
];
export function parseQuest(text: string): QuestDocument {
  const sections: QuestSection[] = [];
  const open: QuestSection[] = [];
  let current: QuestSection | undefined;
  let i = 0;
  function stringEnd(start: number): number {
    let p = start + 1;
    while (p < text.length) {
      if (text[p++] !== '`') continue;
      if (text[p] === '`') { p++; continue; }
      return p;
    }
    throw new Error('存在未闭合的文本，请先在 DSL 中修复');
  }
  while (i < text.length) {
    if (/\s/.test(text[i]!)) { i++; continue; }
    if (text[i] === '#') { const end = text.indexOf('\n', i); i = end < 0 ? text.length : end; continue; }
    const start = i;
    if (text[i] === '[') {
      const end = text.indexOf(']', i);
      if (end < 0) throw new Error('存在未闭合的字段标签，请先在 DSL 中修复');
      const name = text.slice(i + 1, end).trim();
      if (name.startsWith('/')) {
        const match = open.findLastIndex(s => s.name === name.slice(1).trim());
        if (match < 0) throw new Error('字段闭合或嵌套结构无法识别，请在 DSL 中编辑');
        const section = open[match]!;
        section.end = i;
        section.paired = true;
        const position = sections.indexOf(section);
        // Preserve nested blocks as opaque, read-only root fields. Their
        // children must never shadow the quest's name, type or reward fields.
        if (position < sections.length - 1) {
          section.issue = '包含嵌套字段，请在 DSL 中编辑';
          sections.splice(position + 1);
        }
        open.splice(match);
        current = undefined;
      } else {
        if (current) current.end = i;
        current = { name, start, body: end + 1, end: text.length, paired: false, tokens: [], issue: '' };
        sections.push(current);
        open.push(current);
      }
      i = end + 1; continue;
    }
    let kind: QuestToken['kind'] = 'unsupported';
    let markerType: number | undefined;
    let value: string;
    if (text[i] === '`') {
      i = stringEnd(i); kind = 'string'; value = text.slice(start + 1, i - 1).replaceAll('``', '`');
    } else if (text[i] === '{') {
      i++;
      while (i < text.length && text[i] !== '}') { i = text[i] === '`' ? stringEnd(i) : i + 1; }
      if (i >= text.length) throw new Error('存在未闭合的类型标记，请先在 DSL 中修复');
      i++; value = text.slice(start, i);
      const marker = /^\{(5|7|8|10)=\s*`((?:[^`]|``)*)`\s*\}$/.exec(value);
      if (marker) { kind = 'string'; markerType = Number(marker[1]); value = marker[2]!.replaceAll('``', '`'); }
    } else {
      while (i < text.length && !/[\s`\[{#]/.test(text[i]!)) i++;
      value = text.slice(start, i);
      if (/^[+-]?\d+$/.test(value)) kind = 'integer';
    }
    current?.tokens.push({ start, end: i, value, kind, markerType, reference: kind === 'string' ? questReference(value) : undefined });
  }
  for (const section of sections) {
    if (section.issue) continue;
    if (sections.filter((s) => s.name === section.name).length > 1) section.issue = '存在重复字段，请在 DSL 中编辑';
    else if (section.tokens.some((t) => t.kind === 'unsupported')) section.issue = '包含字符串引用或特殊类型，请在 DSL 中编辑';
  }
  return { sections };
}
export function questField(doc: QuestDocument, name: string): QuestSection | undefined { return doc.sections.find((s) => s.name === name); }
export function questValues(doc: QuestDocument, name: string): string[] { return questField(doc, name)?.tokens.map((t) => t.value) ?? []; }
export interface QuestChange { name: string; values: string[]; kind: 'string' | 'integer'; paired?: boolean }
export function editQuest(text: string, changes: QuestChange[]): string {
  const doc = parseQuest(text);
  const newline = text.includes('\r\n') ? '\r\n' : '\n';
  const patches: { start: number; end: number; value: string }[] = [];
  let additions = '';
  const seen = new Set<string>();
  for (const change of changes) {
    if (seen.has(change.name)) throw new Error('一次操作不能重复修改同一字段');
    seen.add(change.name);
    const section = questField(doc, change.name);
    if (section?.issue) throw new Error(`[${change.name}] ${section.issue}`);
    if (section?.tokens.some((t) => t.reference)) throw new Error(`[${change.name}] 请通过修改译文编辑字符串引用`);
    if (section?.tokens.some((t) => t.kind !== change.kind)) throw new Error(`[${change.name}] 数据类型不匹配，请在 DSL 中编辑`);
    const values = change.values.map((v) => {
      if (change.kind === 'string') return '`' + v.replaceAll('`', '``') + '`';
      if (!/^[+-]?\d+$/.test(v) || Number(v) < -2147483648 || Number(v) > 2147483647) throw new Error('请输入 32 位范围内的整数');
      return v;
    });
    if (!section) {
      if (values.length) additions += `${newline}[${change.name}]${newline}\t${values.join('\t')}${newline}${change.paired ? `[/${change.name}]${newline}` : ''}`;
      continue;
    }
    section.tokens.forEach((token, index) => {
      if (change.values[index] === token.value) return;
      const formatted = values[index];
      const value = formatted === undefined ? '' : token.markerType === undefined ? formatted : `{${token.markerType}=${formatted}}`;
      if (text.slice(token.start, token.end) !== value) patches.push({ start: token.start, end: token.end, value });
    });
    if (values.length > section.tokens.length) {
      const position = section.tokens.at(-1)?.end ?? section.body;
      patches.push({ start: position, end: position, value: `${section.tokens.length ? '\t' : newline + '\t'}${values.slice(section.tokens.length).join('\t')}${section.tokens.length ? '' : newline}` });
    }
  }
  let result = text;
  for (const patch of patches.sort((a, b) => b.start - a.start || b.end - a.end)) result = result.slice(0, patch.start) + patch.value + result.slice(patch.end);
  return result + (additions ? (result.endsWith('\n') || !result ? '' : newline) + additions : '');
}
