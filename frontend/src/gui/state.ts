import { reactive, ref, shallowRef } from "vue";
import type { ShopDocument } from "../../bindings/pvfine/services/models";
import type { GUIFile } from "./types";

/** Owned by a pane, so displaying the same file in two panes is independent. */
export function createGUIModes() {
  const entries = reactive(new Map<number, { mode: "text" | "gui"; opened: boolean }>());
  function set(index: number, mode: "text" | "gui") {
    const previous = entries.get(index);
    entries.set(index, { mode, opened: mode === "gui" || !!previous?.opened });
  }
  function prune(indexes: number[]) {
    const remaining = new Set(indexes);
    for (const key of entries.keys()) if (!remaining.has(key)) entries.delete(key);
  }
  return { entries, set, prune, reset: () => entries.clear() };
}

/** One request generation spans identity changes, retries and unmounts. */
export function createShopSession(read: (index: number, text: string) => Promise<ShopDocument | null>) {
  const document = shallowRef<ShopDocument | null>(null);
  const loading = ref(false);
  const error = ref("");
  const tabIndex = ref(0);
  const categoryID = ref("");
  let generation = 0;
  let identity = "";

  function invalidate(clear = false) {
    generation++;
    loading.value = false;
    if (clear) {
      identity = "";
      document.value = null;
      error.value = "";
      tabIndex.value = 0;
      categoryID.value = "";
    }
  }

  async function load(file: GUIFile, archiveIdentity: string) {
    const nextIdentity = `${archiveIdentity}\u0000${file.index}\u0000${file.path}`;
    if (identity !== nextIdentity) {
      invalidate(true);
      identity = nextIdentity;
    }
    const request = ++generation;
    loading.value = true;
    error.value = "";
    try {
      const next = await read(file.index, file.text);
      if (request !== generation) return;
      if (!next) throw new Error("未收到商店数据");
      document.value = next;
      if (!next.tabs?.[tabIndex.value]) tabIndex.value = 0;
      if (!next.categories?.some((category) => category.id === categoryID.value)) {
        categoryID.value = next.categories?.[0]?.id ?? "";
      }
    } catch (cause) {
      if (request === generation) error.value = cause instanceof Error ? cause.message : String(cause);
    } finally {
      if (request === generation) loading.value = false;
    }
  }
  return { document, loading, error, tabIndex, categoryID, load, invalidate };
}
