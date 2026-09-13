<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import {
  dateZhCN,
  NConfigProvider,
  NDialogProvider,
  NMessageProvider,
  NModal,
  zhCN,
} from "naive-ui";
import { Events } from "@wailsio/runtime";
import ScriptWorkbench from "./components/ScriptWorkbench.vue";
import { ScriptWindowService } from "../bindings/pvfine/services";
import { useArchiveStore } from "./stores/archive";
import { useScriptStore } from "./stores/script";
import { useSettingsStore } from "./stores/settings";
import {
  applyTheme,
  getTheme,
  makeThemeOverrides,
  resolveThemeMode,
} from "./theme";

// 独立脚本窗口的根组件。它刻意不挂 CloseGuard / ToolBar / Explorer：那些属于
// 主窗口，而且 app:close-requested 是广播事件，两个窗口都挂 CloseGuard 会让
// 退出确认弹两次。这个窗口走自己的一条关闭流程。

const archive = useArchiveStore();
const script = useScriptStore();
const settings = useSettingsStore();
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

function onSystemThemeChange(event: MediaQueryListEvent): void {
  systemPrefersDark.value = event.matches;
}

const closing = ref(false);
const confirmCloseVisible = ref(false);

function sessionSnapshot() {
  return {
    name: script.currentName,
    source: script.source,
    savedSource: script.savedSource,
  };
}

/**
 * 关闭窗口。Go 侧的关闭钩子会先拦下原生关闭并发出
 * script-window:close-requested，所以这里是唯一的关闭路径。
 */
async function closeNow(): Promise<void> {
  if (closing.value) return;
  closing.value = true;
  try {
    // 未保存内容随 session 交回主窗口，因此不阻止关闭；但脚本正在运行时必须先
    // 停下来，否则后端会拒绝后续操作。
    if (script.running) await script.cancel();
    await ScriptWindowService.CloseScriptWindow(sessionSnapshot());
  } catch {
    // 关闭失败说明窗口还在，允许重试。
    closing.value = false;
  }
}

const offCloseRequested = Events.On("script-window:close-requested", () => {
  // 运行中的关闭要用户确认：关掉意味着停止运行并丢弃本次预览计划。
  if (script.running) {
    confirmCloseVisible.value = true;
    return;
  }
  void closeNow();
});

function onConfirmClose(): void {
  confirmCloseVisible.value = false;
  void closeNow();
}

function onCancelClose(): void {
  confirmCloseVisible.value = false;
  // 用户取消后要复位，否则下一次关闭请求会被 closing 挡住。
  closing.value = false;
}

// 主窗口的快捷键挂在 App.vue 的 onKeydown 上，独立窗口没有那层，所以要自己
// 提供脚本工作区相关的：运行、保存、关闭窗口。
function onKeydown(e: KeyboardEvent): void {
  if (!(e.metaKey || e.ctrlKey)) return;
  if (e.key === "Enter") {
    if (script.canRun) {
      e.preventDefault();
      void script.run().catch(() => {
        // 工作区会展示具体错误。
      });
    }
    return;
  }
  const key = e.key.toLowerCase();
  if (key === "s" && !e.shiftKey) {
    e.preventDefault();
    void script.saveScript().catch(() => {
      // 工作区会展示具体错误。
    });
    return;
  }
  if (key === "w") {
    // 独立窗口里 Ctrl+W 的语义是关闭这个窗口，走与点标题栏关闭相同的确认流程。
    e.preventDefault();
    void closeNow();
  }
}

// 独立窗口的未保存状态要上报主窗口：退出确认在主窗口那一侧，而它读不到
// 这个窗口的 Pinia store。
watch(
  () => script.dirty,
  (dirty) => {
    void Events.Emit("script-window:dirty", { dirty });
  },
  { immediate: true },
);

// 归档在主窗口被关闭后，这个窗口就没有可跑的脚本上下文了（它自己没有打开
// 归档的入口）。此时收起窗口并把内容交回主窗口，避免留下一个无法操作的窗口。
// ready 之前 archive.open 一定是 false（info 还没同步进来），所以必须等初始化
// 完成再开始监听，否则窗口一打开就会把自己关掉。
const ready = ref(false);
watch(
  () => archive.open,
  (isOpen) => {
    if (!ready.value || isOpen) return;
    void closeNow();
  },
);

onMounted(() => {
  void (async () => {
    // 主题与 vim 模式都来自后端设置，独立窗口必须自己加载。
    await settings.load();
    // archive.info 不持久化，新窗口里 archive.open 默认为 false，而
    // script.canRun 依赖它；不同步的话脚本无法运行。
    await archive.refreshInfo();
    await script.syncFromDetachedWindow();
    script.workspaceVisible = true;
    if (!script.directory) void script.refreshFiles();
    ready.value = true;
  })();
  systemThemeQuery.addEventListener("change", onSystemThemeChange);
  window.addEventListener("keydown", onKeydown);
});

onUnmounted(() => {
  offCloseRequested();
  systemThemeQuery.removeEventListener("change", onSystemThemeChange);
  window.removeEventListener("keydown", onKeydown);
});
</script>

<template>
  <NConfigProvider
    :theme="activeTheme.naiveTheme"
    :theme-overrides="themeOverrides"
    :locale="zhCN"
    :date-locale="dateZhCN"
  >
    <NMessageProvider placement="bottom-right">
      <NDialogProvider>
        <div class="script-window-root" :class="{ 'script-window-root--mac': isMac }">
          <ScriptWorkbench :theme-id="activeTheme.id" standalone @close="closeNow" />
        </div>
        <NModal
          v-model:show="confirmCloseVisible"
          preset="dialog"
          type="warning"
          title="脚本仍在运行"
          content="关闭窗口将停止运行并丢弃当前预览，确定关闭吗？"
          positive-text="停止并关闭"
          negative-text="取消"
          @positive-click="onConfirmClose"
          @negative-click="onCancelClose"
          @close="onCancelClose"
        />
      </NDialogProvider>
    </NMessageProvider>
  </NConfigProvider>
</template>

<style scoped>
.script-window-root {
  display: flex;
  flex-direction: column;
  height: 100vh;
  min-height: 0;
  background: var(--pvf-window-background-solid);
}
.script-window-root--mac {
  background: var(--pvf-window-background-glass);
}
/* macOS 标题栏隐藏后，顶部留给红绿灯按钮的拖拽区。 */
.script-window-root--mac :deep(.script-toolbar) {
  padding-left: 82px;
}
</style>
