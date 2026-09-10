<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import {
  dateZhCN,
  NConfigProvider,
  NDialogProvider,
  NMessageProvider,
  zhCN,
} from "naive-ui";
import ToolBar from "./components/ToolBar.vue";
import Explorer from "./components/Explorer.vue";
import EditorTabs from "./components/EditorTabs.vue";
import FileSetSidebar from "./components/FileSetSidebar.vue";
import StatusBar from "./components/StatusBar.vue";
import AdvancedSearchModal from "./components/AdvancedSearchModal.vue";
import BatchProcessModal from "./components/BatchProcessModal.vue";
import ImportModal from "./components/ImportModal.vue";
import VersionPanel from "./components/VersionPanel.vue";
import SettingsModal from "./components/SettingsModal.vue";
import CloseGuard from "./components/CloseGuard.vue";
import { useArchiveStore } from "./stores/archive";
import { useEditorStore } from "./stores/editor";
import { useFileSetStore } from "./stores/fileSets";
import { useSidebarStore } from "./stores/sidebar";
import { useBookmarkStore } from "./stores/bookmarks";
import { useSettingsStore } from "./stores/settings";
import { useVersionStore } from "./stores/version";
import { useImageStore } from "./stores/images";
import {
  applyTheme,
  getTheme,
  makeThemeOverrides,
  resolveThemeMode,
} from "./theme";

const archive = useArchiveStore();
const editor = useEditorStore();
const fileSets = useFileSetStore();
const sidebar = useSidebarStore();
const bookmarks = useBookmarkStore();
const settings = useSettingsStore();
const version = useVersionStore();
const images = useImageStore();
const isMac = /Macintosh|Mac OS X|MacIntel/i.test(
  `${navigator.platform} ${navigator.userAgent}`
);

const systemThemeQuery = window.matchMedia("(prefers-color-scheme: dark)");
const systemPrefersDark = ref(systemThemeQuery.matches);
const activeTheme = computed(() =>
  getTheme(resolveThemeMode(settings.themeMode, systemPrefersDark.value))
);
const themeOverrides = computed(() => makeThemeOverrides(activeTheme.value));

watch(
  activeTheme,
  (theme) => {
    applyTheme(theme);
  },
  { immediate: true }
);

const explorerWidth = ref(300);
const resizing = ref(false);

function onResizeStart() {
  resizing.value = true;
  document.body.style.cursor = "col-resize";
}
function onResizeMove(e: MouseEvent) {
  if (!resizing.value) return;
  explorerWidth.value = Math.min(560, Math.max(200, e.clientX));
}
function onResizeEnd() {
  resizing.value = false;
  document.body.style.cursor = "";
}

onMounted(() => {
  void (async () => {
    await settings.load();
    await images.initialize();
  })();
  void fileSets.load();
  void bookmarks.load();
  systemThemeQuery.addEventListener("change", onSystemThemeChange);
  window.addEventListener("mousemove", onResizeMove);
  window.addEventListener("mouseup", onResizeEnd);
  window.addEventListener("keydown", onKeydown);
});
onUnmounted(() => {
  window.removeEventListener("mousemove", onResizeMove);
  window.removeEventListener("mouseup", onResizeEnd);
  window.removeEventListener("keydown", onKeydown);
  systemThemeQuery.removeEventListener("change", onSystemThemeChange);
});

function onSystemThemeChange(event: MediaQueryListEvent): void {
  systemPrefersDark.value = event.matches;
}

async function onKeydown(e: KeyboardEvent) {
  const mod = e.metaKey || e.ctrlKey;
  if (!mod) return;
  if (e.code === "Backslash") {
    e.preventDefault();
    if (e.shiftKey) {
      editor.split("rows");
    } else {
      editor.split("columns");
    }
    return;
  }
  const key = e.key.toLowerCase();
  if (key === "enter" && version.canCommit) {
    e.preventDefault();
    await editor.flushPending();
    void version.commit();
  } else if (key === "o") {
    e.preventDefault();
    if (!archive.loading) await archive.openDialog();
  } else if (key === "s" && e.shiftKey) {
    e.preventDefault();
    if (archive.open) await editor.saveAs();
  } else if (key === "w") {
    e.preventDefault();
    if (editor.activeKey !== null) editor.closeTab(editor.activeKey);
  }
}
</script>

<template>
  <NConfigProvider :theme="activeTheme.naiveTheme" :theme-overrides="themeOverrides" :locale="zhCN" :date-locale="dateZhCN">
    <NMessageProvider placement="bottom-right">
      <NDialogProvider>
        <CloseGuard />
        <div class="app-root" data-file-drop-target :class="{ 'app-root--mac': isMac }">
          <ToolBar />
          <AdvancedSearchModal />
          <BatchProcessModal />
          <ImportModal />
          <VersionPanel />
          <SettingsModal />
          <div class="app-body">
            <div class="explorer-pane" :style="{ width: explorerWidth + 'px' }">
              <Explorer />
            </div>
            <div class="resize-handle" @mousedown.prevent="onResizeStart" />
            <div class="editor-pane">
              <EditorTabs :theme-id="activeTheme.id" />
            </div>
            <FileSetSidebar v-if="sidebar.visible" />
          </div>
          <StatusBar />
        </div>
      </NDialogProvider>
    </NMessageProvider>
  </NConfigProvider>
</template>

<style scoped>
.app-root {
  display: flex;
  flex-direction: column;
  height: 100vh;
  min-height: 0;
  background: var(--pvf-window-background-solid);
}
.app-root--mac {
  background: var(--pvf-window-background-glass);
}
.app-body {
  flex: 1;
  display: flex;
  min-height: 0;
  overflow: hidden;
}
.explorer-pane {
  flex-shrink: 0;
  min-width: 200px;
  max-width: 560px;
  min-height: 0;
}
.resize-handle {
  width: 4px;
  margin: 0 -2px;
  cursor: col-resize;
  z-index: 10;
  flex-shrink: 0;
}
.resize-handle:hover {
  background: var(--pvf-effect-split-hover);
}
.editor-pane {
  flex: 1;
  min-width: 0;
  min-height: 0;
}
.app-root--mac :deep(.toolbar) {
  padding-left: 82px;
}
</style>
