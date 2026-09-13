import { computed, ref, watch } from "vue";
import { defineStore } from "pinia";
import { Events } from "@wailsio/runtime";
import { ArchiveService, FileSetService } from "../../bindings/pvfine/services";
import type {
  FileSetDocument,
  StoredFileSet,
  StoredFileSetEntry,
  TreeNode,
  ImageReference,
} from "../../bindings/pvfine/services/models";

export interface FileSetEntry {
  fileIndex: number; // -1 = 当前归档中不存在或尚未解析
  path: string;
  name: string;
  ids: string[];
  size: number;
  dataType: number;
  icon: ImageReference | null;
  fieldImage: ImageReference | null;
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
        existingEntry.icon = entry.icon ?? null;
        existingEntry.fieldImage = entry.fieldImage ?? null;
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

  /**
   * 脚本应用文件集变更后重新读取磁盘内容。本地有未保存改动时不覆盖，
   * 改为提示冲突：否则脚本的写入会被下一次保存反向覆盖，或直接丢掉用户的编辑。
   */
  async function reload(): Promise<void> {
    if (!loaded.value) {
      await load();
      return;
    }
    if (dirty.value) {
      loadError.value = "脚本已修改文件集，但本地有未保存的改动，未自动刷新，请先保存或放弃";
      return;
    }
    try {
      const document = await FileSetService.LoadFileSets();
      applyDocument(document, true);
      loadError.value = "";
    } catch (error: any) {
      loadError.value = String(error?.message ?? error);
      console.error("reload file sets failed", error);
    }
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

  /**
   * 用磁盘文档替换内存状态。keepActive 为 true 时（脚本写入后的重载）保留
   * 用户当前选中的文件集，避免刷新把视图切回默认集。
   *
   * 保留 id "default" 的记录就是内置「默认文件集」本身：名字和内容都以磁盘
   * 为准，不再改名成「默认文件集（已保存）」。这样侧边栏显示的名字和
   * 脚本 pvf.fileset(name) 取到的名字始终是同一个。
   */
  function applyDocument(document: FileSetDocument, keepActive = false): void {
    const previousActive = activeSetId.value;
    const storedSets = (document.fileSets ?? []).filter(
      (fileSet): fileSet is StoredFileSet => !!fileSet
    );
    const nextSets = storedSets
      .map(fromStoredFileSet)
      .filter((fileSet): fileSet is FileSet => !!fileSet);

    let defaultSet = createFileSet("default", "默认文件集");
    const persistedDefault = nextSets.find((fileSet) => fileSet.id === "default");
    if (persistedDefault) {
      nextSets.splice(nextSets.indexOf(persistedDefault), 1);
      // 名为「默认文件集」的空记录等同于未保存的内置默认集，不必单独保留。
      if (persistedDefault.entries.length > 0 || persistedDefault.name !== "默认文件集") {
        defaultSet = persistedDefault;
      }
    }
    fileSets.value = [defaultSet, ...nextSets];
    const survived =
      keepActive && fileSets.value.some((fileSet) => fileSet.id === previousActive);
    activeSetId.value = survived ? previousActive : "default";
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
    const persistedSets = fileSets.value.filter(
      (fileSet) =>
        fileSet.id !== "default" ||
        fileSet.name !== "默认文件集" ||
        fileSet.entries.length > 0
    );
    return {
      version: fileSetDocumentVersion,
      activeSetId: persistedSets.some((fileSet) => fileSet.id === activeSetId.value)
        ? activeSetId.value
        : "",
      fileSets: persistedSets.map((fileSet) => ({
        id: fileSet.id,
        name: fileSet.name,
        entries: fileSet.entries.map((entry) => ({
          path: normalizePath(entry.path),
          name: entry.name,
          ids: [...new Set(entry.ids ?? [])],
          icon: entry.icon ?? null,
          fieldImage: entry.fieldImage ?? null,
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
          icon: null,
          fieldImage: null,
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

  async function onArchiveOpened(event: any): Promise<void> {
    const path = String(event?.data?.path ?? event?.path ?? "");
    archivePath = path;
    sessionId.value++;
    const request = ++resolveRequest;
    for (const fileSet of fileSets.value) {
      for (const entry of fileSet.entries) {
        entry.fileIndex = -1;
        entry.icon = null;
        entry.fieldImage = null;
      }
    }
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
      for (const entry of fileSet.entries) {
        entry.fileIndex = -1;
        entry.icon = null;
        entry.fieldImage = null;
      }
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
        entry.icon = node.icon ?? null;
        entry.fieldImage = node.fieldImage ?? null;
      } else {
        entry.icon = null;
        entry.fieldImage = null;
      }
    }
    if (request === resolveRequest) resolving.value = false;
  }

  Events.On("archive:opened", (event: any) => {
    void onArchiveOpened(event);
  });
  Events.On("archive:changed", (event: any) => {
    const path = String(event?.data?.path ?? event?.path ?? "");
    if (path && path === archivePath) void resolveEntries(path);
  });
  Events.On("archive:index-ready", () => {
    if (archivePath) void resolveEntries(archivePath);
  });
  Events.On("archive:index-updated", () => {
    if (archivePath) void resolveEntries(archivePath);
  });
  Events.On("archive:closed", onArchiveClosed);
  // 脚本应用文件集变更后 file-sets.json 已改写，这份内存副本必须重载；
  // 否则用户看到的仍是旧内容，下次保存还会把脚本的改动覆盖回去。
  Events.On("fileset:changed", () => {
    void reload();
  });

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
    reload,
    save,
    createSet,
    renameSet,
    deleteSet,
    addEntries,
    removeEntry,
    clearActive,
  };
});
