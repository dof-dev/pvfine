import { computed, ref } from "vue";
import { defineStore } from "pinia";
import { Events } from "@wailsio/runtime";

export interface FileSetEntry {
  fileIndex: number;
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

/** 临时文件集状态:只存在于当前归档会话内。 */
export const useFileSetStore = defineStore("fileSets", () => {
  const fileSets = ref<FileSet[]>([createFileSet("default", "默认文件集")]);
  const activeSetId = ref("default");
  const visible = ref(true);
  const sessionId = ref(0);
  let nextSetId = 1;
  let nextSetNumber = 2;

  const activeSet = computed(() =>
    fileSets.value.find((fileSet) => fileSet.id === activeSetId.value) ?? fileSets.value[0]
  );

  function createFileSet(id: string, name: string): FileSet {
    return { id, name, entries: [] };
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
    return fileSet;
  }

  function renameSet(id: string, name: string): void {
    const fileSet = fileSets.value.find((item) => item.id === id);
    if (!fileSet) return;
    fileSet.name = assertName(name, id);
  }

  function deleteSet(id: string): boolean {
    if (fileSets.value.length <= 1) return false;
    const index = fileSets.value.findIndex((fileSet) => fileSet.id === id);
    if (index < 0) return false;
    fileSets.value.splice(index, 1);
    if (activeSetId.value === id) {
      activeSetId.value = fileSets.value[Math.min(index, fileSets.value.length - 1)].id;
    }
    return true;
  }

  function normalizePath(path: string): string {
    return path.replaceAll("\\", "/").replace(/^\/+|\/+$/g, "");
  }

  function addEntries(entries: FileSetEntry[]): { added: number; skipped: number } {
    const target = activeSet.value;
    if (!target) return { added: 0, skipped: entries.length };

    const existing = new Map(target.entries.map((entry) => [normalizePath(entry.path), entry]));
    let added = 0;
    let skipped = 0;
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
    }
    return { added, skipped };
  }

  function removeEntry(path: string): void {
    const target = activeSet.value;
    if (!target) return;
    const normalizedPath = normalizePath(path);
    const index = target.entries.findIndex(
      (entry) => normalizePath(entry.path) === normalizedPath
    );
    if (index >= 0) target.entries.splice(index, 1);
  }

  function clearActive(): number {
    const target = activeSet.value;
    if (!target) return 0;
    const count = target.entries.length;
    target.entries.splice(0, target.entries.length);
    return count;
  }

  function resetForArchive(): void {
    sessionId.value++;
    fileSets.value = [createFileSet("default", "默认文件集")];
    activeSetId.value = "default";
    nextSetId = 1;
    nextSetNumber = 2;
  }

  Events.On("archive:opened", resetForArchive);
  Events.On("archive:closed", resetForArchive);

  return {
    fileSets,
    activeSetId,
    activeSet,
    visible,
    sessionId,
    createSet,
    renameSet,
    deleteSet,
    addEntries,
    removeEntry,
    clearActive,
    resetForArchive,
  };
});
