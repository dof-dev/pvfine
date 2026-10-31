<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { NAlert, NButton, NEmpty, NInput, NModal, NSelect, NText, useMessage } from "naive-ui";
import { ArchiveService } from "../../../bindings/pvfine/services";
import type { SearchHit } from "../../../bindings/pvfine/services/models";
import type { GUIProvider } from "../../gui/types";
import { guiPathMatches, newGUIFilePath, searchGUIFiles, type GUISearchCursor } from "../../gui/launcher";
import { useArchiveStore } from "../../stores/archive";
import { useEditorStore } from "../../stores/editor";
import { useExplorerStore } from "../../stores/explorer";
import { useFileGUIStore } from "../../stores/fileGUI";

const props = defineProps<{ provider: GUIProvider }>();
const emit = defineEmits<{ (e: "close"): void }>();
const archive = useArchiveStore(), editor = useEditorStore(), explorer = useExplorerStore(), gui = useFileGUIStore();
const message = useMessage();
const query = ref(""), path = ref(props.provider.launcher.paths[0] ?? ""), error = ref("");
const hits = ref<SearchHit[]>([]), cursor = ref<GUISearchCursor | null>(null);
const loading = ref(false), working = ref(false);
const busy = computed(() => working.value || editor.saving || archive.loading);
const pathOptions = computed(() => props.provider.launcher.paths.map(value => ({ label: value, value })));
let generation = 0;
let timer: ReturnType<typeof setTimeout> | undefined;

async function search(more = false) {
  const request = ++generation, epoch = gui.epoch;
  const current = () => request === generation && epoch === gui.epoch;
  loading.value = true; error.value = "";
  if (!more) { hits.value = []; cursor.value = null; }
  try {
    const result = await searchGUIFiles(props.provider, query.value, more && cursor.value ? cursor.value : { queryIndex: 0, offset: 0 }, ArchiveService.Search, current);
    if (!current()) return;
    hits.value = [...new Map([...(more ? hits.value : []), ...result.hits].map(hit => [hit.fileIndex, hit])).values()];
    cursor.value = result.next;
  } catch (cause) { if (current()) error.value = String(cause); }
  finally { if (current()) loading.value = false; }
}

async function open(index: number, expectedPath: string, guiMode: boolean, paneId = editor.activePaneId) {
  const epoch = gui.epoch;
  await editor.openFile(index, paneId);
  if (epoch !== gui.epoch) return;
  const tab = editor.getFileDraft(index);
  if (!tab || tab.loadError) throw new Error(tab?.loadError || "文件未能打开");
  if (tab.path !== expectedPath || !guiPathMatches(props.provider, tab.path)) throw new Error("文件列表已变化，请重新搜索");
  if (guiMode) editor.setGUIMode(index, "gui", paneId);
  emit("close");
}

async function openHit(hit: SearchHit) {
  if (busy.value) return;
  working.value = true; error.value = "";
  try { await open(hit.fileIndex, hit.path, true); }
  catch (cause) { error.value = String(cause); }
  finally { working.value = false; }
}

async function create() {
  if (busy.value) return;
  working.value = true; error.value = "";
  const epoch = gui.epoch, paneId = editor.activePaneId;
  try {
    const target = newGUIFilePath(props.provider, path.value);
    const node = await ArchiveService.CreateFile(target, 1);
    if (epoch !== gui.epoch) return;
    if (!node) throw new Error("后端未返回新文件信息");
    // 新文件先在文本模式中补充内容，避免空配置无法通过专用 GUI 的结构校验。
    await open(node.fileIndex, node.path, false, paneId);
    message.success(`已新建 ${node.path}，补充内容后可切换 GUI 模式`);
    await archive.refreshInfo();
    if (epoch !== gui.epoch) return;
    await explorer.reload();
  } catch (cause) {
    error.value = String(cause);
    message.error(String(cause));
  } finally { working.value = false; }
}

watch(query, () => {
  generation++; clearTimeout(timer); hits.value = []; cursor.value = null;
  timer = setTimeout(() => void search(), 200);
});
watch(() => gui.epoch, () => emit("close"));
onBeforeUnmount(() => { generation++; clearTimeout(timer); });
void search();
</script>

<template>
  <NModal :show="true" preset="card" :title="`${provider.label} · 文件 GUI`" style="width: 760px; max-width: 94vw" :mask-closable="!working" :closable="!working" :close-on-esc="!working" @update:show="value => { if (!value) emit('close'); }">
    <div class="launcher">
      <NAlert type="info" :show-icon="false">
        打开 {{ provider.launcher.fileTypes }} 文件，切换到 GUI 模式即可使用{{ provider.label }}工具。下方搜索结果点击后会自动进入 GUI 模式。
      </NAlert>
      <div class="search-row">
        <NInput v-model:value="query" clearable placeholder="按名称、ID 或路径搜索此类型文件，支持 * 和 ?" :disabled="busy" aria-label="搜索 GUI 文件" />
        <NButton :loading="loading" :disabled="busy" @click="search()">搜索</NButton>
      </div>
      <NText v-if="error" type="error" role="alert">{{ error }}</NText>
      <div class="results" :aria-busy="loading">
        <button v-for="hit in hits" :key="hit.fileIndex" type="button" :disabled="busy || loading" @click="openHit(hit)">
          <strong>{{ hit.name || hit.path.split('/').at(-1) }}{{ hit.id ? ` #${hit.id}` : '' }}</strong>
          <small>{{ hit.path }}</small>
        </button>
        <NEmpty v-if="!hits.length && !loading" :description="cursor ? '本批未匹配到此类型文件，可继续搜索' : '未找到此类型文件，可更换关键词或新建文件'" />
        <NText v-if="loading">正在搜索…</NText>
      </div>
      <NButton v-if="cursor" :loading="loading" :disabled="busy" @click="search(true)">继续搜索 / 加载更多</NButton>
      <div class="creation">
        <strong>新建{{ provider.label }}文件</strong>
        <NText depth="3">创建空白脚本文件，在文本模式补充内容后切换 GUI；需要注册的文件可通过文件树菜单添加 lst 注册。</NText>
        <NSelect v-if="pathOptions.length > 1" :options="pathOptions" :value="null" placeholder="选择文件类型或固定路径" :disabled="busy" @update:value="value => path = value" />
        <div class="search-row">
          <NInput v-model:value="path" :disabled="busy" placeholder="归档内完整路径" aria-label="新建 GUI 文件路径" @keydown.enter="create" />
          <NButton :loading="working" :disabled="busy" @click="create">新建文件</NButton>
        </div>
      </div>
    </div>
  </NModal>
</template>

<style scoped>
.launcher, .creation { display: flex; flex-direction: column; gap: 12px; }
.search-row { display: flex; gap: 8px; }
.search-row :deep(.n-input) { flex: 1; min-width: 0; }
.results { max-height: 40vh; min-height: 100px; overflow-y: auto; }
.results button { display: flex; flex-direction: column; gap: 4px; width: 100%; padding: 10px; text-align: left; background: transparent; color: var(--pvf-text-primary); border: 0; border-bottom: 1px solid var(--pvf-border-subtle); cursor: pointer; font: inherit; }
.results button:hover { background: var(--pvf-primary-soft); }
.results button:focus-visible { outline: 2px solid var(--pvf-primary); outline-offset: -2px; }
.results small { color: var(--pvf-text-secondary); overflow-wrap: anywhere; }
.creation { border-top: 1px solid var(--pvf-border-subtle); padding-top: 16px; }
</style>
