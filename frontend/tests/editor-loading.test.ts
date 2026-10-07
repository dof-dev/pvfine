import { beforeEach, expect, test, vi } from "vitest";
import { createPinia, setActivePinia } from "pinia";
import { nextTick } from "vue";

const api = vi.hoisted(() => ({ GetFileBasic: vi.fn(), GetFile: vi.fn(), showArchiveEditor: vi.fn(), ApplyShopEdit: vi.fn(), refreshInfo: vi.fn() }));
vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn() } }));
vi.mock("../bindings/pvfine/services", () => ({ EditorService: api, ArchiveService: {}, FileGUIService: api }));
vi.mock("../src/stores/archive", () => ({ useArchiveStore: () => ({ refreshInfo: api.refreshInfo }) }));
vi.mock("../src/stores/script", () => ({ useScriptStore: () => api }));
vi.mock("../src/stores/explorer", () => ({
  useExplorerStore: () => ({ getFilePath: (index: number) => `map/${index}.lst`, selectedKey: null }),
}));
import { useEditorStore } from "../src/stores/editor";
import { longLineThreshold } from "../src/editorWrapping";

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}
function meta(index: number, text = "1 `map/file.map`") {
  return { index, path: `map/${index}.lst`, text, editable: true, dataType: 1, size: text.length, tags: [], annotations: [], modified: false };
}
beforeEach(() => {
  setActivePinia(createPinia());
  vi.resetAllMocks();
  api.GetFile.mockResolvedValue(null);
});

test("自动换行在加载完成前按行长决定，普通文件默认开启", async () => {
  api.GetFileBasic.mockImplementation(async (index) =>
    meta(index, index === 1 ? "x".repeat(longLineThreshold + 1) : "short\nlines")
  );
  const store = useEditorStore();
  await store.openFile(1);
  expect(store.activeTab?.loading).toBe(false);
  expect(store.activeTab?.lineWrapping).toBe(false);
  await store.openFile(2);
  expect(store.activeTab?.lineWrapping).toBe(true);
});

test("换行开关按文件保留，分屏共享，重新打开恢复自动判断", async () => {
  api.GetFileBasic.mockImplementation(async (index) => meta(index));
  const store = useEditorStore();
  await store.openFile(1);
  store.activeTab!.lineWrapping = false;
  store.split("columns");
  expect(store.activeTab?.lineWrapping).toBe(false);
  await store.openFile(2);
  expect(store.activeTab?.lineWrapping).toBe(true);
  store.activateTab("pane-2", 1);
  expect(store.activeTab?.lineWrapping).toBe(false);
  store.closeTab(1, "pane-2");
  store.closeTab(1, "pane-1");
  await store.openFile(1);
  expect(store.activeTab?.lineWrapping).toBe(true);
});

test("立即创建加载标签；重复打开共享请求", async () => {
  const pending = deferred<ReturnType<typeof meta>>();
  api.GetFileBasic.mockReturnValue(pending.promise);
  const store = useEditorStore();
  const opening = store.openFile(1);
  expect(store.activeTab?.title).toBe("1.lst");
  expect(store.activeTab?.loading).toBe(true);
  expect(store.activeTab?.editable).toBe(false);
  await store.openFile(1);
  expect(api.GetFileBasic).toHaveBeenCalledTimes(1);
  pending.resolve(meta(1));
  await opening;
  expect(store.tabs).toHaveLength(1);
  expect(store.activeTab?.loading).toBe(false);
  expect(store.activeTab?.text).toBe(meta(1).text);
});

test("请求完成不抢回用户后来选中的标签", async () => {
  const first = deferred<ReturnType<typeof meta>>();
  api.GetFileBasic.mockImplementation((index) => index === 1 ? first.promise : Promise.resolve(meta(index)));
  const store = useEditorStore();
  const opening = store.openFile(1);
  await store.openFile(2);
  first.resolve(meta(1));
  await opening;
  expect(store.activeKey).toBe(2);
});

test("关闭后重开同一文件，旧请求不能覆盖新标签", async () => {
  const first = deferred<ReturnType<typeof meta>>();
  api.GetFileBasic.mockReturnValueOnce(first.promise).mockResolvedValueOnce(meta(1, "new"));
  const store = useEditorStore();
  const opening = store.openFile(1);
  await nextTick();
  store.closeTab(1);
  await store.openFile(1);
  first.resolve(meta(1, "stale"));
  await opening;
  expect(store.tabs).toHaveLength(1);
  expect(store.activeTab?.text).toBe("new");
});

test("加载失败保留标签并支持重试", async () => {
  api.GetFileBasic.mockRejectedValueOnce(new Error("读取失败")).mockResolvedValueOnce(meta(1));
  const store = useEditorStore();
  await store.openFile(1);
  expect(store.activeTab?.loadError).toBe("读取失败");
  expect(store.opening).toBe(false);
  await store.retryOpenFile(1);
  expect(store.activeTab?.loadError).toBeNull();
  expect(store.activeTab?.editable).toBe(true);
});

test("元数据未返回时正文已可编辑；延迟元数据不覆盖草稿", async () => {
  const pending = deferred<ReturnType<typeof meta>>();
  api.GetFileBasic.mockResolvedValue(meta(1, "original"));
  api.GetFile.mockReturnValue(pending.promise);
  const store = useEditorStore();
  await store.openFile(1);
  expect(store.activeTab?.loading).toBe(false);
  expect(store.activeTab?.editable).toBe(true);
  expect(store.activeTab?.text).toBe("original");
  store.updateContent(1, "draft");
  const enriched = { ...meta(1, "outdated"), icon: "icon.png" };
  pending.resolve(enriched);
  await vi.waitFor(() => expect(store.activeTab?.icon).toBe("icon.png"));
  expect(store.activeTab?.text).toBe("draft");
  expect(store.activeTab?.original).toBe("original");
});

test("元数据加载失败不影响正文与编辑状态", async () => {
  api.GetFileBasic.mockResolvedValue(meta(1));
  api.GetFile.mockRejectedValue(new Error("标注读取失败"));
  const store = useEditorStore();
  await store.openFile(1);
  await nextTick();
  expect(api.GetFile).toHaveBeenCalledTimes(1);
  expect(store.activeTab?.loadError).toBeNull();
  expect(store.activeTab?.text).toBe(meta(1).text);
  expect(store.activeTab?.editable).toBe(true);
  expect(store.activeTab?.loading).toBe(false);
});

test("分屏共享加载结果，关闭其中一处不会丢失文件", async () => {
  const pending = deferred<ReturnType<typeof meta>>();
  api.GetFileBasic.mockReturnValue(pending.promise);
  const store = useEditorStore();
  const opening = store.openFile(1);
  store.split("columns");
  store.closeTab(1, "pane-1");
  pending.resolve(meta(1));
  await opening;
  expect(store.activeTab?.text).toBe(meta(1).text);
  expect(store.activeTab?.loading).toBe(false);
});

test("临时隐藏标注只作用于当前文件，分屏共享，关闭后重开恢复", async () => {
  api.GetFileBasic.mockImplementation((index) => Promise.resolve(meta(index)));
  const store = useEditorStore();
  await store.openFile(1);
  await store.openFile(2);

  store.toggleAnnotationsHidden(1);
  expect(store.tabs.find((tab) => tab.index === 1)?.annotationsHidden).toBe(true);
  expect(store.activeTab?.annotationsHidden).toBe(false);

  store.activateTab("pane-1", 1);
  store.split("columns");
  expect(store.activeTab?.annotationsHidden).toBe(true);
  store.closeTab(1, "pane-2");
  expect(store.tabs.find((tab) => tab.index === 1)?.annotationsHidden).toBe(true);

  store.closeTab(1, "pane-1");
  await store.openFile(1);
  expect(store.activeTab?.annotationsHidden).toBe(false);
});

test("加载中的标签也受数量上限约束", async () => {
  api.GetFileBasic.mockImplementation(async (index) => meta(index));
  const store = useEditorStore();
  const openings = Array.from({ length: 20 }, (_, index) => store.openFile(index));
  await expect(store.openFile(21)).rejects.toThrow("上限 20");
  expect(store.tabs).toHaveLength(20);
  await Promise.all(openings);
});

test("加载标签立即关闭时不发起读取，也不恢复标签", async () => {
  const store = useEditorStore();
  const opening = store.openFile(1);
  store.closeAllTabs();
  await opening;
  expect(api.GetFileBasic).not.toHaveBeenCalled();
  expect(api.GetFile).not.toHaveBeenCalled();
  expect(store.tabs).toHaveLength(0);
  expect(store.opening).toBe(false);
});

test("多个文件同时加载时各自维护状态", async () => {
  const first = deferred<ReturnType<typeof meta>>();
  const second = deferred<ReturnType<typeof meta>>();
  api.GetFileBasic.mockImplementation((index) => index === 1 ? first.promise : second.promise);
  const store = useEditorStore();
  const openingFirst = store.openFile(1);
  const openingSecond = store.openFile(2);
  second.resolve(meta(2));
  await openingSecond;
  expect(store.tabs[0].loading).toBe(true);
  expect(store.tabs[1].loading).toBe(false);
  expect(store.opening).toBe(true);
  first.resolve(meta(1));
  await openingFirst;
  expect(store.opening).toBe(false);
});

test("商店修改携带未保存草稿并同步所有受影响标签，不写磁盘", async () => {
  api.GetFileBasic.mockImplementation(async (index) => meta(index, "original"));
  const store = useEditorStore();
  await store.openFile(1); await store.openFile(2);
  store.updateContent(1, "shop draft"); store.updateContent(2, "item draft");
  const pending = deferred<any>(); api.ApplyShopEdit.mockReturnValue(pending.promise);
  const request = { fileIndex: 1, path: "map/1.lst", text: "shop draft", revision: 1, action: "edit-item" } as any;
  const applying = store.applyShopEdit(request); await nextTick();
  expect(store.guiApplying).toBe(true);
  expect(api.ApplyShopEdit.mock.calls[0][0].drafts).toEqual([
    { fileIndex: 1, path: "map/1.lst", text: "shop draft" }, { fileIndex: 2, path: "map/2.lst", text: "item draft" },
  ]);
  store.updateContent(1, "must not change during apply");
  expect(store.tabs.find((tab) => tab.index === 1)?.text).toBe("shop draft");
  api.GetFile.mockImplementation(async (index) => ({ ...meta(index, `after ${index}`), modified: true }));
  pending.resolve({ revision: 2, files: [1, 2].map((index) => ({ fileIndex: index, path: `map/${index}.lst`, beforeText: index === 1 ? "shop draft" : "item draft", text: `after ${index}` })) });
  await applying;
  for (const tab of store.tabs) { expect(tab.text).toBe(`after ${tab.index}`); expect(tab.original).toBe(tab.text); expect(tab.modified).toBe(true); }
  expect(store.guiApplying).toBe(false);
});

test("商店草稿过期或提交失败保留现有内容并释放锁", async () => {
  api.GetFileBasic.mockResolvedValue(meta(1, "new draft"));
  const store = useEditorStore(); await store.openFile(1);
  const request = { fileIndex: 1, path: "map/1.lst", text: "old draft" } as any;
  await expect(store.applyShopEdit(request)).rejects.toThrow("草稿已变化");
  expect(api.ApplyShopEdit).not.toHaveBeenCalled();
  api.ApplyShopEdit.mockRejectedValue(new Error("已过期"));
  await expect(store.applyShopEdit({ ...request, text: "new draft" })).rejects.toThrow("已过期");
  expect(store.tabs[0].text).toBe("new draft");
  expect(store.guiApplying).toBe(false); expect(store.saving).toBe(false);
});
