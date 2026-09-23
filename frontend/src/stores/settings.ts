import { defineStore } from "pinia";
import { ref } from "vue";
import { SettingsService } from "../../bindings/pvfine/services";
import { Events } from "@wailsio/runtime";
import type { AppSettings } from "../../bindings/pvfine/services/models";
import type { ThemeMode } from "../theme";
import type { ShortcutOverrides } from "../shortcuts";

export type AnnotationTagPlacement = "after-target" | "line-end" | "hidden";
export type ExplorerOpenMode = "single-click" | "double-click";
export type SettingsTab = "general" | "editor" | "npk" | "system" | "shortcuts";
export type { ThemeMode } from "../theme";

export const defaultAutosaveIntervalSeconds = 300;
export const minAutosaveIntervalMinutes = 1;
export const maxAutosaveIntervalMinutes = 120;

const defaultSettings: AppSettings = {
  annotationTagPlacement: "after-target",
  explorerOpenMode: "single-click",
  vimMode: false,
  backupSourceOnSave: true,
  npkDirectory: "",
  theme: "dark",
  autosaveEnabled: false,
  autosavePath: "",
  autosaveIntervalSeconds: defaultAutosaveIntervalSeconds,
  shortcutOverrides: {},
};

export const useSettingsStore = defineStore("settings", () => {
  const visible = ref(false);
  const activeTab = ref<SettingsTab>("general");
  const loaded = ref(false);
  const saving = ref(false);
  const annotationTagPlacement = ref<AnnotationTagPlacement>("after-target");
  const explorerOpenMode = ref<ExplorerOpenMode>("single-click");
  const vimMode = ref(false);
  const backupSourceOnSave = ref(true);
  const npkDirectory = ref("");
  const themeMode = ref<ThemeMode>("dark");
  const autosaveEnabled = ref(false);
  const autosavePath = ref("");
  const autosaveIntervalSeconds = ref(defaultAutosaveIntervalSeconds);
  const shortcutOverrides = ref<ShortcutOverrides>({});

  /** 当前设置的全量快照,避免每次保存都手写所有字段。 */
  function currentSettings(overrides: Partial<AppSettings> = {}): AppSettings {
    return {
      annotationTagPlacement: annotationTagPlacement.value,
      explorerOpenMode: explorerOpenMode.value,
      vimMode: vimMode.value,
      backupSourceOnSave: backupSourceOnSave.value,
      npkDirectory: npkDirectory.value,
      theme: themeMode.value,
      autosaveEnabled: autosaveEnabled.value,
      autosavePath: autosavePath.value,
      autosaveIntervalSeconds: autosaveIntervalSeconds.value,
      shortcutOverrides: shortcutOverrides.value,
      ...overrides,
    };
  }

  async function load() {
    if (loaded.value) return;
    try {
      const settings = await SettingsService.GetSettings();
      annotationTagPlacement.value = normalizePlacement(settings.annotationTagPlacement);
      explorerOpenMode.value = normalizeExplorerOpenMode(settings.explorerOpenMode);
      vimMode.value = normalizeVimMode(settings.vimMode);
      backupSourceOnSave.value = normalizeBackupSourceOnSave(settings.backupSourceOnSave);
      npkDirectory.value = normalizeNPKDirectory(settings.npkDirectory);
      themeMode.value = normalizeThemeMode(settings.theme);
      autosaveEnabled.value = normalizeAutosaveEnabled(settings.autosaveEnabled);
      autosavePath.value = normalizeAutosavePath(settings.autosavePath);
      autosaveIntervalSeconds.value = normalizeAutosaveInterval(settings.autosaveIntervalSeconds);
      shortcutOverrides.value = normalizeShortcutOverrides(settings.shortcutOverrides);
    } catch (error) {
      console.error("load settings failed", error);
      annotationTagPlacement.value = "after-target";
      explorerOpenMode.value = "single-click";
      vimMode.value = false;
      backupSourceOnSave.value = true;
      npkDirectory.value = "";
      themeMode.value = "dark";
      autosaveEnabled.value = false;
      autosavePath.value = "";
      autosaveIntervalSeconds.value = defaultAutosaveIntervalSeconds;
      shortcutOverrides.value = {};
    } finally {
      loaded.value = true;
    }
  }

  async function savePlacement(value: AnnotationTagPlacement) {
    await saveSettings(currentSettings({ annotationTagPlacement: value }));
  }

  async function saveExplorerOpenMode(value: ExplorerOpenMode) {
    await saveSettings(currentSettings({ explorerOpenMode: value }));
  }

  async function saveVimMode(value: boolean) {
    await saveSettings(currentSettings({ vimMode: value }));
  }

  async function saveBackupSourceOnSave(value: boolean) {
    await saveSettings(currentSettings({ backupSourceOnSave: value }));
  }

  async function saveThemeMode(value: ThemeMode) {
    await saveSettings(currentSettings({ theme: value }));
  }

  async function saveAutosaveEnabled(value: boolean) {
    await saveSettings(currentSettings({ autosaveEnabled: value }));
  }

  async function saveAutosavePath(value: string) {
    await saveSettings(currentSettings({ autosavePath: value }));
  }

  async function saveAutosaveInterval(seconds: number) {
    await saveSettings(currentSettings({ autosaveIntervalSeconds: seconds }));
  }

  async function updateShortcutOverrides(overrides: ShortcutOverrides) {
    saving.value = true;
    try {
      const updated = await SettingsService.UpdateShortcutOverrides(overrides);
      shortcutOverrides.value = normalizeShortcutOverrides(updated.shortcutOverrides);
      await Events.Emit("settings:shortcuts-changed");
    } finally {
      saving.value = false;
    }
  }

  async function reloadShortcuts() {
    try {
      const latest = await SettingsService.GetSettings();
      shortcutOverrides.value = normalizeShortcutOverrides(latest.shortcutOverrides);
    } catch (error) {
      console.error("reload shortcuts failed", error);
    }
  }

  Events.On("settings:shortcuts-changed", () => {
    void reloadShortcuts();
  });

  async function saveSettings(next: AppSettings) {
    const previousPlacement = annotationTagPlacement.value;
    const previousExplorerOpenMode = explorerOpenMode.value;
    const previousVimMode = vimMode.value;
    const previousBackupSourceOnSave = backupSourceOnSave.value;
    const previousNPKDirectory = npkDirectory.value;
    const previousThemeMode = themeMode.value;
    const previousAutosaveEnabled = autosaveEnabled.value;
    const previousAutosavePath = autosavePath.value;
    const previousAutosaveInterval = autosaveIntervalSeconds.value;
    const previousShortcutOverrides = shortcutOverrides.value;
    annotationTagPlacement.value = normalizePlacement(next.annotationTagPlacement);
    explorerOpenMode.value = normalizeExplorerOpenMode(next.explorerOpenMode);
    vimMode.value = normalizeVimMode(next.vimMode);
    backupSourceOnSave.value = normalizeBackupSourceOnSave(next.backupSourceOnSave);
    npkDirectory.value = normalizeNPKDirectory(next.npkDirectory);
    themeMode.value = normalizeThemeMode(next.theme);
    autosaveEnabled.value = normalizeAutosaveEnabled(next.autosaveEnabled);
    autosavePath.value = normalizeAutosavePath(next.autosavePath);
    autosaveIntervalSeconds.value = normalizeAutosaveInterval(next.autosaveIntervalSeconds);
    shortcutOverrides.value = normalizeShortcutOverrides(next.shortcutOverrides);
    saving.value = true;
    try {
      await SettingsService.SaveSettings({
        ...defaultSettings,
        ...next,
      });
    } catch (error) {
      annotationTagPlacement.value = previousPlacement;
      explorerOpenMode.value = previousExplorerOpenMode;
      vimMode.value = previousVimMode;
      backupSourceOnSave.value = previousBackupSourceOnSave;
      npkDirectory.value = previousNPKDirectory;
      themeMode.value = previousThemeMode;
      autosaveEnabled.value = previousAutosaveEnabled;
      autosavePath.value = previousAutosavePath;
      autosaveIntervalSeconds.value = previousAutosaveInterval;
      shortcutOverrides.value = previousShortcutOverrides;
      throw error;
    } finally {
      saving.value = false;
    }
  }

  function open(tab?: SettingsTab) {
    if (tab && typeof tab === "string") {
      activeTab.value = tab;
    } else {
      activeTab.value = "general";
    }
    visible.value = true;
    void load();
  }

  return {
    visible,
    activeTab,
    loaded,
    saving,
    annotationTagPlacement,
    explorerOpenMode,
    vimMode,
    backupSourceOnSave,
    npkDirectory,
    themeMode,
    autosaveEnabled,
    autosavePath,
    autosaveIntervalSeconds,
    shortcutOverrides,
    load,
    savePlacement,
    saveExplorerOpenMode,
    saveVimMode,
    saveBackupSourceOnSave,
    saveThemeMode,
    saveAutosaveEnabled,
    saveAutosavePath,
    saveAutosaveInterval,
    updateShortcutOverrides,
    open,
  };
});

function normalizePlacement(value: string): AnnotationTagPlacement {
  if (value === "line-end" || value === "hidden") return value;
  return "after-target";
}

function normalizeExplorerOpenMode(value: string): ExplorerOpenMode {
  return value === "double-click" ? value : "single-click";
}

function normalizeVimMode(value: boolean): boolean {
  return value === true;
}

function normalizeBackupSourceOnSave(value: boolean): boolean {
  return value !== false;
}

function normalizeNPKDirectory(value: string | null | undefined): string {
  return typeof value === "string" ? value.trim() : "";
}

function normalizeThemeMode(value: string | null | undefined): ThemeMode {
  if (value === "light" || value === "system") return value;
  return "dark";
}

function normalizeAutosaveEnabled(value: boolean): boolean {
  return value === true;
}

function normalizeAutosavePath(value: string | null | undefined): string {
  return typeof value === "string" ? value.trim() : "";
}

function normalizeAutosaveInterval(value: number | null | undefined): number {
  const seconds = Number(value ?? 0);
  if (!Number.isFinite(seconds) || seconds <= 0) return defaultAutosaveIntervalSeconds;
  const min = minAutosaveIntervalMinutes * 60;
  const max = maxAutosaveIntervalMinutes * 60;
  return Math.min(max, Math.max(min, Math.round(seconds)));
}

function normalizeShortcutOverrides(value: Record<string, string | undefined> | null | undefined): ShortcutOverrides {
  if (!value || typeof value !== "object") return {};
  const result: ShortcutOverrides = {};
  for (const [command, binding] of Object.entries(value)) {
    if (typeof binding === "string") result[command] = binding;
  }
  return result;
}
