import { parseItem, type ItemSection, type ItemToken } from './item';

export type EffectResourceKind = 'object' | 'particle' | 'appendage' | 'monster' | 'apc';
export interface TriggerParameter { label: string; kind: 'integer' | 'number' | 'string'; default: string; resource?: EffectResourceKind }
export interface TriggerClauseSpec { name: string; label: string; scope: 'if' | 'then'; params: TriggerParameter[]; paired?: boolean; repeat?: boolean; counts?: number[] }
const n = (label: string, value = '0'): TriggerParameter => ({ label, kind: 'integer', default: value });
const s = (label: string, value: string): TriggerParameter => ({ label, kind: 'string', default: value });
const resource = (label: string, kind: EffectResourceKind): TriggerParameter => ({ ...s(label, ''), kind: kind === 'particle' ? 'string' : 'integer', resource: kind });
const spec = (scope: 'if' | 'then', name: string, label: string, params: TriggerParameter[], extra: Partial<TriggerClauseSpec> = {}): TriggerClauseSpec => ({ scope, name, label, params, ...extra });
export const triggerClauses: TriggerClauseSpec[] = [
  ...['attack success', 'event attack success', 'hit', 'casting', 'set my state', 'use command'].map((name, i) => spec('if', name, ['攻击命中', '攻击命中事件', '被攻击', '施放技能', '检测自身状态', '装备操作键'][i]!, [n('启用', '1')])),
  ...['my state', 'my attack state', 'target state', 'my active status', 'target active status', 'target type', 'target grade', 'element'].map((name, i) => spec('if', name, ['自身动作状态', '自身攻击状态', '目标动作状态', '自身异常状态', '目标异常状态', '目标种族', '目标品级', '属性条件'][i]!, [s('类型', ['attack', 'attack', 'down', 'bleeding', 'poison', 'human', 'boss', 'fire'][i]!)])),
  ...['use skill', 'event use skill'].map(name => spec('if', name, name === 'use skill' ? '使用指定技能' : '使用技能事件', [s('职业（原始 token）', 'swordman'), n('技能 ID')], { paired: true, repeat: true })),
  spec('if', 'time', '周期检测', [n('间隔（毫秒）', '1000'), n('启用', '1'), n('延迟（毫秒）', '0')], { counts: [2, 3] }),
  ...['cooltime', 'start cooltime'].map(name => spec('if', name, name === 'cooltime' ? '触发冷却' : '装备起始冷却', [n('时间（毫秒）', '1000')])),
  ...['my appendage', 'not my appendage', 'my appendage count'].map((name, i) => spec('if', name, ['自身有附加物', '自身无附加物', '附加物层数'][i]!, [resource('附加物 ID', 'appendage'), ...(i === 2 ? [n('层数', '1')] : [])])),
  spec('if', 'stat change', 'HP / MP 阈值', [s('属性', 'hp'), s('比较符', '>'), s('单位', '%'), n('阈值', '0')]),
  ...['distance', 'my prev state', 'attack condition', 'attack type'].map((name, i) => spec('if', name, ['目标距离', '自身上一状态', '攻击条件', '伤害类型'][i]!, [s('类型', ['far', 'dash', 'counter', 'physical'][i]!)])),
  ...['combo', 'after attack', 'revive'].map((name, i) => spec('if', name, ['连击数', '攻击后', '复活'][i]!, [n('数值', i === 0 ? '30' : '1')])),
  spec('if', 'keep my state', '持续保持状态', [s('状态', 'stand'), n('毫秒', '10000')]),
  spec('if', 'check equipment rarity', '装备品级条件', [s('部位', 'weapon'), s('比较符', '='), n('品级', '2')]),
  spec('if', 'monster exists', '场上怪物数量', [resource('怪物 ID', 'monster'), s('比较符', '<'), n('数量', '1')]),
  spec('if', 'passive object exists', '场上被动对象条件', [resource('对象 ID', 'object'), s('比较符', '<'), n('时间（毫秒）', '3000')]),
  spec('if', 'check damage', '单次伤害条件', [s('单位', '%'), n('数值', '10')]),
  spec('if', 'party count', '队伍人数', [s('比较符', '='), n('人数', '1')]),
  spec('then', 'target', '动作目标', [s('目标', 'myself'), n('范围 / 参数', '-1')]),
  ...['probability', 'then probability'].map(name => spec('then', name, name === 'probability' ? '触发概率' : '分支概率', [{ ...n('概率（原始值）', '100'), kind: 'number' }])),
  ...['duration', 'equipment duration'].map(name => spec('then', name, name === 'duration' ? '持续时间' : '装备效果持续时间', [n('时间（毫秒）', '3000')])),
  spec('then', 'passive object', '生成被动对象', [resource('对象 ID', 'object'), n('X'), n('Y'), n('Z'), n('等级', '80'), n('跟随参数'), resource('粒子路径（可留空）', 'particle')], { paired: true, repeat: true }),
  ...['stat', 'stat by condition'].map(name => spec('then', name, name === 'stat' ? '属性加成' : '条件属性加成', [s('属性', 'attack speed'), s('单位', '+'), n('数值', '10')], { repeat: true })),
  spec('then', 'add absolute damage', '追加伤害', [s('属性', 'all'), s('运算符', '%'), n('数值', '10')]),
  ...['unique increase damage', 'unique increase critical damage'].map(name => spec('then', name, name === 'unique increase damage' ? '独特增伤' : '独特暴伤', [n('索引'), s('运算符', '%'), n('数值', '10')], { repeat: true })),
  ...['appendage', 'remove appendage', 'appendage unique'].map((name, i) => spec('then', name, ['施加附加物', '移除附加物', '施加唯一附加物'][i]!, [resource('附加物 ID', 'appendage')], { repeat: true })),
  spec('then', 'consume item', '消耗道具', [n('道具 ID'), n('数量', '1')], { repeat: true }),
  spec('then', 'restore', '恢复 HP / MP', [s('资源', 'hp'), s('单位', '%'), n('数值', '10'), n('恢复时间（毫秒）')]),
  spec('then', 'active status', '异常状态', [s('状态', 'stun'), n('等级', '50'), n('参数 2'), n('参数 3'), n('参数 4'), n('参数 5')], { counts: [2, 3, 4, 6] }),
  spec('then', 'buff', '增益状态', [s('类型', 'superarmor'), n('等级', '1')], { counts: [1, 2] }),
  spec('then', 'weakness', '弱化效果', [n('数值', '75'), n('第二值', '10')], { counts: [1, 2] }),
  spec('then', 'elemental weapon', '赋予武器属性', [s('属性', 'light')]),
  spec('then', 'add absolute defense percent', '追加防御百分比', [s('属性', 'all'), n('数值', '10')]),
  ...['summon monster', 'summon apc'].map(name => spec('then', name, name === 'summon monster' ? '召唤怪物' : '召唤 APC', [resource('召唤 ID', name === 'summon monster' ? 'monster' : 'apc'), n('等级', '-1'), n('数量', '1')])),
  spec('then', 'speech on', '头顶台词', [n('参数'), n('显示时间（毫秒）', '2000'), s('台词', '')], { paired: true }),
  spec('then', 'perform skill', '执行技能', [s('职业', 'swordman'), n('技能 ID'), n('等级', '1'), n('参数')]),
  spec('then', 'apply status in range', '范围内应用状态', [s('状态种类', 'hp'), s('数值单位', '%')]),
  spec('then', 'stat change by stat', '按属性改变属性', [s('目标', 'myself'), n('半径', '-1'), s('属性', 'hp max'), n('数值', '4000')]),
  spec('then', 'appendage control', '附加物控制', [s('属性名', 'add condition increase damage'), s('类型', 'string'), n('索引', '3'), n('数值', '5')]),
];
export interface ItemTrigger { condition: ItemSection; response: ItemSection; branches: ItemSection[]; multiple: boolean }
export function readItemTriggers(text: string): { triggers: ItemTrigger[]; issues: string[] } {
  const triggers: ItemTrigger[] = [], issues: string[] = [];
  try {
    const sections = parseItem(text);
    for (let i = 0; i < sections.length; i++) {
      const condition = sections[i]!;
      if (condition.name === 'if') {
        const response = sections[i + 1];
        if (!response || !['then', 'multiple then'].includes(response.name)) { issues.push('[if] 后缺少相邻的 [then] 或 [multiple then]，请在 DSL 中修复'); continue; }
        const multiple = response.name === 'multiple then', branches = multiple ? response.children.filter(c => c.name === 'then') : [response];
        if (!branches.length || condition.tokens.length || response.tokens.length || (multiple && response.children.some(c => c.name !== 'then'))) { issues.push('触发器包含无法识别的容器内容，请在 DSL 中编辑'); i++; continue; }
        triggers.push({ condition, response, branches, multiple }); i++;
      } else if (condition.name === 'then' || condition.name === 'multiple then') issues.push(`存在未配对的 [${condition.name}]，请在 DSL 中修复`);
    }
  } catch (cause) { issues.push(String(cause)); }
  return { triggers, issues };
}
export function triggerClauseSpec(scope: 'if' | 'then', section: ItemSection): TriggerClauseSpec | undefined {
  return triggerClauses.find(c => c.scope === scope && c.name === section.name);
}
function validValue(value: string, parameter: TriggerParameter): boolean {
  if (parameter.kind === 'string') return true;
  if (parameter.kind === 'integer') return /^[+-]?\d+$/.test(value) && Number(value) >= -2147483648 && Number(value) <= 2147483647 && (!parameter.resource || Number(value) >= 0);
  return /^[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:e[+-]?\d+)?$/i.test(value) && Number.isFinite(Number(value)) && Math.abs(Number(value)) <= 3.4028234663852886e38;
}
function validateRows(spec: TriggerClauseSpec, rows: string[][]) {
  if (!rows.length || (!spec.repeat && rows.length !== 1)) throw new Error('参数记录数量无效');
  for (const row of rows) {
    if (!(spec.counts ?? [spec.params.length]).includes(row.length) || row.some((v, i) => !validValue(v, spec.params[i]!))) throw new Error('参数数量或类型无效，数值需在 32 位范围内');
  }
}
export function triggerClauseRows(section: ItemSection, spec: TriggerClauseSpec): string[][] | null {
  if (section.nested || section.paired !== !!spec.paired) return null;
  const width = spec.repeat ? spec.params.length : section.tokens.length;
  if (!width || section.tokens.length % width) return null;
  const rows: string[][] = [];
  for (let i = 0; i < section.tokens.length; i += width) {
    const tokens = section.tokens.slice(i, i + width);
    if (tokens.some((token, index) => spec.params[index]?.kind === 'string' ? token.kind !== 'string' : token.kind !== 'integer' && !(spec.params[index]?.kind === 'number' && validValue(token.value, spec.params[index]!)))) return null;
    rows.push(tokens.map(t => t.value));
  }
  try { validateRows(spec, rows); return rows; } catch { return null; }
}
const quote = (value: string) => '`' + value.replaceAll('`', '``') + '`';
const format = (value: string, param: TriggerParameter) => param.kind === 'string' ? quote(value) : value;
export function triggerClauseText(spec: TriggerClauseSpec, rows: string[][], newline = '\n'): string {
  validateRows(spec, rows);
  return `[${spec.name}]${newline}${rows.map(row => '\t' + row.map((value, i) => format(value, spec.params[i]!)).join('\t')).join(newline)}${newline}${spec.paired ? `[/${spec.name}]${newline}` : ''}`;
}
export type TriggerEdit =
  { kind: 'add-trigger'; condition: string; rows: string[][]; multiple: boolean } |
  { kind: 'delete-trigger'; trigger: number } |
  { kind: 'add-branch'; trigger: number } |
  { kind: 'delete-branch'; trigger: number; branch: number } |
  { kind: 'add-clause'; trigger: number; branch?: number; name: string; rows: string[][] } |
  { kind: 'edit-clause'; trigger: number; branch?: number; clause: number; rows: string[][] } |
  { kind: 'delete-row'; trigger: number; branch?: number; clause: number; row: number } |
  { kind: 'delete-clause'; trigger: number; branch?: number; clause: number };
interface Patch { start: number; end: number; value: string }
/** Adjust syntax indentation only; multiline string contents and token spelling stay exact. */
export function formatItemTriggerBlock(text: string): string {
  const patches = new Map<number, Patch>();
  function indent(position: number, depth: number) {
    const start = Math.max(text.lastIndexOf('\n', position - 1), text.lastIndexOf('\r', position - 1)) + 1;
    const prefix = text.slice(start, position);
    if (!/^[ \t]*$/.test(prefix)) return; // Inline syntax stays inline.
    const value = '\t'.repeat(depth);
    if (prefix !== value) patches.set(start, { start, end: position, value });
  }
  function visit(section: ItemSection, depth: number) {
    indent(section.start, depth);
    for (const token of section.tokens) indent(token.start, depth + 1);
    for (const child of section.children) visit(child, depth + 1);
    if (section.paired) indent(section.closeStart, depth);
  }
  for (const section of parseItem(text)) visit(section, 0);
  for (const patch of [...patches.values()].sort((a, b) => b.start - a.start)) text = text.slice(0, patch.start) + patch.value + text.slice(patch.end);
  return text;
}
/** Apply a single operation to exact source spans; never serialize the trigger tree. */
export function editItemTrigger(text: string, action: TriggerEdit): string {
  const sections = parseItem(text);
  const hasCommand = (before: number) => sections.some(section => section.name === 'command' && section.start < before && section.tokens.length > 0);
  const doc = readItemTriggers(text), newline = text.includes('\r\n') ? '\r\n' : '\n', patches: Patch[] = [];
  const insert = (position: number, value: string) => patches.push({ start: position, end: position, value });
  const insertBeforeClose = (section: ItemSection, value: string) => {
    const lineStart = text.lastIndexOf('\n', section.closeStart - 1) + 1;
    if (/^[ \t]*$/.test(text.slice(lineStart, section.closeStart))) insert(lineStart, value);
    else insert(section.closeStart, newline + value);
  };
  const remove = (section: ItemSection) => patches.push({ start: section.start, end: section.paired ? section.end : section.tokens.at(-1)?.end ?? section.body, value: '' });
  if (action.kind === 'add-trigger') {
    if (doc.issues.length) throw new Error('请先修复现有触发器配对，再新增触发器');
    const spec = triggerClauses.find(c => c.scope === 'if' && c.name === action.condition);
    if (!spec) throw new Error('触发条件无效');
    // Command requires a version-specific binary token; do not synthesize it.
    if (spec.name === 'use command' && !hasCommand(text.length)) throw new Error('操作键需要与客户端匹配的 [command] 数据，请复制已有触发器后在 DSL 中配置');
    const body = triggerClauseText(spec, action.rows, newline);
    const target = triggerClauseText(triggerClauses.find(c => c.scope === 'then' && c.name === 'target')!, [['myself', '-1']], newline);
    const branch = `[then]${newline}${target}${action.multiple ? `[then probability]${newline}\t100.0${newline}` : ''}[/then]${newline}`;
    insert(text.length, (text && !text.endsWith('\n') ? newline : '') + `[if]${newline}${body}[/if]${newline}` + (action.multiple ? `[multiple then]${newline}${branch}[/multiple then]${newline}` : branch));
  } else {
    const trigger = doc.triggers[action.trigger];
    if (!trigger) throw new Error('触发器已变化，请重新编辑');
    if (action.kind === 'delete-trigger') { remove(trigger.condition); remove(trigger.response); }
    else if (action.kind === 'add-branch') {
      const target = triggerClauseText(triggerClauses.find(c => c.name === 'target' && c.scope === 'then')!, [['myself', '-1']], newline);
      const branch = `[then]${newline}${target}[then probability]${newline}\t0.0${newline}[/then]${newline}`;
      if (trigger.multiple) insertBeforeClose(trigger.response, branch);
      else {
        insert(trigger.response.start, `[multiple then]${newline}`);
        if (!trigger.response.children.some(child => child.name === 'then probability')) insertBeforeClose(trigger.response, `[then probability]${newline}\t100.0${newline}`);
        insert(trigger.response.end, newline + branch + `[/multiple then]${newline}`);
      }
    } else if (action.kind === 'delete-branch') {
      if (trigger.branches.length <= 1) throw new Error('至少保留一个动作分支；移除最后分支请删除整个触发器');
      const branch = trigger.branches[action.branch]; if (!branch) throw new Error('分支已变化'); remove(branch);
    } else {
      const scope = action.branch === undefined ? 'if' : 'then';
      const host = scope === 'if' ? trigger.condition : trigger.branches[action.branch!];
      if (!host) throw new Error('分支已变化');
      if (action.kind === 'add-clause') {
        const spec = triggerClauses.find(c => c.scope === scope && c.name === action.name);
        if (!spec) throw new Error('不支持的触发器字段');
        if (spec.name === 'then probability' && host.children.some(child => child.name === spec.name)) throw new Error('分支已有概率，请编辑现有设置');
        if (spec.name === 'use command' && !hasCommand(trigger.condition.start)) throw new Error('操作键需要客户端对应的 [command] 数据，请在 DSL 中配置');
        insertBeforeClose(host, triggerClauseText(spec, action.rows, newline));
      } else {
        const section = host.children[action.clause], spec = section && triggerClauseSpec(scope, section);
        if (!section || !spec) throw new Error('特殊字段请在 DSL 中编辑');
        const rows = triggerClauseRows(section, spec);
        if (!rows) throw new Error('特殊参数结构请在 DSL 中编辑');
        if (action.kind === 'delete-clause') {
          if (scope === 'if' && host.children.length <= 1) throw new Error('至少保留一个触发条件；移除最后条件请删除整个触发器');
          remove(section);
        }
        else if (action.kind === 'delete-row') {
          if (!spec.repeat || !rows[action.row]) throw new Error('参数记录已变化');
          if (rows.length === 1) remove(section);
          else section.tokens.slice(action.row * spec.params.length, (action.row + 1) * spec.params.length).forEach(token => patches.push({ start: token.start, end: token.end, value: '' }));
        }
        else {
          validateRows(spec, action.rows);
          if (rows.length !== action.rows.length || rows.some((r, i) => r.length !== action.rows[i]!.length)) {
            // Preserve every existing row and comment; only appending rows is safe.
            if (!spec.repeat || action.rows.length < rows.length || rows.some((r, i) => r.length !== action.rows[i]!.length)) throw new Error('已有记录不可直接改变参数数量；请分开删除或新增字段');
            insert(section.paired ? section.closeStart : section.tokens.at(-1)!.end, newline + action.rows.slice(rows.length).map(row => '\t' + row.map((v, i) => format(v, spec.params[i]!)).join('\t')).join(newline) + newline);
          }
          section.tokens.forEach((token: ItemToken, i) => {
            const row = Math.floor(i / rows[0]!.length), column = i % rows[0]!.length, value = action.rows[row]![column]!;
            if (value === token.value) return;
            const raw = format(value, spec.params[column]!);
            patches.push({ start: token.start, end: token.end, value: token.marker ? `{${token.marker}=${raw}}` : raw });
          });
        }
      }
    }
  }
  for (const patch of patches.sort((a, b) => b.start - a.start || b.end - a.end)) text = text.slice(0, patch.start) + patch.value + text.slice(patch.end);
  parseItem(text);
  if (patches.length && action.kind !== 'delete-trigger') {
    const updated = readItemTriggers(text).triggers;
    const trigger = action.kind === 'add-trigger' ? updated.at(-1) : updated[action.trigger];
    if (trigger) {
      const start = trigger.condition.start, end = trigger.response.end;
      text = text.slice(0, start) + formatItemTriggerBlock(text.slice(start, end)) + text.slice(end);
    }
  }
  return text;
}
