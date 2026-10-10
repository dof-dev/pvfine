<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from "vue";
import { NButton, NEmpty, NInputNumber, NModal, NRadio, NRadioGroup, NSlider, NSpin, useMessage } from "naive-ui";
import { FileGUIService } from "../../../bindings/pvfine/services";
import type { SkillDocument, SkillNumber } from "../../../bindings/pvfine/services/models";
import type { GUIFile } from "../../gui/types";
import { editSkillNumbers, skillPreview, type SkillOperation } from "../../gui/skill";
import { useEditorStore } from "../../stores/editor";
import { useFileGUIStore } from "../../stores/fileGUI";

const props = defineProps<{ file: GUIFile; active: boolean }>();
const editor = useEditorStore();
const gui = useFileGUIStore();
const message = useMessage();
const document = ref<SkillDocument | null>(null);
const loading = ref(false);
const error = ref("");
const selectedMode = ref("dungeon");
const level = ref(1);
let generation = 0;
let timer: ReturnType<typeof setTimeout> | undefined;
const sourceText = ref("");
const mode = computed(() => {
  const m = document.value?.modes?.find((m) => m.id === selectedMode.value) ?? document.value?.modes?.[0];
  return m && { ...m, static: m.static ?? [], levels: (m.levels ?? []).map((row) => row ?? []), properties: m.properties ?? [], issues: m.issues ?? [] };
});
const maxLevel = computed(() => Math.max(1, mode.value?.levels.length ?? 0));
const ready = computed(() => !loading.value && !error.value && sourceText.value === props.file.text);
const canEdit = computed(() => ready.value && props.file.editable && !editor.saving && !editor.guiApplying);
const dirty = computed(() => {
  const tab = editor.getFileDraft(props.file.index);
  return !!tab && tab.path === props.file.path && editor.isTextDirty(tab);
});
const preview = computed(() => mode.value?.properties.map((p) => skillPreview(p, mode.value!, level.value)) ?? []);
const edit = reactive({ show: false, dynamic: false, column: 0, row: 1, all: true, operation: "set" as SkillOperation, operand: null as number | null, text: "", epoch: 0, mode: "" });
const operations = [{ label: "直接赋值", value: "set" }, { label: "+ 固定数值", value: "add" }, { label: "× 固定数值", value: "multiply" }];
const scopes = [{ label: "全部等级", value: "all" }, { label: "仅当前等级", value: "current" }];
const scope = computed({ get: () => edit.all ? "all" : "current", set: (v) => { edit.all = v === "all"; } });
const editTokens = computed(() => {
  const m = mode.value;
  if (!m) return [];
  if (!edit.dynamic) return m.static[edit.column] ? [m.static[edit.column]!] : [];
  return edit.all ? m.levels.map((r) => r[edit.column]!).filter(Boolean) : [m.levels[edit.row - 1]?.[edit.column]!].filter(Boolean);
});
const changes = computed(() => editTokens.value.slice(0, 5).map((t) => {
  const v = edit.operand ?? 0;
  const result = edit.operation === "set" ? v : edit.operation === "add" ? t.value + v : t.value * v;
  return `${t.value} → ${t.tokenType === 0 ? Math.round(result) : Math.fround(result)}`;
}));

async function load() {
  const current = ++generation;
  const epoch = gui.epoch;
  const text = props.file.text;
  const index = props.file.index;
  loading.value = true;
  error.value = "";
  try {
    const result = await FileGUIService.ReadSkill(index, text);
    if (current !== generation || epoch !== gui.epoch || index !== props.file.index || text !== props.file.text) return;
    document.value = result;
    sourceText.value = text;
    level.value = Math.min(level.value, maxLevel.value);
  } catch (e) {
    if (current === generation && epoch === gui.epoch) error.value = String(e);
  } finally { if (current === generation) loading.value = false; }
}
watch(() => [props.file.index, props.file.text, gui.epoch, gui.revision, props.active], () => {
  generation++;
  clearTimeout(timer);
  if (props.active) timer = setTimeout(load, 80);
}, { immediate: true });
watch(maxLevel, (n) => { level.value = Math.min(level.value, n); });
watch(() => gui.epoch, () => { document.value = null; edit.show = false; });
onBeforeUnmount(() => { generation++; clearTimeout(timer); });
function openEdit(dynamic: boolean, column: number, row = level.value) {
  if (!canEdit.value || !mode.value) return;
  const token = dynamic ? mode.value.levels[row - 1]?.[column] : mode.value.static[column];
  if (!token) return;
  Object.assign(edit, { show: true, dynamic, column, row, all: dynamic, operation: "set", operand: token.value, text: props.file.text, epoch: gui.epoch, mode: mode.value.id });
}
function apply() {
  if (!canEdit.value || edit.operand === null) return;
  try {
    if (edit.text !== props.file.text || edit.epoch !== gui.epoch || edit.mode !== mode.value?.id) throw new Error("技能数据已变化，请重新打开表单");
    const text = editSkillNumbers(edit.text, editTokens.value as SkillNumber[], edit.operation, edit.operand);
    editor.updateContent(props.file.index, text);
    edit.show = false;
    message.success("已更新技能草稿，保存后写入归档");
  } catch (e) { message.error(String(e)); }
}
async function save() {
  if (!props.file.editable || !dirty.value || edit.show || editor.saving || editor.guiApplying) return;
  const epoch = gui.epoch;
  try {
    const saved = await editor.saveFileTab(props.file.index);
    if (saved && epoch === gui.epoch) message.success("已保存当前技能到工作区");
  } catch (e) {
    if (epoch === gui.epoch) message.error(`保存技能失败：${String(e)}`);
  }
}
</script>

<template>
  <div class="skill-viewer">
    <header class="skill-header">
      <div><h2>{{ document?.name || '技能参数' }}</h2></div>
      <div class="mode-tabs" role="group" aria-label="技能数值模式">
        <NButton v-for="m in document?.modes" :key="m.id" :type="mode?.id === m.id ? 'primary' : 'default'" :disabled="loading || edit.show" @click="selectedMode = m.id">{{ m.id === 'dungeon' ? '地下城' : '决斗场' }}</NButton>
        <NButton type="primary" :loading="editor.saving" :disabled="!file.editable || !dirty || edit.show || editor.saving || editor.guiApplying" @click="save">保存技能</NButton>
      </div>
    </header>
    <div v-if="error" class="error" role="alert">{{ error }} <NButton size="small" @click="load">重新加载</NButton></div>
    <NSpin :show="loading">
      <template v-if="mode">
        <div v-for="issue in mode.issues" :key="issue" class="issue">{{ issue }}</div>
        <div class="skill-workspace">
          <section class="data-region">
            <h3>静态数据 <small>{{ mode.static.length }} 项</small></h3>
            <div v-if="mode.static.length" class="static-grid">
              <button v-for="(n, i) in mode.static" :key="i" class="static-value" :disabled="!canEdit" @click="openEdit(false, i)"><span>索引 {{ i }}</span><strong>{{ n.value }}</strong></button>
            </div>
            <NEmpty v-else size="small" description="没有可用的静态数据" />
            <h3>等级数据 <small>{{ mode.levels.length }} 级 / 每级 {{ mode.width }} 项</small></h3>
            <div v-if="mode.levels.length" class="table-scroll">
              <table><thead><tr><th>等级</th><th v-for="i in mode.width" :key="i">索引 {{ i - 1 }}</th></tr></thead>
                <tbody><tr v-for="(row, r) in mode.levels" :key="r" :class="{ current: r + 1 === level }"><th><button @click="level = r + 1">{{ r + 1 }}</button></th><td v-for="(n, c) in row" :key="c"><button :disabled="!canEdit" :aria-label="`等级 ${r + 1} 索引 ${c} 数值 ${n.value}`" @click="openEdit(true, c, r + 1)">{{ n.value }}</button></td></tr></tbody>
              </table>
            </div>
            <NEmpty v-else size="small" description="没有可用的等级数据" />
          </section>
          <aside class="preview-region">
            <h3>属性描述预览</h3>
            <div class="level-control"><label>当前等级</label><NInputNumber v-model:value="level" :min="1" :max="maxLevel" :precision="0" :show-button="true" :update-value-on-input="false" @update:value="(v) => level = v ?? 1" /></div>
            <NSlider v-model:value="level" :min="1" :max="maxLevel" :step="1" :disabled="maxLevel === 1" />
            <div class="range-label"><span>1 级</span><span>{{ maxLevel }} 级</span></div>
            <div v-for="(parts, p) in preview" :key="p" class="skill-description"><template v-for="(part, i) in parts" :key="i"><button v-if="part.token && part.binding" class="description-value" :disabled="!canEdit" :title="`${part.binding.dynamic ? '动态' : '静态'}索引 ${part.binding.index}，显示倍率 ×${part.binding.multiplier}`" @click="openEdit(part.binding.dynamic, part.binding.index)">{{ part.text }}</button><span v-else>{{ part.text }}</span></template></div>
            <NEmpty v-if="!preview.length" description="未配置属性描述，可直接编辑数据表" size="small" />
          </aside>
        </div>
      </template>
      <NEmpty v-else-if="!loading && !error" description="该技能没有地下城或决斗场数值区块" />
    </NSpin>
    <NModal v-model:show="edit.show" preset="card" :title="`修改${edit.dynamic ? '动态' : '静态'}数据 · 索引 ${edit.column}`" style="width: 470px; max-width: 95vw" :mask-closable="false">
      <div class="edit-form">
        <div v-if="edit.dynamic" class="edit-field">
          <span>作用范围</span>
          <NRadioGroup v-model:value="scope" class="edit-options" aria-label="作用范围">
            <NRadio v-for="option in scopes" :key="option.value" :value="option.value" :label="option.label" />
          </NRadioGroup>
          <span class="hint">当前等级：{{ edit.row }}；将修改 {{ editTokens.length }} 个数值</span>
        </div>
        <div class="edit-field">
          <span>修改方式</span>
          <NRadioGroup v-model:value="edit.operation" class="edit-options" aria-label="修改方式">
            <NRadio v-for="option in operations" :key="option.value" :value="option.value" :label="option.label" />
          </NRadioGroup>
        </div>
        <label>数值<NInputNumber v-model:value="edit.operand" :show-button="false" /></label>
        <div class="change-preview"><div v-for="(change, i) in changes" :key="i">{{ change }}</div><span v-if="editTokens.length > 5">… 共 {{ editTokens.length }} 项</span></div>
        <div class="edit-actions"><NButton @click="edit.show = false">取消</NButton><NButton type="primary" :disabled="!canEdit || edit.operand === null" @click="apply">应用到草稿</NButton></div>
      </div>
    </NModal>
  </div>
</template>

<style scoped>
.skill-viewer { width: 100%; min-width: 0; color: var(--pvf-text-primary); }
.skill-header { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 10px 0 18px; border-bottom: 1px solid var(--pvf-border-subtle); }
h2 { margin: 0 0 6px; font-size: 19px; } h3 { font-size: 14px; margin: 18px 0 12px; }
small, .hint, .range-label { color: var(--pvf-text-secondary); font-size: 12px; font-weight: normal; }
small { margin-left: 8px; } .mode-tabs, .edit-actions { display: flex; gap: 8px; }
.skill-workspace { display: grid; grid-template-columns: minmax(0, 1fr) 320px; gap: 24px; }
.data-region { min-width: 0; } .preview-region { align-self: start; position: sticky; top: 0; padding: 0 16px 20px; background: var(--pvf-surface-card); border-left: 2px solid var(--pvf-primary); }
.static-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(90px, 1fr)); gap: 6px; }
button { font: inherit; color: inherit; cursor: pointer; } button:disabled { cursor: default; } button:focus-visible { outline: 2px solid var(--pvf-primary); outline-offset: -2px; }
.static-value { text-align: left; border: 1px solid var(--pvf-border-subtle); background: var(--pvf-surface-inset); padding: 8px 10px; border-radius: 4px; }
.static-value span { display: block; color: var(--pvf-text-secondary); font-size: 11px; margin-bottom: 3px; } .static-value strong { font-variant-numeric: tabular-nums; font-weight: 500; }
.static-value:hover:enabled, td button:hover:enabled { background: var(--pvf-surface-hover); color: var(--pvf-primary); }
.table-scroll { overflow: auto; max-height: 540px; border: 1px solid var(--pvf-border-subtle); }
table { border-collapse: separate; border-spacing: 0; width: 100%; font-variant-numeric: tabular-nums; }
th, td { border-right: 1px solid var(--pvf-border-subtle); border-bottom: 1px solid var(--pvf-border-subtle); min-width: 78px; text-align: right; }
thead th { position: sticky; top: 0; z-index: 2; padding: 8px; background: var(--pvf-surface-elevated); font-size: 12px; font-weight: 500; }
tbody th { position: sticky; left: 0; background: var(--pvf-surface-elevated); min-width: 50px; }
td button, th button { width: 100%; border: none; background: transparent; padding: 7px 10px; text-align: right; }
tr.current td { background: var(--pvf-primary-soft); }
.level-control { display: flex; justify-content: space-between; align-items: center; margin-bottom: 14px; } .level-control .n-input-number { width: 100px; }
.range-label { display: flex; justify-content: space-between; } .skill-description { white-space: pre-wrap; line-height: 2.2; margin-top: 18px; user-select: text; }
.description-value { border: none; border-bottom: 1px dashed var(--pvf-primary); border-radius: 3px; padding: 0 3px; color: var(--pvf-primary); background: var(--pvf-primary-soft); line-height: 1.6; }
.issue, .error { padding: 10px; margin: 8px 0; background: var(--pvf-surface-warning); }
.edit-form { display: flex; flex-direction: column; gap: 14px; } .edit-form p { margin: 0; } .edit-form > label, .edit-field { display: grid; gap: 6px; }
.edit-options { display: flex; flex-wrap: wrap; gap: 8px 16px; }
.change-preview { font-variant-numeric: tabular-nums; background: var(--pvf-surface-inset); padding: 12px; line-height: 1.8; } .edit-actions { justify-content: flex-end; }
@media (max-width: 850px) { .skill-workspace { grid-template-columns: 1fr; } .preview-region { grid-row: 1; position: static; } .skill-header { flex-wrap: wrap; } }
</style>
