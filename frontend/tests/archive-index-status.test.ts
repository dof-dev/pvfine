import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { createPinia, setActivePinia } from "pinia";

const api = vi.hoisted(() => ({
  Open: vi.fn(),
  OpenDialog: vi.fn(),
  Close: vi.fn(),
  IndexStatus: vi.fn(),
  RebuildSearchIndex: vi.fn(),
}));
const events = vi.hoisted(() => new Map<string, (event: unknown) => void>());
vi.mock("@wailsio/runtime", () => ({
  Events: { On: (name: string, callback: (event: unknown) => void) => events.set(name, callback) },
}));
vi.mock("../bindings/pvfine/services", () => ({ ArchiveService: api, EditorService: {} }));

import { useArchiveStore } from "../src/stores/archive";

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

function emit(name: string, data?: unknown) {
  events.get(name)?.({ data });
}

async function flush() {
  await Promise.resolve();
  await Promise.resolve();
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.resetAllMocks();
  events.clear();
  setActivePinia(createPinia());
  vi.stubGlobal("window", {
    setInterval: (...args: Parameters<typeof setInterval>) => globalThis.setInterval(...args),
    clearInterval: (handle: number) => globalThis.clearInterval(handle),
    localStorage: { getItem: () => null, setItem: vi.fn() },
  });
  api.IndexStatus.mockResolvedValue({ state: "building", stage: "sqlite" });
  api.Close.mockResolvedValue(undefined);
});

test("string table events retain timings without restarting semantic indexing", () => {
  const store = useArchiveStore();
  store.info = { path: "/game/Script.pvf" } as NonNullable<typeof store.info>;
  emit("archive:string-table-index", {
    archivePath: "/game/Script.pvf",
    stats: { revision: 4, state: "ready", tableBuilds: 1, entries: 100, mappingDurationMs: 2, tableDurationMs: 12, buildDurationMs: 14 },
  });
  expect(store.stringTableIndex.buildDurationMs).toBe(14);
  expect(store.stringTableIndex.tableBuilds).toBe(1);
  expect(store.stringTableIndex.entries).toBe(100);
  expect(vi.getTimerCount()).toBe(0);
  emit("archive:string-table-index", { archivePath: "/game/Script.pvf", stats: { revision: 3, buildDurationMs: 10 } });
  expect(store.stringTableIndex.buildDurationMs).toBe(14);
  emit("archive:string-table-index", { archivePath: "/game/old.pvf", stats: { revision: 10, buildDurationMs: 200 } });
  expect(store.stringTableIndex.buildDurationMs).toBe(14);
  emit("archive:string-table-index", {
    archivePath: "/game/Script.pvf", kind: "snapshot", stats: { revision: 0, state: "idle" },
  });
  expect(store.stringTableIndex.state).toBe("idle");
  expect(store.stringTableIndex.buildDurationMs).toBe(0);
  emit("archive:closed");
  expect(store.stringTableIndex.state).toBe("idle");
  expect(store.stringTableIndex.buildDurationMs).toBe(0);
});

test("index polling restores missing string table statistics", async () => {
  api.IndexStatus.mockResolvedValue({
    state: "ready", stringTableIndex: { revision: 4, state: "ready", tableBuilds: 2, buildDurationMs: 30 },
  });
  const store = useArchiveStore();
  emit("archive:opened", { path: "/game/Script.pvf" });
  await flush();
  expect(store.stringTableIndex.buildDurationMs).toBe(30);
  expect(store.stringTableIndex.tableBuilds).toBe(2);
  expect(store.indexReady).toBe(true);
});

afterEach(() => {
  vi.clearAllTimers();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

test("a stale poll cannot overwrite the ready event or restart indexing", async () => {
  const pending = deferred<{ state: string }>();
  api.IndexStatus.mockReturnValueOnce(pending.promise);
  const store = useArchiveStore();
  emit("archive:opened", { path: "/game/Script.pvf" });
  emit("archive:index-ready", { state: "ready", stage: "ready-sqlite", done: 10, total: 10 });
  pending.resolve({ state: "building" });
  await flush();
  expect(store.indexReady).toBe(true);
  expect(store.indexing).toBe(false);
  expect(store.indexStatus.stage).toBe("ready-sqlite");
  expect(vi.getTimerCount()).toBe(0);
});

test("a stale poll cannot overwrite an error event", async () => {
  const pending = deferred<{ state: string }>();
  api.IndexStatus.mockReturnValueOnce(pending.promise);
  const store = useArchiveStore();
  emit("archive:opened", { path: "/game/Script.pvf" });
  emit("archive:index-error", { state: "error", error: "build failed" });
  pending.resolve({ state: "building" });
  await flush();
  expect(store.indexStatus.state).toBe("error");
  expect(store.indexStatus.error).toBe("build failed");
  expect(vi.getTimerCount()).toBe(0);
});

test("a stale poll cannot overwrite newer progress", async () => {
  const pending = deferred<{ state: string; done: number }>();
  api.IndexStatus.mockReturnValueOnce(pending.promise);
  const store = useArchiveStore();
  emit("archive:opened", { path: "/game/Script.pvf" });
  emit("archive:index-progress", { state: "building", done: 20, total: 100 });
  pending.resolve({ state: "building", done: 5 });
  await flush();
  expect(store.indexStatus.done).toBe(20);
  expect(vi.getTimerCount()).toBe(1);
});

test("a poll from a closed archive cannot change the next archive's state", async () => {
  const pending = deferred<{ state: string }>();
  api.IndexStatus.mockReturnValueOnce(pending.promise);
  const store = useArchiveStore();
  emit("archive:opened", { path: "/game/first.pvf" });
  emit("archive:closed");
  emit("archive:opened", { path: "/game/second.pvf" });
  pending.resolve({ state: "ready" });
  await flush();
  expect(store.info?.path).toBe("/game/second.pvf");
  expect(store.indexing).toBe(true);
  await vi.advanceTimersByTimeAsync(250);
  expect(api.IndexStatus).toHaveBeenCalledTimes(2);
});

test("polling retries transient errors and recovers a missed ready event", async () => {
  api.IndexStatus.mockRejectedValueOnce(new Error("temporary error"))
    .mockResolvedValueOnce({ state: "ready", stage: "ready-sqlite" });
  const store = useArchiveStore();
  emit("archive:opened", { path: "/game/Script.pvf" });
  await flush();
  expect(store.indexing).toBe(true);
  await vi.advanceTimersByTimeAsync(250);
  expect(store.indexReady).toBe(true);
  expect(vi.getTimerCount()).toBe(0);
});

test("background refresh keeps polling until refreshing is false", async () => {
  const store = useArchiveStore();
  emit("archive:opened", { path: "/game/Script.pvf" });
  await flush();
  emit("archive:index-ready", { state: "ready", refreshing: true });
  api.IndexStatus.mockResolvedValueOnce({ state: "ready", refreshing: false });
  expect(vi.getTimerCount()).toBe(1);
  await vi.advanceTimersByTimeAsync(250);
  expect(store.refreshingIndex).toBe(false);
  expect(vi.getTimerCount()).toBe(0);
});

test("rebuild starts fallback polling when progress and ready events are missed", async () => {
  const store = useArchiveStore();
  store.info = { path: "/game/Script.pvf" } as NonNullable<typeof store.info>;
  api.RebuildSearchIndex.mockResolvedValue({ state: "building" });
  api.IndexStatus.mockResolvedValueOnce({ state: "ready" });
  await store.rebuildSearchIndex();
  await flush();
  expect(store.indexReady).toBe(true);
  expect(api.IndexStatus).toHaveBeenCalledTimes(1);
  expect(vi.getTimerCount()).toBe(0);
});

test("a stale rebuild response cannot overwrite the ready event", async () => {
  const pending = deferred<{ state: string }>();
  const store = useArchiveStore();
  store.info = { path: "/game/Script.pvf" } as NonNullable<typeof store.info>;
  api.RebuildSearchIndex.mockReturnValueOnce(pending.promise);
  const rebuilding = store.rebuildSearchIndex();
  emit("archive:index-ready", { state: "ready", stage: "ready-sqlite" });
  pending.resolve({ state: "building" });
  await rebuilding;
  expect(store.indexReady).toBe(true);
  expect(vi.getTimerCount()).toBe(0);
});
