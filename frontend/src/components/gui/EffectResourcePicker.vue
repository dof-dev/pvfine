<script setup lang="ts">
import { computed, h, onBeforeUnmount, ref, watch } from 'vue';
import { NButton, NInput, NSelect, type SelectOption } from 'naive-ui';
import { ArchiveService } from '../../../bindings/pvfine/services';
import type { SearchHit } from '../../../bindings/pvfine/services/models';
import type { EffectResourceKind } from '../../gui/itemTriggers';
import { useFileGUIStore } from '../../stores/fileGUI';
const props = defineProps<{ modelValue: string; kind: EffectResourceKind; disabled?: boolean }>();
const emit = defineEmits<{ (e: 'update:modelValue', value: string): void }>();
const gui = useFileGUIStore(), hits = ref<SearchHit[]>([]), query = ref(''), error = ref(''), loading = ref(false), cursor = ref(-1);
const labels: Record<EffectResourceKind, string> = { object: '被动对象', particle: '粒子', appendage: '附加物', monster: '怪物', apc: 'APC' };
const options = computed(() => hits.value.map(hit => ({ value: props.kind === 'particle' ? hit.path : hit.id, label: `${hit.name || hit.path} ${hit.id ? '#' + hit.id : ''}`, path: hit.path })));
let generation = 0, timer: ReturnType<typeof setTimeout> | undefined;
async function search(more = false) {
  const request = ++generation, epoch = gui.epoch;
  loading.value = true; error.value = '';
  if (!more) { hits.value = []; cursor.value = -1; }
  try {
    const result = await ArchiveService.SearchEffectResources(props.kind, query.value, more ? cursor.value : 0, 30);
    if (request !== generation || epoch !== gui.epoch) return;
    hits.value = [...(more ? hits.value : []), ...(result?.hits ?? []).filter((hit): hit is SearchHit => !!hit)];
    cursor.value = result?.nextCursor ?? -1;
  } catch (cause) { if (request === generation) error.value = String(cause); }
  finally { if (request === generation) loading.value = false; }
}
function schedule(value: string) { query.value = value; generation++; clearTimeout(timer); hits.value = []; cursor.value = -1; timer = setTimeout(() => void search(), 180); }
function renderLabel(option: SelectOption) { return h('span', {title: String(option.path ?? '')}, String(option.label)); }
function reset() { generation++; clearTimeout(timer); hits.value = []; cursor.value = -1; error.value = ''; loading.value = false; }
watch(() => [props.kind, props.disabled, gui.epoch, gui.revision], reset);
onBeforeUnmount(reset);
</script>
<template>
  <div class="resource-picker">
    <NInput :value="modelValue" :disabled="disabled" :input-props="{ 'aria-label': `${labels[kind]}原始值` }" :placeholder="kind === 'particle' ? 'PVF 路径，可留空' : '输入 ID，或从下方搜索选择'" @update:value="emit('update:modelValue', $event)" />
    <NSelect :value="null" :disabled="disabled" filterable remote :loading="loading" :options="options" :render-label="renderLabel" :placeholder="`搜索当前归档中的${labels[kind]}`" :input-props="{ 'aria-label': `${labels[kind]}搜索` }" @search="schedule" @update:show="value => { if (value && !hits.length) search(); }" @update:value="value => { if (value !== null) emit('update:modelValue', String(value)); }">
      <template #empty>{{ error || (loading ? '正在读取…' : '没有匹配的资源，可直接输入原始值') }}</template>
      <template #action><NButton v-if="error" text @click="search()">重试搜索</NButton><NButton v-else-if="cursor >= 0" text :loading="loading" @click="search(true)">加载更多</NButton></template>
    </NSelect>
  </div>
</template>
<style scoped>.resource-picker{display:grid;gap:6px}</style>
