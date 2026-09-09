import { computed, ref } from "vue";
import { defineStore } from "pinia";
import { ArchiveService, BookmarkService } from "../../bindings/pvfine/services";
import type {
  BookmarkBook,
  BookmarkBookFile,
  BookmarkDocument,
  BookmarkEntry as StoredBookmarkEntry,
  BookmarkGroup as StoredBookmarkGroup,
  TreeNode,
} from "../../bindings/pvfine/services/models";
import { Events } from "@wailsio/runtime";

export const builtinBookmarkBookID = "builtin";

export interface BookmarkEntry {
  path: string;
  name: string;
  fileIndex: number;
}

export interface BookmarkGroup {
  id: string;
  name: string;
  groups: BookmarkGroup[];
  entries: BookmarkEntry[];
}

export interface BookmarkBookState {
  id: string;
  name: string;
  builtin: boolean;
  editable: boolean;
  groups: BookmarkGroup[];
  entries: BookmarkEntry[];
}

export interface BookmarkInput {
  path: string;
  name?: string;
  fileIndex?: number;
}

const bookmarkDocumentVersion = 1;

/** 书签簿状态：路径跨 PVF 持久化，fileIndex 按当前归档动态解析。 */
export const useBookmarkStore = defineStore("bookmarks", () => {
  const books = ref<BookmarkBookState[]>([]);
  const activeBookId = ref(builtinBookmarkBookID);
  const selectedGroupId = ref("");
  const loaded = ref(false);
  const resolving = ref(false);
  const saving = ref(false);
  const saveError = ref("");
  const loadError = ref("");
  const sessionId = ref(0);

  let loadPromise: Promise<void> | null = null;
  let saveChain: Promise<void> = Promise.resolve();
  let mutationVersion = 0;
  let savedVersion = 0;
  let archivePath = "";
  let resolveRequest = 0;
  let bookSequence = 1;
  let groupSequence = 1;

  const activeBook = computed(
    () => books.value.find((book) => book.id === activeBookId.value) ?? books.value[0] ?? null
  );
  const activeGroup = computed(() =>
    activeBook.value ? findGroup(activeBook.value.groups, selectedGroupId.value) : null
  );
  const activeGroupId = computed(() => activeGroup.value?.id ?? "");
  const activeBookEditable = computed(() => !!activeBook.value?.editable);
  const dirty = computed(() => mutationVersion !== savedVersion);
  const hasCustomBooks = computed(() => books.value.some((book) => !book.builtin));

  function normalizePath(path: string): string {
    return path.replaceAll("\\", "/").replace(/^\/+|\/+$/g, "");
  }

  function basename(path: string): string {
    const normalized = normalizePath(path);
    return normalized.slice(normalized.lastIndexOf("/") + 1) || normalized;
  }

  function newBookID(): string {
    const ids = new Set(books.value.map((book) => book.id));
    let id = "";
    do {
      id = `bookmark-${bookSequence++}`;
    } while (ids.has(id));
    return id;
  }

  function newGroupID(reservedIDs = new Set<string>()): string {
    const ids = new Set(reservedIDs);
    for (const book of books.value) collectGroupIDs(book.groups, ids);
    let id = "";
    do {
      id = `bookmark-group-${groupSequence++}`;
    } while (ids.has(id));
    return id;
  }

  function collectGroupIDs(groups: BookmarkGroup[], result: Set<string>): void {
    for (const group of groups) {
      result.add(group.id);
      collectGroupIDs(group.groups, result);
    }
  }

  function uniqueBookName(base: string): string {
    const normalized = base.trim() || "书签簿";
    const names = new Set(books.value.map((book) => book.name));
    if (!names.has(normalized)) return normalized;
    let number = 2;
    let next = `${normalized} ${number}`;
    while (names.has(next)) next = `${normalized} ${++number}`;
    return next;
  }

  function uniqueGroupName(parent: BookmarkGroup | null, base: string, ignoreID = ""): string {
    const normalized = base.trim() || "分组";
    const groups = parent?.groups ?? activeBook.value?.groups ?? [];
    const names = new Set(groups.filter((group) => group.id !== ignoreID).map((group) => group.name));
    if (!names.has(normalized)) return normalized;
    let number = 2;
    let next = `${normalized} ${number}`;
    while (names.has(next)) next = `${normalized} ${++number}`;
    return next;
  }

  function createBook(requestedName?: string): BookmarkBookState {
    assertLoaded();
    const name = uniqueBookName(requestedName?.trim() || `书签簿 ${bookSequence}`);
    const book: BookmarkBookState = {
      id: newBookID(),
      name,
      builtin: false,
      editable: true,
      groups: [],
      entries: [],
    };
    books.value.push(book);
    activeBookId.value = book.id;
    selectedGroupId.value = "";
    markDirty();
    void persist();
    return book;
  }

  function renameBook(id: string, requestedName: string): void {
    assertLoaded();
    const book = getBook(id);
    if (!book || !book.editable) throw new Error("当前书签簿不可编辑");
    const name = requestedName.trim();
    if (!name) throw new Error("书签簿名称不能为空");
    if (books.value.some((item) => item.id !== id && item.name === name)) {
      throw new Error("书签簿名称不能重复");
    }
    if (book.name === name) return;
    book.name = name;
    markDirty();
    void persist();
  }

  function deleteBook(id: string): boolean {
    assertLoaded();
    const book = getBook(id);
    if (!book || !book.editable || book.builtin) return false;
    const index = books.value.findIndex((item) => item.id === id);
    if (index < 0) return false;
    books.value.splice(index, 1);
    if (activeBookId.value === id) {
      activeBookId.value = books.value[index]?.id ?? books.value[index - 1]?.id ?? builtinBookmarkBookID;
      selectedGroupId.value = "";
    }
    markDirty();
    void persist();
    return true;
  }

  function copyBook(sourceID: string): BookmarkBookState | null {
    assertLoaded();
    const source = getBook(sourceID);
    if (!source) return null;
    const previousGroupID = selectedGroupId.value;
    const copiedGroupIDs = new Map<string, string>();
    const reservedGroupIDs = new Set<string>();
    const copy: BookmarkBookState = {
      id: newBookID(),
      name: uniqueBookName(`${source.name}（副本）`),
      builtin: false,
      editable: true,
      groups: cloneGroupsWithNewIDs(source.groups, reservedGroupIDs, copiedGroupIDs),
      entries: cloneEntries(source.entries),
    };
    books.value.push(copy);
    activeBookId.value = copy.id;
    selectedGroupId.value = copiedGroupIDs.get(previousGroupID) ?? "";
    markDirty();
    void persist();
    return copy;
  }

  function createGroup(parentID = "", requestedName?: string): BookmarkGroup {
    assertLoaded();
    const book = activeBook.value;
    assertEditable(book);
    const parent = parentID ? findGroup(book.groups, parentID) : null;
    if (parentID && !parent) throw new Error("目标分组不存在");
    const group: BookmarkGroup = {
      id: newGroupID(),
      name: uniqueGroupName(parent, requestedName?.trim() || `分组 ${Math.max(1, groupSequence - 1)}`),
      groups: [],
      entries: [],
    };
    (parent?.groups ?? book.groups).push(group);
    selectedGroupId.value = group.id;
    markDirty();
    void persist();
    return group;
  }

  function renameGroup(id: string, requestedName: string): void {
    assertLoaded();
    const book = activeBook.value;
    assertEditable(book);
    const located = findGroupWithParent(book?.groups ?? [], id);
    if (!located) throw new Error("分组不存在");
    const name = requestedName.trim();
    if (!name) throw new Error("分组名称不能为空");
    const unique = uniqueGroupName(located.parent, name, id);
    if (unique !== name) throw new Error("同级分组名称不能重复");
    located.group.name = name;
    markDirty();
    void persist();
  }

  function deleteGroup(id: string): boolean {
    assertLoaded();
    const book = activeBook.value;
    assertEditable(book);
    if (!book) return false;
    const located = findGroupWithParent(book.groups, id);
    if (!located) return false;
    const destinationGroups = located.parent?.groups ?? book.groups;
    const index = destinationGroups.findIndex((group) => group.id === id);
    if (index < 0) return false;
    const destination = located.parent?.entries ?? book.entries;
    appendUniqueEntries(destination, located.group.entries);
    const existingGroupNames = new Set(
      destinationGroups.filter((group) => group.id !== id).map((group) => group.name)
    );
    const promotedGroups = located.group.groups.map((child) => {
      child.name = uniqueNameFromSet(existingGroupNames, child.name);
      existingGroupNames.add(child.name);
      return child;
    });
    destinationGroups.splice(index, 1, ...promotedGroups);
    if (selectedGroupId.value === id || isDescendantGroup(located.group, selectedGroupId.value)) {
      selectedGroupId.value = located.parent?.id ?? "";
    }
    markDirty();
    void persist();
    return true;
  }

  function moveEntry(path: string, targetGroupID = "", sourceGroupID = ""): boolean {
    assertLoaded();
    const book = activeBook.value;
    assertEditable(book);
    if (!book) return false;
    const normalized = normalizePath(path);
    const target = targetGroupID ? findGroup(book.groups, targetGroupID) : null;
    if (targetGroupID && !target) throw new Error("目标分组不存在");
    const source = sourceGroupID ? findGroup(book.groups, sourceGroupID) : null;
    if (sourceGroupID && !source) throw new Error("来源分组不存在");
    const sourceEntries = source?.entries ?? book.entries;
    const index = sourceEntries.findIndex((entry) => entry.path === normalized);
    if (index < 0) return false;
    const [entry] = sourceEntries.splice(index, 1);
    const destination = target?.entries ?? book.entries;
    if (!destination.some((item) => item.path === normalized)) destination.push(entry);
    markDirty();
    void persist();
    return true;
  }

  function addEntries(entries: BookmarkInput[], targetGroupID = selectedGroupId.value): { added: number; skipped: number } {
    assertLoaded();
    const book = activeBook.value;
    assertEditable(book);
    if (!book) return { added: 0, skipped: entries.length };
    const target = targetGroupID ? findGroup(book.groups, targetGroupID) : null;
    if (targetGroupID && !target) throw new Error("目标分组不存在");
    const destination = target?.entries ?? book.entries;
    const existing = new Set(destination.map((entry) => entry.path));
    let added = 0;
    let skipped = 0;
    for (const item of entries) {
      const path = normalizePath(item.path);
      if (!path || existing.has(path)) {
        skipped++;
        continue;
      }
      destination.push({
        path,
        name: item.name?.trim() || basename(path),
        fileIndex: item.fileIndex ?? -1,
      });
      existing.add(path);
      added++;
    }
    if (added > 0) {
      markDirty();
      void persist();
    }
    return { added, skipped };
  }

  function removeEntry(path: string, groupID = ""): boolean {
    assertLoaded();
    const book = activeBook.value;
    assertEditable(book);
    if (!book) return false;
    const group = groupID ? findGroup(book.groups, groupID) : null;
    if (groupID && !group) throw new Error("来源分组不存在");
    const entries = group?.entries ?? book.entries;
    const index = entries.findIndex((entry) => entry.path === normalizePath(path));
    if (index < 0) return false;
    entries.splice(index, 1);
    markDirty();
    void persist();
    return true;
  }

  function renameEntry(path: string, name: string, groupID = ""): void {
    assertLoaded();
    const book = activeBook.value;
    assertEditable(book);
    if (!book) return;
    const group = groupID ? findGroup(book.groups, groupID) : null;
    if (groupID && !group) throw new Error("来源分组不存在");
    const entry = (group?.entries ?? book.entries).find((item) => item.path === normalizePath(path));
    if (!entry) throw new Error("书签不存在");
    const normalized = name.trim();
    if (!normalized) throw new Error("书签名称不能为空");
    if (entry.name === normalized) return;
    entry.name = normalized;
    markDirty();
    void persist();
  }

  function setActiveBook(id: string): void {
    if (!loaded.value) return;
    if (!getBook(id) || activeBookId.value === id) return;
    activeBookId.value = id;
    selectedGroupId.value = "";
    markDirty();
    void persist();
  }

  function selectGroup(id: string): void {
    if (!id || !activeBook.value || findGroup(activeBook.value.groups, id)) {
      selectedGroupId.value = id;
    }
  }

  function getBook(id: string): BookmarkBookState | undefined {
    return books.value.find((book) => book.id === id);
  }

  function findEntry(path: string, book = activeBook.value): { entry: BookmarkEntry; groupID: string } | null {
    if (!book) return null;
    const normalized = normalizePath(path);
    const rootEntry = book.entries.find((entry) => entry.path === normalized);
    if (rootEntry) return { entry: rootEntry, groupID: "" };
    return findEntryInGroups(book.groups, normalized);
  }

  function findEntryInGroups(groups: BookmarkGroup[], path: string): { entry: BookmarkEntry; groupID: string } | null {
    for (const group of groups) {
      const entry = group.entries.find((item) => item.path === path);
      if (entry) return { entry, groupID: group.id };
      const nested = findEntryInGroups(group.groups, path);
      if (nested) return nested;
    }
    return null;
  }

  function isBookmarked(path: string, book = activeBook.value): boolean {
    return !!findEntry(path, book);
  }

  function isBookmarkedInGroup(
    path: string,
    groupID = selectedGroupId.value,
    book = activeBook.value
  ): boolean {
    if (!book) return false;
    const normalized = normalizePath(path);
    if (groupID) {
      const group = findGroup(book.groups, groupID);
      return !!group?.entries.some((entry) => entry.path === normalized);
    }
    return book.entries.some((entry) => entry.path === normalized);
  }

  function assertEditable(book: BookmarkBookState | null | undefined): asserts book is BookmarkBookState {
    if (!book) throw new Error("尚未加载书签簿");
    if (!book.editable) throw new Error("当前书签簿不可编辑");
  }

  function assertLoaded(): void {
    if (!loaded.value) throw new Error("书签簿正在加载");
  }

  function findGroup(groups: BookmarkGroup[], id: string): BookmarkGroup | null {
    if (!id) return null;
    for (const group of groups) {
      if (group.id === id) return group;
      const nested = findGroup(group.groups, id);
      if (nested) return nested;
    }
    return null;
  }

  function findGroupWithParent(
    groups: BookmarkGroup[],
    id: string,
    parent: BookmarkGroup | null = null
  ): { group: BookmarkGroup; parent: BookmarkGroup | null } | null {
    for (const group of groups) {
      if (group.id === id) return { group, parent };
      const nested = findGroupWithParent(group.groups, id, group);
      if (nested) return nested;
    }
    return null;
  }

  function isDescendantGroup(group: BookmarkGroup, id: string): boolean {
    return !!findGroup(group.groups, id);
  }

  function cloneEntries(entries: BookmarkEntry[]): BookmarkEntry[] {
    return entries.map((entry) => ({ ...entry }));
  }

  function cloneGroups(groups: BookmarkGroup[]): BookmarkGroup[] {
    return groups.map((group) => ({
      id: group.id,
      name: group.name,
      groups: cloneGroups(group.groups),
      entries: cloneEntries(group.entries),
    }));
  }

  function cloneGroupsWithNewIDs(
    groups: BookmarkGroup[],
    reservedIDs: Set<string>,
    copiedIDs: Map<string, string>
  ): BookmarkGroup[] {
    return groups.map((group) => {
      const id = newGroupID(reservedIDs);
      reservedIDs.add(id);
      copiedIDs.set(group.id, id);
      return {
        id,
        name: group.name,
        groups: cloneGroupsWithNewIDs(group.groups, reservedIDs, copiedIDs),
        entries: cloneEntries(group.entries),
      };
    });
  }

  function appendUniqueEntries(destination: BookmarkEntry[], incoming: BookmarkEntry[]): void {
    const paths = new Set(destination.map((entry) => entry.path));
    for (const entry of incoming) {
      if (paths.has(entry.path)) continue;
      destination.push(entry);
      paths.add(entry.path);
    }
  }

  function uniqueNameFromSet(names: Set<string>, base: string): string {
    const normalized = base.trim() || "分组";
    if (!names.has(normalized)) return normalized;
    let number = 2;
    let next = `${normalized} ${number}`;
    while (names.has(next)) next = `${normalized} ${++number}`;
    return next;
  }

  function fromServiceBook(book: BookmarkBook): BookmarkBookState {
    return {
      id: String(book.id ?? "").trim(),
      name: String(book.name ?? "").trim(),
      builtin: !!book.builtin,
      editable: !!book.editable,
      groups: (book.groups ?? []).filter(Boolean).map(fromServiceGroup),
      entries: (book.entries ?? []).filter(Boolean).map((entry) => fromServiceEntry(entry)),
    };
  }

  function fromServiceGroup(group: StoredBookmarkGroup): BookmarkGroup {
    return {
      id: String(group.id ?? "").trim(),
      name: String(group.name ?? "").trim(),
      groups: (group.groups ?? []).filter(Boolean).map(fromServiceGroup),
      entries: (group.entries ?? []).filter(Boolean).map((entry) => fromServiceEntry(entry)),
    };
  }

  function fromServiceEntry(entry: StoredBookmarkEntry): BookmarkEntry {
    const path = normalizePath(String(entry.path ?? ""));
    return {
      path,
      name: String(entry.name ?? "").trim() || basename(path),
      fileIndex: -1,
    };
  }

  function toServiceBook(book: BookmarkBookState): BookmarkBook {
    return {
      id: book.id,
      name: book.name,
      builtin: book.builtin,
      editable: book.editable,
      groups: book.groups.map(toServiceGroup),
      entries: book.entries.map(toServiceEntry),
    };
  }

  function toServiceGroup(group: BookmarkGroup): StoredBookmarkGroup {
    return {
      id: group.id,
      name: group.name,
      groups: group.groups.map(toServiceGroup),
      entries: group.entries.map(toServiceEntry),
    };
  }

  function toServiceEntry(entry: BookmarkEntry): StoredBookmarkEntry {
    return { path: normalizePath(entry.path), name: entry.name.trim() || basename(entry.path) };
  }

  function toDocument(): BookmarkDocument {
    return {
      version: bookmarkDocumentVersion,
      activeBookId: activeBookId.value,
      books: books.value.map(toServiceBook),
    };
  }

  function toBookFile(book: BookmarkBookState): BookmarkBookFile {
    return {
      version: bookmarkDocumentVersion,
      name: book.name,
      groups: book.groups.map(toServiceGroup),
      entries: book.entries.map(toServiceEntry),
    };
  }

  function applyDocument(document: BookmarkDocument): void {
    const nextBooks = (document.books ?? [])
      .filter(Boolean)
      .map(fromServiceBook)
      .filter((book) => book.id && book.name);
    books.value = nextBooks;
    activeBookId.value = nextBooks.some((book) => book.id === document.activeBookId)
      ? document.activeBookId
      : builtinBookmarkBookID;
    if (!nextBooks.some((book) => book.id === activeBookId.value)) {
      activeBookId.value = nextBooks[0]?.id ?? builtinBookmarkBookID;
    }
    selectedGroupId.value = "";
    recomputeCounters();
    mutationVersion++;
    savedVersion = mutationVersion;
    saveError.value = "";
  }

  function recomputeCounters(): void {
    let maxBook = 0;
    let maxGroup = 0;
    for (const book of books.value) {
      const bookMatch = /^bookmark-(\d+)$/.exec(book.id);
      if (bookMatch) maxBook = Math.max(maxBook, Number(bookMatch[1]));
      const groupIDs = new Set<string>();
      collectGroupIDs(book.groups, groupIDs);
      for (const id of groupIDs) {
        const groupMatch = /^bookmark-group-(\d+)$/.exec(id);
        if (groupMatch) maxGroup = Math.max(maxGroup, Number(groupMatch[1]));
      }
    }
    bookSequence = maxBook + 1;
    groupSequence = maxGroup + 1;
  }

  function resetInMemory(): void {
    books.value = [];
    activeBookId.value = builtinBookmarkBookID;
    selectedGroupId.value = "";
    recomputeCounters();
    mutationVersion++;
    savedVersion = mutationVersion;
  }

  async function load(): Promise<void> {
    if (loaded.value) return;
    if (loadPromise) return loadPromise;
    loadPromise = (async () => {
      try {
        applyDocument(await BookmarkService.LoadBookmarks());
        loadError.value = "";
      } catch (error: any) {
        loadError.value = String(error?.message ?? error);
        console.error("load bookmarks failed", error);
        resetInMemory();
      } finally {
        loaded.value = true;
        loadPromise = null;
      }
    })();
    return loadPromise;
  }

  function markDirty(): void {
    mutationVersion++;
    saveError.value = "";
  }

  async function persist(): Promise<void> {
    if (!loaded.value) return;
    const version = mutationVersion;
    const snapshot = toDocument();
    saveChain = saveChain
      .catch(() => undefined)
      .then(async () => {
        saving.value = true;
        try {
          await BookmarkService.SaveBookmarks(snapshot);
          savedVersion = Math.max(savedVersion, version);
          if (version === mutationVersion) saveError.value = "";
        } catch (error: any) {
          saveError.value = String(error?.message ?? error);
          throw error;
        } finally {
          saving.value = false;
        }
      });
    try {
      await saveChain;
    } catch {
      // UI exposes saveError and retry; mutations remain in memory.
    }
  }

  async function retrySave(): Promise<void> {
    if (!dirty.value) return;
    await persist();
  }

  async function importBook(): Promise<BookmarkBookState | null> {
    await load();
    const imported = await BookmarkService.ImportBookmarkBookDialog();
    if (!imported) return null;
    const importedName = String(imported.name ?? "").trim() || "导入书签簿";
    const book: BookmarkBookState = {
      id: newBookID(),
      name: uniqueBookName(importedName),
      builtin: false,
      editable: true,
      groups: regenerateImportedGroups(imported.groups ?? []),
      entries: (imported.entries ?? []).filter(Boolean).map(fromServiceEntry),
    };
    books.value.push(book);
    activeBookId.value = book.id;
    selectedGroupId.value = "";
    markDirty();
    await persist();
    if (archivePath) void resolveEntries(archivePath);
    return book;
  }

  function regenerateImportedGroups(groups: StoredBookmarkGroup[]): BookmarkGroup[] {
    const reservedIDs = new Set<string>();
    return cloneGroupsWithNewIDs(
      groups.filter(Boolean).map(fromServiceGroup),
      reservedIDs,
      new Map<string, string>()
    );
  }

  async function exportActiveBook(): Promise<string | null> {
    await load();
    const book = activeBook.value;
    if (!book) return null;
    const path = await BookmarkService.ExportBookmarkBookDialog(toBookFile(book));
    return path || null;
  }

  async function resolveEntries(path: string, request = resolveRequest): Promise<void> {
    if (!path || request !== resolveRequest || !loaded.value) return;
    const allEntries = books.value.flatMap((book) => flattenEntries(book));
    const paths = [...new Set(allEntries.map((entry) => normalizePath(entry.path)).filter(Boolean))];
    for (const entry of allEntries) entry.fileIndex = -1;
    if (paths.length === 0) return;
    resolving.value = true;
    try {
      const nodes = (await ArchiveService.ResolveFiles(paths)) ?? [];
      if (request !== resolveRequest || archivePath !== path) return;
      const byPath = new Map(
        nodes
          .filter((node): node is TreeNode => !!node && !node.isDir && node.fileIndex >= 0)
          .map((node) => [normalizePath(node.path), node.fileIndex])
      );
      for (const entry of allEntries) entry.fileIndex = byPath.get(normalizePath(entry.path)) ?? -1;
    } catch (error) {
      console.error("resolve bookmark entries failed", error);
    } finally {
      if (request === resolveRequest) resolving.value = false;
    }
  }

  function flattenEntries(book: BookmarkBookState): BookmarkEntry[] {
    const result = [...book.entries];
    const append = (groups: BookmarkGroup[]) => {
      for (const group of groups) {
        result.push(...group.entries);
        append(group.groups);
      }
    };
    append(book.groups);
    return result;
  }

  function onArchiveOpened(event: any): void {
    const path = String(event?.data?.path ?? event?.path ?? "");
    archivePath = path;
    sessionId.value++;
    const request = ++resolveRequest;
    void load().then(() => resolveEntries(path, request));
  }

  function onArchiveChanged(event: any): void {
    const path = String(event?.data?.path ?? event?.path ?? "");
    if (path && path === archivePath) void resolveEntries(path);
    else if (!path && archivePath) void resolveEntries(archivePath);
  }

  function onArchiveClosed(): void {
    archivePath = "";
    resolveRequest++;
    resolving.value = false;
    sessionId.value++;
    for (const book of books.value) {
      for (const entry of flattenEntries(book)) entry.fileIndex = -1;
    }
  }

  Events.On("archive:opened", onArchiveOpened);
  Events.On("archive:changed", onArchiveChanged);
  Events.On("archive:reloaded", onArchiveChanged);
  Events.On("archive:closed", onArchiveClosed);

  return {
    books,
    activeBookId,
    activeBook,
    activeGroup,
    activeGroupId,
    activeBookEditable,
    selectedGroupId,
    loaded,
    resolving,
    saving,
    saveError,
    loadError,
    sessionId,
    dirty,
    hasCustomBooks,
    load,
    retrySave,
    createBook,
    renameBook,
    deleteBook,
    copyBook,
    createGroup,
    renameGroup,
    deleteGroup,
    moveEntry,
    addEntries,
    removeEntry,
    renameEntry,
    setActiveBook,
    selectGroup,
    findEntry,
    isBookmarked,
    isBookmarkedInGroup,
    importBook,
    exportActiveBook,
    resolveEntries,
    normalizePath,
  };
});
