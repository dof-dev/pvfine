import { defineStore } from "pinia";
import { ref } from "vue";
import { SettingsService } from "../../bindings/pvfine/services";
import type { AppSettings } from "../../bindings/pvfine/services/models";

export type AnnotationTagPlacement = "after-target" | "line-end" | "hidden";
export type ExplorerOpenMode = "single-click" | "double-click";

const defaultSettings: AppSettings = {
  annotationTagPlacement: "after-target",
  explorerOpenMode: "single-click",
};

export const useSettingsStore = defineStore("settings", () => {
  const visible = ref(false);
  const loaded = ref(false);
  const saving = ref(false);
  const annotationTagPlacement = ref<AnnotationTagPlacement>("after-target");
  const explorerOpenMode = ref<ExplorerOpenMode>("single-click");

  async function load() {
    if (loaded.value) return;
    try {
      const settings = await SettingsService.GetSettings();
      annotationTagPlacement.value = normalizePlacement(settings.annotationTagPlacement);
      explorerOpenMode.value = normalizeExplorerOpenMode(settings.explorerOpenMode);
    } catch (error) {
      console.error("load settings failed", error);
      annotationTagPlacement.value = "after-target";
      explorerOpenMode.value = "single-click";
    } finally {
      loaded.value = true;
    }
  }

  async function savePlacement(value: AnnotationTagPlacement) {
    await saveSettings({
      annotationTagPlacement: value,
      explorerOpenMode: explorerOpenMode.value,
    });
  }

  async function saveExplorerOpenMode(value: ExplorerOpenMode) {
    await saveSettings({
      annotationTagPlacement: annotationTagPlacement.value,
      explorerOpenMode: value,
    });
  }

  async function saveSettings(next: AppSettings) {
    const previousPlacement = annotationTagPlacement.value;
    const previousExplorerOpenMode = explorerOpenMode.value;
    annotationTagPlacement.value = normalizePlacement(next.annotationTagPlacement);
    explorerOpenMode.value = normalizeExplorerOpenMode(next.explorerOpenMode);
    saving.value = true;
    try {
      await SettingsService.SaveSettings({
        ...defaultSettings,
        ...next,
      });
    } catch (error) {
      annotationTagPlacement.value = previousPlacement;
      explorerOpenMode.value = previousExplorerOpenMode;
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
    load,
    savePlacement,
    saveExplorerOpenMode,
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
