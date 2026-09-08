import { defineStore } from "pinia";
import { ref } from "vue";
import { SettingsService } from "../../bindings/pvfine/services";
import type { AppSettings } from "../../bindings/pvfine/services/models";

export type AnnotationTagPlacement = "after-target" | "line-end" | "hidden";

const defaultSettings: AppSettings = {
  annotationTagPlacement: "after-target",
};

export const useSettingsStore = defineStore("settings", () => {
  const visible = ref(false);
  const loaded = ref(false);
  const saving = ref(false);
  const annotationTagPlacement = ref<AnnotationTagPlacement>("after-target");

  async function load() {
    if (loaded.value) return;
    try {
      const settings = await SettingsService.GetSettings();
      annotationTagPlacement.value = normalizePlacement(settings.annotationTagPlacement);
    } catch (error) {
      console.error("load settings failed", error);
      annotationTagPlacement.value = "after-target";
    } finally {
      loaded.value = true;
    }
  }

  async function savePlacement(value: AnnotationTagPlacement) {
    const previous = annotationTagPlacement.value;
    annotationTagPlacement.value = value;
    saving.value = true;
    try {
      await SettingsService.SaveSettings({
        ...defaultSettings,
        annotationTagPlacement: value,
      });
    } catch (error) {
      annotationTagPlacement.value = previous;
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
    load,
    savePlacement,
    open,
  };
});

function normalizePlacement(value: string): AnnotationTagPlacement {
  if (value === "line-end" || value === "hidden") return value;
  return "after-target";
}
