import { computed, reactive, ref, shallowRef } from "vue";
import type { ShopDocument, WorldDropDocument, WorldDropLevel } from "../../bindings/pvfine/services/models";
import type { GUIFile } from "./types";
import { areLevelsEqual, toEditableLevels, toWorldDropLevels, type EditableWorldDropLevel } from "./worldDropForm";

export type GUIGuard = () => boolean | Promise<boolean>;

/** Owned by a pane, so displaying the same file in two panes is independent. */
export function createGUIModes() {
  const entries = reactive(new Map<number, { mode: "text" | "gui"; opened: boolean }>());
  const guards = new Map<number, GUIGuard>();

  function registerGuard(index: number, guard: GUIGuard) {
    guards.set(index, guard);
    return () => {
      if (guards.get(index) === guard) {
        guards.delete(index);
      }
    };
  }

  function set(index: number, mode: "text" | "gui") {
    const previous = entries.get(index);
    entries.set(index, { mode, opened: mode === "gui" || !!previous?.opened });
  }

  async function requestMode(index: number, mode: "text" | "gui"): Promise<boolean> {
    const current = entries.get(index)?.mode ?? "text";
    if (current === "gui" && mode === "text") {
      const guard = guards.get(index);
      if (guard) {
        const allowed = await guard();
        if (!allowed) return false;
      }
    }
    set(index, mode);
    return true;
  }

  function prune(indexes: number[]) {
    const remaining = new Set(indexes);
    for (const key of entries.keys()) if (!remaining.has(key)) entries.delete(key);
    for (const key of guards.keys()) if (!remaining.has(key)) guards.delete(key);
  }

  return {
    entries,
    set,
    requestMode,
    registerGuard,
    prune,
    reset: () => {
      entries.clear();
      guards.clear();
    },
  };
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

/** World Drop session manages document loading, selection and generation safety. */
export function createWorldDropSession(
  read: (index: number, text: string) => Promise<WorldDropDocument | null>,
) {
  const document = shallowRef<WorldDropDocument | null>(null);
  const loading = ref(false);
  const error = ref("");
  const selectedLevel = ref<number | null>(null);
  let generation = 0;
  let identity = "";

  function invalidate(clear = false) {
    generation++;
    loading.value = false;
    if (clear) {
      identity = "";
      document.value = null;
      error.value = "";
      selectedLevel.value = null;
    }
  }

  async function load(file: GUIFile, archiveIdentity: string): Promise<boolean> {
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
      if (request !== generation) return false;
      if (!next) throw new Error("未收到全局掉率数据");
      document.value = next;
      const levels = next.levels ?? [];
      if (selectedLevel.value === null || !levels.some((l) => l.level === selectedLevel.value)) {
        selectedLevel.value = levels[0]?.level ?? null;
      }
      return true;
    } catch (cause) {
      if (request === generation) error.value = cause instanceof Error ? cause.message : String(cause);
      return false;
    } finally {
      if (request === generation) loading.value = false;
    }
  }

  return { document, loading, error, selectedLevel, load, invalidate };
}

/** Keep the editor baseline stable while a newer document is being read. */
export function createWorldDropDraftState() {
  const editableLevels = ref<EditableWorldDropLevel[]>([]);
  const baselineLevels = shallowRef<WorldDropLevel[] | null>(null);
  const isDirty = computed(() =>
    baselineLevels.value !== null && !areLevelsEqual(toWorldDropLevels(editableLevels.value), baselineLevels.value),
  );

  function sync(document: WorldDropDocument | null): void {
    baselineLevels.value = document?.levels ?? null;
    editableLevels.value = toEditableLevels(document?.levels ?? null);
  }

  return { editableLevels, isDirty, sync };
}
