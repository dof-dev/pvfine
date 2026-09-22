import { beforeEach, expect, test, vi } from "vitest";
import { createPinia, setActivePinia } from "pinia";

const api = vi.hoisted(() => ({
  CreateSnapshot: vi.fn(),
  Status: vi.fn(),
  PendingRecovery: vi.fn(),
  Restore: vi.fn(),
  Discard: vi.fn(),
  DropForWorkspace: vi.fn(),
  ChooseCachePath: vi.fn(),
  saveAllDirty: vi.fn(),
}));

const state = vi.hoisted(() => ({
  archive: { open: true, loading: false, modifiedCount: 0, info: null as unknown },
  editor: { dirtyCount: 0, saving: false },
  version: { status: { loading: false, changedFiles: 0, needsSave: false } },
  settings: { autosaveEnabled: true, autosaveIntervalSeconds: 300, autosavePath: "" },
  fileSets: { dirty: false },
  script: { workspaceDetached: false, dirty: false },
}));

vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn() } }));
vi.mock("../bindings/pvfine/services", () => ({ AutosaveService: api }));
vi.mock("../src/stores/archive", () => ({ useArchiveStore: () => state.archive }));
vi.mock("../src/stores/editor", () => ({ useEditorStore: () => Object.assign(state.editor, { saveAllDirty: api.saveAllDirty }) }));
vi.mock("../src/stores/version", () => ({ useVersionStore: () => state.version }));
vi.mock("../src/stores/fileSets", () => ({ useFileSetStore: () => state.fileSets }));
vi.mock("../src/stores/script", () => ({ useScriptStore: () => state.script }));
vi.mock("../src/stores/settings", () => ({
  useSettingsStore: () => state.settings,
  minAutosaveIntervalMinutes: 1,
  maxAutosaveIntervalMinutes: 120,
  defaultAutosaveIntervalSeconds: 300,
}));

import { useAutosaveStore } from "../src/stores/autosave";
import { setDialogBusy } from "../src/dialogBusy";

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: Error) => void;
  const promise = new Promise<T>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}

const statusPayload = {
  enabled: true,
  path: "/tmp/autosave.pvf",
  defaultPath: "/cache/pvfine/autosave.pvf",
  exists: false,
  cachedAt: 0,
  sourcePath: "",
  sizeBytes: 0,
  running: false,
  lastError: "",
};

beforeEach(() => {
  setActivePinia(createPinia());
  vi.clearAllMocks();
  // 惰性转发:假定时器替换 globalThis 后 window 上的实现同步生效。
  vi.stubGlobal("window", {
    setInterval: (...args: Parameters<typeof setInterval>) => globalThis.setInterval(...args),
    clearInterval: (handle: number) => globalThis.clearInterval(handle),
  });
  state.archive.open = true;
  state.archive.loading = false;
  state.archive.modifiedCount = 0;
  state.archive.info = null;
  state.editor.dirtyCount = 0;
  state.editor.saving = false;
  state.version.status.loading = false;
  state.settings.autosaveEnabled = true;
  state.settings.autosaveIntervalSeconds = 300;
  api.Status.mockResolvedValue({ ...statusPayload });
  api.PendingRecovery.mockResolvedValue(null);
  api.CreateSnapshot.mockResolvedValue(undefined);
  api.Discard.mockResolvedValue(true);
  api.DropForWorkspace.mockResolvedValue(undefined);
  api.saveAllDirty.mockResolvedValue(1);
});

test("干净工作区与关闭开关都不写缓存", async () => {
  const store = useAutosaveStore();
  await store.tick();
  expect(api.CreateSnapshot).not.toHaveBeenCalled();

  state.settings.autosaveEnabled = false;
  state.archive.modifiedCount = 3;
  await store.tick();
  expect(api.CreateSnapshot).not.toHaveBeenCalled();
});

test("有改动时先提交编辑器草稿再快照", async () => {
  const store = useAutosaveStore();
  state.editor.dirtyCount = 2;
  await store.tick();
  expect(api.saveAllDirty).toHaveBeenCalledTimes(1);
  expect(api.CreateSnapshot).toHaveBeenCalledTimes(1);
  expect(store.lastSnapshotAt).toBeGreaterThan(0);
  expect(store.lastError).toBe("");
});

test("保存中、版本加载中或没有归档时跳过", async () => {
  const store = useAutosaveStore();
  state.archive.modifiedCount = 1;

  state.editor.saving = true;
  await store.tick();
  state.editor.saving = false;
  state.version.status.loading = true;
  await store.tick();
  state.version.status.loading = false;
  state.archive.open = false;
  await store.tick();
  expect(api.CreateSnapshot).not.toHaveBeenCalled();
});

test("快照失败只记录错误，不抛出", async () => {
  const store = useAutosaveStore();
  state.archive.modifiedCount = 1;
  api.CreateSnapshot.mockRejectedValue(new Error("磁盘已满"));
  await store.tick();
  expect(store.lastError).toBe("磁盘已满");
  expect(store.running).toBe(false);
});

test("定时器按设置的间隔触发快照", async () => {
  vi.useFakeTimers();
  try {
    const store = useAutosaveStore();
    state.settings.autosaveIntervalSeconds = 60;
    state.archive.modifiedCount = 1;
    store.restartTimer();
    await vi.advanceTimersByTimeAsync(59_000);
    expect(api.CreateSnapshot).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1_000);
    expect(api.CreateSnapshot).toHaveBeenCalledTimes(1);
    store.stopTimer();
  } finally {
    vi.useRealTimers();
  }
});

test("启动检查待恢复备份并可用恢复/丢弃处理", async () => {
  const store = useAutosaveStore();
  const pending = {
    sourcePath: "/game/Script.pvf",
    sourceName: "Script.pvf",
    cachedAt: 100,
    sizeBytes: 2048,
    pendingFiles: 2,
    sourceExists: true,
    sourceChanged: false,
  };
  api.PendingRecovery.mockResolvedValue(pending);
  expect(await store.checkRecovery()).toEqual(pending);

  const restored = { path: "/game/Script.pvf", modifiedCount: 2 };
  api.Restore.mockResolvedValue(restored);
  expect(await store.restore()).toBe(true);
  expect(store.pendingRecovery).toBeNull();
  expect(state.archive.info).toEqual(restored);

  expect(await store.discard()).toBe(true);
  await store.discardForWorkspace();
  expect(api.DropForWorkspace).toHaveBeenCalled();
});

test("恢复失败时错误向上抛出并保留待恢复信息", async () => {
  const store = useAutosaveStore();
  store.pendingRecovery = {
    sourcePath: "/game/Script.pvf",
    sourceName: "Script.pvf",
    cachedAt: 100,
    sizeBytes: 2048,
    pendingFiles: 1,
    sourceExists: false,
    sourceChanged: false,
  };
  api.Restore.mockRejectedValue(new Error("原文件不存在,请先找回文件再恢复备份: /game/Script.pvf"));
  await expect(store.restore()).rejects.toThrow("原文件不存在");
  expect(store.pendingRecovery).not.toBeNull();
  expect(store.lastError).toContain("原文件不存在");
});

test("恢复期间暴露 restoring 状态，重复调用直接忽略", async () => {
  const store = useAutosaveStore();
  const pending = deferred<unknown>();
  api.Restore.mockReturnValue(pending.promise);

  const first = store.restore();
  expect(store.restoring).toBe(true);
  expect(await store.restore()).toBe(false);
  expect(api.Restore).toHaveBeenCalledTimes(1);

  pending.resolve({ path: "/game/Script.pvf", modifiedCount: 1 });
  expect(await first).toBe(true);
  expect(store.restoring).toBe(false);
});

test("恢复弹窗忙碌态锁定关闭入口并切换按钮文案", () => {
  const instance: any = {
    loading: false,
    positiveText: "恢复",
    negativeButtonProps: {},
    closable: true,
    maskClosable: true,
    closeOnEsc: true,
  };
  setDialogBusy(instance, true, "恢复", "正在恢复…");
  expect(instance).toMatchObject({
    loading: true,
    positiveText: "正在恢复…",
    negativeButtonProps: { disabled: true },
    closable: false,
    maskClosable: false,
    closeOnEsc: false,
  });
  setDialogBusy(instance, false, "恢复");
  expect(instance).toMatchObject({
    loading: false,
    positiveText: "恢复",
    negativeButtonProps: { disabled: false },
    closable: true,
    maskClosable: true,
    closeOnEsc: true,
  });
});

test("initialize 只检查一次恢复并启动定时器", async () => {
  vi.useFakeTimers();
  try {
    const store = useAutosaveStore();
    state.archive.modifiedCount = 1;
    api.PendingRecovery.mockResolvedValue(null);
    await store.initialize();
    await store.initialize();
    expect(api.PendingRecovery).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(300_000);
    expect(api.CreateSnapshot).toHaveBeenCalled();
    store.stopTimer();
  } finally {
    vi.useRealTimers();
  }
});
