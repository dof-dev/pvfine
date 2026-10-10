<script setup lang="ts">
import { computed, defineAsyncComponent, onBeforeUnmount, ref, watch } from "vue";
import { NButton, NEmpty, NInput, NModal, NSelect, NSpin, useMessage } from "naive-ui";
import { FileGUIService } from "../../../bindings/pvfine/services";
import type { SkillEntry } from "../../../bindings/pvfine/services/models";
import { useEditorStore } from "../../stores/editor";
import { useFileGUIStore } from "../../stores/fileGUI";
import { skillJobNames } from "../../gui/skill";
const SkillViewer = defineAsyncComponent(() => import("./SkillViewer.vue"));

const props = defineProps<{ show: boolean }>();
const emit = defineEmits<{ (e: "update:show", value: boolean): void }>();
const editor = useEditorStore();
const gui = useFileGUIStore();
const message = useMessage();
const entries = ref<SkillEntry[]>([]);
const query = ref("");
const submittedQuery = ref("");
const job = ref<string | null>(null);
const loading = ref(false);
const opening = ref(false);
const error = ref("");
const selected = ref<string | null>(null);
let request = 0;
const jobs = computed(() => Array.from(new Set(entries.value.map((s) => s.job))).sort().map((value) => ({ value, label: skillJobNames[value] || value })));
const filtered = computed(() => {
  const q = submittedQuery.value;
  return entries.value.filter((s) => (!job.value || s.job === job.value) && (!q || `${s.name} ${s.path} ${skillJobNames[s.job] || s.job}`.toLowerCase().includes(q)));
});
function search(event: KeyboardEvent) {
  if (event.isComposing || event.keyCode === 229) return;
  submittedQuery.value = query.value.trim().toLowerCase();
}
const tab = computed(() => selected.value === null ? undefined : editor.getFileDraft(selected.value));
const file = computed(() => tab.value && !tab.value.loading && !tab.value.loadError ? { index: tab.value.index, path: tab.value.path, text: tab.value.text, editable: tab.value.editable } : null);
async function scan() {
  const id = ++request;
  const epoch = gui.epoch;
  loading.value = true;
  error.value = "";
  try {
    const result = await FileGUIService.ListSkills();
    if (id === request && epoch === gui.epoch) entries.value = result ?? [];
  } catch (e) { if (id === request) error.value = String(e); }
  finally { if (id === request) loading.value = false; }
}
async function select(entry: SkillEntry) {
  if (opening.value || editor.saving) return;
  const epoch = gui.epoch;
  opening.value = true;
  try {
    const target = await editor.openGUIFile(entry.fileIndex);
    if (epoch !== gui.epoch) return;
    if (target.path !== entry.path) throw new Error("技能列表已变化，请重新扫描");
    selected.value = entry.path;
  } catch (e) { message.error(String(e)); }
  finally { opening.value = false; }
}
async function showFile() {
  if (!file.value || opening.value || editor.saving) return;
  const index = file.value.index;
  const epoch = gui.epoch;
  opening.value = true;
  try {
    await editor.openFile(index);
    if (epoch !== gui.epoch) return;
    editor.setGUIMode(index, "gui");
    emit("update:show", false);
  } catch (e) { message.error(String(e)); }
  finally { opening.value = false; }
}
watch(() => props.show, (show) => { if (show) void scan(); else request++; }, { immediate: true });
watch(() => gui.epoch, () => { request++; entries.value = []; selected.value = null; job.value = null; emit("update:show", false); });
onBeforeUnmount(() => { request++; });
</script>

<template>
  <NModal :show="show" @update:show="emit('update:show', $event)" preset="card" title="技能参数" style="width: 1400px; max-width: 96vw" :content-style="{ padding: '0' }">
    <div class="browser-layout">
      <aside class="skill-list">
        <NInput v-model:value="query" clearable placeholder="输入关键词，按回车搜索" @keydown.enter="search" @clear="submittedQuery = ''" />
        <NSelect v-model:value="job" clearable :options="jobs" placeholder="全部职业" />
        <div class="list-count">{{ filtered.length }} / {{ entries.length }} 个技能 <NButton text size="tiny" :disabled="loading" @click="scan">重新扫描</NButton></div>
        <div v-if="error" role="alert">{{ error }} <NButton @click="scan">重试</NButton></div>
        <NSpin :show="loading || opening">
          <div v-memo="[filtered, selected, opening, loading]" class="list-scroll">
            <button v-for="entry in filtered" :key="entry.fileIndex" :class="{ selected: selected === entry.path }" :disabled="opening || loading" @click="select(entry)"><strong>{{ entry.name }}</strong><span>{{ skillJobNames[entry.job] || entry.job }}</span><small>{{ entry.path }}</small></button>
            <NEmpty v-if="!loading && !filtered.length && !error" description="未找到技能" />
          </div>
        </NSpin>
      </aside>
      <main class="skill-detail">
        <div v-if="file" class="file-bar"><span>{{ file.path }}</span><NButton size="small" :loading="opening" :disabled="opening || editor.saving" @click="showFile">在文件 GUI 中打开</NButton></div>
        <SkillViewer v-if="file" :key="`${gui.epoch}:${file.index}`" :file="file" :active="show" />
        <NEmpty v-else description="从左侧选择技能，查看并调整参数" />
      </main>
    </div>
  </NModal>
</template>

<style scoped>
.browser-layout { display: grid; grid-template-columns: 260px minmax(0, 1fr); height: min(78vh, 900px); }
.skill-list { padding: 14px; display: flex; flex-direction: column; gap: 10px; border-right: 1px solid var(--pvf-border-subtle); min-height: 0; }
.list-count { display: flex; align-items: center; justify-content: space-between; color: var(--pvf-text-secondary); font-size: 12px; }
.skill-list :deep(.n-spin-container), .skill-list :deep(.n-spin-content) { flex: 1; min-height: 0; height: 100%; overflow: hidden; }
.list-scroll { overflow-y: auto; height: 100%; }
.list-scroll button { display: flex; flex-direction: column; gap: 4px; width: 100%; padding: 10px; text-align: left; color: var(--pvf-text-primary); font: inherit; background: transparent; border: 1px solid transparent; border-bottom-color: var(--pvf-border-subtle); cursor: pointer; }
.list-scroll button:hover, .list-scroll button.selected { background: var(--pvf-primary-soft); } .list-scroll button.selected { border-left-color: var(--pvf-primary); }
.list-scroll button:focus-visible { outline: 2px solid var(--pvf-primary); outline-offset: -2px; }
.list-scroll span, .list-scroll small { font-size: 11px; color: var(--pvf-text-secondary); overflow-wrap: anywhere; }
.skill-detail { padding: 14px 20px; overflow: auto; min-width: 0; } .file-bar { display: flex; align-items: center; gap: 10px; justify-content: space-between; font-size: 12px; color: var(--pvf-text-secondary); overflow-wrap: anywhere; }
@media (max-width: 800px) { .browser-layout { grid-template-columns: 200px minmax(0, 1fr); } .skill-detail { padding: 10px; } }
</style>
