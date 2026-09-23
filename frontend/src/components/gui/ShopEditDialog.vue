<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { NAlert, NButton, NCheckbox, NInput, NModal, NSpin } from "naive-ui";
import { FileGUIService } from "../../../bindings/pvfine/services";
import type { ShopDocument, ShopEditRequest, ShopEditResult, ShopEntry, ShopItem } from "../../../bindings/pvfine/services/models";
import type { GUIFile } from "../../gui/types";
import { costForm, materialInputs, validateCosts } from "../../gui/shopForm";
import { useEditorStore } from "../../stores/editor";
import ItemPicker from "../ItemPicker.vue";
import ShopCostFields from "./ShopCostFields.vue";

const props = defineProps<{ file: GUIFile; document: ShopDocument; tabIndex: number; categoryID: string; action: string; entry?: ShopEntry }>();
const emit = defineEmits<{ (e: "close"): void; (e: "applied", result: ShopEditResult, tabIndex: number): void }>();
const editor = useEditorStore();
const source = { ...props.file };
const snapshot = props.document;
const targetTab = props.tabIndex;
const tab = snapshot.tabs?.[targetTab];
const sourceSnapshots = new Map(editor.tabs.map((tab) => [tab.path, { text: tab.text, dirty: tab.text !== tab.original }]));
const itemID = ref(props.entry?.item.id ?? "");
const item = ref<ShopItem | null>(props.entry?.item ?? null);
const form = ref(costForm(item.value));
const setGold = ref(props.action !== "batch-costs");
const setMaterials = ref(props.action !== "batch-costs");
const name = ref(tab?.name ?? "");
const newName = ref("");
const busy = ref(false);
const itemLoading = ref(false);
const error = ref("");
const itemError = ref("");
const deleteTabConfirm = ref(false);
let itemRequest = 0;
const editingItem = computed(() => props.action === "edit-item" || props.action === "add-item");
const title = computed(() => ({ "edit-item": "编辑商品", "add-item": "添加商品", "delete-item": "删除商品", "batch-costs": "批量设置分页成本", "manage-tabs": "分页管理" })[props.action]);
const uniqueItems = computed(() => new Set((tab?.groups ?? []).flatMap((group) => (group.items ?? []).map((entry) => entry.item.path || entry.item.id))).size);
const formError = computed(() => validateCosts(form.value, setGold.value, setMaterials.value));
const canApply = computed(() => !busy.value && !itemLoading.value && !itemError.value && !formError.value && (editingItem.value ? !!item.value && item.value.id === itemID.value : setGold.value || setMaterials.value));

function drafts() { return editor.tabs.filter((tab) => tab.text !== tab.original).map((tab) => ({ fileIndex: tab.index, path: tab.path, text: tab.text })); }
watch(itemID, async (id) => {
  if (!editingItem.value) return;
  const request = ++itemRequest;
  item.value = null; itemError.value = "";
  if (!id) { form.value = costForm(); itemLoading.value = false; return; }
  itemLoading.value = true;
  const currentDrafts = drafts();
  const requestSnapshots = new Map(editor.tabs.map((tab) => [tab.path, { text: tab.text, dirty: tab.text !== tab.original }]));
  try {
    const result = await FileGUIService.ReadItem(id, currentDrafts);
    if (request !== itemRequest) return;
    if (!result) throw new Error("物品不存在");
    item.value = result; form.value = costForm(result);
    const previous = requestSnapshots.get(result.path);
    if (previous) sourceSnapshots.set(result.path, previous);
  } catch (cause) { if (request === itemRequest) itemError.value = String(cause); }
  finally { if (request === itemRequest) itemLoading.value = false; }
}, { immediate: true });
onBeforeUnmount(() => { itemRequest++; });

function verifyItemDrafts(action: string) {
  if (!setGold.value && !setMaterials.value) return;
  const paths = action === "batch-costs"
    ? (tab?.groups ?? []).flatMap((group) => (group.items ?? []).map((entry) => entry.item.path))
    : item.value ? [item.value.path] : [];
  for (const path of paths) {
    const previous = sourceSnapshots.get(path);
    const current = editor.tabs.find((tab) => tab.path === path);
    if (previous ? (current ? current.text !== previous.text : previous.dirty) : current && current.text !== current.original) {
      throw new Error("关联商品草稿已变化，请关闭表单并重新打开");
    }
  }
}
async function apply(action: string) {
  if (busy.value) return;
  error.value = "";
  const costs = ["edit-item", "add-item", "batch-costs"].includes(action);
  if (costs && !canApply.value) { error.value = formError.value || itemError.value || "请选择商品或需要设置的成本"; return; }
  busy.value = true;
  try {
    if (costs) verifyItemDrafts(action);
    const request: ShopEditRequest = {
      fileIndex: source.index, path: source.path, text: source.text, revision: snapshot.revision,
      action, tabIndex: targetTab, sourceStart: props.entry?.sourceStart ?? -1,
      categoryId: props.categoryID, itemId: itemID.value, name: action === "add-tab" ? newName.value : name.value,
      setGold: costs && setGold.value, gold: form.value.gold,
      setMaterials: costs && setMaterials.value, materials: materialInputs(form.value), drafts: [],
    };
    const result = await editor.applyShopEdit(request);
    const nextTab = action === "add-tab" ? snapshot.tabs?.length ?? 0 : action === "delete-tab" ? Math.max(0, targetTab - 1) : targetTab;
    emit("applied", result, nextTab);
  } catch (cause) { error.value = String(cause); }
  finally { busy.value = false; }
}
</script>

<template>
  <NModal :show="true" preset="card" :title="title" :mask-closable="!busy" :closable="!busy"
    :close-on-esc="!busy" style="width: min(680px, calc(100vw - 40px))" @update:show="(show: boolean) => { if (!show && !busy) emit('close'); }">
    <div class="shop-dialog-body">
      <NAlert v-if="error" type="error">{{ error }}</NAlert>
      <template v-if="action === 'manage-tabs'">
        <label>新增分页</label>
        <div class="shop-dialog-row"><NInput v-model:value="newName" placeholder="分页名称" :disabled="busy" /><NButton :disabled="busy || !newName.trim()" @click="apply('add-tab')">新增</NButton></div>
        <template v-if="tab">
          <label>当前分页：{{ tab.name }}</label>
          <div class="shop-dialog-row"><NInput v-model:value="name" placeholder="新的分页名称" :disabled="busy" /><NButton :disabled="busy || !name.trim()" @click="apply('rename-tab')">重命名</NButton></div>
          <NButton type="error" secondary :disabled="busy" @click="deleteTabConfirm = true">删除当前分页</NButton>
          <NAlert v-if="deleteTabConfirm" type="warning">
            删除“{{ tab.name }}”及其所有分类中的商品引用，保留物品文件。
            <NButton type="error" :loading="busy" @click="apply('delete-tab')">确认删除分页</NButton>
          </NAlert>
        </template>
      </template>
      <template v-else-if="action === 'delete-item'">
        <p>从当前分页和分类中删除“{{ entry?.item.name }}”（ID {{ entry?.item.id }}）的这一条记录？物品文件将保留。</p>
        <div class="shop-dialog-footer"><NButton :disabled="busy" @click="emit('close')">取消</NButton><NButton type="error" :loading="busy" @click="apply('delete-item')">确认删除</NButton></div>
      </template>
      <template v-else>
        <template v-if="editingItem">
          <label>商品</label>
          <ItemPicker v-model="itemID" :label="item?.name" :disabled="busy" />
          <div v-if="itemLoading" role="status"><NSpin :size="16" /> 正在读取商品成本…</div>
          <NAlert v-if="itemError" type="error">{{ itemError }}</NAlert>
        </template>
        <template v-else>
          <div class="shop-dialog-row"><NCheckbox v-model:checked="setGold" :disabled="busy">设置金币</NCheckbox><NCheckbox v-model:checked="setMaterials" :disabled="busy">设置兑换道具</NCheckbox></div>
        </template>
        <ShopCostFields v-model="form" :disabled="busy || itemLoading || (editingItem && !item)" :gold-enabled="setGold" :materials-enabled="setMaterials" />
        <NAlert type="warning">价格与兑换道具保存在物品文件中，修改会影响所有引用该物品的商店。</NAlert>
        <div v-if="formError" class="shop-form-error">{{ formError }}</div>
        <div class="shop-dialog-footer"><NButton :disabled="busy" @click="emit('close')">取消</NButton><NButton type="primary" :loading="busy" :disabled="!canApply" @click="apply(action)">确认修改</NButton></div>
      </template>
    </div>
  </NModal>
</template>

<style scoped>
.shop-dialog-body { display: flex; flex-direction: column; gap: 14px; }
.shop-dialog-row { display: flex; align-items: center; gap: 10px; }
.shop-dialog-footer { display: flex; justify-content: flex-end; gap: 10px; margin-top: 6px; }
.shop-form-error { color: var(--pvf-error); }
</style>
