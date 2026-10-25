import { defineStore } from "pinia";
import { ref } from "vue";
import { ScriptTagTranslationService } from "../../bindings/pvfine/services";

export interface ScriptTagComment {
  section: string;
  comment: string;
}

/**
 * Holds the section -> comment map parsed from the read-only dcn86.xml
 * translation file. The backend decodes XML entities (such as &#10; into
 * newlines) and keeps ``` code fences verbatim, so the frontend can render
 * the comment directly.
 */
export const useScriptTagTranslationsStore = defineStore("scriptTagTranslations", () => {
  const comments = ref<Map<string, string>>(new Map());
  const loaded = ref(false);
  const loading = ref(false);
  const error = ref("");

  async function ensureLoaded(force = false): Promise<void> {
    if (loaded.value || loading.value) {
      if (force && loaded.value) {
        loaded.value = false;
      } else {
        return;
      }
    }
    loading.value = true;
    try {
      const items = await ScriptTagTranslationService.GetScriptTagTranslations();
      const map = new Map<string, string>();
      for (const item of items ?? []) {
        if (!item.section) continue;
        map.set(item.section, item.comment ?? "");
      }
      comments.value = map;
      loaded.value = true;
      error.value = "";
    } catch (err) {
      error.value = err instanceof Error ? err.message : String(err);
      console.error("load script tag translations failed", err);
    } finally {
      loading.value = false;
    }
  }

  function commentFor(section: string): string | undefined {
    return comments.value.get(section);
  }

  return { comments, loaded, loading, error, ensureLoaded, commentFor };
});
