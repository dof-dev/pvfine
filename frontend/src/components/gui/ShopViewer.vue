<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { FileGUIService } from "../../../bindings/pvfine/services";
import ShopEditDialog from "./ShopEditDialog.vue";
import type { ShopEntry, ShopEditResult } from "../../../bindings/pvfine/services/models";
import { useEditorStore } from "../../stores/editor";
import ImageThumbnail from "../ImageThumbnail.vue";
import { createShopSession } from "../../gui/state";
import type { GUIFile } from "../../gui/types";
import { useFileGUIStore } from "../../stores/fileGUI";
import { rarityColor } from "../../rarity";

const props = defineProps<{ file: GUIFile; active: boolean }>();
const emit = defineEmits<{ (e: "close"): void }>();

const gui = useFileGUIStore();
const editor = useEditorStore();
const dialog = ref<{ kind: string; entry?: ShopEntry; key: number } | null>(null);
let dialogSequence = 0;
const editMessage = ref("");
const canEdit = computed(() => props.file.editable && !loading.value && !error.value && !editor.saving);
function openEdit(kind: string, entry?: ShopEntry) {
  if (canEdit.value) dialog.value = { kind, entry, key: ++dialogSequence };
}
async function applied(result: ShopEditResult, nextTab: number) {
  dialog.value = null;
  editMessage.value = editor.guiRefreshWarning || `已更新 ${result.files?.length ?? 0} 个文件，保存 PVF 后落盘`;
  await reload();
  tabIndex.value = nextTab;
}
watch(() => [props.file.index, props.file.path, props.active, gui.epoch], () => { dialog.value = null; editMessage.value = ""; });
const session = createShopSession(FileGUIService.ReadShop);
const { document, loading, error, tabIndex, categoryID } = session;

const items = computed(() => document.value?.tabs?.[tabIndex.value]?.groups
  ?.filter((group) => !!group)
  .filter((group) => !document.value?.categoryType || group.categoryId === categoryID.value)
  .flatMap((group) => group.items ?? []) ?? []);

const currentCategoryName = computed(() => {
  if (!document.value?.categories?.length) return "全部";
  return document.value.categories.find((c) => c.id === categoryID.value)?.name ?? "全部";
});

const number = (value: string) => value.replace(/\B(?=(\d{3})+(?!\d))/g, ",");
// 商品名按稀有度着色；没有 [rarity] 的物品（如材料）保留默认的青色。
function itemNameStyle(entry: ShopEntry): Record<string, string> | undefined {
  const color = rarityColor(entry.item.rarity);
  return color ? { color } : undefined;
}
function reload() { return session.load(props.file, String(gui.epoch)); }

watch(
  () => [props.active, props.file.index, props.file.path, props.file.text, gui.epoch, gui.revision] as const,
  () => { if (editor.guiApplying) return; if (props.active) void reload(); else session.invalidate(); },
  { immediate: true, flush: "sync" },
);
onBeforeUnmount(() => session.invalidate(true));
</script>

<template>
  <section class="shop-view" :aria-busy="loading" aria-label="商店商品">
    <header class="shop-title">
      <div class="shop-title-placeholder" aria-hidden="true" />
      <h2>{{ document?.name ?? "商店" }}</h2>
      <button
        type="button"
        class="shop-close-btn"
        title="关闭商店界面"
        aria-label="关闭"
        @click="emit('close')"
      >
        <svg width="8" height="8" viewBox="0 0 8 8" fill="none" aria-hidden="true">
          <path d="M1 1L7 7M7 1L1 7" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
        </svg>
      </button>
    </header>

    <div v-if="error" class="shop-error" role="alert">
      <span>{{ document ? "更新失败，以下为上次加载的内容。" : "商店加载失败。" }}{{ error }}</span>
      <button type="button" @click="reload">重试</button>
    </div>

    <div v-if="!document && loading" class="shop-skeleton" aria-hidden="true">
      <div class="skeleton-navigation">
        <div class="skeleton-tabs skeleton-pulse" />
        <div class="skeleton-category skeleton-pulse" />
      </div>
      <div class="shop-grid">
        <div v-for="i in 10" :key="i" class="shop-card skeleton-card">
          <div class="skeleton-icon skeleton-pulse" />
          <div class="shop-card-main">
            <div class="skeleton-name skeleton-pulse" />
            <div class="skeleton-price skeleton-pulse" />
          </div>
        </div>
      </div>
    </div>

    <div v-else-if="document" class="shop-content">
      <div class="shop-navigation">
        <div class="shop-tabs" role="tablist" aria-label="商品分页">
          <button
            v-for="(tab, i) in document.tabs"
            :key="tab.sourceStart"
            type="button"
            role="tab"
            :aria-selected="tabIndex === i"
            :disabled="loading"
            class="shop-tab-btn"
            :class="{ selected: tabIndex === i }"
            @click="tabIndex = i"
          >
            {{ tab.name }}
          </button>
        </div>

        <div v-if="document.categoryType" class="shop-category-box">
          <span class="shop-category-label">{{ currentCategoryName }}</span>
          <span class="shop-category-arrow" aria-hidden="true">
            <svg width="8" height="6" viewBox="0 0 8 6" fill="none">
              <path d="M1 1.5L4 4.5L7 1.5" stroke="#eed28b" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
          </span>
          <select
            v-model="categoryID"
            :disabled="loading"
            class="shop-category-select"
            :aria-label="document.categoryType === 'basic job' ? '职业分类' : '商品分类'"
          >
            <option v-for="category in document.categories" :key="category.id" :value="category.id">
              {{ category.name }}
            </option>
          </select>
        </div>
      </div>

      <div class="shop-products" role="tabpanel" :aria-label="document.tabs?.[tabIndex]?.name">
        <div v-if="items.length" class="shop-grid">
          <article v-for="entry in items" :key="entry.sourceStart" class="shop-card">
            <div v-if="file.editable" class="shop-item-actions" role="toolbar" :aria-label="`操作商品 ${entry.item.name}`">
              <button
                type="button"
                class="shop-item-btn shop-item-btn-edit"
                :disabled="!canEdit"
                :aria-label="`编辑商品 ${entry.item.id}`"
                title="编辑商品"
                @click="openEdit('edit-item', entry)"
              >
                <svg width="14" height="14" viewBox="0 0 16 16" fill="none" aria-hidden="true">
                  <path d="M11.5 2.5l2 2L5 13H3v-2l8.5-8.5z" stroke="currentColor" stroke-width="1.3" stroke-linejoin="round" />
                  <path d="M9.5 4.5l2 2" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" />
                </svg>
              </button>
              <button
                type="button"
                class="shop-item-btn shop-item-btn-delete"
                :disabled="!canEdit"
                :aria-label="`删除商品 ${entry.item.id}`"
                title="删除商品"
                @click="openEdit('delete-item', entry)"
              >
                <svg width="14" height="14" viewBox="0 0 16 16" fill="none" aria-hidden="true">
                  <path d="M2.5 4h11M6 4V2.5a.5.5 0 0 1 .5-.5h3a.5.5 0 0 1 .5.5V4M4.5 4l.65 8.45A1.5 1.5 0 0 0 6.64 14h2.72a1.5 1.5 0 0 0 1.49-1.55L11.5 4" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round" />
                  <path d="M6.5 7v4M9.5 7v4" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" />
                </svg>
              </button>
            </div>
            <div class="shop-icon">
              <ImageThumbnail :reference="entry.item.icon" :size="32" show-fallback animated />
            </div>
            <div class="shop-card-main">
              <div class="shop-item-name" :style="itemNameStyle(entry)" :title="`${entry.item.name}\nID: ${entry.item.id}`">
                {{ entry.item.name }}
              </div>
              <div class="shop-costs">
                <span v-if="!entry.item.costs?.length" class="shop-cost-box shop-no-price">未配置价格</span>
                <span
                  v-for="(cost, i) in entry.item.costs"
                  :key="i"
                  class="shop-cost-box"
                  :title="cost.kind === 'gold' ? '金币' : `${cost.name} (ID: ${cost.itemId})`"
                >
                  <span class="shop-cost-amount">{{ number(cost.quantity) }}</span>
                  <svg v-if="cost.kind === 'gold'" class="gold-icon" width="14" height="14" viewBox="0 0 14 14" role="img" aria-label="金币">
                    <ellipse cx="6" cy="9.5" rx="4.8" ry="2.6" fill="#84500d" stroke="#a46d1b" stroke-width="0.5" />
                    <ellipse cx="6" cy="8.2" rx="4.8" ry="2.5" fill="#dfa421" stroke="#f6ce56" stroke-width="0.5" />
                    <ellipse cx="8.5" cy="5.2" rx="4.5" ry="2.4" fill="#84500d" stroke="#a46d1b" stroke-width="0.5" />
                    <ellipse cx="8.5" cy="4.2" rx="4.5" ry="2.3" fill="#f4c840" stroke="#ffeb86" stroke-width="0.5" />
                    <ellipse cx="8.5" cy="4.2" rx="3" ry="1.4" fill="none" stroke="#fff4a8" stroke-width="0.5" stroke-opacity="0.8" />
                  </svg>
                  <ImageThumbnail v-else :reference="cost.icon" :size="14" show-fallback animated />
                  <span class="sr-only">{{ cost.name }}</span>
                </span>
              </div>
            </div>
          </article>
        </div>
        <div v-else class="shop-empty">当前分页和分类下没有商品</div>
      </div>

      <footer class="shop-footer">
        <div class="shop-actions">
          <button type="button" class="shop-action-btn" :disabled="!canEdit || !document.tabs?.length" @click="openEdit('add-item')">添加商品</button>
          <button type="button" class="shop-action-btn" :disabled="!canEdit" @click="openEdit('manage-tabs')">分页管理</button>
          <button type="button" class="shop-action-btn" :disabled="!canEdit || !document.tabs?.length" @click="openEdit('batch-costs')">批量设置</button>
        </div>
        <div v-if="!error && document" class="shop-footer-meta" :title="`当前显示 ${items.length} 件商品`">
          <span class="shop-meta-dot" />
          <span>{{ items.length }} 件商品</span>
        </div>
      </footer>

      <div v-if="loading || editMessage" class="shop-edit-status" role="status">{{ loading ? "正在更新…" : editMessage }}</div>
      <details v-if="document.issues?.length" class="shop-issues">
        <summary>{{ document.issues.length }} 条数据提示</summary>
        <ul><li v-for="(issue, i) in document.issues" :key="i">{{ issue.message }}</li></ul>
      </details>
    </div>
    <ShopEditDialog v-if="dialog && document" :key="dialog.key" :file="file" :document="document"
      :tab-index="tabIndex" :category-i-d="categoryID" :action="dialog.kind" :entry="dialog.entry"
      @close="dialog = null" @applied="applied" />
  </section>
</template>

<style scoped>
.shop-item-actions {
  position: absolute;
  inset: 0;
  z-index: 3;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 12px;
  background: rgba(10, 15, 23, 0.78);
  backdrop-filter: blur(2px);
  -webkit-backdrop-filter: blur(2px);
  opacity: 0;
  pointer-events: none;
  transition: opacity 0.18s cubic-bezier(0.4, 0, 0.2, 1);
}

.shop-card:hover .shop-item-actions,
.shop-card:focus-within .shop-item-actions {
  opacity: 1;
  pointer-events: auto;
}

.shop-item-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  padding: 0;
  border-radius: 4px;
  cursor: pointer;
  transform: scale(0.85);
  transition: transform 0.16s cubic-bezier(0.34, 1.56, 0.64, 1),
    background 0.15s ease,
    border-color 0.15s ease,
    color 0.15s ease,
    box-shadow 0.15s ease;
}

.shop-item-btn svg {
  flex-shrink: 0;
  pointer-events: none;
}

.shop-card:hover .shop-item-btn,
.shop-card:focus-within .shop-item-btn {
  transform: scale(1);
}

.shop-item-btn-edit {
  background: linear-gradient(180deg, #19365c 0%, #0f223b 100%);
  border: 1px solid #2b568c;
  color: #9cd2fa;
  box-shadow: 0 2px 5px rgba(0, 0, 0, 0.5), inset 0 1px 0 rgba(255, 255, 255, 0.16);
}

.shop-item-btn-edit:hover:not(:disabled) {
  background: linear-gradient(180deg, #254e85 0%, #15345d 100%);
  border-color: #4b92ec;
  color: #ffffff;
  box-shadow: 0 3px 8px rgba(39, 111, 204, 0.45), inset 0 1px 0 rgba(255, 255, 255, 0.3);
  transform: scale(1.1);
}

.shop-item-btn-delete {
  background: linear-gradient(180deg, #3d1b1b 0%, #261111 100%);
  border: 1px solid #6b2d2d;
  color: #fca5a5;
  box-shadow: 0 2px 5px rgba(0, 0, 0, 0.5), inset 0 1px 0 rgba(255, 255, 255, 0.16);
}

.shop-item-btn-delete:hover:not(:disabled) {
  background: linear-gradient(180deg, #5b2222 0%, #3b1515 100%);
  border-color: #a84242;
  color: #ffffff;
  box-shadow: 0 3px 8px rgba(184, 45, 45, 0.45), inset 0 1px 0 rgba(255, 255, 255, 0.3);
  transform: scale(1.1);
}

.shop-item-btn:active:not(:disabled) {
  transform: scale(0.95);
}

.shop-item-btn:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.shop-item-btn:focus-visible,
.shop-action-btn:focus-visible {
  outline: 2px solid #83c9e6;
  outline-offset: 1px;
}

.shop-action-btn:not(:disabled) {
  cursor: pointer;
  opacity: 1;
}

.shop-action-btn:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.shop-edit-status {
  padding: 0 10px 6px;
  color: #a9c3d5;
  font-size: 11px;
}

.shop-view {
  color: #eee6d5;
  width: 100%;
  max-width: 480px;
  height: 100%;
  min-height: 320px;
  margin: 0 auto;
  background: transparent;
  border: 1px solid #14243b;
  box-shadow: 0 4px 20px rgba(0, 0, 0, 0.6), inset 0 0 0 1px #1d3350;
  border-radius: 2px;
  box-sizing: border-box;
  font-size: 12px;
  font-family: "Microsoft YaHei", "PingFang SC", "SimSun", sans-serif;
  user-select: none;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.shop-title {
  flex: 0 0 28px;
  display: grid;
  grid-template-columns: 20px 1fr 20px;
  align-items: center;
  height: 28px;
  padding: 0 8px;
  background: linear-gradient(180deg, #183c6d 0%, #11284a 52%, #0a1b33 100%);
  border-bottom: 1px solid #060e18;
  box-shadow: inset 0 1px 0 rgba(77, 147, 230, 0.35);
  border-radius: 2px 2px 0 0;
  box-sizing: border-box;
}

.shop-title h2 {
  margin: 0;
  color: #eed28b;
  font-size: 13px;
  font-weight: bold;
  text-align: center;
  text-shadow: 0 1px 2px #000, 0 0 2px #000, 1px 0 0 #000, -1px 0 0 #000;
  letter-spacing: 0.5px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.shop-title-placeholder {
  width: 16px;
  height: 16px;
}

.shop-close-btn {
  width: 16px;
  height: 16px;
  padding: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(180deg, #1d3e69 0%, #0d1e34 100%);
  border: 1px solid #28548e;
  border-radius: 2px;
  color: #9cbde6;
  cursor: pointer;
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.15);
  transition: all 0.15s ease;
}

.shop-close-btn:hover {
  background: #255088;
  border-color: #3b74bf;
  color: #ffffff;
}

.shop-content {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  animation: shop-reveal 150ms ease-out;
}

.shop-navigation {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 8px 10px 6px;
  box-sizing: border-box;
}

.shop-tabs {
  display: flex;
  flex: 1;
  flex-wrap: wrap;
  gap: 4px;
}

.shop-tab-btn {
  font-family: inherit;
  font-size: 12px;
  color: #8c7f6e;
  background: #171412;
  border: 1px solid #4a3e2e;
  border-radius: 2px;
  padding: 3px 10px;
  cursor: pointer;
  transition: all 0.15s ease;
  white-space: nowrap;
}

.shop-tab-btn:hover:not(:disabled) {
  color: #c9b593;
  border-color: #6e5c44;
  background: #231e19;
}

.shop-tab-btn.selected {
  color: #ffe090;
  font-weight: bold;
  background: linear-gradient(180deg, #3d311c 0%, #201a11 100%);
  border-color: #cfab5f;
  box-shadow: inset 0 1px 0 #eed08c, inset 0 0 4px rgba(238, 208, 140, 0.2);
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.9);
}

.shop-tab-btn:disabled {
  opacity: 0.55;
  cursor: wait;
}

.shop-tab-btn:focus-visible,
.shop-close-btn:focus-visible,
.shop-category-select:focus-visible,
.shop-issues summary:focus-visible {
  outline: 2px solid #83c9e6;
  outline-offset: 1px;
}

.shop-category-box {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: space-between;
  height: 22px;
  min-width: 82px;
  background: #080a0c;
  border: 1px solid #544430;
  border-radius: 2px;
  box-shadow: inset 0 1px 2px rgba(0, 0, 0, 0.8);
  box-sizing: border-box;
}

.shop-category-label {
  color: #dfca92;
  font-size: 12px;
  padding: 0 7px;
  white-space: nowrap;
  user-select: none;
}

.shop-category-arrow {
  width: 20px;
  height: 20px;
  background: linear-gradient(180deg, #184175 0%, #0d2342 100%);
  border-left: 1px solid #234c82;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  box-sizing: border-box;
}

.shop-category-select {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  opacity: 0;
  cursor: pointer;
}

.shop-category-select option {
  background: #181d24;
  color: #eed28b;
}

.shop-products {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 0 10px;
  box-sizing: border-box;
}

.shop-products::-webkit-scrollbar {
  width: 6px;
}
.shop-products::-webkit-scrollbar-track {
  background: #080b0f;
  border-left: 1px solid #1c2635;
}
.shop-products::-webkit-scrollbar-thumb {
  background: #2a3d52;
  border-radius: 1px;
}
.shop-products::-webkit-scrollbar-thumb:hover {
  background: #3b5573;
}

.shop-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 4px 6px;
  align-content: start;
}

.shop-card {
  position: relative;
  display: grid;
  grid-template-columns: 36px minmax(0, 1fr);
  gap: 6px;
  align-items: center;
  min-height: 56px;
  padding: 4px 6px;
  box-sizing: border-box;
  border: 1px solid #282f37;
  border-radius: 2px;
  background: linear-gradient(180deg, #171c21 0%, #101317 100%);
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.07), inset 0 -1px 0 rgba(0, 0, 0, 0.7), 0 1px 2px rgba(0, 0, 0, 0.5);
  transition: border-color 0.15s ease, background 0.15s ease;
  overflow: hidden;
}

.shop-card:hover {
  border-color: #445160;
  background: linear-gradient(180deg, #1d2228 0%, #14171b 100%);
}

.shop-icon {
  width: 36px;
  height: 36px;
  flex-shrink: 0;
  border: 1px solid #363e48;
  background: #080a0c;
  box-shadow: inset 1px 1px 2px rgba(0, 0, 0, 0.9);
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 1px;
  overflow: hidden;
}

.shop-card-main {
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  min-width: 0;
  height: 100%;
  padding: 1px 0;
}

.shop-item-name {
  color: #7ed8ec;
  font-size: 12px;
  font-weight: 500;
  line-height: 1.25;
  text-shadow: 0 1px 2px #000, 0 0 2px #000;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  word-break: break-word;
}

.shop-costs {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  align-items: center;
  gap: 4px;
  margin-top: 2px;
}

.shop-cost-box {
  display: inline-flex;
  align-items: center;
  justify-content: flex-end;
  gap: 4px;
  height: 18px;
  padding: 0 5px;
  background: #06080a;
  border: 1px solid #20252b;
  border-radius: 2px;
  box-shadow: inset 1px 1px 2px rgba(0, 0, 0, 0.85);
  box-sizing: border-box;
}

.shop-cost-amount {
  color: #ffffff;
  font-size: 11px;
  font-family: Tahoma, "Segoe UI", Arial, sans-serif;
  font-variant-numeric: tabular-nums;
  letter-spacing: 0.2px;
  text-shadow: 0 1px 1px #000;
}

.gold-icon {
  flex: none;
}

.shop-no-price {
  color: #8c7f6e;
  font-size: 11px;
}

.shop-footer {
  flex: 0 0 auto;
  position: relative;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 8px 10px 10px;
  margin-top: auto;
  border-top: 1px solid rgba(255, 255, 255, 0.04);
}

.shop-actions {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
}

.shop-action-btn {
  font-family: inherit;
  font-size: 12px;
  color: #63bbf3;
  background: linear-gradient(180deg, #183a66 0%, #102542 50%, #0a172a 100%);
  border: 1px solid #255188;
  border-radius: 2px;
  padding: 3px 14px;
  height: 24px;
  box-shadow: inset 0 1px 0 #3a6fae, inset 0 -1px 0 #050c17, 0 1px 2px rgba(0, 0, 0, 0.6);
  text-shadow: 0 1px 2px #000;
  cursor: default;
  opacity: 0.85;
  transition: all 0.15s ease;
  user-select: none;
}

.shop-footer-meta {
  position: absolute;
  right: 12px;
  display: inline-flex;
  align-items: center;
  gap: 5px;
  color: #7b889b;
  font-size: 11px;
}

@media (max-width: 420px) {
  .shop-footer-meta {
    display: none;
  }
}

.shop-meta-dot {
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: #3986c7;
}

.shop-empty {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 40px 12px;
  text-align: center;
  color: #8c7f6e;
  font-size: 12px;
}

.shop-error {
  flex: 0 0 auto;
  margin: 8px 10px;
  padding: 8px 12px;
  background: rgba(120, 30, 20, 0.7);
  border: 1px solid #9e3d30;
  border-radius: 2px;
  color: #ffd8d0;
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 12px;
}

.shop-error span {
  flex: 1;
  overflow-wrap: anywhere;
}

.shop-error button {
  font: inherit;
  background: #3a1c18;
  border: 1px solid #c25244;
  color: #fff;
  border-radius: 2px;
  padding: 2px 8px;
  cursor: pointer;
}

.shop-issues {
  flex: 0 0 auto;
  max-height: 120px;
  overflow-y: auto;
  margin: 4px 10px 8px;
  padding: 6px 10px;
  background: rgba(15, 20, 26, 0.85);
  border: 1px solid #3d4957;
  border-radius: 2px;
  color: #e5cd90;
  font-size: 11px;
}

.shop-issues summary {
  cursor: pointer;
}

.shop-issues ul {
  margin: 6px 0 0;
  padding-left: 18px;
  line-height: 1.5;
  color: #b0c0d0;
}

/* Skeleton styles */
.shop-skeleton {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  padding: 0 10px 10px;
}

.skeleton-navigation {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 0;
}

.skeleton-tabs {
  width: 150px;
  height: 24px;
}

.skeleton-category {
  width: 82px;
  height: 22px;
}

.shop-skeleton .shop-grid {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
}

.skeleton-card {
  pointer-events: none;
}

.skeleton-icon {
  width: 36px;
  height: 36px;
}

.skeleton-name {
  height: 12px;
  margin-top: 2px;
  width: 85%;
}

.skeleton-price {
  height: 16px;
  width: 65%;
  align-self: flex-end;
}

.skeleton-pulse {
  background: #1e242d;
  border-radius: 2px;
  animation: shop-pulse 1.3s ease-in-out infinite alternate;
}

.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
}

@keyframes shop-pulse {
  to {
    opacity: 0.35;
  }
}

@keyframes shop-reveal {
  from {
    opacity: 0;
  }
  to {
    opacity: 1;
  }
}

@media (prefers-reduced-motion: reduce) {
  .skeleton-pulse,
  .shop-content {
    animation: none;
  }

  .shop-item-actions,
  .shop-item-btn {
    transition: none;
    transform: none;
  }
}
</style>
