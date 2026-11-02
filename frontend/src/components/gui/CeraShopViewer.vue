<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, shallowRef, watch } from 'vue';
import { NButton, NEmpty, NInput, NModal, NPagination, NSelect, useMessage } from 'naive-ui';
import { ArchiveService, FileGUIService } from '../../../bindings/pvfine/services';
import type { ItemAppearance } from '../../../bindings/pvfine/services/models';
import type { GUIFile } from '../../gui/types';
import { ceraDate, ceraFieldLabel, ceraQuantityIndex, ceraSchemas, ceraTabLabels, ceraTabProducts, changeCeraProduct, newCeraValues, readCeraShop, type CeraDocument, type CeraProduct } from '../../gui/ceraShop';
import { useEditorStore } from '../../stores/editor';
import { useFileGUIStore } from '../../stores/fileGUI';
import { rarityTextStyle } from '../../rarity';
import ImageThumbnail from '../ImageThumbnail.vue';
import ItemPicker from '../ItemPicker.vue';

const props = defineProps<{ file: GUIFile; active: boolean; paneId?: string }>();
const emit = defineEmits<{ (e: 'close'): void }>();
const editor = useEditorStore(), gui = useFileGUIStore(), message = useMessage();
const doc = shallowRef<CeraDocument | null>(null), error = ref('');
const tab = ref(''), category = ref(''), query = ref(''), page = ref(1);
const pageSize = 60;
// One switch applies to all categories and files in this editor session.
const nameMode = computed({ get: () => gui.ceraShopNameMode, set: (value: 'product' | 'item') => { gui.ceraShopNameMode = value; } });
const names = shallowRef<Record<string, string>>({}), appearances = shallowRef<Record<string, ItemAppearance | null | undefined>>({});
const metadataError = ref(''), namesLoading = ref(false);
let generation = 0, iconGeneration = 0, disposed = false;
const form = reactive({ show: false, action: 'add' as 'add' | 'edit' | 'delete', section: 'item', values: [] as string[], target: undefined as CeraProduct | undefined, text: '', index: -1, path: '', epoch: 0, revision: 0 });
const schemas = computed(() => doc.value?.schemas ?? ceraSchemas);
const schema = computed(() => schemas.value[form.section] ?? ceraSchemas.item!);
const canEdit = computed(() => props.active && props.file.editable && !error.value && !editor.saving && !editor.guiApplying);
const dirty = computed(() => { const draft = editor.getFileDraft(props.file.index); return !!draft && draft.path === props.file.path && editor.isTextDirty(draft); });
const tabs = computed(() => doc.value?.tabs.filter(t => t !== 'exit') ?? []);
const tabCounts = computed(() => Object.fromEntries(tabs.value.map(t => [t, doc.value ? ceraTabProducts(doc.value, t).length : 0])));
const tabProducts = computed(() => doc.value ? ceraTabProducts(doc.value, tab.value) : []);
const categories = computed(() => Object.entries(schemas.value).filter(([name, value]) => value.tab === tab.value || tabProducts.value.some(p => p.section === name)).map(([value, schema]) => ({ value, label: schema.label + (doc.value?.blocked.has(value) ? '（布局未识别）' : '') })));
const editCategories = computed(() => Object.entries(schemas.value).filter(([name]) => !doc.value?.blocked.has(name) && !schemas.value[name]!.readOnly).map(([value, schema]) => ({ value, label: schema.label })));
const products = computed(() => {
  const q = query.value.trim().toLocaleLowerCase();
  return tabProducts.value.filter(p => (!category.value || p.section === category.value) && (!q || `${p.id} ${p.itemId} ${p.name} ${names.value[p.itemId] ?? ''}`.toLocaleLowerCase().includes(q)));
});
const shown = computed(() => products.value.slice((page.value - 1) * pageSize, page.value * pageSize));
const primaryFields = computed(() => [...new Set([0, 1, ceraQuantityIndex(schema.value), schema.value.name, schema.value.gold, schema.value.cash, schema.value.date].filter(i => i >= 0))]);
const extraFields = computed(() => Array.from({ length: schema.value.width }, (_, i) => i).filter(i => !primaryFields.value.includes(i)));
const number = (value: string) => Number(value).toLocaleString('zh-CN');
function displayedName(p: CeraProduct) { if (Number(p.itemId) <= 0) return p.name || `商品 #${p.id}`; return nameMode.value === 'item' ? names.value[p.itemId] || `物品 #${p.itemId}` : p.name || names.value[p.itemId] || `商品 #${p.id}`; }
const openingItem = ref(false);
async function openItemFile(event: MouseEvent, itemId: string) {
  if (!event.metaKey && !event.ctrlKey) return;
  event.preventDefault();
  event.stopPropagation();
  if (!props.active || openingItem.value || editor.saving || editor.guiApplying) return;
  const epoch = gui.epoch, sourceIndex = props.file.index, sourcePath = props.file.path, paneId = props.paneId;
  openingItem.value = true;
  try {
    const result = await ArchiveService.SearchItems(itemId, 0, 1);
    if (disposed || epoch !== gui.epoch || !props.active || sourceIndex !== props.file.index || sourcePath !== props.file.path) return;
    const target = result?.hits?.find(hit => hit?.id === itemId);
    if (!target || target.fileIndex < 0) throw new Error(`找不到物品 #${itemId} 对应的文件`);
    await editor.openFile(target.fileIndex, paneId);
  } catch (cause) { if (!disposed && epoch === gui.epoch && props.active) message.error(`打开物品文件失败：${String(cause)}`); }
  finally { openingItem.value = false; }
}
function read() {
  form.show = false;
  try {
    doc.value = readCeraShop(props.file.text); error.value = '';
    if (!tabs.value.includes(tab.value)) tab.value = tabs.value.find(t => ceraTabProducts(doc.value!, t).length > 0) ?? tabs.value[0] ?? '';
  } catch (cause) { error.value = String(cause); doc.value = null; }
}
watch(() => [props.file.index, props.file.path, props.file.text, gui.epoch], read, { immediate: true });
watch(tab, () => { category.value = ''; page.value = 1; });
watch([category, query], () => { page.value = 1; });
watch(() => products.value.length, length => { page.value = Math.min(page.value, Math.max(1, Math.ceil(length / pageSize))); });
watch(() => [props.active, gui.epoch, gui.revision, props.file.index, props.file.path], () => {
  generation++; iconGeneration++; names.value = {}; appearances.value = {}; metadataError.value = ''; namesLoading.value = false; form.show = false;
  if (props.active) { void loadNames(); void loadIcons(); }
}, { immediate: true });
watch(() => doc.value, () => { if (props.active) void loadNames(); });
watch(shown, () => { if (props.active) void loadIcons(); });
async function loadNames() {
  const request = ++generation;
  const visible = shown.value.map(p => p.itemId), all = doc.value?.products.map(p => p.itemId) ?? [];
  const ids = [...new Set([...visible, ...all])].filter(id => Number(id) > 0 && !Object.hasOwn(names.value, id));
  namesLoading.value = !!ids.length;
  try {
    for (let i = 0; i < ids.length; i += 400) {
      const batch = ids.slice(i, i + 400), result = await FileGUIService.ReadItemNames(batch);
      if (request !== generation) return;
      names.value = { ...names.value, ...Object.fromEntries(batch.map(id => [id, result?.[id] ?? ''])) };
    }
  } catch (cause) { if (request === generation) metadataError.value = `物品名加载失败：${String(cause)}`; }
  finally { if (request === generation) namesLoading.value = false; }
}
async function loadIcons() {
  const request = ++iconGeneration;
  const ids = [...new Set(shown.value.map(p => p.itemId))].filter(id => Number(id) > 0 && !Object.hasOwn(appearances.value, id));
  if (!ids.length) return;
  try {
    const result = await FileGUIService.ReadItemAppearances(ids);
    if (request !== iconGeneration) return;
    appearances.value = { ...appearances.value, ...Object.fromEntries(ids.map(id => [id, result?.[id] ?? null])) };
  } catch (cause) { if (request === iconGeneration) metadataError.value = `图标加载失败：${String(cause)}`; }
}
function retryMetadata() { metadataError.value = ''; void loadNames(); void loadIcons(); }
onBeforeUnmount(() => { disposed = true; generation++; iconGeneration++; });
function setFormSection(value: string) {
  try { form.section = value; form.values = newCeraValues(doc.value!, value); }
  catch (cause) { message.error(String(cause)); }
}
function openForm(action: 'add' | 'edit' | 'delete', product?: CeraProduct) {
  if (!canEdit.value || !doc.value || product && schemas.value[product.section]?.readOnly) return;
  try {
    const section = product?.section ?? (category.value && !doc.value.blocked.has(category.value) && !schemas.value[category.value]?.readOnly ? category.value : categories.value.find(c => !doc.value!.blocked.has(c.value) && !schemas.value[c.value]?.readOnly)?.value ?? 'item');
    const values = product ? product.fields.map(t => t.value) : newCeraValues(doc.value, section);
    Object.assign(form, { show: true, action, section, values, target: product, text: props.file.text, index: props.file.index, path: props.file.path, epoch: gui.epoch, revision: gui.revision });
  } catch (cause) { message.error(String(cause)); }
}
function apply() {
  try {
    if (!canEdit.value || form.text !== props.file.text || form.index !== props.file.index || form.path !== props.file.path || form.epoch !== gui.epoch || form.revision !== gui.revision) throw new Error('文件或归档已变化，请重新打开表单');
    editor.updateContent(props.file.index, changeCeraProduct(form.text, form.action, form.section, form.values, form.target));
    form.show = false;
    if (form.action === 'add') { tab.value = schemas.value[form.section]!.tab; category.value = form.section; query.value = ''; }
    message.success('已更新商城草稿，保存后写入工作区');
  } catch (cause) { message.error(String(cause)); }
}
async function save() {
  if (!canEdit.value || !dirty.value || form.show) return;
  const epoch = gui.epoch;
  try { if (await editor.saveFileTab(props.file.index) && epoch === gui.epoch) message.success('已保存到工作区；保存 PVF 后写入归档文件'); }
  catch (cause) { if (epoch === gui.epoch) message.error(String(cause)); }
}
</script>

<template>
  <section class="cera-shop" aria-label="商城 GUI">
    <div class="cera-toolbar">
      <div class="cera-name-mode" role="group" aria-label="名称显示方式"><button type="button" :aria-pressed="nameMode === 'product'" @click="nameMode = 'product'">商品名</button><button type="button" :aria-pressed="nameMode === 'item'" @click="nameMode = 'item'">物品名</button></div>
      <div class="cera-toolbar-actions"><NButton size="small" :disabled="!canEdit || tab === 'derivative'" @click="openForm('add')">新增商品</NButton><NButton size="small" type="primary" :loading="editor.saving" :disabled="!canEdit || !dirty || form.show" @click="save">保存商城{{ dirty ? ' *' : '' }}</NButton></div>
    </div>
    <div v-if="error" class="cera-error" role="alert">{{ error }}<NButton size="tiny" @click="read">重新加载</NButton></div>
    <template v-else-if="doc">
      <nav class="cera-tabs" role="tablist" aria-label="商城商品类型"><button v-for="t in tabs" :key="t" type="button" role="tab" :aria-selected="tab === t" @click="tab = t">{{ ceraTabLabels[t] ?? t }} <span>{{ tabCounts[t] }}</span></button><button v-if="doc.tabs.includes('exit')" type="button" class="cera-exit" @click="emit('close')">退出</button></nav>
      <div class="cera-filters"><NInput v-model:value="query" size="small" clearable placeholder="搜索商品名、物品名或编号" aria-label="搜索商城商品" /><NSelect v-model:value="category" size="small" :options="[{ value: '', label: '全部分类' }, ...categories]" aria-label="商品子分类" /></div>
      <p v-if="tab === 'mileage'" class="cera-hint">按商品积分配置聚合；这里显示商品售价，积分参数保持原值。</p>
      <p v-if="tab === 'hotandnew'" class="cera-hint">按热卖与新品配置聚合，编辑与删除会同步到原商品分类。</p>
      <p v-if="tab === 'derivative'" class="cera-hint">周边商品为外部链接配置，仅展示原始名称和价格参数。</p>
      <div v-if="metadataError" class="cera-error" role="alert">{{ metadataError }}<NButton size="tiny" @click="retryMetadata">重试</NButton></div>
      <div class="cera-products" role="tabpanel" :aria-label="ceraTabLabels[tab] ?? tab">
        <div v-if="shown.length" class="cera-grid">
          <article v-for="p in shown" :key="`${p.section}:${p.row}`" class="cera-card">
            <div class="cera-icon"><ImageThumbnail :reference="appearances[p.itemId]?.icon" :size="40" show-fallback animated /></div>
            <div class="cera-card-content"><div class="cera-product-name" :style="rarityTextStyle(appearances[p.itemId]?.rarity)" :title="`${p.name || '未配置商品名'}\n${names[p.itemId] || '物品 #' + p.itemId}`">{{ displayedName(p) }}</div><div class="cera-ids" :title="`商品编号 ${p.id} / 物品编号 ${p.itemId}`"><span>{{ p.id ? '#' + p.id : '外部商品' }}</span> <button v-if="Number(p.itemId) > 0" type="button" class="cera-item-link" :disabled="openingItem || editor.saving || editor.guiApplying || !active" :aria-label="`物品编号 ${p.itemId}，Cmd 或 Ctrl 点击打开物品文件`" title="Cmd / Ctrl + 点击打开物品文件" @click="openItemFile($event, p.itemId)">物品 {{ p.itemId }}</button></div><div class="cera-price"><span v-if="Number(p.cash) > 0" class="cera-cash">{{ number(p.cash) }} <small>{{ schemas[p.section]?.readOnly ? '价格参数' : '点券' }}</small></span><span v-if="Number(p.gold) > 0" class="cera-gold">{{ number(p.gold) }} <small>金币</small></span><span v-if="Number(p.cash) <= 0 && Number(p.gold) <= 0" class="cera-zero">{{ !p.cash && !p.gold ? '未配置价格' : '价格 0' }}</span><span v-if="Number(p.quantity) > 1" class="cera-quantity">×{{ p.quantity }}</span></div><div v-if="p.date" class="cera-date" title="尚未确认此参数是否代表过期时间">日期参数 {{ ceraDate(p.date) }}</div></div>
            <div v-if="!schemas[p.section]?.readOnly" class="cera-card-actions"><button type="button" :disabled="!canEdit" :aria-label="`编辑商品 ${p.id}`" @click="openForm('edit', p)">编辑</button><button type="button" class="cera-delete" :disabled="!canEdit" :aria-label="`删除商品 ${p.id}`" @click="openForm('delete', p)">删除</button></div>
          </article>
        </div>
        <NEmpty v-else description="当前分类没有匹配商品" class="cera-empty"><template #extra><NButton size="small" :disabled="!canEdit || tab === 'derivative'" @click="openForm('add')">新增商品</NButton></template></NEmpty>
      </div>
      <footer class="cera-footer"><span>{{ products.length.toLocaleString('zh-CN') }} 件匹配商品{{ namesLoading ? ' · 正在加载物品名…' : '' }}</span><NPagination v-model:page="page" :page-size="pageSize" :item-count="products.length" :page-slot="5" simple /></footer>
      <details v-if="doc.issues.length" class="cera-issues"><summary>{{ doc.issues.length }} 条数据提示</summary><p v-for="(issue, i) in doc.issues" :key="i">{{ issue }}</p></details>
    </template>
    <NModal v-model:show="form.show" preset="card" :title="form.action === 'delete' ? '删除商品' : form.action === 'add' ? '新增商品' : '编辑商品'" :mask-closable="false" style="width: 660px; max-width: 95vw">
      <div v-if="form.action === 'delete'" class="cera-delete-confirm"><p>删除商品 #{{ form.target?.id }}「{{ form.target ? displayedName(form.target) : '' }}」？</p><p>将同时移除已识别的限购、推荐、赠送限制和仓库映射记录。实际物品文件保留。</p></div>
      <div v-else class="cera-form">
        <label class="cera-form-wide">商品分类<NSelect :value="form.section" :options="editCategories" :disabled="form.action !== 'add'" @update:value="setFormSection" /></label>
        <template v-for="i in primaryFields" :key="i"><label :class="{ 'cera-form-wide': i === schema.name || i === 1 }">{{ ceraFieldLabel(schema, i) }}<template v-if="i === 1"><NInput v-model:value="form.values[i]" aria-label="物品编号" placeholder="物品编号" /><ItemPicker v-model="form.values[i]" :label="names[form.values[i]!]" @select="item => { if (item && schema.name >= 0 && !form.values[schema.name]) form.values[schema.name] = item.name; }" /></template><NInput v-else v-model:value="form.values[i]" :aria-label="ceraFieldLabel(schema, i)" :placeholder="i === schema.date ? '保留原始格式，如 20241105' : ''" /></label></template>
        <p class="cera-form-wide cera-form-hint">商品编号自动递增，也可手动修改；改号会同步已识别的商品引用。日期参数含义尚未确认，按原值展示。</p>
        <details class="cera-form-wide"><summary>其他参数</summary><div class="cera-extra-fields"><label v-for="i in extraFields" :key="i">{{ ceraFieldLabel(schema, i) }}<NInput v-model:value="form.values[i]" :aria-label="ceraFieldLabel(schema, i)" /></label></div></details>
      </div>
      <div class="cera-dialog-actions"><NButton @click="form.show = false">取消</NButton><NButton :type="form.action === 'delete' ? 'error' : 'primary'" :disabled="!canEdit" @click="apply">{{ form.action === 'delete' ? '删除商品' : '应用到草稿' }}</NButton></div>
    </NModal>
  </section>
</template>

<style scoped>
.cera-shop { width: 100%; min-width: 0; height: 100%; display: flex; flex-direction: column; overflow: hidden; color: #eee6d5; font: 12px/1.5 "Microsoft YaHei", "PingFang SC", sans-serif; border: 1px solid #243950; border-radius: 3px; background: #101820; }
.cera-heading { display: flex; align-items: center; justify-content: space-between; padding: 9px 14px; background: linear-gradient(180deg, #183c6d, #102642); border-bottom: 1px solid #314d68; }
.cera-heading > div { display: flex; align-items: baseline; gap: 12px; }.cera-heading h2 { color: #eed28b; font-size: 15px; margin: 0; }.cera-heading span { color: #9bb5d2; font-size: 11px; }
.cera-close { border: 0; background: transparent; color: #b8cde5; font-size: 22px; line-height: 20px; cursor: pointer; }
.cera-toolbar { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 10px; padding: 12px 14px; }.cera-toolbar-actions { display: flex; gap: 8px; }
.cera-name-mode { display: inline-flex; border: 1px solid #42546b; border-radius: 3px; overflow: hidden; }.cera-name-mode button { border: 0; background: #111e2a; color: #9caec3; font: inherit; padding: 4px 12px; cursor: pointer; }.cera-name-mode button[aria-pressed="true"] { color: #ffe1a1; background: #354257; box-shadow: inset 0 -2px #cfab5f; }
.cera-tabs { display: flex; overflow-x: auto; flex-shrink: 0; padding: 0 14px; border-bottom: 1px solid #3c4650; gap: 3px; }.cera-tabs button { font: inherit; border: 1px solid transparent; border-bottom: 0; background: transparent; color: #a5b3c1; padding: 7px 11px; white-space: nowrap; cursor: pointer; }.cera-tabs button[aria-selected="true"] { color: #ffe090; background: #292b28; border-color: #6e6044; box-shadow: inset 0 2px #cfab5f; }.cera-tabs span { font-size: 10px; margin-left: 3px; opacity: .7; }.cera-tabs .cera-exit { margin-left: auto; }
.cera-filters { display: grid; grid-template-columns: minmax(0, 1fr) 165px; padding: 12px 14px; gap: 10px; }.cera-hint { margin: -4px 14px 10px; color: #acb9c9; font-size: 11px; }
.cera-products { flex: 1; min-height: 0; overflow: auto; padding: 0 14px 14px; }.cera-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(min(260px, 100%), 1fr)); gap: 8px; align-content: start; }
.cera-card { display: grid; grid-template-columns: 46px minmax(0, 1fr); position: relative; gap: 10px; padding: 12px 12px 35px; min-height: 123px; box-sizing: border-box; background: linear-gradient(180deg, #1b2631, #131c25); border: 1px solid #32404d; border-radius: 2px; }.cera-card:hover, .cera-card:focus-within { border-color: #68778a; }.cera-icon { width: 44px; height: 44px; border: 1px solid #46505a; display: flex; justify-content: center; align-items: center; background: #090e14; }.cera-card-content { min-width: 0; }.cera-product-name { color: #8dd5ec; font-weight: 500; line-height: 1.5; min-height: 18px; overflow-wrap: anywhere; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
.cera-ids { color: #8b9aab; font-size: 10px; margin: 4px 0 7px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-variant-numeric: tabular-nums; }.cera-item-link { margin-left: 7px; padding: 0; border: 0; background: transparent; color: inherit; font: inherit; cursor: pointer; }.cera-item-link:hover:enabled { color: #a6dcff; text-decoration: underline; }.cera-item-link:disabled { opacity: .5; cursor: default; }.cera-price { display: flex; flex-wrap: wrap; gap: 8px; align-items: baseline; font-variant-numeric: tabular-nums; }.cera-price small { font-size: 10px; font-weight: normal; }.cera-cash { color: #a6dcff; font-size: 15px; font-weight: 600; }.cera-gold { color: #f1cf7d; font-size: 14px; }.cera-zero, .cera-quantity { color: #a9b6c7; font-size: 11px; }.cera-date { color: #c5ad83; font-size: 10px; margin-top: 5px; }
.cera-card-actions { position: absolute; bottom: 6px; right: 8px; display: flex; gap: 5px; }.cera-card-actions button { border: 1px solid #3e5871; border-radius: 2px; background: #192d40; color: #b3d5f2; padding: 2px 8px; font: inherit; font-size: 11px; cursor: pointer; }.cera-card-actions .cera-delete { background: transparent; border-color: transparent; color: #c89e9e; }.cera-card-actions button:disabled { opacity: .4; cursor: default; }
.cera-footer { display: flex; align-items: center; flex-wrap: wrap; gap: 10px; justify-content: space-between; padding: 9px 14px; border-top: 1px solid #2e3d4c; color: #9caec3; font-size: 11px; }.cera-error { margin: 8px 14px; padding: 8px; color: #f3b7ae; background: #3d2427; display: flex; align-items: center; gap: 10px; }.cera-empty { padding: 55px 0; }.cera-issues { padding: 8px 14px; color: #d7ba7d; max-height: 110px; overflow: auto; }.cera-issues summary { cursor: pointer; }.cera-issues p { margin: 6px 0; }
.cera-form, .cera-extra-fields { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 14px; }.cera-form label, .cera-extra-fields label { display: flex; flex-direction: column; gap: 6px; font-size: 12px; }.cera-form-wide { grid-column: 1 / -1; }.cera-form-hint, .cera-delete-confirm p + p { font-size: 12px; color: var(--pvf-text-secondary); margin: 0; line-height: 1.7; }.cera-extra-fields { padding-top: 14px; }.cera-form summary { cursor: pointer; font-size: 12px; }.cera-dialog-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 22px; }
button:focus-visible { outline: 2px solid #83c9e6; outline-offset: -2px; }
@media (max-width: 480px) { .cera-filters { grid-template-columns: 1fr; gap: 8px; }.cera-toolbar { gap: 8px; }.cera-tabs { padding: 0 8px; }.cera-tabs button { padding: 7px 9px; }.cera-form, .cera-extra-fields { grid-template-columns: 1fr; } }
</style>
