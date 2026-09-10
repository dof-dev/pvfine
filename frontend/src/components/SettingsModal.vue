<script setup lang="ts">
import { computed, ref } from "vue";
import {
  ArrowSync24Regular,
  CheckmarkCircle24Regular,
  Code24Regular,
  Copy24Regular,
  CursorHover24Regular,
  Desktop24Regular,
  DismissCircle24Regular,
  DocumentSync24Regular,
  FolderOpen24Regular,
  Image24Regular,
  Info24Regular,
  PaintBrush24Regular,
  Settings24Regular,
  ShieldCheckmark24Regular,
  Tag24Regular,
  WeatherMoon24Regular,
  WeatherSunny24Regular,
  Wrench24Regular,
} from "@vicons/fluent";
import {
  NAlert,
  NButton,
  NIcon,
  NModal,
  NRadioButton,
  NRadioGroup,
  NSpin,
  NSwitch,
  NTag,
  NTooltip,
  useMessage,
} from "naive-ui";
import { AnnotationService, RenderingService, UpdateService } from "../../bindings/pvfine/services";
import {
  useSettingsStore,
  type AnnotationTagPlacement,
  type ExplorerOpenMode,
  type ThemeMode,
} from "../stores/settings";
import { useEditorStore } from "../stores/editor";
import { useExplorerStore } from "../stores/explorer";
import { useImageStore } from "../stores/images";

type TabKey = "general" | "editor" | "npk" | "system";

const settings = useSettingsStore();
const editor = useEditorStore();
const explorer = useExplorerStore();
const images = useImageStore();
const message = useMessage();

const activeTab = ref<TabKey>("general");
const checkingUpdates = ref(false);
const reloadingAnnotations = ref(false);
const reloadingRendering = ref(false);
const selectingNPK = ref(false);
const rebuildingNPK = ref(false);

const tabs = [
  { id: "general" as const, label: "常规与外观", icon: PaintBrush24Regular },
  { id: "editor" as const, label: "代码编辑器", icon: Code24Regular },
  { id: "npk" as const, label: "NPK 资源库", icon: Image24Regular },
  { id: "system" as const, label: "系统维护", icon: Wrench24Regular },
];

const npkProgressPercent = computed(() => {
  const { total, done } = images.status;
  if (!total || total <= 0) return 0;
  return Math.min(100, Math.round((done / total) * 100));
});

async function onPlacementChange(value: string | number | boolean) {
  try {
    await settings.savePlacement(value as AnnotationTagPlacement);
  } catch (error: any) {
    message.error(`保存设置失败: ${error?.message ?? error}`);
  }
}

async function onExplorerOpenModeChange(value: string | number | boolean) {
  try {
    await settings.saveExplorerOpenMode(value as ExplorerOpenMode);
  } catch (error: any) {
    message.error(`保存设置失败: ${error?.message ?? error}`);
  }
}

async function onVimModeChange(value: boolean) {
  try {
    await settings.saveVimMode(value);
  } catch (error: any) {
    message.error(`保存设置失败: ${error?.message ?? error}`);
  }
}

async function onBackupSourceOnSaveChange(value: boolean) {
  try {
    await settings.saveBackupSourceOnSave(value);
  } catch (error: any) {
    message.error(`保存设置失败: ${error?.message ?? error}`);
  }
}

async function onThemeModeChange(value: ThemeMode) {
  try {
    await settings.saveThemeMode(value);
  } catch (error: any) {
    message.error(`保存设置失败: ${error?.message ?? error}`);
  }
}

async function onReloadAnnotations() {
  if (reloadingAnnotations.value) return;
  reloadingAnnotations.value = true;
  try {
    await editor.flushPending();
    const result = await AnnotationService.ReloadRules();
    await Promise.all([editor.refreshAnnotations(), explorer.refreshAnnotations()]);
    message.success(
      `已重载 ${result.ruleCount} 条标注规则、${result.relationCount} 个关联类型`
    );
  } catch (error: any) {
    message.error(`重载标注规则失败: ${error?.message ?? error}`);
  } finally {
    reloadingAnnotations.value = false;
  }
}

async function onReloadRendering() {
  if (reloadingRendering.value) return;
  reloadingRendering.value = true;
  try {
    await editor.flushPending();
    const result = await RenderingService.ReloadRules();
    await editor.refreshRenderedText();
    message.success(`已重载 ${result.ruleCount} 条渲染规则`);
  } catch (error: any) {
    message.error(`重载渲染规则失败: ${error?.message ?? error}`);
  } finally {
    reloadingRendering.value = false;
  }
}

async function onCheckUpdates() {
  if (checkingUpdates.value) return;
  checkingUpdates.value = true;
  try {
    await UpdateService.CheckForUpdates();
  } catch (error: any) {
    message.error(`检查更新失败: ${error?.message ?? error}`);
  } finally {
    checkingUpdates.value = false;
  }
}

async function onSelectNPKDirectory(): Promise<void> {
  if (selectingNPK.value) return;
  selectingNPK.value = true;
  try {
    const next = await images.selectDirectory();
    settings.npkDirectory = next.directory ?? "";
    if (next.directory) message.success("已选择 NPK 目录，开始建立图标索引");
  } catch (error: any) {
    message.error(`选择 NPK 目录失败: ${error?.message ?? error}`);
  } finally {
    selectingNPK.value = false;
  }
}

async function onRebuildNPKIndex(): Promise<void> {
  if (rebuildingNPK.value || !images.status.directory) return;
  rebuildingNPK.value = true;
  try {
    await images.rebuild();
  } catch (error: any) {
    message.error(`重建图标索引失败: ${error?.message ?? error}`);
  } finally {
    rebuildingNPK.value = false;
  }
}

async function copyNPKDirectory(): Promise<void> {
  if (!settings.npkDirectory) return;
  try {
    await navigator.clipboard.writeText(settings.npkDirectory);
    message.success("已复制 NPK 目录路径");
  } catch {
    message.warning("复制路径失败，请手动选择复制");
  }
}
</script>

<template>
  <NModal
    v-model:show="settings.visible"
    preset="card"
    :bordered="false"
    :mask-closable="!settings.saving"
    :close-on-esc="!settings.saving"
    class="settings-modal"
    :style="{ width: 'min(680px, calc(100vw - 32px))' }"
  >
    <template #header>
      <div class="settings-modal-header">
        <NIcon :size="20" class="settings-modal-header-icon">
          <Settings24Regular />
        </NIcon>
        <span class="settings-modal-header-title">偏好设置</span>
      </div>
    </template>

    <NSpin :show="!settings.loaded || settings.saving">
      <div class="settings-container">
        <!-- 顶部导航分段栏 -->
        <nav class="settings-nav" aria-label="设置分类">
          <button
            v-for="tab in tabs"
            :key="tab.id"
            type="button"
            class="settings-nav-item"
            :class="{ 'settings-nav-item--active': activeTab === tab.id }"
            @click="activeTab = tab.id"
          >
            <NIcon :size="16" class="settings-nav-icon">
              <component :is="tab.icon" />
            </NIcon>
            <span class="settings-nav-label">{{ tab.label }}</span>
            <span
              v-if="tab.id === 'npk' && images.status.directory"
              class="settings-nav-dot"
              :class="`settings-nav-dot--${images.status.state}`"
            />
          </button>
        </nav>

        <!-- Tab 1: 常规与外观 -->
        <div v-show="activeTab === 'general'" class="settings-tab-panel">
          <!-- 主题选择 -->
          <section class="settings-group">
            <div class="group-header">
              <div class="group-title">外观主题</div>
              <div class="group-subtitle">选择界面的色彩风格，支持深色、浅色或跟随操作系统自动切换</div>
            </div>

            <div class="theme-grid">
              <!-- 深色 -->
              <button
                type="button"
                class="theme-card"
                :class="{ 'theme-card--active': settings.themeMode === 'dark' }"
                @click="onThemeModeChange('dark')"
              >
                <div class="theme-preview theme-preview--dark">
                  <div class="preview-titlebar">
                    <span class="preview-dot dot-red" />
                    <span class="preview-dot dot-yellow" />
                    <span class="preview-dot dot-green" />
                  </div>
                  <div class="preview-body">
                    <div class="preview-line line-short accent-blue" />
                    <div class="preview-line line-long accent-muted" />
                    <div class="preview-line line-med accent-green" />
                  </div>
                </div>
                <div class="theme-card-footer">
                  <div class="theme-card-title">
                    <NIcon :size="16"><WeatherMoon24Regular /></NIcon>
                    <span>深色模式</span>
                  </div>
                  <NIcon
                    v-if="settings.themeMode === 'dark'"
                    :size="18"
                    class="theme-check-icon"
                  >
                    <CheckmarkCircle24Regular />
                  </NIcon>
                </div>
              </button>

              <!-- 浅色 -->
              <button
                type="button"
                class="theme-card"
                :class="{ 'theme-card--active': settings.themeMode === 'light' }"
                @click="onThemeModeChange('light')"
              >
                <div class="theme-preview theme-preview--light">
                  <div class="preview-titlebar">
                    <span class="preview-dot dot-red" />
                    <span class="preview-dot dot-yellow" />
                    <span class="preview-dot dot-green" />
                  </div>
                  <div class="preview-body">
                    <div class="preview-line line-short accent-blue" />
                    <div class="preview-line line-long accent-muted" />
                    <div class="preview-line line-med accent-green" />
                  </div>
                </div>
                <div class="theme-card-footer">
                  <div class="theme-card-title">
                    <NIcon :size="16"><WeatherSunny24Regular /></NIcon>
                    <span>浅色模式</span>
                  </div>
                  <NIcon
                    v-if="settings.themeMode === 'light'"
                    :size="18"
                    class="theme-check-icon"
                  >
                    <CheckmarkCircle24Regular />
                  </NIcon>
                </div>
              </button>

              <!-- 跟随系统 -->
              <button
                type="button"
                class="theme-card"
                :class="{ 'theme-card--active': settings.themeMode === 'system' }"
                @click="onThemeModeChange('system')"
              >
                <div class="theme-preview theme-preview--system">
                  <div class="preview-titlebar">
                    <span class="preview-dot dot-red" />
                    <span class="preview-dot dot-yellow" />
                    <span class="preview-dot dot-green" />
                  </div>
                  <div class="preview-body preview-body--split">
                    <div class="split-half split-half--light">
                      <div class="preview-line line-short accent-blue" />
                      <div class="preview-line line-long accent-muted" />
                    </div>
                    <div class="split-half split-half--dark">
                      <div class="preview-line line-short accent-blue" />
                      <div class="preview-line line-long accent-muted" />
                    </div>
                  </div>
                </div>
                <div class="theme-card-footer">
                  <div class="theme-card-title">
                    <NIcon :size="16"><Desktop24Regular /></NIcon>
                    <span>跟随系统</span>
                  </div>
                  <NIcon
                    v-if="settings.themeMode === 'system'"
                    :size="18"
                    class="theme-check-icon"
                  >
                    <CheckmarkCircle24Regular />
                  </NIcon>
                </div>
              </button>
            </div>
          </section>

          <!-- 交互与文件 -->
          <section class="settings-group">
            <div class="group-header">
              <div class="group-title">交互与文件</div>
            </div>

            <div class="settings-card">
              <div class="setting-item">
                <div class="setting-item-icon">
                  <NIcon :size="18"><CursorHover24Regular /></NIcon>
                </div>
                <div class="setting-item-content">
                  <div class="setting-item-label">节点打开触发方式</div>
                  <div class="setting-item-desc">设置资源管理器目录树中的文件使用单击还是双击进行打开</div>
                </div>
                <div class="setting-item-control">
                  <NRadioGroup
                    :value="settings.explorerOpenMode"
                    size="small"
                    @update:value="onExplorerOpenModeChange"
                  >
                    <NRadioButton value="single-click">单击打开</NRadioButton>
                    <NRadioButton value="double-click">双击打开</NRadioButton>
                  </NRadioGroup>
                </div>
              </div>

              <div class="setting-card-divider" />

              <div class="setting-item">
                <div class="setting-item-icon">
                  <NIcon :size="18"><ShieldCheckmark24Regular /></NIcon>
                </div>
                <div class="setting-item-content">
                  <div class="setting-item-label">覆盖保存前自动备份</div>
                  <div class="setting-item-desc">保存并覆盖原 PVF 归档前，自动在同目录下生成同名的 <code>.bak</code> 备份文件</div>
                </div>
                <div class="setting-item-control">
                  <NSwitch
                    :value="settings.backupSourceOnSave"
                    @update:value="onBackupSourceOnSaveChange"
                  />
                </div>
              </div>
            </div>
          </section>
        </div>

        <!-- Tab 2: 代码编辑器 -->
        <div v-show="activeTab === 'editor'" class="settings-tab-panel">
          <!-- 标注规则渲染 -->
          <section class="settings-group">
            <div class="group-header">
              <div class="group-title">规则标注排版</div>
              <div class="group-subtitle">调整 PVF 规则标注在代码编辑器中的显示位置与渲染行为</div>
            </div>

            <div class="settings-card">
              <div class="setting-item">
                <div class="setting-item-icon">
                  <NIcon :size="18"><Tag24Regular /></NIcon>
                </div>
                <div class="setting-item-content">
                  <div class="setting-item-label">Tag 标注显示位置</div>
                  <div class="setting-item-desc">选择规则名称、关联说明与代码 Tag 的停靠排版方式</div>
                </div>
                <div class="setting-item-control">
                  <NRadioGroup
                    :value="settings.annotationTagPlacement"
                    size="small"
                    @update:value="onPlacementChange"
                  >
                    <NRadioButton value="after-target">紧随内容</NRadioButton>
                    <NRadioButton value="line-end">行末对齐</NRadioButton>
                    <NRadioButton value="hidden">隐藏标注</NRadioButton>
                  </NRadioGroup>
                </div>
              </div>

              <!-- 标注效果实时预览 -->
              <div class="preview-annotation-box">
                <div class="preview-annotation-title">实时排版预览：</div>
                <div class="preview-code-block">
                  <div
                    v-if="settings.annotationTagPlacement === 'after-target'"
                    class="preview-code-row"
                  >
                    <span class="code-token code-key">[name]</span>
                    <span class="code-token code-string">`魔剑-阿波菲斯`</span>
                    <span class="preview-tag preview-tag--blue">巨剑</span>
                  </div>
                  <div
                    v-else-if="settings.annotationTagPlacement === 'line-end'"
                    class="preview-code-row preview-code-row--line-end"
                  >
                    <span class="code-token code-key">[name]</span>
                    <span class="code-token code-string">`魔剑-阿波菲斯`</span>
                    <span class="preview-tag preview-tag--blue preview-tag--end">巨剑</span>
                  </div>
                  <div v-else class="preview-code-row">
                    <span class="code-token code-key">[name]</span>
                    <span class="code-token code-string">`魔剑-阿波菲斯`</span>
                    <span class="preview-code-muted">（Tag 标注已隐藏）</span>
                  </div>
                </div>
              </div>
            </div>
          </section>

          <!-- 编辑模式 -->
          <section class="settings-group">
            <div class="group-header">
              <div class="group-title">编辑模式</div>
            </div>

            <div class="settings-card">
              <div class="setting-item">
                <div class="setting-item-icon">
                  <NIcon :size="18"><Code24Regular /></NIcon>
                </div>
                <div class="setting-item-content">
                  <div class="setting-item-label-row">
                    <span class="setting-item-label">Vim 键位模式</span>
                    <NTag size="small" round :bordered="false" type="info">Modal</NTag>
                  </div>
                  <div class="setting-item-desc">启用后支持 Vim 普通、插入与可视模态，提供熟悉高效的代码导航与操作指令</div>
                </div>
                <div class="setting-item-control">
                  <NSwitch :value="settings.vimMode" @update:value="onVimModeChange" />
                </div>
              </div>
            </div>
          </section>
        </div>

        <!-- Tab 3: NPK 资源库 -->
        <div v-show="activeTab === 'npk'" class="settings-tab-panel">
          <!-- NPK 概览说明 -->
          <div class="hero-card">
            <div class="hero-card-icon">
              <NIcon :size="28"><Image24Regular /></NIcon>
            </div>
            <div class="hero-card-body">
              <div class="hero-card-header">
                <span class="hero-card-title">NPK 游戏图像资源库</span>
                <NTag
                  v-if="images.status.state === 'ready'"
                  type="success"
                  size="small"
                  round
                  :bordered="false"
                >
                  <template #icon><NIcon><CheckmarkCircle24Regular /></NIcon></template>
                  索引就绪
                </NTag>
                <NTag
                  v-else-if="images.status.state === 'building'"
                  type="info"
                  size="small"
                  round
                  :bordered="false"
                >
                  <template #icon><NIcon class="spinning"><ArrowSync24Regular /></NIcon></template>
                  扫描中
                </NTag>
                <NTag
                  v-else-if="images.status.state === 'error'"
                  type="error"
                  size="small"
                  round
                  :bordered="false"
                >
                  <template #icon><NIcon><DismissCircle24Regular /></NIcon></template>
                  索引异常
                </NTag>
                <NTag v-else size="small" round :bordered="false">未配置目录</NTag>
              </div>
              <div class="hero-card-desc">
                配置 DNF 客户端的 ImagePacks2 目录后，pvfine 可实时解析武器、装备、消耗品和技能的原版图标，并在脚本代码中提供直观的悬浮图像预览。
              </div>
            </div>
          </div>

          <!-- 目录选择卡片 -->
          <section class="settings-group">
            <div class="group-header">
              <div class="group-title">NPK 目录路径</div>
            </div>

            <div class="settings-card">
              <div class="npk-path-row">
                <div class="npk-path-icon">
                  <NIcon :size="20"><FolderOpen24Regular /></NIcon>
                </div>
                <div class="npk-path-text-wrapper" :title="settings.npkDirectory || ''">
                  <span v-if="settings.npkDirectory" class="npk-path-text">
                    {{ settings.npkDirectory }}
                  </span>
                  <span v-else class="npk-path-placeholder">
                    尚未选择目录，请点击右侧按钮选择客户端 ImagePacks2 文件夹
                  </span>
                </div>
                <div class="npk-path-actions">
                  <NTooltip v-if="settings.npkDirectory" trigger="hover">
                    <template #trigger>
                      <NButton
                        quaternary
                        size="small"
                        aria-label="复制路径"
                        @click="copyNPKDirectory"
                      >
                        <template #icon><NIcon><Copy24Regular /></NIcon></template>
                      </NButton>
                    </template>
                    复制目录路径
                  </NTooltip>
                  <NButton
                    secondary
                    size="small"
                    :loading="selectingNPK"
                    @click="onSelectNPKDirectory"
                  >
                    <template #icon><NIcon><FolderOpen24Regular /></NIcon></template>
                    {{ settings.npkDirectory ? "更改目录" : "选择目录" }}
                  </NButton>
                </div>
              </div>

              <!-- 正在扫描时：进度条 -->
              <div v-if="images.status.state === 'building'" class="npk-progress-card">
                <div class="npk-progress-header">
                  <span class="npk-progress-title">
                    {{ images.status.total > 0 ? `正在扫描 NPK 资源 (${images.status.done}/${images.status.total})` : "正在准备扫描资源包..." }}
                  </span>
                  <span class="npk-progress-percent">{{ npkProgressPercent }}%</span>
                </div>
                <div class="npk-progress-track">
                  <div
                    class="npk-progress-bar"
                    :style="{ width: `${npkProgressPercent}%` }"
                  />
                </div>
              </div>

              <!-- 就绪时：数据指标网格 -->
              <div v-else-if="images.status.state === 'ready'" class="npk-metrics-grid">
                <div class="metric-item">
                  <div class="metric-value">{{ images.status.npkFiles.toLocaleString() }}</div>
                  <div class="metric-label">NPK 资源包</div>
                </div>
                <div class="metric-item">
                  <div class="metric-value">{{ images.status.imgFiles.toLocaleString() }}</div>
                  <div class="metric-label">IMG 镜像文件</div>
                </div>
                <div class="metric-item">
                  <div class="metric-value">{{ images.status.imageCount.toLocaleString() }}</div>
                  <div class="metric-label">有效图标资产</div>
                </div>
              </div>

              <div
                v-if="images.status.state === 'ready' && (images.status.skipped > 0 || images.status.duplicates > 0)"
                class="npk-notes-row"
              >
                <NIcon :size="14" class="notes-icon"><Info24Regular /></NIcon>
                <span>
                  {{
                    [
                      images.status.skipped > 0 ? `跳过 ${images.status.skipped} 项非标准文件` : "",
                      images.status.duplicates > 0 ? `去重 ${images.status.duplicates} 项重复资源` : "",
                    ].filter(Boolean).join("，")
                  }}
                </span>
              </div>

              <!-- 异常告警 -->
              <div v-else-if="images.status.state === 'error'" class="npk-error-box">
                <NAlert type="error" :bordered="false" title="图标索引建立失败">
                  {{ images.status.error || "扫描过程中发生未知错误，请检查目录权限或文件是否被占用" }}
                </NAlert>
              </div>

              <div class="setting-card-divider" />

              <!-- 重建按钮 -->
              <div class="setting-item">
                <div class="setting-item-icon">
                  <NIcon :size="18"><ArrowSync24Regular /></NIcon>
                </div>
                <div class="setting-item-content">
                  <div class="setting-item-label">重建全量图标索引</div>
                  <div class="setting-item-desc">当客户端更新补丁、新增 NPK 或图标出现缺失时，可点击重建全量映射</div>
                </div>
                <div class="setting-item-control">
                  <NButton
                    size="small"
                    secondary
                    :loading="rebuildingNPK"
                    :disabled="!images.status.directory || images.status.state === 'building'"
                    @click="onRebuildNPKIndex"
                  >
                    <template #icon><NIcon><ArrowSync24Regular /></NIcon></template>
                    重建索引
                  </NButton>
                </div>
              </div>
            </div>
          </section>
        </div>

        <!-- Tab 4: 系统维护 -->
        <div v-show="activeTab === 'system'" class="settings-tab-panel">
          <section class="settings-group">
            <div class="group-header">
              <div class="group-title">维护与热重载</div>
              <div class="group-subtitle">快速维护本地标注与脚本渲染规则，并检测应用程序更新</div>
            </div>

            <div class="settings-card">
              <div class="setting-item">
                <div class="setting-item-icon">
                  <NIcon :size="18"><DocumentSync24Regular /></NIcon>
                </div>
                <div class="setting-item-content">
                  <div class="setting-item-label">标注规则热重载</div>
                  <div class="setting-item-desc">从磁盘重新加载标注规则定义与关联数据类型。编辑了本地规则文件后无需重启应用，点击即可即时同步刷新。</div>
                </div>
                <div class="setting-item-control">
                  <NButton
                    size="small"
                    secondary
                    :loading="reloadingAnnotations"
                    aria-label="重载标注规则"
                    @click="onReloadAnnotations"
                  >
                    <template #icon><NIcon><DocumentSync24Regular /></NIcon></template>
                    立即重载
                  </NButton>
                </div>
              </div>

              <div class="setting-card-divider" />

              <div class="setting-item">
                <div class="setting-item-icon">
                  <NIcon :size="18"><DocumentSync24Regular /></NIcon>
                </div>
                <div class="setting-item-content">
                  <div class="setting-item-label">渲染规则热重载</div>
                  <div class="setting-item-desc">从磁盘重新加载脚本展示格式，并刷新当前编辑器中的未修改文件。</div>
                </div>
                <div class="setting-item-control">
                  <NButton
                    size="small"
                    secondary
                    :loading="reloadingRendering"
                    aria-label="重载渲染规则"
                    @click="onReloadRendering"
                  >
                    <template #icon><NIcon><DocumentSync24Regular /></NIcon></template>
                    立即重载
                  </NButton>
                </div>
              </div>

              <div class="setting-card-divider" />

              <div class="setting-item">
                <div class="setting-item-icon">
                  <NIcon :size="18"><ArrowSync24Regular /></NIcon>
                </div>
                <div class="setting-item-content">
                  <div class="setting-item-label">检查版本更新</div>
                  <div class="setting-item-desc">连接 GitHub Release 发布频道检测 pvfine 是否有可用的新版本与性能优化</div>
                </div>
                <div class="setting-item-control">
                  <NButton
                    size="small"
                    secondary
                    :loading="checkingUpdates"
                    aria-label="检查更新"
                    @click="onCheckUpdates"
                  >
                    <template #icon><NIcon><ArrowSync24Regular /></NIcon></template>
                    检查更新
                  </NButton>
                </div>
              </div>
            </div>
          </section>

          <!-- 关于应用信息 -->
          <div class="about-card">
            <div class="about-logo-row">
              <div class="about-brand">pvfine</div>
              <NTag size="small" round :bordered="false" type="primary">v0.0.0</NTag>
            </div>
            <div class="about-desc">
              基于 Wails 3、Go 与 Vue 3 构建的高性能、现代化的 DNF PVF 脚本交互式编辑工具。
            </div>
            <div class="about-badges">
              <span class="about-pill">Go 1.24</span>
              <span class="about-pill">Vue 3</span>
              <span class="about-pill">CodeMirror 6</span>
              <span class="about-pill">Naive UI</span>
            </div>
          </div>
        </div>
      </div>
    </NSpin>
  </NModal>
</template>

<style scoped>
/* 弹窗头部 */
.settings-modal-header {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 15px;
  font-weight: 600;
  color: var(--pvf-text-primary);
}
.settings-modal-header-icon {
  color: var(--pvf-primary);
}

.settings-container {
  min-height: 440px;
}

/* 顶部导航分段栏 */
.settings-nav {
  display: flex;
  gap: 4px;
  padding: 3px;
  margin-bottom: 20px;
  background: var(--pvf-surface-subtle);
  border: 1px solid var(--pvf-border-subtle);
  border-radius: 9px;
}
.settings-nav-item {
  position: relative;
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 7px;
  padding: 8px 12px;
  background: transparent;
  border: none;
  border-radius: 7px;
  color: var(--pvf-text-secondary);
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.16s ease;
  user-select: none;
}
.settings-nav-item:hover {
  color: var(--pvf-text-primary);
  background: var(--pvf-surface-hover);
}
.settings-nav-item:focus-visible {
  outline: none;
  box-shadow: 0 0 0 2px var(--pvf-effect-focus-ring);
}
.settings-nav-item--active {
  color: var(--pvf-text-primary);
  background: var(--pvf-surface-card);
  font-weight: 600;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1), 0 0 0 1px var(--pvf-border-faint);
}
.settings-nav-icon {
  display: flex;
  align-items: center;
}
.settings-nav-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  transition: background-color 0.2s ease;
}
.settings-nav-dot--ready {
  background-color: var(--pvf-success);
}
.settings-nav-dot--building {
  background-color: var(--pvf-primary);
  animation: pulse-dot 1.2s infinite ease-in-out;
}
.settings-nav-dot--error {
  background-color: var(--pvf-error);
}

@keyframes pulse-dot {
  0%, 100% {
    opacity: 1;
    transform: scale(1);
  }
  50% {
    opacity: 0.4;
    transform: scale(1.3);
  }
}

/* 分组通用样式 */
.settings-tab-panel {
  display: flex;
  flex-direction: column;
  gap: 20px;
}
.settings-group {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.group-header {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.group-title {
  color: var(--pvf-text-primary);
  font-size: 13px;
  font-weight: 600;
}
.group-subtitle {
  color: var(--pvf-text-faint);
  font-size: 12px;
}

/* 卡片容器 */
.settings-card {
  background: var(--pvf-surface-card);
  border: 1px solid var(--pvf-border-subtle);
  border-radius: 10px;
  overflow: hidden;
}
.setting-card-divider {
  height: 1px;
  margin: 0 16px;
  background: var(--pvf-border-faint);
}

/* 设置项行 */
.setting-item {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 14px 16px;
}
.setting-item-icon {
  flex: 0 0 34px;
  height: 34px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--pvf-surface-subtle);
  border: 1px solid var(--pvf-border-faint);
  border-radius: 8px;
  color: var(--pvf-primary);
}
.setting-item-content {
  flex: 1;
  min-width: 0;
}
.setting-item-label-row {
  display: flex;
  align-items: center;
  gap: 8px;
}
.setting-item-label {
  color: var(--pvf-text-primary);
  font-size: 13px;
  font-weight: 500;
}
.setting-item-desc {
  margin-top: 3px;
  color: var(--pvf-text-faint);
  font-size: 12px;
  line-height: 1.45;
}
.setting-item-desc code {
  padding: 1px 4px;
  background: var(--pvf-surface-code);
  border-radius: 3px;
  font-family: ui-monospace, monospace;
}
.setting-item-control {
  flex: 0 0 auto;
}

/* 主题选择网格 */
.theme-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 12px;
}
.theme-card {
  display: flex;
  flex-direction: column;
  padding: 10px;
  background: var(--pvf-surface-card);
  border: 1.5px solid var(--pvf-border-subtle);
  border-radius: 10px;
  cursor: pointer;
  transition: all 0.18s ease;
  user-select: none;
  text-align: left;
}
.theme-card:hover {
  border-color: var(--pvf-primary-hover);
  background: var(--pvf-surface-hover);
  transform: translateY(-1px);
}
.theme-card:focus-visible {
  outline: none;
  box-shadow: 0 0 0 2px var(--pvf-effect-focus-ring);
}
.theme-card--active {
  border-color: var(--pvf-primary);
  background: var(--pvf-primary-soft);
  box-shadow: 0 0 0 1px var(--pvf-primary);
}
.theme-preview {
  height: 60px;
  border-radius: 6px;
  border: 1px solid rgba(128, 128, 128, 0.2);
  display: flex;
  flex-direction: column;
  overflow: hidden;
  margin-bottom: 10px;
}
.theme-preview--dark {
  background: #181a20;
}
.theme-preview--light {
  background: #f3f5f8;
}
.preview-titlebar {
  height: 16px;
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 0 6px;
  background: rgba(128, 128, 128, 0.08);
  border-bottom: 1px solid rgba(128, 128, 128, 0.15);
}
.preview-dot {
  width: 5px;
  height: 5px;
  border-radius: 50%;
}
.dot-red { background: #ff5f56; }
.dot-yellow { background: #ffbd2e; }
.dot-green { background: #27c93f; }

.preview-body {
  flex: 1;
  padding: 8px 10px;
  display: flex;
  flex-direction: column;
  gap: 5px;
}
.preview-body--split {
  padding: 0;
  flex-direction: row;
}
.split-half {
  flex: 1;
  padding: 8px 6px;
  display: flex;
  flex-direction: column;
  gap: 5px;
}
.split-half--light {
  background: #ffffff;
}
.split-half--dark {
  background: #1e2129;
}
.preview-line {
  height: 4px;
  border-radius: 2px;
}
.line-short { width: 40%; }
.line-long { width: 85%; }
.line-med { width: 60%; }
.accent-blue { background: #4f8cff; }
.accent-green { background: #63e2b7; }
.accent-muted { background: rgba(128, 128, 128, 0.3); }

.theme-card-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 2px 4px;
}
.theme-card-title {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  font-weight: 500;
  color: var(--pvf-text-primary);
}
.theme-check-icon {
  color: var(--pvf-primary);
}

/* 标注排版预览框 */
.preview-annotation-box {
  padding: 12px 16px 14px;
  background: var(--pvf-surface-subtle);
  border-top: 1px solid var(--pvf-border-faint);
}
.preview-annotation-title {
  font-size: 11px;
  font-weight: 500;
  color: var(--pvf-text-faint);
  margin-bottom: 8px;
}
.preview-code-block {
  padding: 8px 12px;
  background: var(--pvf-surface-code);
  border: 1px solid var(--pvf-border-faint);
  border-radius: 6px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
}
.preview-code-row {
  display: flex;
  align-items: center;
  gap: 8px;
}
.preview-code-row--line-end {
  justify-content: space-between;
}
.code-key {
  color: var(--pvf-primary);
  font-weight: 600;
}
.code-string {
  color: var(--pvf-editor-syntax-string);
}
.preview-code-muted {
  color: var(--pvf-text-faint);
  font-size: 11px;
  font-family: -apple-system, BlinkMacSystemFont, sans-serif;
}
.preview-tag {
  display: inline-flex;
  align-items: center;
  padding: 1px 6px;
  font-size: 11px;
  border-radius: 3px;
  font-family: -apple-system, BlinkMacSystemFont, sans-serif;
}
.preview-tag--blue {
  background: var(--pvf-editor-annotation-surface);
  color: var(--pvf-editor-annotation-text);
  border: 1px solid var(--pvf-editor-annotation-border);
}
.preview-tag--end {
  margin-left: auto;
}

/* NPK Hero 说明卡片 */
.hero-card {
  display: flex;
  gap: 16px;
  padding: 16px;
  background: var(--pvf-surface-elevated);
  border: 1px solid var(--pvf-border-subtle);
  border-radius: 10px;
}
.hero-card-icon {
  flex: 0 0 44px;
  height: 44px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--pvf-primary-soft);
  color: var(--pvf-primary);
  border-radius: 10px;
}
.hero-card-body {
  flex: 1;
  min-width: 0;
}
.hero-card-header {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 4px;
}
.hero-card-title {
  color: var(--pvf-text-primary);
  font-size: 14px;
  font-weight: 600;
}
.hero-card-desc {
  color: var(--pvf-text-secondary);
  font-size: 12px;
  line-height: 1.5;
}

/* NPK 路径选择行 */
.npk-path-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 14px 16px;
}
.npk-path-icon {
  color: var(--pvf-text-faint);
  display: flex;
  align-items: center;
}
.npk-path-text-wrapper {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.npk-path-text {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
  color: var(--pvf-text-primary);
  user-select: text;
  -webkit-user-select: text;
}
.npk-path-placeholder {
  font-size: 12px;
  color: var(--pvf-text-faint);
  font-style: italic;
}
.npk-path-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 0 0 auto;
}

/* NPK 扫描进度条 */
.npk-progress-card {
  padding: 12px 16px 16px;
  background: var(--pvf-surface-subtle);
  border-top: 1px solid var(--pvf-border-faint);
}
.npk-progress-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 8px;
  font-size: 12px;
}
.npk-progress-title {
  color: var(--pvf-primary);
  font-weight: 500;
}
.npk-progress-percent {
  color: var(--pvf-text-muted);
  font-variant-numeric: tabular-nums;
  font-weight: 600;
}
.npk-progress-track {
  height: 6px;
  background: var(--pvf-surface-inset);
  border-radius: 3px;
  overflow: hidden;
}
.npk-progress-bar {
  height: 100%;
  background: var(--pvf-primary);
  border-radius: 3px;
  transition: width 0.25s ease-out;
}

/* NPK 统计网格 */
.npk-metrics-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 1px;
  background: var(--pvf-border-faint);
  border-top: 1px solid var(--pvf-border-faint);
}
.metric-item {
  padding: 12px 16px;
  background: var(--pvf-surface-card);
  text-align: center;
}
.metric-value {
  font-size: 18px;
  font-weight: 600;
  color: var(--pvf-text-primary);
  font-variant-numeric: tabular-nums;
}
.metric-label {
  margin-top: 2px;
  font-size: 11px;
  color: var(--pvf-text-faint);
}
.npk-notes-row {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 16px;
  background: var(--pvf-surface-subtle);
  color: var(--pvf-text-faint);
  font-size: 11px;
  border-top: 1px solid var(--pvf-border-faint);
}
.notes-icon {
  color: var(--pvf-info);
}

.npk-error-box {
  padding: 12px 16px;
  border-top: 1px solid var(--pvf-border-faint);
}

/* 关于卡片 */
.about-card {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 16px;
  background: var(--pvf-surface-subtle);
  border: 1px solid var(--pvf-border-faint);
  border-radius: 10px;
}
.about-logo-row {
  display: flex;
  align-items: center;
  gap: 10px;
}
.about-brand {
  font-size: 16px;
  font-weight: 700;
  color: var(--pvf-text-primary);
  letter-spacing: -0.2px;
}
.about-desc {
  color: var(--pvf-text-secondary);
  font-size: 12px;
  line-height: 1.45;
}
.about-badges {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 4px;
}
.about-pill {
  display: inline-flex;
  padding: 2px 8px;
  background: var(--pvf-surface-card);
  border: 1px solid var(--pvf-border-subtle);
  border-radius: 4px;
  font-size: 11px;
  color: var(--pvf-text-muted);
}

/* 动画 */
.spinning {
  animation: spin 1s linear infinite;
}
@keyframes spin {
  from {
    transform: rotate(0deg);
  }
  to {
    transform: rotate(360deg);
  }
}

@media (max-width: 600px) {
  .theme-grid {
    grid-template-columns: 1fr;
  }
  .settings-nav {
    flex-wrap: wrap;
  }
  .settings-nav-item {
    flex: 1 1 45%;
  }
  .npk-metrics-grid {
    grid-template-columns: 1fr;
  }
}
</style>
