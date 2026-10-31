<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, shallowRef, toRef, watch } from "vue";
import { NButton, NDropdown, NEmpty, NModal, NSelect, NSlider, NSpin, useMessage } from "naive-ui";
import { FileGUIService } from "../../../bindings/pvfine/services";
import type { SkillTreeCell, SkillTreeDropTarget } from "../../gui/skillTree";
import type { SkillTreeDocument, SkillTreeNode, SkillTreeRegistration, SkillTreeSkill } from "../../../bindings/pvfine/services/models";
import ImageThumbnail from "../ImageThumbnail.vue";
import { useFileGUIStore } from "../../stores/fileGUI";
import { useEditorStore } from "../../stores/editor";
import { skillJobNames } from "../../gui/skill";
import { skillBranchNames, skillTreeEdgeLayout, groupSkillTreeCells, skillTreeRowLevels, skillTreeCellNode, skillTreeCellSelectionKey, skillTreeDropTarget, moveSkillTreeCell, skillTreeCanvasHeight, addSkillTreeNode, removeSkillTreeNode, editSkillTreeLink } from "../../gui/skillTree";

const props = defineProps<{ show: boolean }>();
const emit = defineEmits<{ (e: "update:show", value: boolean): void }>();
const gui = useFileGUIStore();
const editor = useEditorStore();
const message = useMessage();
const registrations = ref<SkillTreeRegistration[]>([]);
const doc = ref<SkillTreeDocument | null>(null);
const job = toRef(gui.skillTreeSelection, "job");
const kind = toRef(gui.skillTreeSelection, "kind");
const branch = toRef(gui.skillTreeSelection, "branch");
const selected = ref<SkillTreeNode | null>(null);
const loading = ref(false);
const error = ref("");
const zoom = ref(130);
const opening = ref(false);
const savingTree = ref(false);
const canvas = ref<HTMLElement | null>(null);
const nodeInsetX = (47 - 34) / 2, nodeInsetY = (67 - 56) / 2;
const nodeMenu = ref<{show: boolean; x: number; y: number; node: SkillTreeNode | null; edgeIndex: number | null}>({show: false, x: 0, y: 0, node: null, edgeIndex: null});
const menuOptions = computed(() => (nodeMenu.value.edgeIndex === null ? [
  {key: "add-link", label: "添加后继关联"}, {key: "delete", label: "从技能树删除当前技能"},
] : [
  {key: "edit-link", label: "修改关联目标"}, {key: "delete-link", label: "删除这条关联"},
]).map(option => ({...option, disabled: !canEdit.value})));
const treeDraft = computed(() => doc.value ? editor.getFileDraft(doc.value.fileIndex) : undefined);
const dirty = computed(() => !!treeDraft.value && editor.isTextDirty(treeDraft.value));
const canEdit = computed(() => !!doc.value && !!treeDraft.value?.editable && treeDraft.value.text === doc.value.text && treeDraft.value.path.toLowerCase() === doc.value.path.toLowerCase() && !loading.value && !opening.value && !savingTree.value && !editor.saving && !editor.guiApplying);
interface TreeDrag {
  pointer: number; clientX: number; clientY: number; offsetX: number; offsetY: number;
  cell: SkillTreeCell; node: SkillTreeNode; epoch: number; text: string; active: boolean;
  target: SkillTreeDropTarget;
}
const drag = shallowRef<TreeDrag | null>(null);
interface TreeLink {
  source: SkillTreeNode; index: number; epoch: number; text: string;
  pointer: number | null; x: number; y: number; target: SkillTreeNode | null;
}
const link = shallowRef<TreeLink | null>(null);
const selectedEdge = shallowRef<{source: SkillTreeNode; index: number} | null>(null);
const linkValid = computed(() => !!link.value?.target && link.value.target.id !== link.value.source.id && !(link.value.source.next ?? []).some((id, index) => id === link.value!.target!.id && index !== link.value!.index));
const linkPreview = computed(() => {
  const state = link.value;
  if (!state) return "";
  const startX = state.source.x + 34, startY = state.source.y + 26;
  const endX = state.target ? state.target.x + 16 : state.x, endY = state.target ? state.target.y + 16 : state.y;
  return `M ${startX} ${startY} H ${(startX + endX) / 2} V ${endY} H ${endX}`;
});
interface SkillAddition {
  x: number; y: number; text: string; epoch: number; job: string; kind: string; branch: string;
  path: string; loading: boolean; error: string; id: number | null; choices: SkillTreeSkill[];
}
const addition = ref<SkillAddition | null>(null);
const adding = ref(false);
const addOptions = computed(() => (addition.value?.choices ?? []).map(skill => ({value: skill.id, label: `${skill.name} #${skill.id}`})));

let suppressClick = false;
let reloadTimer: ReturnType<typeof setTimeout> | undefined;
let request = 0;
const jobOptions = computed(() => [...new Set(registrations.value.map(r => r.job))].map(value => ({value, label: skillJobNames[value.replace(/[\[\] ]/g, "")] || value})));
const kinds = computed(() => registrations.value.filter(r => r.job === job.value).map(r => ({value: r.kind, label: r.kind === "sp" ? "SP · 技能" : "TP · 特性技能"})));
const branches = computed(() => (doc.value?.branches ?? []).map(b => ({value: b.name, label: skillBranchNames[b.name] || b.name})));
const currentBranch = computed(() => doc.value?.branches?.find(b => b.name === branch.value));
const nodes = computed(() => currentBranch.value?.nodes ?? []);
const rowLevels = computed(() => skillTreeRowLevels(nodes.value, kind.value));
const cells = computed(() => groupSkillTreeCells(nodes.value));
const selectedCell = computed(() => cells.value.find(cell => cell.nodes.includes(selected.value!)));
function cellSelectionKey(cell: SkillTreeCell) {
  return skillTreeCellSelectionKey(doc.value?.path ?? "", job.value, kind.value, branch.value, cell.key);
}
function cellNode(cell: SkillTreeCell) {
  return skillTreeCellNode(cell, gui.skillTreeCellSelections[cellSelectionKey(cell)]);
}
function selectCellNode(node: SkillTreeNode) {
  const cell = selectedCell.value;
  if (!cell || !cell.nodes.includes(node)) return;
  gui.skillTreeCellSelections[cellSelectionKey(cell)] = node.id;
  selected.value = node;
}
const width = computed(() => Math.max(564, ...nodes.value.map(n => n.x + 47)));
const height = computed(() => skillTreeCanvasHeight(nodes.value));
const rows = computed(() => Array.from({length: Math.ceil(height.value / 67)}, (_, i) => i));
const edges = computed(() => {
  const byID = new Map(nodes.value.map(n => [n.id, n]));
  return nodes.value.flatMap((source, index) => (source.next ?? []).flatMap((id, order) => {
    const target = byID.get(id);
    return target ? [{key: `${index}:${order}`, source, target, order, ...skillTreeEdgeLayout(source, target)}] : [];
  }));
});
function linked(n: SkillTreeNode) {
  return selected.value && (selected.value.id === n.id || selected.value.next?.includes(n.id) || n.next?.includes(selected.value.id));
}
async function scan() {
  const id = ++request, epoch = gui.epoch;
  nodeMenu.value.show = false; link.value = null; selectedEdge.value = null;
  loading.value = true; error.value = ""; doc.value = null; selected.value = null;
  try {
    const result = await FileGUIService.ListSkillTrees();
    if (id !== request || epoch !== gui.epoch) return;
    registrations.value = result ?? [];
    if (!registrations.value.some(r => r.job === job.value)) job.value = registrations.value[0]?.job ?? null;
    if (!kinds.value.some(k => k.value === kind.value)) kind.value = kinds.value[0]?.value ?? "sp";
    if (job.value) await load();
  } catch (e) {if (id === request) error.value = String(e);}
  finally {if (id === request) loading.value = false;}
}
async function load(preserveView = false) {
  if (!props.show || !job.value) return;
  const id = ++request, epoch = gui.epoch;
  const selection = preserveView ? selected.value : null;
  const commonSelected = !!selection && !!doc.value?.common?.includes(selection);
  clearTimeout(reloadTimer);
  nodeMenu.value.show = false; link.value = null; selectedEdge.value = null;
  loading.value = true; error.value = "";
  // 更新当前树时保留画布 DOM，避免内容短暂清空使滚动位置被浏览器归零。
  if (!preserveView) { doc.value = null; selected.value = null; }
  try {
    let result = await FileGUIService.ReadSkillTree(job.value, kind.value);
    if (id !== request || epoch !== gui.epoch || !result) return;
    const draft = await editor.openGUIFile(result.fileIndex);
    if (id !== request || epoch !== gui.epoch) return;
    if (draft.path.toLowerCase() !== result.path.toLowerCase()) throw new Error("技能树文件已变化，请重新读取");
    const text = draft.text;
    if (draft.editable && text !== result.text) result = await FileGUIService.ReadSkillTreeDraft(job.value, kind.value, text);
    if (id !== request || epoch !== gui.epoch) return;
    if (draft.editable && draft.text !== text) { void load(preserveView); return; }
    doc.value = result;
    if (!result?.branches?.some(b => b.name === branch.value)) branch.value = result?.branches?.[0]?.name ?? null;
    if (selection) selected.value = (commonSelected ? result?.common : nodes.value)?.find(node => node.id === selection.id) ?? null;
  } catch (e) {if (id === request) error.value = String(e);}
  finally {if (id === request) loading.value = false;}
}
function changeJob(value: string) {
  job.value = value;
  if (!kinds.value.some(k => k.value === kind.value)) kind.value = kinds.value[0]?.value ?? "sp";
  void load();
}
async function openSkill() {
  const skill = selected.value;
  if (!skill || skill.fileIndex < 0 || opening.value || editor.saving) return;
  const epoch = gui.epoch;
  opening.value = true;
  try {
    await editor.openFile(skill.fileIndex);
    if (epoch === gui.epoch) emit("update:show", false);
  } catch (e) {message.error(String(e));}
  finally {opening.value = false;}
}
async function openTreeSource(event: MouseEvent | KeyboardEvent) {
  if (!event.metaKey && !event.ctrlKey) return;
  event.preventDefault();
  const tree = doc.value;
  if (!tree || tree.fileIndex < 0 || opening.value || editor.saving) return;
  const epoch = gui.epoch;
  opening.value = true;
  try {
    await editor.openFile(tree.fileIndex);
    if (epoch === gui.epoch) emit("update:show", false);
  } catch (e) {message.error(String(e));}
  finally {opening.value = false;}
}
function clickCell(cell: SkillTreeCell) {
  if (suppressClick) { suppressClick = false; return; }
  if (link.value?.pointer === null) { void applyLink(link.value, cellNode(cell).id); return; }
  selectedEdge.value = null;
  selected.value = cellNode(cell);
}
function openNodeMenu(event: MouseEvent, cell: SkillTreeCell) {
  cancelDrag();
  selected.value = cellNode(cell);
  nodeMenu.value = {show: true, x: event.clientX, y: event.clientY, node: selected.value, edgeIndex: null};
}
async function deleteNode(node: SkillTreeNode | null) {
  nodeMenu.value.show = false;
  if (!node || !canEdit.value || !currentBranch.value || !treeDraft.value || !selectedCell.value) return;
  try {
    const cell = selectedCell.value;
    const text = removeSkillTreeNode(doc.value!.text, currentBranch.value, node);
    const selectionKey = cellSelectionKey(cell);
    if (gui.skillTreeCellSelections[selectionKey] === node.id) delete gui.skillTreeCellSelections[selectionKey];
    editor.updateContent(treeDraft.value.index, text);
    const epoch = gui.epoch;
    await load(true);
    if (epoch === gui.epoch) {
      const remaining = cells.value.find(value => value.key === cell.key);
      selected.value = remaining ? cellNode(remaining) : null;
    }
  } catch (e) { message.error(String(e)); }
}
function revealNode(node: SkillTreeNode) {
  selected.value = node;
  const cell = cells.value.find(cell => cell.nodes.includes(node));
  if (cell) gui.skillTreeCellSelections[cellSelectionKey(cell)] = node.id;
}
function selectEdge(source: SkillTreeNode, index: number) {
  if (link.value) return;
  revealNode(source);
  selectedEdge.value = {source, index};
}
function openEdgeMenu(event: MouseEvent, source: SkillTreeNode, index: number) {
  cancelDrag(); link.value = null;
  selectEdge(source, index);
  nodeMenu.value = {show: true, x: event.clientX, y: event.clientY, node: source, edgeIndex: index};
}
function selectNodeMenu(key: string) {
  const {node, edgeIndex} = nodeMenu.value;
  nodeMenu.value.show = false;
  if (!node || !canEdit.value) return;
  if (key === "delete") void deleteNode(node);
  else if (key === "delete-link" && edgeIndex !== null) void applyLink(makeLink(node, edgeIndex), null);
  else if (key === "add-link" || key === "edit-link") beginLink(node, edgeIndex ?? -1);
}
function makeLink(source: SkillTreeNode, index: number): TreeLink {
  return {source, index, epoch: gui.epoch, text: doc.value!.text, pointer: null, x: source.x + 50, y: source.y + 26, target: null};
}
function beginLink(source: SkillTreeNode, index: number, event?: PointerEvent) {
  if (!canEdit.value || (event && event.button !== 0)) return;
  cancelDrag(); nodeMenu.value.show = false;
  revealNode(source);
  link.value = makeLink(source, index);
  if (event) {
    link.value = {...link.value, pointer: event.pointerId};
    (event.currentTarget as Element).setPointerCapture(event.pointerId);
    moveLink(event);
  }
}
function moveLink(event: PointerEvent) {
  const state = link.value;
  if (!state || !canvas.value || (state.pointer !== null && state.pointer !== event.pointerId)) return;
  if (!canEdit.value || state.epoch !== gui.epoch || state.text !== doc.value?.text) { link.value = null; return; }
  const rect = canvas.value.getBoundingClientRect(), scale = zoom.value / 100;
  const x = (event.clientX - rect.left) / scale - 40 - nodeInsetX, y = (event.clientY - rect.top) / scale - nodeInsetY;
  const cell = cells.value.find(cell => {
    const n = cellNode(cell);
    return x >= n.x && x <= n.x + 34 && y >= n.y && y <= n.y + 56;
  });
  link.value = {...state, x, y, target: cell ? cellNode(cell) : null};
}
function finishLink(event: PointerEvent) {
  moveLink(event);
  const state = link.value;
  if (!state || state.pointer !== event.pointerId) return;
  link.value = null;
  suppressClick = true;
  setTimeout(() => { suppressClick = false; }, 0);
  if (state.target) void applyLink(state, state.target.id);
}
async function applyLink(state: TreeLink, target: number | null) {
  link.value = null;
  try {
    if (!canEdit.value || !currentBranch.value || !treeDraft.value || state.epoch !== gui.epoch || state.text !== doc.value?.text) throw new Error("技能树已变化，关联编辑已取消");
    const text = editSkillTreeLink(state.text, currentBranch.value, state.source, state.index, target);
    if (text === state.text) return;
    editor.updateContent(treeDraft.value.index, text);
    await load(true);
  } catch (e) { message.error(String(e)); }
}
function cancelLink() { link.value = null; }
function onTreeKeydown(event: KeyboardEvent) {
  if (!props.show || event.key !== "Escape" || !link.value) return;
  event.preventDefault(); event.stopImmediatePropagation(); link.value = null;
}
onMounted(() => window.addEventListener("keydown", onTreeKeydown, true));
function cancelDrag() { drag.value = null; }
function startDrag(event: PointerEvent, cell: SkillTreeCell) {
  if (link.value || !canEdit.value || event.button !== 0 || event.metaKey || event.ctrlKey || !canvas.value) return;
  const node = cellNode(cell);
  const rect = canvas.value.getBoundingClientRect();
  const scale = zoom.value / 100;
  drag.value = {pointer: event.pointerId, clientX: event.clientX, clientY: event.clientY,
    offsetX: (event.clientX - rect.left) / scale - node.x - 40 - nodeInsetX,
    offsetY: (event.clientY - rect.top) / scale - node.y - nodeInsetY,
    cell, node, epoch: gui.epoch, text: doc.value!.text, active: false,
    target: {x: node.x, y: node.y, valid: true, reason: ""}};
  (event.currentTarget as HTMLElement).setPointerCapture(event.pointerId);
}
function moveDrag(event: PointerEvent) {
  const current = drag.value;
  if (!current || current.pointer !== event.pointerId || !canvas.value) return;
  if (!canEdit.value || current.epoch !== gui.epoch || current.text !== doc.value?.text) { cancelDrag(); return; }
  if (!current.active && Math.hypot(event.clientX - current.clientX, event.clientY - current.clientY) < 4) return;
  event.preventDefault();
  const rect = canvas.value.getBoundingClientRect();
  const scale = zoom.value / 100;
  const x = (event.clientX - rect.left) / scale - 40 - nodeInsetX - current.offsetX;
  const y = (event.clientY - rect.top) / scale - nodeInsetY - current.offsetY;
  drag.value = {...current, active: true, target: skillTreeDropTarget(nodes.value, current.cell, x, y, width.value, height.value)};
}
async function finishDrag(event: PointerEvent) {
  moveDrag(event);
  const current = drag.value;
  if (!current || current.pointer !== event.pointerId) return;
  cancelDrag();
  if (!current.active) return;
  suppressClick = true;
  setTimeout(() => { suppressClick = false; }, 0);
  if (!current.target.valid) { message.warning(current.target.reason); return; }
  if (current.target.x === current.node.x && current.target.y === current.node.y) return;
  try {
    const draft = treeDraft.value;
    if (!canEdit.value || !draft || current.epoch !== gui.epoch || draft.text !== current.text) throw new Error("技能树已变化，拖拽已取消");
    const text = moveSkillTreeCell(current.text, nodes.value, current.cell, current.target.x, current.target.y, width.value, height.value);
    const remembered = gui.skillTreeCellSelections[cellSelectionKey(current.cell)];
    const targetKey = `${current.target.x}:${current.target.y}`;
    editor.updateContent(draft.index, text);
    if (remembered !== undefined) {
      gui.skillTreeCellSelections[skillTreeCellSelectionKey(doc.value!.path, job.value, kind.value, branch.value, targetKey)] = remembered;
      delete gui.skillTreeCellSelections[cellSelectionKey(current.cell)];
    }
    await load(true);
    if (current.epoch === gui.epoch) selected.value = nodes.value.find(node => node.id === current.node.id && node.x === current.target.x && node.y === current.target.y) ?? null;
  } catch (e) {message.error(String(e));}
}
async function saveTree() {
  const tree = doc.value;
  if (!tree || !canEdit.value || !dirty.value) return;
  const epoch = gui.epoch;
  savingTree.value = true;
  try {
    const saved = await editor.saveFileTab(tree.fileIndex);
    if (saved && epoch === gui.epoch) { await load(true); message.success("技能树已保存到工作区"); }
  } catch (e) {if (epoch === gui.epoch) message.error(String(e));}
  finally {savingTree.value = false;}
}
async function loadAddChoices(snapshot: SkillAddition) {
  snapshot.loading = true; snapshot.error = "";
  try {
    const choices = await FileGUIService.ListSkillTreeSkills(snapshot.job);
    if (addition.value !== snapshot || snapshot.epoch !== gui.epoch) return;
    const existing = new Set(nodes.value.map(node => node.id));
    snapshot.choices = (choices ?? []).filter(skill => !existing.has(skill.id));
  } catch (e) { if (addition.value === snapshot) snapshot.error = String(e); }
  finally { if (addition.value === snapshot) snapshot.loading = false; }
}
function clickBlank(event: MouseEvent) {
  if (suppressClick) { suppressClick = false; return; }
  if (link.value) { link.value = null; return; }
  selectedEdge.value = null;
  if (!canEdit.value || drag.value || adding.value || !canvas.value || !currentBranch.value || !job.value || !branch.value) return;
  if ((event.target as HTMLElement).closest("button, .drop-preview")) return;
  const rect = canvas.value.getBoundingClientRect(), scale = zoom.value / 100;
  const rawX = (event.clientX - rect.left) / scale - 40;
  const rawY = (event.clientY - rect.top) / scale;
  if (rawX < 0 || rawY < 0) return;
  const x = Math.floor(rawX / 47) * 47, y = Math.floor(rawY / 67) * 67;
  const target = skillTreeDropTarget(nodes.value, {key: "", nodes: []}, x, y, width.value, height.value);
  if (!target.valid) return;
  if (currentBranch.value.insertOffset < 0) { message.warning("分支缺少结束标签，请先修正文本"); return; }
  addition.value = {x, y, text: doc.value!.text, epoch: gui.epoch, job: job.value, kind: kind.value,
    branch: branch.value, path: doc.value!.path, loading: true, error: "", id: null, choices: []};
  void loadAddChoices(addition.value);
}
async function confirmAdd() {
  const snapshot = addition.value;
  if (!snapshot || snapshot.id === null || snapshot.loading || adding.value) return;
  try {
    if (!canEdit.value || !currentBranch.value || snapshot.epoch !== gui.epoch || treeDraft.value?.text !== snapshot.text || doc.value?.path !== snapshot.path || job.value !== snapshot.job || kind.value !== snapshot.kind || branch.value !== snapshot.branch) throw new Error("技能树已变化，请重新选择空白位置");
    if (!snapshot.choices.some(skill => skill.id === snapshot.id)) throw new Error("请选择当前职业的技能");
    const text = addSkillTreeNode(snapshot.text, currentBranch.value, snapshot.id, snapshot.x, snapshot.y, width.value, height.value);
    adding.value = true;
    editor.updateContent(treeDraft.value!.index, text);
    addition.value = null;
    await load(true);
    if (snapshot.epoch === gui.epoch) selected.value = nodes.value.find(node => node.id === snapshot.id && node.x === snapshot.x && node.y === snapshot.y) ?? null;
  } catch (e) { message.error(String(e)); }
  finally { adding.value = false; }
}
watch(() => treeDraft.value?.text, (text) => {
  if (loading.value || !doc.value || !treeDraft.value?.editable || text === undefined || text === doc.value.text) return;
  link.value = null; selectedEdge.value = null; cancelDrag(); addition.value = null; clearTimeout(reloadTimer);
  reloadTimer = setTimeout(() => { if (props.show) void load(true); }, 80);
});
watch(() => props.show, show => {if (show) void scan(); else { link.value = null; selectedEdge.value = null; nodeMenu.value.show = false; request++; cancelDrag(); addition.value = null; }}, {immediate: true});
watch(branch, () => {link.value = null; selectedEdge.value = null; nodeMenu.value.show = false; selected.value = null; cancelDrag(); addition.value = null;});
watch(() => gui.epoch, () => {link.value = null; selectedEdge.value = null; nodeMenu.value.show = false; addition.value = null; cancelDrag(); request++; doc.value = null; registrations.value = []; loading.value = false; emit("update:show", false);});
onBeforeUnmount(() => {window.removeEventListener("keydown", onTreeKeydown, true); request++; cancelDrag(); addition.value = null; clearTimeout(reloadTimer);});
</script>

<template>
  <NDropdown trigger="manual" :show="nodeMenu.show" :x="nodeMenu.x" :y="nodeMenu.y" :options="menuOptions" @select="selectNodeMenu" @clickoutside="nodeMenu.show = false" />
  <NModal v-if="addition" :show="true" preset="card" title="添加技能" style="width: 520px; max-width: 92vw" @update:show="value => { if (!value) addition = null; }">
    <p>添加到第 {{ addition.x / 47 + 1 }} 列、第 {{ addition.y / 67 + 1 }} 行。请选择当前职业尚未加入此分支的技能。</p>
    <NSelect v-model:value="addition.id" :options="addOptions" filterable clearable :loading="addition.loading" :disabled="addition.loading || adding" placeholder="搜索技能名或ID" aria-label="选择要添加的技能" />
    <p v-if="addition.error" role="alert">{{ addition.error }} <NButton size="small" @click="loadAddChoices(addition!)">重试</NButton></p>
    <p v-else-if="!addition.loading && !addOptions.length">没有可添加的技能。</p>
    <template #footer><div class="add-actions"><NButton @click="addition = null">取消</NButton><NButton type="primary" :disabled="addition.id === null || addition.loading || adding" @click="confirmAdd">添加到技能树</NButton></div></template>
  </NModal>
  <NModal :show="show" preset="card" title="技能树" style="width: 1450px; max-width: 96vw" :content-style="{ padding: '0' }" @update:show="emit('update:show', $event)">
    <div class="tree-toolbar">
      <NSelect :value="job" :options="jobOptions" placeholder="选择职业" aria-label="职业" style="width: 200px" :disabled="loading || savingTree || !!drag" @update:value="changeJob" />
      <NSelect v-model:value="branch" :options="branches" placeholder="选择转职" aria-label="转职" style="width: 180px" :disabled="loading || savingTree || !!drag" />
      <div class="tree-kinds"><NButton v-for="k in kinds" :key="k.value" :type="kind === k.value ? 'primary' : 'default'" size="small" :disabled="loading || savingTree || !!drag" @click="kind = k.value; load()">{{ k.label }}</NButton></div>
      <span class="readonly">{{ dirty ? '未保存' : canEdit ? '可拖拽调整位置' : '只读' }}</span>
      <NButton size="small" :disabled="!canEdit || !dirty || !!drag" :loading="savingTree" @click="saveTree">保存到工作区</NButton>
      <NButton size="small" :disabled="loading || savingTree || !!drag" @click="scan">重新读取</NButton>
    </div>
    <div v-if="error" class="tree-error" role="alert">{{ error }} <NButton size="small" @click="scan">重试</NButton></div>
    <NSpin :show="loading">
      <div class="tree-layout">
        <main class="tree-scroll" aria-label="技能树画布">
          <template v-if="doc">
            <section class="common-skills" aria-label="通用技能">
              <span class="common-label">通用</span>
              <button v-for="(n, i) in doc.common" :key="i" class="common-node" :class="{ selected: selected === n }" :title="`${n.name} #${n.id}`" @click="selected = n">
                <ImageThumbnail :reference="n.icon" :size="36" show-fallback /><small>{{ n.name }}</small>
              </button>
              <span v-if="!doc.common?.length" class="muted">无通用技能</span>
            </section>
            <div v-if="currentBranch" class="canvas-space" :style="{ width: `${(width + 40) * zoom / 100}px`, height: `${height * zoom / 100}px` }">
              <div ref="canvas" class="tree-canvas" @click="clickBlank" @pointermove="moveLink" :class="{ linking: !!link, dragging: drag?.active, editable: canEdit }" :style="{ width: `${width + 40}px`, height: `${height}px`, transform: `scale(${zoom / 100})` }">
                <div v-for="row in rows" :key="row" class="tree-row" :class="{ alternate: row % 2 === 0 }" :style="{ top: `${row * 67}px` }"><span v-if="kind === 'sp'">{{ rowLevels.get(row) }}</span><span v-if="row === rows.length - 1 && canEdit" class="append-label">点击添加技能，或拖到此行新增一行</span></div>
                <svg class="tree-edges" :width="width" :height="height" :style="{ left: `${40 + nodeInsetX}px`, top: `${nodeInsetY}px` }" aria-label="技能后继关系">
                  <defs><marker id="skill-tree-arrow" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="6" markerHeight="6" orient="auto-start-reverse"><path d="M 0 0 L 8 4 L 0 8 z" fill="context-stroke" /></marker></defs>
                  <g v-for="edge in edges" :key="edge.key">
                    <path :d="edge.path" fill="none" :stroke="selectedEdge?.source === edge.source && selectedEdge.index === edge.order ? '#78cfa0' : selected && (edge.source.id === selected.id || edge.target.id === selected.id) ? '#e8b65e' : '#87744f'" stroke-width="1.7" marker-end="url(#skill-tree-arrow)" />
                    <path class="edge-hit" :d="edge.path" fill="none" stroke="transparent" stroke-width="12" role="button" tabindex="0" :aria-label="`关联 ${edge.source.name} → ${edge.target.name}（第 ${edge.order + 1} 条）`" @click.stop="selectEdge(edge.source, edge.order)" @keydown.enter.stop.prevent="selectEdge(edge.source, edge.order)" @contextmenu.prevent.stop="openEdgeMenu($event, edge.source, edge.order)"><title>点击后拖动目标端修改，右键删除关联</title></path>
                    <circle v-if="canEdit && selectedEdge?.source === edge.source && selectedEdge.index === edge.order" class="edge-endpoint" :cx="edge.end.x" :cy="edge.end.y" r="5" role="button" tabindex="0" aria-label="拖动修改关联目标" @pointerdown.stop.prevent="beginLink(edge.source, edge.order, $event)" @pointermove.stop="moveLink" @pointerup.stop="finishLink" @pointercancel="cancelLink" @lostpointercapture="cancelLink" @click.stop @keydown.enter.stop.prevent="beginLink(edge.source, edge.order)" />
                  </g>
                  <path v-if="link" class="link-preview" :class="{ valid: linkValid, invalid: link?.target && !linkValid }" :d="linkPreview" fill="none" stroke-width="2" stroke-dasharray="5 3" marker-end="url(#skill-tree-arrow)" />
                </svg>
                <button v-for="cell in cells" :key="cell.key" class="tree-node" :class="{ 'link-target': link?.target && cell.nodes.includes(link.target), 'link-invalid': link?.target && cell.nodes.includes(link.target) && !linkValid, selected: cell.nodes.includes(selected!), linked: cell.nodes.some(linked), movable: canEdit, moving: drag?.active && drag.cell.key === cell.key }" :style="{ left: `${cellNode(cell).x + 40 + nodeInsetX}px`, top: `${cellNode(cell).y + nodeInsetY}px` }" :title="cell.nodes.map(n => `${n.name} #${n.id}`).join('\n')" :aria-label="`${cellNode(cell).name}，技能 ${cellNode(cell).id}${cell.nodes.length > 1 ? `，同位置共 ${cell.nodes.length} 个技能` : ''}`" @click="clickCell(cell)" @contextmenu.prevent.stop="openNodeMenu($event, cell)" @pointerdown="startDrag($event, cell)" @pointermove="moveDrag" @pointerup="finishDrag" @pointercancel="cancelDrag" @lostpointercapture="cancelDrag" @dragstart.prevent>
                  <ImageThumbnail :reference="cellNode(cell).icon" :size="32" show-fallback /><small><span>{{ cellNode(cell).name }}</span></small>
                  <span v-if="cell.nodes.length > 1" class="overlap-count">{{ cell.nodes.length }}</span>
                </button>
                <button v-if="selected && selectedCell && canEdit && !drag" class="link-handle" :style="{ left: `${selected.x + 40 + nodeInsetX + 29}px`, top: `${selected.y + nodeInsetY + 21}px` }" title="拖到目标技能以添加后继关联；Enter 后点击目标技能" aria-label="拖动添加后继关联" @pointerdown.stop.prevent="beginLink(selected!, -1, $event)" @pointermove.stop="moveLink" @pointerup.stop="finishLink" @pointercancel="cancelLink" @lostpointercapture="cancelLink" @click.stop @keydown.enter.stop.prevent="beginLink(selected!, -1)" />
                <div v-if="drag?.active" class="drop-preview" :class="{ invalid: !drag.target.valid }" :style="{ left: `${drag.target.x + 40 + nodeInsetX}px`, top: `${drag.target.y + nodeInsetY}px` }" aria-hidden="true"><ImageThumbnail :reference="drag.node.icon" :size="32" show-fallback /></div>
              </div>
            </div>
            <NEmpty v-else description="当前分支未配置技能" class="tree-empty" />
          </template>
          <NEmpty v-else-if="!loading && !error" description="归档中没有注册技能树" class="tree-empty" />
        </main>
        <aside class="tree-detail">
          <template v-if="selected">
            <div v-if="selectedCell && selectedCell.nodes.length > 1" class="overlap-picker">
              <div class="overlap-title">同位置技能（{{ selectedCell.nodes.length }}）</div>
              <button v-for="(n, i) in selectedCell.nodes" :key="i" :class="{ active: selected === n }" :aria-pressed="selected === n" @click="selectCellNode(n)"><span>#{{ n.id }}</span>{{ n.name }}</button>
            </div>
            <div class="detail-title"><ImageThumbnail :reference="selected.icon" :size="48" show-fallback /><div><strong>{{ selected.name }}</strong><span>技能 #{{ selected.id }}</span></div></div>
            <p class="skill-explain">{{ selected.explain || '暂无技能描述' }}</p>
            <NButton block :disabled="selected.fileIndex < 0 || opening || editor.saving" :loading="opening" @click="openSkill">打开技能文件</NButton>
            <NButton v-if="selectedCell" class="delete-skill" block type="error" secondary :disabled="!canEdit || !!drag" @click="deleteNode(selected)">从技能树删除当前技能</NButton>
          </template>
          <NEmpty v-else description="点击技能查看详情与关联" />
          <div class="tree-notes"><p>选中技能后，拖动右侧圆点到目标技能可添加关联；点击连线后拖动目标端可修改，右键连线可删除。Esc 取消连线。</p><p>点击空白网格可添加当前职业的技能，右键或通过信息面板可删除当前技能。拖拽同位置技能会整组移动，绿色框表示可放置；修改后保存到工作区。图标需配置对应客户端的 NPK 资源。</p></div>
          <details v-if="doc?.issues?.length" class="tree-issues"><summary>数据提示（{{ doc.issues.length }}）</summary><p v-for="(issue, i) in doc.issues" :key="i">{{ issue }}</p></details>
        </aside>
      </div>
    </NSpin>
    <footer class="tree-footer"><button class="source-path" :disabled="!doc || opening || editor.saving" title="Cmd / Ctrl + 单击，在编辑器打开技能树文件" @click="openTreeSource" @keydown.enter="openTreeSource">{{ doc?.path || '从当前 PVF 读取' }}</button><span v-if="link" role="status">{{ link.index < 0 ? '添加关联' : '修改关联目标' }}：{{ link.source.name }} → {{ link.target?.name || '拖到或点击目标技能' }} <NButton size="tiny" @click="cancelLink">取消</NButton></span><span v-if="drag?.active" role="status">{{ drag.target.valid ? `放置到 ${drag.target.x / 47}, ${drag.target.y / 67}` : drag.target.reason }}</span><span>{{ nodes.length }} 个技能 · {{ edges.length }} 条连线</span><label>缩放 {{ zoom }}%</label><NSlider v-model:value="zoom" :disabled="!!drag || loading || savingTree" :min="80" :max="240" :step="10" style="width: 110px" aria-label="画布缩放" /></footer>
  </NModal>
</template>

<style scoped>
.add-actions { display: flex; justify-content: flex-end; gap: 10px; }
.tree-toolbar { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; padding: 12px 18px; border-bottom: 1px solid var(--pvf-border-subtle); }
.tree-kinds { display: flex; gap: 6px; } .readonly { margin-left: auto; color: var(--pvf-text-secondary); font-size: 12px; }
.tree-layout { display: grid; grid-template-columns: minmax(0, 1fr) 270px; height: min(72vh, 900px); min-height: 300px; }
.tree-scroll { overflow: auto; min-width: 0; background: #171a1d; }
.common-skills { display: flex; gap: 10px; align-items: center; min-width: max-content; padding: 18px 24px; border-bottom: 1px solid #454239; background: #202429; }
.common-label { color: #cab789; font-size: 13px; padding-right: 10px; writing-mode: vertical-rl; letter-spacing: 5px; }
.common-node { width: 62px; display: flex; flex-direction: column; align-items: center; gap: 7px; background: transparent; color: #d8d8d4; border: 1px solid transparent; padding: 5px 2px; cursor: pointer; }
.common-node small { font-size: 11px; width: 100%; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
.canvas-space { position: relative; margin-top: 14px; }
.tree-canvas { position: relative; transform-origin: top left; }
.tree-row { position: absolute; width: 100%; height: 67px; color: #9b967e; font-size: 10px; }
.tree-row.alternate { background-color: #202428; } .tree-row > span { position: absolute; top: 18px; left: 12px; }
.tree-row .append-label { left: 40px; right: 0; top: 0; height: 67px; display: flex; align-items: center; justify-content: center; color: #818577; font-size: 10px; pointer-events: none; border-top: 1px dashed #ffffff1a; }
.tree-edges { position: absolute; top: 6px; pointer-events: none; }
.edge-hit { pointer-events: stroke; cursor: pointer; }
.edge-hit:focus-visible { stroke: #78cfa066; outline: none; }
.edge-endpoint { fill: #171a1d; stroke: #78cfa0; stroke-width: 2; pointer-events: all; cursor: crosshair; }
.link-preview { stroke: #e8b65e; pointer-events: none; } .link-preview.valid { stroke: #78cfa0; } .link-preview.invalid { stroke: #e06c75; }
.link-handle { position: absolute; width: 10px; height: 10px; box-sizing: border-box; border: 2px solid #78cfa0; border-radius: 50%; background: #171a1d; padding: 0; cursor: crosshair; touch-action: none; z-index: 5; }
.tree-canvas.linking .tree-node { cursor: crosshair; }
.tree-node.link-target { outline: 2px solid #78cfa0; } .tree-node.link-invalid { outline-color: #e06c75; }
.tree-node { box-sizing: border-box; position: absolute; width: 34px; height: 56px; display: flex; flex-direction: column; align-items: center; padding: 0; border: 1px solid #666356; border-radius: 2px; background: #12171b; color: #c2bca9; cursor: pointer; }
.tree-node.movable { cursor: grab; touch-action: none; user-select: none; } .tree-node.moving { opacity: .35; cursor: grabbing; }
.tree-canvas.editable .tree-row { background-image: linear-gradient(to right, #ffffff0d 1px, transparent 1px); background-size: 47px 67px; background-position: 40px 0; cursor: crosshair; }
.tree-canvas.dragging { background-image: linear-gradient(to right, #ffffff14 1px, transparent 1px), linear-gradient(to bottom, #ffffff14 1px, transparent 1px); background-size: 47px 67px; background-position: 40px 0; }
.drop-preview { position: absolute; width: 34px; height: 56px; box-sizing: border-box; border: 2px solid #78cfa0; background: #183a2cbb; z-index: 4; pointer-events: none; } .drop-preview.invalid { border-color: #e06c75; background: #592c34bb; }
.overlap-count { position: absolute; right: -6px; top: -7px; min-width: 13px; height: 13px; padding: 0 2px; box-sizing: border-box; border-radius: 7px; background: #d4ad63; color: #211e18; font-size: 9px; line-height: 13px; font-weight: 700; }
.overlap-picker { margin-bottom: 20px; } .overlap-title { margin-bottom: 8px; font-size: 12px; color: var(--pvf-text-secondary); }
.overlap-picker button { display: flex; align-items: baseline; gap: 8px; width: 100%; padding: 8px; background: transparent; border: 1px solid var(--pvf-border-subtle); color: var(--pvf-text-primary); text-align: left; cursor: pointer; font: inherit; font-size: 12px; }
.overlap-picker button + button { border-top: 0; } .overlap-picker button.active { background: var(--pvf-primary-soft); color: var(--pvf-primary); } .overlap-picker button span { color: var(--pvf-text-secondary); font-size: 11px; }
.tree-node small { box-sizing: border-box; width: 100%; min-width: 0; min-height: 0; flex: 1; display: flex; align-items: center; line-height: 10px; font-size: 8px; padding-top: 2px; }
.tree-node small > span { width: 100%; min-width: 0; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; overflow-wrap: anywhere; text-align: center; } .tree-node.linked { border-color: #af9158; }
.tree-node:hover, .tree-node.selected, .common-node:hover, .common-node.selected { border-color: #f1c779; outline: 1px solid #c69c4c; color: #ffe0a4; }
button:focus-visible { outline: 2px solid #f1c779; outline-offset: 3px; }
.tree-detail { overflow: auto; border-left: 1px solid var(--pvf-border-subtle); padding: 22px 16px; }
.detail-title { display: flex; align-items: center; gap: 12px; } .detail-title > div { display: flex; flex-direction: column; gap: 6px; } .detail-title span { font-size: 12px; color: var(--pvf-text-secondary); }
.skill-explain { margin: 22px 0; font-size: 13px; line-height: 1.75; white-space: pre-wrap; overflow-wrap: anywhere; }
.delete-skill { margin-top: 12px; }
.tree-notes { margin-top: 28px; color: var(--pvf-text-secondary); font-size: 12px; line-height: 1.7; } .tree-issues { font-size: 12px; overflow-wrap: anywhere; } .tree-issues summary { cursor: pointer; color: #ba9450; }
.tree-footer { display: flex; align-items: center; gap: 16px; padding: 10px 18px; border-top: 1px solid var(--pvf-border-subtle); color: var(--pvf-text-secondary); font-size: 12px; } .source-path { flex: 1; min-width: 0; overflow-wrap: anywhere; padding: 0; border: 0; background: transparent; color: inherit; font: inherit; text-align: left; cursor: pointer; user-select: text; }
.source-path:hover:not(:disabled) { text-decoration: underline; }
.tree-error { padding: 12px 18px; color: var(--pvf-danger, #c95151); } .tree-empty { margin: 70px 0; } .muted { color: #aaa; }
@media (max-width: 850px) { .tree-layout { grid-template-columns: minmax(0, 1fr) 210px; } .tree-footer { flex-wrap: wrap; } }
</style>
