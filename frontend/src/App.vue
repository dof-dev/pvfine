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
import DropRateEditorModal from "./components/DropRateEditorModal.vue";
import SettingsModal from "./components/SettingsModal.vue";
import CloseGuard from "./components/CloseGuard.vue";
import EditorCloseGuard from "./components/EditorCloseGuard.vue";
import RecoveryPrompt from "./components/RecoveryPrompt.vue";
import { useArchiveStore } from "./stores/archive";
import { useAutosaveStore } from "./stores/autosave";
import { useEditorStore } from "./stores/editor";
import { useFileSetStore } from "./stores/fileSets";
import { useBookmarkStore } from "./stores/bookmarks";
import { useSettingsStore } from "./stores/settings";
import { useVersionStore } from "./stores/version";
import { useImageStore } from "./stores/images";
import { useScriptStore } from "./stores/script";
import { useAdvancedSearchStore } from "./stores/advancedSearch";
import { dispatchShortcut, type ShortcutCommandId } from "./shortcuts";
import {
  applyTheme,
  getTheme,
  makeThemeOverrides,
  resolveThemeMode,
} from "./theme";

const archive = useArchiveStore();
const autosave = useAutosaveStore();
const editor = useEditorStore();
const fileSets = useFileSetStore();
const bookmarks = useBookmarkStore();
const settings = useSettingsStore();
const version = useVersionStore();
const images = useImageStore();
const script = useScriptStore();
const advancedSearch = useAdvancedSearchStore();
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
    // 定时缓存依赖已加载的设置(开关/间隔/缓存路径),恢复提示也必须先于任何
    // 归档加载,否则会覆盖用户刚打开的工作区。
    await autosave.initialize();
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

function onKeydown(e: KeyboardEvent): void {
  const overlayOpen = settings.visible || advancedSearch.visible || version.visible ||
    !!document.querySelector(".n-modal-mask, .n-dialog-mask") ||
    (e.target instanceof Element && !!e.target.closest(".n-modal, .n-dialog, [role='dialog']"));
  dispatchShortcut(e, settings.shortcutOverrides, !!overlayOpen, executeShortcut, (command) =>
    command === "workspace.execute" && version.visible && !settings.visible &&
    !advancedSearch.visible && !document.querySelector(".n-dialog-mask") &&
    e.target instanceof Element && !!e.target.closest(".version-modal"),
    shortcutAvailable,
  );
}

function shortcutAvailable(command: ShortcutCommandId): boolean {
  switch (command) {
    case "archive.open": return !archive.loading;
    case "workspace.save": return script.workspaceVisible || archive.open;
    case "archive.saveAs": return archive.open;
    case "workspace.close": return editor.activeKey !== null;
    case "editor.closeOthers": return archive.open && !script.workspaceVisible && editor.tabs.length > 1 && editor.activeKey !== null;
    case "editor.closeAll": return archive.open && !script.workspaceVisible && editor.tabs.length > 0;
    case "editor.splitColumns":
    case "editor.splitRows": return archive.open && !script.workspaceVisible;
    case "workspace.execute": return version.visible ? version.canCommit : script.workspaceVisible && script.canRun;
    case "search.advanced":
    case "version.open":
    case "workspace.script": return archive.open;
    case "settings.open": return true;
    case "workspace.archive": return script.workspaceVisible;
  }
}

function executeShortcut(command: ShortcutCommandId): void {
  switch (command) {
    case "archive.open":
      if (!archive.loading) void archive.openDialog();
      break;
    case "workspace.save":
      if (script.workspaceVisible) {
        void script.saveScript().catch(() => { /* 工作区显示具体错误。 */ });
      } else if (archive.open) {
        void editor.saveActiveTab();
      }
      break;
    case "archive.saveAs":
      if (archive.open) void editor.saveAs();
      break;
    case "workspace.close":
      if (editor.activeKey !== null) editor.requestCloseTab(editor.activeKey, editor.activePaneId);
      break;
    case "editor.closeOthers":
      if (editor.activeKey !== null) editor.requestCloseOthers(editor.activeKey);
      break;
    case "editor.closeAll":
      editor.requestCloseAll();
      break;
    case "editor.splitColumns":
      if (archive.open && !script.workspaceVisible) editor.split("columns");
      break;
    case "editor.splitRows":
      if (archive.open && !script.workspaceVisible) editor.split("rows");
      break;
    case "workspace.execute":
      if (version.visible && version.canCommit) void version.commit();
      else if (script.workspaceVisible && script.canRun) void script.run();
      break;
    case "search.advanced":
      if (archive.open) advancedSearch.open();
      break;
    case "version.open":
      if (archive.open) version.open();
      break;
    case "settings.open":
      settings.open();
      break;
    case "workspace.archive":
      if (script.workspaceVisible) script.hideWorkspace();
      break;
    case "workspace.script":
      if (archive.open) script.showWorkspace();
      break;
  }
}
</script>

<template>
  <NConfigProvider :theme="activeTheme.naiveTheme" :theme-overrides="themeOverrides" :locale="zhCN" :date-locale="dateZhCN">
    <NMessageProvider placement="bottom-right">
      <NDialogProvider>
        <CloseGuard />
        <EditorCloseGuard />
        <RecoveryPrompt />
        <div class="app-root" data-file-drop-target :class="{ 'app-root--mac': isMac }">
          <ToolBar />
          <AdvancedSearchModal />
          <BatchProcessModal />
          <ImportModal />
          <VersionPanel />
          <DropRateEditorModal />
          <SettingsModal />
          <div class="app-body">
            <div class="explorer-pane" :style="{ width: explorerWidth + 'px' }">
              <Explorer />
            </div>
            <div class="resize-handle" @mousedown.prevent="onResizeStart" />
            <div class="editor-pane">
              <EditorTabs :theme-id="activeTheme.id" />
            </div>
            <FileSetSidebar />
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
