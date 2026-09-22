<script setup lang="ts">
import { computed, onBeforeUnmount, watch } from "vue";
import { FileGUIService } from "../../../bindings/pvfine/services";
import ImageThumbnail from "../ImageThumbnail.vue";
import { createShopSession } from "../../gui/state";
import type { GUIFile } from "../../gui/types";
import { useFileGUIStore } from "../../stores/fileGUI";

const props = defineProps<{ file: GUIFile; active: boolean }>();
const gui = useFileGUIStore();
const session = createShopSession(FileGUIService.ReadShop);
const { document, loading, error, tabIndex, categoryID } = session;
const items = computed(() => document.value?.tabs?.[tabIndex.value]?.groups
  ?.filter((group) => !!group)
  .filter((group) => !document.value?.categoryType || group.categoryId === categoryID.value)
  .flatMap((group) => group.items ?? []) ?? []);
const number = (value: string) => value.replace(/\B(?=(\d{3})+(?!\d))/g, ",");
function reload() { return session.load(props.file, String(gui.epoch)); }
watch(
  () => [props.active, props.file.index, props.file.path, props.file.text, gui.epoch, gui.revision] as const,
  () => { if (props.active) void reload(); else session.invalidate(); },
  { immediate: true, flush: "sync" },
);
onBeforeUnmount(() => session.invalidate(true));
</script>

<template>
  <section class="shop-view" :aria-busy="loading" aria-label="商店商品">
    <header class="shop-title">
      <h2>{{ document?.name ?? "商店" }}</h2>
      <span>只读展示</span>
    </header>
    <div class="shop-status" role="status" aria-live="polite">
      <template v-if="loading">{{ document ? "正在更新…" : "正在加载商店…" }}</template>
      <template v-else-if="!error && document">{{ items.length }} 件商品</template>
    </div>
    <div v-if="error" class="shop-error" role="alert">
      <span>{{ document ? "更新失败，以下为上次加载的内容。" : "商店加载失败。" }}{{ error }}</span>
      <button @click="reload">重试</button>
    </div>
    <div v-if="!document && loading" class="shop-skeleton" aria-hidden="true">
      <div class="skeleton-tabs skeleton-pulse" />
      <div class="shop-grid">
        <div v-for="i in 10" :key="i" class="shop-card skeleton-card">
          <div class="skeleton-icon skeleton-pulse" />
          <div class="skeleton-name skeleton-pulse" />
          <div class="skeleton-price skeleton-pulse" />
        </div>
      </div>
    </div>
    <div v-else-if="document" class="shop-content">
      <div class="shop-navigation">
        <div class="shop-tabs" role="tablist" aria-label="商品分页">
          <button v-for="(tab, i) in document.tabs" :key="tab.sourceStart" role="tab"
            :aria-selected="tabIndex === i" :disabled="loading" :class="{ selected: tabIndex === i }"
            @click="tabIndex = i">{{ tab.name }}</button>
        </div>
        <label v-if="document.categoryType" class="shop-category">
          <span class="sr-only">{{ document.categoryType === 'basic job' ? '职业' : '大分类' }}</span>
          <select v-model="categoryID" :disabled="loading" aria-label="大分类">
            <option v-for="category in document.categories" :key="category.id" :value="category.id">{{ category.name }}</option>
          </select>
        </label>
      </div>
      <div class="shop-products" role="tabpanel" :aria-label="document.tabs?.[tabIndex]?.name">
        <div v-if="items.length" class="shop-grid">
          <article v-for="entry in items" :key="entry.sourceStart" class="shop-card">
            <div class="shop-icon"><ImageThumbnail :reference="entry.item.icon" :size="32" show-fallback animated /></div>
            <div class="shop-item-name" :title="`${entry.item.name}\nID: ${entry.item.id}`">{{ entry.item.name }}</div>
            <div class="shop-costs">
              <span v-if="!entry.item.costs?.length" class="shop-no-price">未配置价格</span>
              <span v-for="(cost, i) in entry.item.costs" :key="i" class="shop-cost"
                :title="cost.kind === 'gold' ? '金币' : `${cost.name} (ID: ${cost.itemId})`">
                {{ number(cost.quantity) }}
                <svg v-if="cost.kind === 'gold'" class="gold-icon" width="16" height="16" viewBox="0 0 16 16" role="img" aria-label="金币">
                  <ellipse cx="6" cy="11" rx="5" ry="3" fill="#a16c22" stroke="#f2d36d" />
                  <ellipse cx="6" cy="9" rx="5" ry="2.5" fill="#e4b846" stroke="#ffe29a" />
                  <ellipse cx="10" cy="5" rx="4.5" ry="2.5" fill="#efc85a" stroke="#ffe29a" />
                  <path d="M6 5v3c0 3 8 3 8 0V5" fill="none" stroke="#d69d34" />
                </svg>
                <ImageThumbnail v-else :reference="cost.icon" :size="16" show-fallback animated />
                <span class="sr-only">{{ cost.name }}</span>
              </span>
            </div>
          </article>
        </div>
        <div v-else class="shop-empty">当前分页和分类下没有商品</div>
      </div>
      <details v-if="document.issues?.length" class="shop-issues">
        <summary>{{ document.issues.length }} 条数据提示</summary>
        <ul><li v-for="(issue, i) in document.issues" :key="i">{{ issue.message }}</li></ul>
      </details>
    </div>
  </section>
</template>

<style scoped>
.shop-view { --shop-gold: #c9ad6d; color: #eee6d5; min-width: 380px; width: 100%; max-width: 680px; min-height: 100%; margin: 0 auto; background: #29251f; border: 1px solid #796443; box-sizing: border-box; font-size: 13px; }
.shop-title { display: flex; align-items: center; justify-content: space-between; gap: 16px; min-height: 38px; padding: 0 14px; background: linear-gradient(#284769, #13263d); border-bottom: 1px solid #8d774c; }
.shop-title h2 { margin: 0; color: #e7cb83; font-size: 15px; font-weight: 600; }
.shop-title > span { font-size: 11px; color: #b2bcca; white-space: nowrap; }
.shop-status { height: 25px; display: flex; align-items: center; justify-content: flex-end; padding: 0 12px; color: #beae90; font-size: 11px; }
.shop-content, .shop-skeleton { padding: 0 10px 12px; }
.shop-content { animation: shop-reveal 150ms ease-out; }
.shop-navigation { display: flex; align-items: flex-start; flex-wrap: wrap; gap: 8px; padding-bottom: 9px; }
.shop-tabs { display: flex; flex: 1; flex-wrap: wrap; gap: 3px; }
.shop-view button, .shop-view select { font: inherit; color: #cfc3a7; background: #211e19; border: 1px solid #736040; border-radius: 2px; padding: 4px 8px; cursor: pointer; }
.shop-tabs button.selected { color: #fff0c3; background: #665338; border-color: #c5a565; box-shadow: inset 0 1px #d0b57555; }
.shop-view button:disabled, .shop-view select:disabled { opacity: .55; cursor: wait; }
.shop-view button:focus-visible, .shop-view select:focus-visible, .shop-view summary:focus-visible { outline: 2px solid #83c9e6; outline-offset: 2px; }
.shop-category select { max-width: 160px; }
.shop-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 5px; }
.shop-card { display: grid; grid-template-columns: 36px minmax(0, 1fr); grid-template-rows: minmax(36px, auto) auto; gap: 4px 7px; padding: 7px; min-height: 72px; box-sizing: border-box; border: 1px solid #665537; border-radius: 2px; background: linear-gradient(120deg, #38332a, #24221d); box-shadow: inset 0 0 0 1px #171611; }
.shop-icon { width: 34px; height: 34px; border: 1px solid #87734c; background: #141412; display: flex; align-items: center; justify-content: center; }
.shop-item-name { color: #9bdbea; line-height: 1.4; overflow-wrap: anywhere; }
.shop-costs { grid-column: 1 / -1; display: flex; flex-wrap: wrap; justify-content: flex-end; align-items: center; gap: 3px 9px; min-height: 18px; }
.shop-cost { display: inline-flex; align-items: center; gap: 4px; color: #f4f0e3; font-size: 12px; font-variant-numeric: tabular-nums; }
.gold-icon { flex: none; }
.shop-no-price { color: #b6aa93; font-size: 11px; }
.shop-empty { padding: 40px 12px; text-align: center; color: #beae90; }
.shop-error { margin: 0 10px 10px; padding: 10px; background: #462c27; color: #ffd1b3; display: flex; align-items: center; gap: 10px; }
.shop-error span { flex: 1; overflow-wrap: anywhere; }
.shop-issues { color: #d6b877; margin-top: 12px; font-size: 12px; }
.shop-issues summary { cursor: pointer; }
.shop-issues ul { padding-left: 20px; line-height: 1.6; }
.skeleton-tabs { width: 65%; height: 26px; margin-bottom: 10px; }
.skeleton-icon { width: 34px; height: 34px; }
.skeleton-name { height: 12px; margin-top: 5px; width: 90%; }
.skeleton-price { grid-column: 2; justify-self: end; height: 12px; width: 70%; }
.skeleton-pulse { background: #514938; border-radius: 2px; animation: shop-pulse 1.3s ease-in-out infinite alternate; }
.sr-only { position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px; overflow: hidden; clip: rect(0,0,0,0); white-space: nowrap; }
@keyframes shop-pulse { to { opacity: .35; } }
@keyframes shop-reveal { from { opacity: 0; } to { opacity: 1; } }
@media (prefers-reduced-motion: reduce) { .skeleton-pulse, .shop-content { animation: none; } }
</style>
