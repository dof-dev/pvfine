import { computed, ref, watch } from "vue";
import { defineStore } from "pinia";
import { Events } from "@wailsio/runtime";
import { ArchiveService, FileSetService } from "../../bindings/pvfine/services";
import type {
  FileSetDocument,
  StoredFileSet,
  StoredFileSetEntry,
  TreeNode,
} from "../../bindings/pvfine/services/models";

export interface FileSetEntry {
  fileIndex: number; // -1 = 当前归档中不存在或尚未解析
  path: string;
  name: string;
  ids: string[];
  size: number;
  dataType: number;
}

export interface FileSet {
  id: string;
  name: string;
  entries: FileSetEntry[];
}

const fileSetDocumentVersion = 1;

/** 文件集状态:路径跨 PVF 持久化,文件索引按当前归档动态解析。 */
export const useFileSetStore = defineStore("fileSets", () => {
  const fileSets = ref<FileSet[]>([createFileSet("default", "默认文件集")]);
  const activeSetId = ref("default");
  const visible = ref(true);
  const sessionId = ref(0);
  const loaded = ref(false);
  const resolving = ref(false);
  const saving = ref(false);
  const dirty = ref(false);
  const loadError = ref("");
  let nextSetId = 1;
  let nextSetNumber = 2;
  let mutationVersion = 0;
  let loadPromise: Promise<void> | null = null;
  let archivePath = "";
  let resolveRequest = 0;

  const activeSet = computed(() =>
    fileSets.value.find((fileSet) => fileSet.id === activeSetId.value) ?? fileSets.value[0]
  );

  function createFileSet(id: string, name: string, entries: FileSetEntry[] = []): FileSet {
    return { id, name, entries };
  }

  function markDirty(): void {
    mutationVersion++;
    dirty.value = true;
  }

  function assertName(name: string, currentId?: string): string {
    const normalized = name.trim();
    if (!normalized) throw new Error("文件集名称不能为空");
    if (
      fileSets.value.some(
        (fileSet) => fileSet.id !== currentId && fileSet.name === normalized
      )
    ) {
      throw new Error("文件集名称不能重复");
    }
    return normalized;
  }

  function createSet(requestedName?: string): FileSet {
    let name = requestedName?.trim();
    if (!name) {
      name = `文件集 ${nextSetNumber++}`;
      while (fileSets.value.some((fileSet) => fileSet.name === name)) {
        name = `文件集 ${nextSetNumber++}`;
      }
    }
    name = assertName(name);
    const fileSet = createFileSet(`set-${nextSetId++}`, name);
    fileSets.value.push(fileSet);
    activeSetId.value = fileSet.id;
    markDirty();
    return fileSet;
  }

  function renameSet(id: string, name: string): void {
    const fileSet = fileSets.value.find((item) => item.id === id);
    if (!fileSet) return;
    const normalized = assertName(name, id);
    if (fileSet.name === normalized) return;
    fileSet.name = normalized;
    markDirty();
  }

  function deleteSet(id: string): boolean {
    const index = fileSets.value.findIndex((fileSet) => fileSet.id === id);
    if (index < 0) return false;
    fileSets.value.splice(index, 1);
    if (activeSetId.value === id) {
      const next = fileSets.value[index] ?? fileSets.value[index - 1];
      activeSetId.value = next?.id ?? "";
    }
    markDirty();
    return true;
  }

  function normalizePath(path: string): string {
    return path.replaceAll("\\", "/").replace(/^\/+|\/+$/g, "");
  }

  function addEntries(entries: FileSetEntry[]): { added: number; skipped: number } {
    let target = activeSet.value;
    if (!target) {
      const id = fileSets.value.length === 0 ? "default" : `set-${nextSetId++}`;
      const name = fileSets.value.length === 0 ? "默认文件集" : `文件集 ${nextSetNumber++}`;
      target = createFileSet(id, name);
      fileSets.value.push(target);
      activeSetId.value = target.id;
    }

    const existing = new Map(target.entries.map((entry) => [normalizePath(entry.path), entry]));
    let added = 0;
    let skipped = 0;
    let changed = false;
    for (const entry of entries) {
      const path = normalizePath(entry.path);
      if (entry.fileIndex < 0 || !path) {
        skipped++;
        continue;
      }
      const existingEntry = existing.get(path);
      if (existingEntry) {
        existingEntry.fileIndex = entry.fileIndex;
        existingEntry.name = entry.name;
        existingEntry.size = entry.size;
        existingEntry.dataType = entry.dataType;
        existingEntry.ids = [...new Set(entry.ids ?? [])];
        skipped++;
        changed = true;
        continue;
      }
      const nextEntry = {
        ...entry,
        path,
        ids: [...new Set(entry.ids ?? [])],
      };
      target.entries.push(nextEntry);
      existing.set(path, nextEntry);
      added++;
      changed = true;
    }
    if (changed) markDirty();
    return { added, skipped };
  }

  function removeEntry(path: string): void {
    const target = activeSet.value;
    if (!target) return;
    const normalizedPath = normalizePath(path);
    const index = target.entries.findIndex(
      (entry) => normalizePath(entry.path) === normalizedPath
    );
    if (index >= 0) {
      target.entries.splice(index, 1);
      markDirty();
    }
  }

  function clearActive(): number {
    const target = activeSet.value;
    if (!target) return 0;
    const count = target.entries.length;
    if (count > 0) {
      target.entries.splice(0, count);
      markDirty();
    }
    return count;
  }

  watch(activeSetId, (next, previous) => {
    if (loaded.value && next !== previous) markDirty();
  });

  async function load(): Promise<void> {
    if (loaded.value) return;
    if (loadPromise) return loadPromise;
    loadPromise = (async () => {
      try {
        const document = await FileSetService.LoadFileSets();
        applyDocument(document);
        loadError.value = "";
      } catch (error: any) {
        loadError.value = String(error?.message ?? error);
        console.error("load file sets failed", error);
        resetInMemory();
      } finally {
        loaded.value = true;
        loadPromise = null;
      }
    })();
    return loadPromise;
  }

  async function save(): Promise<void> {
    if (saving.value) return;
    await load();
    const version = mutationVersion;
    saving.value = true;
    try {
      await FileSetService.SaveFileSets(toDocument());
      if (version === mutationVersion) dirty.value = false;
    } finally {
      saving.value = false;
    }
  }

  function applyDocument(document: FileSetDocument): void {
    const storedSets = (document.fileSets ?? []).filter(
      (fileSet): fileSet is StoredFileSet => !!fileSet
    );
    const nextSets = storedSets
      .map(fromStoredFileSet)
      .filter((fileSet): fileSet is FileSet => !!fileSet);

    const persistedDefault = nextSets.find((fileSet) => fileSet.id === "default");
    if (persistedDefault) {
      if (persistedDefault.entries.length === 0 && persistedDefault.name === "默认文件集") {
        nextSets.splice(nextSets.indexOf(persistedDefault), 1);
      } else {
        persistedDefault.id = nextSetID(nextSets);
        if (persistedDefault.name === "默认文件集") {
          persistedDefault.name = uniqueSetName("默认文件集（已保存）", nextSets);
        }
      }
    }
    fileSets.value = [createFileSet("default", "默认文件集"), ...nextSets];
    activeSetId.value = "default";
    recomputeCounters();
    dirty.value = false;
    mutationVersion++;
  }

  function resetInMemory(): void {
    fileSets.value = [createFileSet("default", "默认文件集")];
    activeSetId.value = "default";
    recomputeCounters();
    dirty.value = false;
    mutationVersion++;
  }

  function toDocument(): FileSetDocument {
    return {
      version: fileSetDocumentVersion,
      activeSetId: activeSetId.value,
      fileSets: fileSets.value.map((fileSet) => ({
        id: fileSet.id,
        name: fileSet.name,
        entries: fileSet.entries.map((entry) => ({
          path: normalizePath(entry.path),
          name: entry.name,
          ids: [...new Set(entry.ids ?? [])],
          size: entry.size,
          dataType: entry.dataType,
        })),
      })),
    };
  }

  function fromStoredFileSet(fileSet: StoredFileSet): FileSet | null {
    const id = String(fileSet.id ?? "").trim();
    const name = String(fileSet.name ?? "").trim();
    if (!id || !name) return null;
    const entries = (fileSet.entries ?? [])
      .filter((entry): entry is StoredFileSetEntry => !!entry)
      .map((entry) => {
        const path = normalizePath(String(entry.path ?? ""));
        return {
          fileIndex: -1,
          path,
          name: String(entry.name ?? "") || path.split("/").pop() || path,
          ids: [...new Set(entry.ids ?? [])],
          size: Number(entry.size ?? 0),
          dataType: Number(entry.dataType ?? 0),
        };
      })
      .filter((entry) => !!entry.path);
    return createFileSet(id, name, dedupeEntries(entries));
  }

  function dedupeEntries(entries: FileSetEntry[]): FileSetEntry[] {
    const seen = new Set<string>();
    return entries.filter((entry) => {
      if (seen.has(entry.path)) return false;
      seen.add(entry.path);
      return true;
    });
  }

  function recomputeCounters(): void {
    let maxID = 0;
    let maxNumber = 1;
    for (const fileSet of fileSets.value) {
      const idMatch = /^set-(\d+)$/.exec(fileSet.id);
      if (idMatch) maxID = Math.max(maxID, Number(idMatch[1]));
      const nameMatch = /^文件集 (\d+)$/.exec(fileSet.name);
      if (nameMatch) maxNumber = Math.max(maxNumber, Number(nameMatch[1]));
    }
    nextSetId = maxID + 1;
    nextSetNumber = Math.max(2, maxNumber + 1);
  }

  function nextSetID(sets: FileSet[]): string {
    let number = 1;
    const ids = new Set(sets.map((fileSet) => fileSet.id));
    while (ids.has(`set-${number}`)) number++;
    return `set-${number}`;
  }

  function uniqueSetName(base: string, sets: FileSet[]): string {
    const names = new Set(sets.map((fileSet) => fileSet.name));
    if (!names.has(base)) return base;
    let number = 2;
    let name = `${base} ${number}`;
    while (names.has(name)) name = `${base} ${++number}`;
    return name;
  }

  async function onArchiveOpened(event: any): Promise<void> {
    const path = String(event?.data?.path ?? event?.path ?? "");
    archivePath = path;
    sessionId.value++;
    const request = ++resolveRequest;
    await load();
    if (request !== resolveRequest || archivePath !== path) return;
    await resolveEntries(path, request);
  }

  function onArchiveClosed(): void {
    archivePath = "";
    resolveRequest++;
    resolving.value = false;
    sessionId.value++;
    for (const fileSet of fileSets.value) {
      for (const entry of fileSet.entries) entry.fileIndex = -1;
    }
  }

  async function resolveEntries(path: string, request = resolveRequest): Promise<void> {
    if (!path || request !== resolveRequest) return;
    const entries = fileSets.value.flatMap((fileSet) => fileSet.entries);
    const paths = [...new Set(entries.map((entry) => normalizePath(entry.path)).filter(Boolean))];
    if (paths.length === 0) return;
    resolving.value = true;
    let nodes: (TreeNode | null)[] | null;
    try {
      nodes = await ArchiveService.ResolveFiles(paths);
    } catch (error) {
      console.error("resolve file set entries failed", error);
      if (request === resolveRequest) resolving.value = false;
      return;
    }
    if (request !== resolveRequest || archivePath !== path) return;
    const byPath = new Map(
      (nodes ?? [])
        .filter((node): node is TreeNode => !!node && !node.isDir && node.fileIndex >= 0)
        .map((node) => [normalizePath(node.path), node])
    );
    for (const entry of entries) {
      const node = byPath.get(normalizePath(entry.path));
      entry.fileIndex = node?.fileIndex ?? -1;
      if (node) {
        entry.size = node.size;
        entry.dataType = node.dataType;
        const ids = [...new Set((node.tags ?? []).map((tag) => tag.id).filter(Boolean))];
        const names = [
          ...new Set((node.tags ?? []).map((tag) => tag.name.trim()).filter(Boolean)),
        ];
        entry.ids = ids;
        entry.name = names.join(" / ") || node.name || entry.path.split("/").pop() || entry.path;
      }
    }
    if (request === resolveRequest) resolving.value = false;
  }

  Events.On("archive:opened", (event: any) => {
    void onArchiveOpened(event);
  });
  Events.On("archive:closed", onArchiveClosed);

  return {
    fileSets,
    activeSetId,
    activeSet,
    visible,
    sessionId,
    loaded,
    resolving,
    saving,
    dirty,
    loadError,
    load,
    save,
    createSet,
    renameSet,
    deleteSet,
    addEntries,
    removeEntry,
    clearActive,
  };
});
