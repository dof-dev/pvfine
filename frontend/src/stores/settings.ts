import { defineStore } from "pinia";
import { ref } from "vue";
import { SettingsService } from "../../bindings/pvfine/services";
import type { AppSettings } from "../../bindings/pvfine/services/models";

export type AnnotationTagPlacement = "after-target" | "line-end" | "hidden";
export type ExplorerOpenMode = "single-click" | "double-click";

const defaultSettings: AppSettings = {
  annotationTagPlacement: "after-target",
  explorerOpenMode: "single-click",
  vimMode: false,
  backupSourceOnSave: true,
};

export const useSettingsStore = defineStore("settings", () => {
  const visible = ref(false);
  const loaded = ref(false);
  const saving = ref(false);
  const annotationTagPlacement = ref<AnnotationTagPlacement>("after-target");
  const explorerOpenMode = ref<ExplorerOpenMode>("single-click");
  const vimMode = ref(false);
  const backupSourceOnSave = ref(true);

  async function load() {
    if (loaded.value) return;
    try {
      const settings = await SettingsService.GetSettings();
      annotationTagPlacement.value = normalizePlacement(settings.annotationTagPlacement);
      explorerOpenMode.value = normalizeExplorerOpenMode(settings.explorerOpenMode);
      vimMode.value = normalizeVimMode(settings.vimMode);
      backupSourceOnSave.value = normalizeBackupSourceOnSave(settings.backupSourceOnSave);
    } catch (error) {
      console.error("load settings failed", error);
      annotationTagPlacement.value = "after-target";
      explorerOpenMode.value = "single-click";
      vimMode.value = false;
      backupSourceOnSave.value = true;
    } finally {
      loaded.value = true;
    }
  }

  async function savePlacement(value: AnnotationTagPlacement) {
    await saveSettings({
      annotationTagPlacement: value,
      explorerOpenMode: explorerOpenMode.value,
      vimMode: vimMode.value,
      backupSourceOnSave: backupSourceOnSave.value,
    });
  }

  async function saveExplorerOpenMode(value: ExplorerOpenMode) {
    await saveSettings({
      annotationTagPlacement: annotationTagPlacement.value,
      explorerOpenMode: value,
      vimMode: vimMode.value,
      backupSourceOnSave: backupSourceOnSave.value,
    });
  }

  async function saveVimMode(value: boolean) {
    await saveSettings({
      annotationTagPlacement: annotationTagPlacement.value,
      explorerOpenMode: explorerOpenMode.value,
      vimMode: value,
      backupSourceOnSave: backupSourceOnSave.value,
    });
  }

  async function saveBackupSourceOnSave(value: boolean) {
    await saveSettings({
      annotationTagPlacement: annotationTagPlacement.value,
      explorerOpenMode: explorerOpenMode.value,
      vimMode: vimMode.value,
      backupSourceOnSave: value,
    });
  }

  async function saveSettings(next: AppSettings) {
    const previousPlacement = annotationTagPlacement.value;
    const previousExplorerOpenMode = explorerOpenMode.value;
    const previousVimMode = vimMode.value;
    const previousBackupSourceOnSave = backupSourceOnSave.value;
    annotationTagPlacement.value = normalizePlacement(next.annotationTagPlacement);
    explorerOpenMode.value = normalizeExplorerOpenMode(next.explorerOpenMode);
    vimMode.value = normalizeVimMode(next.vimMode);
    backupSourceOnSave.value = normalizeBackupSourceOnSave(next.backupSourceOnSave);
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
      throw error;
    } finally {
      saving.value = false;
    }
  }

  function open() {
    visible.value = true;
    void load();
  }

  return {
    visible,
    loaded,
    saving,
    annotationTagPlacement,
    explorerOpenMode,
    vimMode,
    backupSourceOnSave,
    load,
    savePlacement,
    saveExplorerOpenMode,
    saveVimMode,
    saveBackupSourceOnSave,
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
