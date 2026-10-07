import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { createPinia, disposePinia, setActivePinia, type Pinia } from "pinia";
import { nextTick } from "vue";

const mocks = vi.hoisted(() => ({
  Prepare: vi.fn(),
  Release: vi.fn(),
  ExecuteSelectionDialog: vi.fn(),
  Formats: vi.fn(),
  events: new Map<string, () => void>(),
}));
vi.mock("../bindings/pvfine/services", () => ({ ExportService: mocks }));
vi.mock("@wailsio/runtime", () => ({ Events: { On: (name: string, handler: () => void) => mocks.events.set(name, handler) } }));

import { modNameError, modVersionError, useExportStore } from "../src/stores/export";
import { buildExportTree, exportFileKey } from "../src/utils/export-tree";

const files = [
  { path: "stackable/a.stk", required: false, dependencyCount: 0 },
  { path: "stackable/b.stk", required: false, dependencyCount: 0 },
  { path: "string/demo.str", required: false, dependencyCount: 0 },
];

let pinia: Pinia;
beforeEach(() => {
  pinia = createPinia();
  setActivePinia(pinia);
  vi.clearAllMocks();
  mocks.events.clear();
  mocks.Prepare.mockResolvedValue({ id: "1", fileCount: 3, dependencyCount: 0, skippedDeletes: 0, warnings: [], files });
  mocks.Release.mockResolvedValue(undefined);
  mocks.Formats.mockResolvedValue([{ id: "110USextend", name: "110USextend", canRead: false, canWrite: true, operations: [] }]);
  mocks.ExecuteSelectionDialog.mockResolvedValue("");
});
afterEach(() => {
  useExportStore().close();
  disposePinia(pinia);
});

const source = { source: "working", scopes: [], commitId: "" };
async function settle() {
  await nextTick();
  await useExportStore().refresh();
  await nextTick();
}
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((yes) => { resolve = yes; });
  return { promise, resolve };
}

test("所有来源默认直接导出，取消不执行或写盘", async () => {
  const store = useExportStore();
  for (const kind of ["working", "commit", "selection", "memory"]) {
    const result = store.open({ ...source, source: kind }, "导出");
    await settle();
    expect(store.mode).toBe("direct");
    expect(store.includeDependencies).toBe(false);
    expect(store.version).toBe("1.0");
    expect(store.canExport).toBe(true);
    expect(mocks.Prepare.mock.lastCall?.[0].source).toBe(kind);
    store.close();
    expect(await result).toBe("");
  }
  expect(mocks.ExecuteSelectionDialog).not.toHaveBeenCalled();
  expect(mocks.Release).toHaveBeenCalled();
});

test("mod 模式校验名称、格式和依赖开关，并显示删除警告", async () => {
  const store = useExportStore();
  void store.open(source, "导出当前改动");
  await settle();
  store.mode = "mod";
  await settle();
  expect(store.nameError).not.toBe("");
  expect(store.canExport).toBe(false);
  store.name = "中文 MOD";
  store.includeDependencies = true;
  mocks.Prepare.mockResolvedValue({ id: "mod", fileCount: 4, dependencyCount: 3, skippedDeletes: 2, warnings: ["跳过 2 个删除"], files });
  await settle();
  expect(mocks.Prepare.mock.lastCall?.[0]).toMatchObject({
    mode: "mod", format: "110USextend", name: "中文 MOD", includeDependencies: true,
  });
  expect(store.preview?.warnings).toEqual(["跳过 2 个删除"]);
  expect(store.preview?.dependencyCount).toBe(3);
  store.mode = "direct";
  await settle();
  expect(mocks.Prepare.mock.lastCall?.[0].includeDependencies).toBe(false);
});

test("名称验证覆盖路径和 Windows 保留名", () => {
  for (const name of ["", ".hidden", "_hidden", "CON", "nul.txt", "COM1", "a/b", "a\\b", "tail.", " tail", "tail ", "a:b"]) {
    expect(modNameError(name), name).not.toBe("");
  }
  expect(modNameError("合法名称")).toBe("");
});

test("自定义版本传入元数据，无需释放旧快照，关闭后重置", async () => {
  const store = useExportStore();
  void store.open(source, "导出");
  store.mode = "mod";
  store.name = "测试";
  await settle();
  mocks.Release.mockClear();
  store.version = "2.1-beta";
  await settle();
  expect(mocks.Prepare.mock.lastCall?.[0].version).toBe("2.1-beta");
  expect(mocks.Release).not.toHaveBeenCalled();
  expect(store.canExport).toBe(true);
  store.version = "bad\nversion";
  await settle();
  expect(store.versionError).not.toBe("");
  expect(store.canExport).toBe(false);
  store.close();
  expect(mocks.Release).toHaveBeenCalledWith("1");
  void store.open(source, "导出");
  expect(store.version).toBe("1.0");
});

test("快速改名时保留已完成的旧内容供下一次预检复用", async () => {
  const store = useExportStore();
  void store.open(source, "导出");
  store.mode = "mod";
  store.name = "初始名称";
  await settle();
  mocks.Release.mockClear();
  const pending = deferred<{ id: string; fileCount: number; dependencyCount: number; skippedDeletes: number; warnings: string[]; files: typeof files }>();
  mocks.Prepare.mockReturnValueOnce(pending.promise);
  store.name = "中间名称";
  await nextTick();
  await Promise.resolve();
  store.name = "最终名称";
  await nextTick();
  pending.resolve({ id: "cache", fileCount: 3, dependencyCount: 0, skippedDeletes: 0, warnings: [], files });
  await settle();
  expect(mocks.Release).not.toHaveBeenCalledWith("cache");
  expect(mocks.Prepare.mock.lastCall?.[0].name).toBe("最终名称");
  expect(store.canExport).toBe(true);
});

test("版本不强制 semver，空值使用默认值，非法控制字符和长度阻止导出", () => {
  for (const value of ["", "1.0", "v2.3-beta", "中文测试版"]) expect(modVersionError(value)).toBe("");
  for (const value of [" 1.0", "1.0 ", "x\nx", "x".repeat(129), "中".repeat(43)]) expect(modVersionError(value)).not.toBe("");
});

test("重复确认仅执行一次，执行期间不能关闭，成功返回目录", async () => {
  const store = useExportStore();
  const result = store.open(source, "导出");
  await settle();
  const pending = deferred<string>();
  mocks.ExecuteSelectionDialog.mockReturnValue(pending.promise);
  const first = store.execute();
  await store.execute();
  store.close();
  expect(store.visible).toBe(true);
  expect(mocks.ExecuteSelectionDialog).toHaveBeenCalledTimes(1);
  pending.resolve("D:/mods/测试");
  await first;
  expect(await result).toBe("D:/mods/测试");
  expect(store.visible).toBe(false);
});

test("目录选择取消无错误，可重试；过期预览禁用导出", async () => {
  const store = useExportStore();
  void store.open(source, "导出");
  await settle();
  await store.execute();
  expect(store.visible).toBe(true);
  expect(store.error).toBe("");
  mocks.ExecuteSelectionDialog.mockRejectedValue(new Error("导出预览已过期"));
  await store.execute();
  expect(store.canExport).toBe(false);
  expect(store.error).toContain("重新准备");
  mocks.ExecuteSelectionDialog.mockResolvedValue("");
  await settle();
  expect(store.canExport).toBe(true);
});

test("版本变化使预览失效，归档关闭取消流程", async () => {
  const store = useExportStore();
  const result = store.open(source, "导出");
  await settle();
  mocks.events.get("version:changed")?.();
  expect(store.preview).toBeNull();
  expect(store.canExport).toBe(false);
  expect(store.error).toContain("重新准备");
  mocks.events.get("archive:closed")?.();
  expect(await result).toBe("");
  expect(store.visible).toBe(false);
});

test("取消慢预检释放返回任务，旧结果不能覆盖新弹窗", async () => {
  const store = useExportStore();
  const pending = deferred<{ id: string; fileCount: number; dependencyCount: number; skippedDeletes: number; warnings: string[] }>();
  mocks.Prepare.mockReturnValueOnce(pending.promise);
  const oldResult = store.open(source, "旧导出");
  await nextTick();
  await Promise.resolve();
  store.close();
  expect(await oldResult).toBe("");
  void store.open({ ...source, source: "selection" }, "新导出");
  pending.resolve({ id: "old", fileCount: 999, dependencyCount: 0, skippedDeletes: 0, warnings: [] });
  await settle();
  expect(mocks.Release).toHaveBeenCalledWith("old");
  expect(store.preview?.fileCount).toBe(3);
  expect(store.title).toBe("新导出");
});

test("树显示完整输出路径、目录优先、必需元数据不可移除", () => {
  const tree = buildExportTree([
    { path: "pack.json", required: true, dependencyCount: 0 },
    { path: "pvf/stackable/中文.stk", required: false, dependencyCount: 0 },
    { path: "merge/string/demo.str", required: false, dependencyCount: 2 },
  ]);
  expect(tree.map((node) => node.label)).toEqual(["merge", "pvf", "pack.json"]);
  expect(tree[2]).toMatchObject({ key: exportFileKey("pack.json"), checkboxDisabled: true });
  expect(tree[1]?.children?.[0]?.children?.[0]).toMatchObject({
    key: exportFileKey("pvf/stackable/中文.stk"), label: "中文.stk",
  });
});

test("移除文件传给后端，全不选禁用导出，刷新保留排除，新会话重置", async () => {
  const store = useExportStore();
  void store.open({ ...source, source: "memory" }, "内存改动");
  await settle();
  store.selectFiles([exportFileKey(files[0]!.path)]);
  expect(store.selectedCount).toBe(1);
  await store.execute();
  expect(mocks.ExecuteSelectionDialog).toHaveBeenCalledWith("1", [files[1]!.path, files[2]!.path]);
  await settle();
  expect(store.selectedCount).toBe(1);
  store.selectFiles([]);
  expect(store.canExport).toBe(false);
  await store.execute();
  expect(mocks.ExecuteSelectionDialog).toHaveBeenCalledTimes(1);
  store.close();
  void store.open(source, "新导出");
  await settle();
  expect(store.selectedCount).toBe(3);
});

test("移除补齐依赖更新统计，必需文件始终勾选", async () => {
  const store = useExportStore();
  const modFiles = [
    { path: "pack.json", required: true, dependencyCount: 0 },
    { path: "pvf/a.stk", required: false, dependencyCount: 0 },
    { path: "merge/a.str", required: false, dependencyCount: 2 },
  ];
  mocks.Prepare.mockResolvedValue({ id: "mod", fileCount: 2, dependencyCount: 2, skippedDeletes: 0, warnings: [], files: modFiles });
  void store.open(source, "导出");
  store.mode = "mod";
  store.name = "测试";
  store.includeDependencies = true;
  await settle();
  store.selectFiles([exportFileKey("pvf/a.stk")]);
  expect(store.checkedKeys).toContain(exportFileKey("pack.json"));
  expect(store.selectedDependencyCount).toBe(0);
  expect(store.selectedCount).toBe(1);
  await store.execute();
  expect(mocks.ExecuteSelectionDialog).toHaveBeenCalledWith("mod", ["merge/a.str"]);
});
