<script setup lang="ts">
import { computed, defineAsyncComponent, onBeforeUnmount, ref, watch } from 'vue';
import { NAlert, NButton, NEmpty, NInput, NModal, NSelect, NSpin } from 'naive-ui';
import { FileGUIService } from '../../../bindings/pvfine/services';
import type { SkillTreeSkill } from '../../../bindings/pvfine/services/models';
import type { SkillDataSelection } from '../../gui/skill';
import { skillJobNames } from '../../gui/skill';
import { useEditorStore } from '../../stores/editor';
import { useFileGUIStore } from '../../stores/fileGUI';
const SkillViewer = defineAsyncComponent(() => import('./SkillViewer.vue'));
const props = defineProps<{ show: boolean; job: string; skill: string }>();
const emit = defineEmits<{
  (e: 'update:show', value: boolean): void;
  (e: 'select-data', value: SkillDataSelection & { job: string; skill: string }): void;
}>();
const editor = useEditorStore(), gui = useFileGUIStore();
const jobs = ref<{ value: string; label: string }[]>([]), job = ref<string | null>(null);
const skills = ref<SkillTreeSkill[]>([]), selected = ref<SkillTreeSkill | null>(null);
const query = ref(''), loading = ref(false), opening = ref(false), error = ref('');
let request = 0, selectionRequest = 0;
const filtered = computed(() => skills.value.filter(s => `${s.id} ${s.name} ${s.path}`.toLowerCase().includes(query.value.trim().toLowerCase())));
const file = computed(() => {
  const target = selected.value, tab = target && editor.getFileDraft(target.path);
  return tab && tab.index === target?.fileIndex && !tab.loading && !tab.loadError
    ? { index: tab.index, path: tab.path, text: tab.text, editable: false } : null;
});
async function openSkill(skill: SkillTreeSkill) {
  const current = ++selectionRequest, epoch = gui.epoch, sourceJob = job.value;
  selected.value = null; opening.value = true; error.value = '';
  try {
    const tab = await editor.openGUIFile(skill.fileIndex);
    if (current !== selectionRequest || epoch !== gui.epoch || !props.show || sourceJob !== job.value) return;
    if (tab.path !== skill.path) throw new Error('技能列表已变化，请重新读取');
    selected.value = skill;
  } catch (cause) { if (current === selectionRequest) error.value = String(cause); }
  finally { if (current === selectionRequest) opening.value = false; }
}
async function loadSkills() {
  const current = ++request, epoch = gui.epoch, sourceJob = job.value;
  selectionRequest++; selected.value = null; skills.value = []; opening.value = false; error.value = '';
  loading.value = true;
  try {
    if (!sourceJob) return;
    const result = await FileGUIService.ListSkillTreeSkills(sourceJob);
    if (current !== request || epoch !== gui.epoch || !props.show) return;
    skills.value = result ?? [];
    const initial = sourceJob === props.job ? skills.value.find(s => String(s.id) === props.skill) : undefined;
    if (initial) await openSkill(initial);
  } catch (cause) { if (current === request) error.value = String(cause); }
  finally { if (current === request) loading.value = false; }
}
async function initialize() {
  const current = ++request, epoch = gui.epoch;
  selectionRequest++; selected.value = null; skills.value = []; jobs.value = []; query.value = ''; error.value = ''; loading.value = true;
  try {
    const result = await FileGUIService.ListSkillTrees();
    if (current !== request || epoch !== gui.epoch || !props.show) return;
    jobs.value = [...new Set([...(result ?? []).map(r => r.job), props.job].filter(value => /^\[[a-z][a-z0-9 _-]*\]$/i.test(value) && value !== '[all]'))]
      .map(value => ({ value, label: skillJobNames[value.slice(1, -1)] || value }));
    const initial = jobs.value.find(j => j.value === props.job)?.value ?? jobs.value[0]?.value ?? null;
    if (job.value === initial) await loadSkills(); else job.value = initial;
  } catch (cause) { if (current === request) error.value = String(cause); }
  finally { if (current === request) loading.value = false; }
}
function choose(value: SkillDataSelection) {
  if (!props.show || loading.value || opening.value || !selected.value || !job.value || !file.value) return;
  emit('select-data', { ...value, job: job.value, skill: String(selected.value.id) });
  emit('update:show', false);
}
watch(job, () => { query.value = ''; if (props.show) void loadSkills(); });
watch(() => props.show, show => { if (show) void initialize(); else { request++; selectionRequest++; loading.value = false; opening.value = false; selected.value = null; } }, { immediate: true });
watch(() => JSON.stringify([gui.epoch, gui.revision]), () => { request++; selectionRequest++; selected.value = null; emit('update:show', false); });
onBeforeUnmount(() => { request++; selectionRequest++; });
</script>

<template>
  <NModal :show="show" preset="card" title="选择技能数据" style="width: 1400px; max-width: 96vw" :content-style="{ padding: '0' }" @update:show="emit('update:show', $event)">
    <div class="picker-layout">
      <aside class="picker-list">
        <NSelect v-model:value="job" filterable :options="jobs" placeholder="选择职业" :disabled="loading || opening" aria-label="选择数据职业" />
        <NInput v-model:value="query" clearable placeholder="搜索技能名称或 ID" :input-props="{ 'aria-label': '搜索技能数据' }" />
        <NButton size="small" :disabled="loading || opening" @click="initialize">重新读取</NButton>
        <NSpin :show="loading || opening">
          <div class="list-scroll"><button v-for="skill in filtered" :key="skill.id" :class="{ selected: selected?.id === skill.id }" :disabled="loading || opening" @click="openSkill(skill)"><strong>{{ skill.name }}</strong><small>#{{ skill.id }} · {{ skill.path }}</small></button><NEmpty v-if="!loading && !filtered.length" size="small" description="没有匹配的已登记技能" /></div>
        </NSpin>
      </aside>
      <main class="picker-detail">
        <NAlert v-if="error" type="error">{{ error }}</NAlert>
        <SkillViewer v-if="file" :key="`${gui.epoch}:${job}:${file.index}`" :file="file" :active="show && !loading && !opening" selecting @select-data="choose" />
        <NEmpty v-else-if="!loading && !opening" description="选择技能，再点击描述或数据表中的数值" />
      </main>
    </div>
  </NModal>
</template>

<style scoped>
.picker-layout { display: grid; grid-template-columns: 250px minmax(0, 1fr); height: min(80vh, 900px); }
.picker-list { padding: 14px; display: flex; flex-direction: column; gap: 10px; min-height: 0; border-right: 1px solid var(--pvf-border-subtle); }
.picker-list :deep(.n-spin-container), .picker-list :deep(.n-spin-content) { flex: 1; min-height: 0; height: 100%; overflow: hidden; }
.list-scroll { overflow: auto; height: 100%; }.list-scroll button { display: grid; gap: 5px; width: 100%; padding: 10px; text-align: left; color: var(--pvf-text-primary); background: transparent; border: 1px solid transparent; border-bottom-color: var(--pvf-border-subtle); cursor: pointer; font: inherit; }
.list-scroll button.selected, .list-scroll button:hover:enabled { background: var(--pvf-primary-soft); }.list-scroll small { color: var(--pvf-text-secondary); overflow-wrap: anywhere; }.picker-detail { overflow: auto; min-width: 0; padding: 14px 20px; }
@media(max-width: 700px) { .picker-layout { grid-template-columns: 1fr; grid-template-rows: 220px minmax(0, 1fr); }.picker-list { border-bottom: 1px solid var(--pvf-border-subtle); }.picker-detail { padding: 12px; } }
</style>
