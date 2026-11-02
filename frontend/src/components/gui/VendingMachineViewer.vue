<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from "vue";
import { NAlert, NButton, NCheckbox, NEmpty, NInputNumber, NModal, NSpin, useDialog, useMessage } from "naive-ui";
import { FileGUIService } from "../../../bindings/pvfine/services";
import type { VendingDocument, VendingGroup, VendingReward } from "../../../bindings/pvfine/services/models";
import type { GUIFile } from "../../gui/types";
import { addVendingGroup, addVendingReward, editVendingNumbers, removeVendingGroup, removeVendingReward, vendingInteger, vendingProbability, vendingTotal } from "../../gui/vendingMachine";
import { useEditorStore } from "../../stores/editor";
import { useFileGUIStore } from "../../stores/fileGUI";
import ItemPicker from "../ItemPicker.vue";

const props = defineProps<{ file: GUIFile; active: boolean }>();
const editor = useEditorStore(), gui = useFileGUIStore(), message = useMessage(), dialog = useDialog();
const document = ref<VendingDocument | null>(null), sourceText = ref(""), error = ref("");
const loading = ref(true), selected = ref(0);
const groups = computed(() => document.value?.groups ?? []);
const group = computed(() => groups.value[selected.value]);
const rewards = computed(() => group.value?.rewards ?? []);
const total = computed(() => vendingTotal(rewards.value));
const ready = computed(() => props.active && !loading.value && !error.value && sourceText.value === props.file.text);
const canEdit = computed(() => ready.value && props.file.editable && !editor.saving && !editor.guiApplying);
const dirty = computed(() => { const tab = editor.getFileDraft(props.file.index); return !!tab && tab.path === props.file.path && editor.isTextDirty(tab); });
const form = reactive({ show: false, kind: "reward" as "reward" | "group" | "new-group", text: "", index: -1, epoch: -1, group: null as VendingGroup | null, reward: null as VendingReward | null, number: 1 as number | null, item: "", name: "", quantity: 1 as number | null, weight: 1 as number | null, announce: false });
const formTitle = computed(() => form.kind === "new-group" ? "新增抽奖分组" : form.kind === "group" ? "编辑分组与材料" : form.reward ? "编辑奖品" : "新增奖品");
let generation = 0;
let timer: ReturnType<typeof setTimeout> | undefined;
async function load() {
  const request = ++generation, epoch = gui.epoch, text = props.file.text, index = props.file.index;
  loading.value = true; error.value = "";
  try {
    const result = await FileGUIService.ReadVendingMachine(index, text);
    if (request !== generation || epoch !== gui.epoch || text !== props.file.text || index !== props.file.index) return;
    if (!result) throw new Error("未返回抽奖机数据");
    document.value = result; sourceText.value = text;
    selected.value = Math.min(selected.value, Math.max(0, (result.groups?.length ?? 0) - 1));
  } catch (cause) { if (request === generation && epoch === gui.epoch) { error.value = String(cause); document.value = null; } }
  finally { if (request === generation) loading.value = false; }
}
watch(() => [props.file.index, props.file.path, props.file.text, props.active, gui.epoch, gui.revision], () => {
  generation++; clearTimeout(timer); form.show = false; loading.value = true;
  if (props.active) timer = setTimeout(load, 80);
}, { immediate: true });
watch(() => [props.file.index, gui.epoch], () => { document.value = null; selected.value = 0; });
onBeforeUnmount(() => { generation++; clearTimeout(timer); });
function open(kind: typeof form.kind, reward?: VendingReward) {
  if (!canEdit.value || (kind !== "new-group" && !group.value)) return;
  const g = group.value;
  const ids = new Set(groups.value.map(g => g.number.value));
  let next = 1; while (ids.has(next)) next++;
  Object.assign(form, { show: true, kind, text: props.file.text, index: props.file.index, epoch: gui.epoch, group: g ?? null, reward: reward ?? null,
    number: kind === "new-group" ? next : g?.number.value ?? 1,
    item: String(kind === "reward" ? reward?.fields?.[0]?.value ?? "" : kind === "group" ? g?.material?.[0]?.value ?? "" : ""),
    name: kind === "reward" ? reward?.name ?? "" : kind === "group" ? g?.materialName ?? "" : "",
    quantity: kind === "reward" ? reward?.fields?.[2]?.value ?? 1 : kind === "group" ? g?.material?.[1]?.value ?? 1 : 1,
    weight: reward?.fields?.[1]?.value ?? 1, announce: reward?.fields?.[3]?.value === 1 });
}
function update(text: string) {
  editor.updateContent(props.file.index, text);
  loading.value = true; form.show = false;
}
function apply() {
  try {
    if (!canEdit.value || form.text !== props.file.text || form.index !== props.file.index || form.epoch !== gui.epoch) throw new Error("文件已变化，请重新打开表单");
    if (!form.item.trim()) throw new Error("请选择物品或输入物品 ID");
    const item = vendingInteger(Number(form.item)), quantity = vendingInteger(form.quantity);
    if (form.kind === "reward") {
      const values = [item, vendingInteger(form.weight), quantity, form.announce ? 1 : 0];
      update(form.reward ? editVendingNumbers(form.text, form.reward.fields ?? [], values) : addVendingReward(form.text, form.group!, values));
    } else {
      const number = vendingInteger(form.number);
      if (groups.value.some(g => g.number.value === number && (form.kind === "new-group" || g.start !== form.group?.start))) throw new Error("组编号已存在");
      if (form.kind === "new-group") {
        selected.value = groups.value.length;
        update(addVendingGroup(form.text, number, item, quantity));
      } else update(editVendingNumbers(form.text, [form.group!.number, ...form.group!.material!], [number, item, quantity]));
    }
    message.success("已更新草稿，保存后写入工作区");
  } catch (cause) { message.error(String(cause)); }
}
function remove(reward?: VendingReward) {
  if (!canEdit.value || !group.value) return;
  const text = props.file.text, index = props.file.index, epoch = gui.epoch, g = group.value;
  dialog.warning({ title: reward ? "删除奖品" : "删除分组", content: reward ? `删除 ${reward.name || '物品 #' + reward.fields?.[0]?.value}？` : `删除分组 ${g.number.value} 及其全部奖品和参数？`, positiveText: "删除", negativeText: "取消",
    onPositiveClick: () => {
      try {
        if (!canEdit.value || text !== props.file.text || index !== props.file.index || epoch !== gui.epoch) throw new Error("文件已变化，请重新操作");
        update(reward ? removeVendingReward(text, reward) : removeVendingGroup(text, g));
      } catch (cause) { message.error(String(cause)); }
    } });
}
async function save() {
  if (!canEdit.value || !dirty.value || form.show) return;
  const epoch = gui.epoch;
  try { if (await editor.saveFileTab(props.file.index) && epoch === gui.epoch) message.success("已保存到工作区；保存 PVF 后写入归档文件"); }
  catch (cause) { if (epoch === gui.epoch) message.error(String(cause)); }
}
</script>

<template>
  <div class="vending-viewer">
    <header class="vending-header">
      <div><h2>抽奖机</h2><span class="muted">{{ groups.length }} 个分组<span v-if="dirty"> · 有未保存修改</span></span></div>
      <div class="actions"><NButton size="small" :disabled="!canEdit || form.show" @click="open('new-group')">新增分组</NButton><NButton size="small" type="primary" :disabled="!canEdit || !dirty || form.show" :loading="editor.saving" @click="save">保存抽奖机</NButton></div>
    </header>
    <NAlert v-if="error" type="error" title="无法读取抽奖机" role="alert">{{ error }}<p>请在文本模式修复格式后重试。</p><NButton size="small" @click="load">重新加载</NButton></NAlert>
    <NSpin :show="loading">
      <div v-if="groups.length" class="vending-layout">
        <nav class="group-list" aria-label="抽奖分组">
          <button v-for="(g, i) in groups" :key="g.start" :class="{ selected: selected === i }" :aria-pressed="selected === i" :disabled="loading || form.show" @click="selected = i"><strong>分组 {{ g.number.value }}</strong><small>{{ g.rewards?.length ?? 0 }} 个奖品</small></button>
        </nav>
        <main v-if="group" class="group-detail">
          <section class="material-bar" aria-label="抽奖消耗材料"><div><span class="muted">每次抽奖消耗</span><p><strong>{{ group.materialName || '未解析物品' }}</strong> <span class="muted">#{{ group.material?.[0]?.value }}</span> <b>× {{ group.material?.[1]?.value }}</b></p></div><div class="actions"><NButton size="small" :disabled="!canEdit || form.show" @click="open('group')">编辑分组</NButton><NButton size="small" type="error" quaternary :disabled="!canEdit || form.show" @click="remove()">删除分组</NButton></div></section>
          <div class="rewards-heading"><h3>奖品池</h3><span class="muted">总权重 {{ total.toLocaleString() }}</span><NButton size="small" :disabled="!canEdit || form.show" @click="open('reward')">新增奖品</NButton></div>
          <NAlert v-if="rewards.length && total === 0" type="warning" :show-icon="false">总权重为 0，无法计算抽取概率。</NAlert>
          <div v-if="rewards.length" class="table-scroll"><table><thead><tr><th>物品</th><th>权重</th><th>概率</th><th>数量</th><th>公告</th><th>操作</th></tr></thead><tbody>
            <tr v-for="reward in rewards" :key="reward.fields?.[0]?.start"><td><strong>{{ reward.name || '未解析物品' }}</strong><small>#{{ reward.fields?.[0]?.value }}</small></td><td>{{ reward.fields?.[1]?.value.toLocaleString() }}</td><td class="probability"><span>{{ vendingProbability(reward.fields?.[1]?.value ?? 0, total) }}</span><div class="probability-track" aria-hidden="true"><i :style="{ width: `${total ? (reward.fields?.[1]?.value ?? 0) / total * 100 : 0}%` }" /></div></td><td>{{ reward.fields?.[2]?.value }}</td><td>{{ reward.fields?.[3]?.value ? '是' : '否' }}</td><td><div class="actions"><NButton size="tiny" :disabled="!canEdit || form.show" @click="open('reward', reward)">编辑</NButton><NButton size="tiny" quaternary type="error" :disabled="!canEdit || form.show" @click="remove(reward)">删除</NButton></div></td></tr>
          </tbody></table></div>
          <NEmpty v-else description="分组暂无奖品，点击“新增奖品”配置奖品池" />
          <p class="muted note">概率 = 奖品权重 ÷ 本组总权重。同一物品的不同记录分别计算。</p>
        </main>
      </div>
      <NEmpty v-else-if="!loading && !error" description="暂无抽奖分组，点击“新增分组”开始配置" />
    </NSpin>
    <NModal v-model:show="form.show" preset="card" :title="formTitle" style="width: 560px; max-width: 95vw" :mask-closable="false">
      <div class="vending-form">
        <label v-if="form.kind !== 'reward'">组编号<NInputNumber v-model:value="form.number" :min="0" :max="2147483647" :precision="0" :disabled="!canEdit" aria-label="组编号" /></label>
        <label>{{ form.kind === 'reward' ? '奖品物品' : '所需材料' }}<ItemPicker v-model="form.item" :label="form.name" :disabled="!canEdit" @select="form.name = $event?.name ?? ''" /></label>
        <label>物品 ID<NInputNumber :value="form.item.trim() ? Number(form.item) : null" :min="0" :max="2147483647" :precision="0" :disabled="!canEdit" aria-label="物品 ID" @update:value="form.item = $event === null ? '' : String($event); form.name = ''" /></label>
        <label>数量<NInputNumber v-model:value="form.quantity" :min="0" :max="2147483647" :precision="0" :disabled="!canEdit" aria-label="数量" /></label>
        <template v-if="form.kind === 'reward'"><label>权重<NInputNumber v-model:value="form.weight" :min="0" :max="2147483647" :precision="0" :disabled="!canEdit" aria-label="权重" /></label><NCheckbox v-model:checked="form.announce" :disabled="!canEdit">获得奖品时公告</NCheckbox></template>
        <div class="actions form-actions"><NButton @click="form.show = false">取消</NButton><NButton type="primary" :disabled="!canEdit" @click="apply">应用到草稿</NButton></div>
      </div>
    </NModal>
  </div>
</template>

<style scoped>
.vending-viewer { width: 100%; min-width: 0; color: var(--pvf-text-primary); font-size: 13px; }
.vending-header, .material-bar, .rewards-heading, .actions { display: flex; align-items: center; gap: 12px; }
.vending-header { justify-content: space-between; flex-wrap: wrap; padding: 4px 0 16px; border-bottom: 1px solid var(--pvf-border-subtle); margin-bottom: 16px; }
h2 { margin: 0 0 4px; font-size: 16px; } h3 { margin: 0; font-size: 14px; }
.muted, small { color: var(--pvf-text-secondary); font-size: 12px; }
.vending-layout { display: grid; grid-template-columns: 140px minmax(0, 1fr); gap: 20px; }
.group-list { border-right: 1px solid var(--pvf-border-subtle); padding-right: 12px; }
.group-list button { display: flex; flex-direction: column; gap: 6px; width: 100%; padding: 12px; margin-bottom: 4px; border: 1px solid transparent; background: transparent; color: inherit; text-align: left; font: inherit; cursor: pointer; }
.group-list button.selected { color: var(--pvf-primary); background: var(--pvf-primary-soft); border-left-color: var(--pvf-primary); }
.group-list button:hover { background: var(--pvf-primary-soft); }
.group-list button:focus-visible { outline: 2px solid var(--pvf-primary); }
.group-detail { min-width: 0; }.material-bar { justify-content: space-between; flex-wrap: wrap; padding-bottom: 14px; border-bottom: 1px solid var(--pvf-border-subtle); }.material-bar p { margin: 6px 0 0; }.material-bar b { margin-left: 12px; }
.rewards-heading { margin: 18px 0 12px; }.rewards-heading .n-button { margin-left: auto; }
.table-scroll { overflow-x: auto; } table { width: 100%; min-width: 620px; border-collapse: collapse; font-variant-numeric: tabular-nums; text-align: left; } th { font-weight: 500; color: var(--pvf-text-secondary); font-size: 12px; } th, td { padding: 10px 12px; border-bottom: 1px solid var(--pvf-border-subtle); } td:first-child { min-width: 130px; } td strong { font-weight: 500; } td small { display: block; margin-top: 3px; } tbody tr:hover { background: var(--pvf-primary-soft); }
.probability { min-width: 100px; }.probability-track { height: 3px; margin-top: 5px; background: var(--pvf-border-subtle); }.probability-track i { display: block; height: 100%; background: var(--pvf-primary); }
.note { margin-top: 14px; }.vending-form { display: grid; gap: 14px; }.vending-form label { display: grid; gap: 6px; }.actions { flex-shrink: 0; gap: 8px; }.form-actions { justify-content: flex-end; margin-top: 8px; }
@media (max-width: 850px) { .vending-layout { grid-template-columns: minmax(0, 1fr); gap: 12px; }.group-list { display: flex; overflow-x: auto; border-right: 0; border-bottom: 1px solid var(--pvf-border-subtle); padding: 0; }.group-list button { width: auto; min-width: 110px; }.group-list button.selected { border-left-color: transparent; border-bottom-color: var(--pvf-primary); } }
</style>
