<script setup lang="ts">
import { computed, h, onBeforeUnmount, ref, watch } from "vue";
import { NButton, NSelect } from "naive-ui";
import type { SelectOption } from "naive-ui";
import { ArchiveService } from "../../bindings/pvfine/services";
import type { SearchHit } from "../../bindings/pvfine/services/models";
import { useFileGUIStore } from "../stores/fileGUI";
import ImageThumbnail from "./ImageThumbnail.vue";

const props = defineProps<{ modelValue: string; disabled?: boolean; label?: string }>();
const emit = defineEmits<{ (e: "update:modelValue", id: string): void; (e: "select", item: SearchHit | null): void }>();
const gui = useFileGUIStore();
const hits = ref<SearchHit[]>([]);
const loading = ref(false);
const error = ref("");
const query = ref("");
const cursor = ref(-1);
let generation = 0;
let timer: ReturnType<typeof setTimeout> | undefined;
const options = computed<SelectOption[]>(() => hits.value.map((item) => ({ label: `${item.name || '未命名'} · ${item.id}`, value: item.id, item })));
function fallback(value: string | number): SelectOption { return { value, label: props.label ? `${props.label} · ${value}` : `物品 #${value}` }; }
function renderLabel(option: SelectOption) {
  const item = option.item as SearchHit | undefined;
  const label = option.value === props.modelValue && props.label ? `${props.label} · ${option.value}` : String(option.label);
  return h("span", { class: "item-picker-option", title: item?.path }, [
    h(ImageThumbnail, { reference: item?.icon, size: 20, showFallback: true }),
    h("span", label),
  ]);
}
async function search(more = false) {
  const request = ++generation;
  loading.value = true; error.value = "";
  if (!more) { hits.value = []; cursor.value = -1; }
  try {
    const result = await ArchiveService.SearchItems(query.value.trim(), more ? cursor.value : 0, 30);
    if (request !== generation) return;
    const next = result?.hits?.filter((item): item is SearchHit => !!item) ?? [];
    const byID = new Map((more ? hits.value : []).map((item) => [item.id, item]));
    for (const item of next) if (!byID.has(item.id)) byID.set(item.id, item);
    hits.value = [...byID.values()]; cursor.value = result?.nextCursor ?? -1;
  } catch (cause) { if (request === generation) error.value = String(cause); }
  finally { if (request === generation) loading.value = false; }
}
function onSearch(value: string) {
  query.value = value; generation++; clearTimeout(timer);
  hits.value = []; cursor.value = -1; error.value = "";
  loading.value = !!value.trim();
  if (value.trim()) timer = setTimeout(() => void search(), 180);
}
function select(id: string | null) {
  emit("update:modelValue", id ?? "");
  emit("select", hits.value.find((item) => item.id === id) ?? null);
}
watch(() => gui.epoch, () => { generation++; clearTimeout(timer); hits.value = []; loading.value = false; error.value = ""; cursor.value = -1; });
onBeforeUnmount(() => { generation++; clearTimeout(timer); });
</script>

<template>
  <div class="item-picker">
    <NSelect :value="modelValue || null" :disabled="disabled" filterable remote clearable
      :options="options" :loading="loading" :fallback-option="fallback" :render-label="renderLabel"
      placeholder="输入物品 ID、名称或路径检索" @search="onSearch" @update:value="select">
      <template #empty>{{ error || (loading ? '正在搜索…' : query ? '没有匹配的装备或道具' : '输入 ID 或名称开始搜索') }}</template>
      <template #action>
        <NButton v-if="error" text @click="search()">重试搜索</NButton>
        <NButton v-else-if="cursor >= 0" text :loading="loading" @click="search(true)">加载更多</NButton>
      </template>
    </NSelect>
  </div>
</template>

<style>
.item-picker-option { display: inline-flex; align-items: center; gap: 8px; min-width: 0; }
.item-picker-option > span:last-child { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.item-picker { min-width: 0; flex: 1; }
</style>
