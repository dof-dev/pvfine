import { beforeEach, describe, expect, it, vi } from "vitest";
import { createPinia, setActivePinia } from "pinia";
import { nextTick, ref } from "vue";

const api = vi.hoisted(() => ({
  GetFile: vi.fn(),
  ResolveFiles: vi.fn(),
  ApplyWorldDropEdit: vi.fn(),
  showArchiveEditor: vi.fn(),
  refreshInfo: vi.fn(),
  SetText: vi.fn(),
  GetAnnotations: vi.fn(),
  Save: vi.fn(),
  SaveAsDialog: vi.fn(),
}));

vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn() } }));
vi.mock("../bindings/pvfine/services", () => ({
  EditorService: {
    GetFile: api.GetFile,
    SetText: api.SetText,
    GetAnnotations: api.GetAnnotations,
    Save: api.Save,
    SaveAsDialog: api.SaveAsDialog,
  },
  ArchiveService: { ResolveFiles: api.ResolveFiles },
  FileGUIService: { ApplyWorldDropEdit: api.ApplyWorldDropEdit },
}));
vi.mock("../src/stores/archive", () => ({
  useArchiveStore: () => ({ refreshInfo: api.refreshInfo, open: true }),
}));
vi.mock("../src/stores/script", () => ({ useScriptStore: () => api }));
vi.mock("../src/stores/explorer", () => ({
  useExplorerStore: () => ({
    getFilePath: (index: number) => (index === 10 ? "etc/worlddrop.etc" : `file/${index}.txt`),
    selectedKey: null,
    refreshTreeTags: vi.fn(),
  }),
}));

import { useEditorStore } from "../src/stores/editor";

describe("world drop entry & editor store integration", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.clearAllMocks();
  });

  it("openWorldDrop: 归档中缺少 etc/worlddrop.etc 时明确抛错", async () => {
    api.ResolveFiles.mockResolvedValue([]);
    const store = useEditorStore();

    await expect(store.openWorldDrop()).rejects.toThrow("未找到全局掉率文件 etc/worlddrop.etc");
  });

  it("openWorldDrop: 成功解析后打开标签并切换到 GUI 模式", async () => {
    api.ResolveFiles.mockResolvedValue([
      { name: "worlddrop.etc", path: "etc/worlddrop.etc", isDir: false, fileIndex: 10 },
    ]);
    api.GetFile.mockResolvedValue({
      index: 10,
      path: "etc/worlddrop.etc",
      text: "[world drop]\n\t1\t0\n\t-1\n",
      editable: true,
      dataType: 1,
      size: 30,
      tags: [],
      annotations: [],
      modified: false,
    });

    const store = useEditorStore();
    const result = await store.openWorldDrop();
    expect(result).toBe(true);

    // 标签应已打开且活动
    expect(store.activeTab?.index).toBe(10);
    expect(store.activeTab?.path).toBe("etc/worlddrop.etc");

    // 目标窗格中的模式应为 gui
    const modes = store.getGUIModes(store.activePaneId);
    expect(modes.entries.get(10)?.mode).toBe("gui");
  });

  it("openWorldDrop: 已打开的标签会被复用并切换到 GUI 模式", async () => {
    api.ResolveFiles.mockResolvedValue([
      { name: "worlddrop.etc", path: "etc/worlddrop.etc", isDir: false, fileIndex: 10 },
    ]);
    api.GetFile.mockResolvedValue({
      index: 10,
      path: "etc/worlddrop.etc",
      text: "[world drop]\n\t1\t0\n\t-1\n",
      editable: true,
      dataType: 1,
      size: 30,
      tags: [],
      annotations: [],
      modified: false,
    });

    const store = useEditorStore();
    // 先以常规文本方式打开
    await store.openFile(10);
    const modes = store.getGUIModes(store.activePaneId);
    expect(modes.entries.get(10)?.mode ?? "text").toBe("text");

    // 点击全局掉率入口复用
    await store.openWorldDrop();
    expect(store.tabs.filter((t) => t.index === 10)).toHaveLength(1);
    expect(modes.entries.get(10)?.mode).toBe("gui");
  });

  it("applyWorldDropEdit: 草稿文本不匹配时拒绝提交；成功后更新标签内容与 modified", async () => {
    api.GetFile.mockResolvedValue({
      index: 10,
      path: "etc/worlddrop.etc",
      text: "original text",
      editable: true,
      dataType: 1,
      size: 30,
      tags: [],
      annotations: [],
      modified: false,
    });

    const store = useEditorStore();
    await store.openFile(10);
    const tab = store.tabs.find((t) => t.index === 10)!;

    // 草稿已被修改而请求带着旧文本
    tab.text = "modified draft";
    await expect(
      store.applyWorldDropEdit({
        fileIndex: 10,
        path: "etc/worlddrop.etc",
        text: "stale text",
        revision: 1,
        levels: [],
      }),
    ).rejects.toThrow("全局掉率草稿已变化");

    // 正常匹配提交
    const newText = "[world drop]\n\t5\t0\n\t100\t50\n\t-1\n";
    api.ApplyWorldDropEdit.mockResolvedValue({
      revision: 2,
      modifiedCount: 1,
      files: [
        {
          fileIndex: 10,
          path: "etc/worlddrop.etc",
          beforeText: "modified draft",
          text: newText,
        },
      ],
    });
    api.GetFile.mockResolvedValue({
      index: 10,
      path: "etc/worlddrop.etc",
      text: newText,
      editable: true,
      dataType: 1,
      size: newText.length,
      tags: [],
      annotations: [],
      modified: true,
    });

    const result = await store.applyWorldDropEdit({
      fileIndex: 10,
      path: "etc/worlddrop.etc",
      text: "modified draft",
      revision: 1,
      levels: [],
    });

    expect(result.revision).toBe(2);
    expect(tab.text).toBe(newText);
    expect(tab.original).toBe(newText);
    expect(tab.modified).toBe(true);
  });

  it("registerGUIDirtyChecker: GUI 未保存修改能够被 isDirty 与 dirtyCount 感知", async () => {
    api.GetFile.mockResolvedValue({
      index: 10,
      path: "etc/worlddrop.etc",
      text: "original text",
      editable: true,
      dataType: 1,
      size: 30,
      tags: [],
      annotations: [],
      modified: false,
    });

    const store = useEditorStore();
    await store.openFile(10);
    const tab = store.tabs.find((t) => t.index === 10)!;

    expect(store.dirtyCount).toBe(0);

    const guiDirty = ref(true);
    const unregister = store.registerGUIDirtyChecker(10, () => guiDirty.value);

    expect(store.dirtyCount).toBe(1);

    guiDirty.value = false;
    expect(store.dirtyCount).toBe(0);

    unregister();
    guiDirty.value = true;
    expect(store.dirtyCount).toBe(0);
  });

  it("registerGUIDirtyChecker: 支持多分屏独立注册，任意窗格有修改即判定为 dirty，注销一个不影响其他", async () => {
    api.GetFile.mockResolvedValue({
      index: 10,
      path: "etc/worlddrop.etc",
      text: "original text",
      editable: true,
      dataType: 1,
      size: 30,
      tags: [],
      annotations: [],
      modified: false,
    });

    const store = useEditorStore();
    await store.openFile(10);
    expect(store.dirtyCount).toBe(0);

    const pane1Dirty = ref(false);
    const pane2Dirty = ref(false);

    const unreg1 = store.registerGUIDirtyChecker(10, () => pane1Dirty.value);
    const unreg2 = store.registerGUIDirtyChecker(10, () => pane2Dirty.value);

    // 此时两个都为 false
    expect(store.hasGUIDirty(10)).toBe(false);
    expect(store.dirtyCount).toBe(0);

    // 窗格 1 修改
    pane1Dirty.value = true;
    expect(store.hasGUIDirty(10)).toBe(true);
    expect(store.dirtyCount).toBe(1);

    // 窗格 2 也修改
    pane2Dirty.value = true;
    expect(store.hasGUIDirty(10)).toBe(true);

    // 窗格 1 恢复，但窗格 2 仍然 dirty
    pane1Dirty.value = false;
    expect(store.hasGUIDirty(10)).toBe(true);
    expect(store.dirtyCount).toBe(1);

    // 窗格 2 恢复
    pane2Dirty.value = false;
    expect(store.hasGUIDirty(10)).toBe(false);
    expect(store.dirtyCount).toBe(0);

    // 窗格 1 再次 dirty，然后注销窗格 2，窗格 1 仍生效
    pane1Dirty.value = true;
    unreg2();
    expect(store.hasGUIDirty(10)).toBe(true);

    // 注销窗格 1
    unreg1();
    expect(store.hasGUIDirty(10)).toBe(false);
  });

  it("save / saveAs / saveActiveTab: 阻止存在 GUI 未应用修改时的普通保存，并给出明确提示；草稿保存保持正常", async () => {
    api.GetFile.mockResolvedValue({
      index: 10,
      path: "etc/worlddrop.etc",
      text: "original text",
      editable: true,
      dataType: 1,
      size: 30,
      tags: [],
      annotations: [],
      modified: false,
    });
    api.SetText.mockResolvedValue(undefined);
    api.GetAnnotations.mockResolvedValue([]);
    api.Save.mockResolvedValue({ files: [] });
    api.SaveAsDialog.mockResolvedValue("/path/to/new.pvf");

    const store = useEditorStore();
    await store.openFile(10);
    const tab = store.tabs.find((t) => t.index === 10)!;

    // 仅在 GUI 中有未应用修改（tab.text === tab.original）
    const guiDirty = ref(true);
    const unreg = store.registerGUIDirtyChecker(10, () => guiDirty.value);

    expect(store.hasGUIOnlyDirty(tab)).toBe(true);

    // 尝试 saveActiveTab
    await expect(store.saveActiveTab()).rejects.toThrow(
      "当前文件存在未应用的界面修改，请先在界面中点击【应用修改】后再保存",
    );
    expect(api.SetText).not.toHaveBeenCalled();

    // 尝试 save
    await expect(store.save()).rejects.toThrow(
      "存在未应用的界面修改，请先在界面中点击【应用修改】后再保存",
    );
    expect(api.Save).not.toHaveBeenCalled();

    // 尝试 saveAs
    await expect(store.saveAs()).rejects.toThrow(
      "存在未应用的界面修改，请先在界面中点击【应用修改】后再保存",
    );
    expect(api.SaveAsDialog).not.toHaveBeenCalled();

    // 取消 GUI dirty 后，如果仅有普通文本草稿修改，正常保存成功
    unreg();
    expect(store.hasGUIOnlyDirty(tab)).toBe(false);

    tab.text = "edited text draft";
    expect(store.isTextDirty(tab)).toBe(true);

    const activeSaved = await store.saveActiveTab();
    expect(activeSaved).toBe(true);
    expect(api.SetText).toHaveBeenCalledWith(10, "edited text draft");

    tab.text = "another draft";
    const saveResult = await store.save();
    expect(saveResult).toBeDefined();
    expect(api.Save).toHaveBeenCalled();
  });

  it("save / saveAs / saveActiveTab: 文本草稿与 GUI 表单同时 dirty 时，同样必须阻止保存并禁止静默遗留 GUI 修改", async () => {
    api.GetFile.mockResolvedValue({
      index: 10,
      path: "etc/worlddrop.etc",
      text: "original text",
      editable: true,
      dataType: 1,
      size: 30,
      tags: [],
      annotations: [],
      modified: false,
    });
    api.SetText.mockResolvedValue(undefined);
    api.GetAnnotations.mockResolvedValue([]);
    api.Save.mockResolvedValue({ files: [] });
    api.SaveAsDialog.mockResolvedValue("/path/to/new.pvf");

    const store = useEditorStore();
    await store.openFile(10);
    const tab = store.tabs.find((t) => t.index === 10)!;

    // 模拟同时修改了 DSL 文本与 GUI 表单
    tab.text = "modified text in dsl";
    expect(store.isTextDirty(tab)).toBe(true);

    const guiDirty = ref(true);
    const unreg = store.registerGUIDirtyChecker(10, () => guiDirty.value);
    expect(store.hasGUIDirty(tab.index)).toBe(true);
    // hasGUIOnlyDirty 为 false（因为文本也是脏的），但仍有 GUI 脏数据待应用
    expect(store.hasGUIOnlyDirty(tab)).toBe(false);

    // 1. saveActiveTab 必须拦截，禁止只保存 DSL 文本而静默遗留 GUI 修改
    await expect(store.saveActiveTab()).rejects.toThrow(
      "当前文件存在未应用的界面修改，请先在界面中点击【应用修改】后再保存",
    );
    expect(api.SetText).not.toHaveBeenCalled();

    // 2. save 必须拦截
    await expect(store.save()).rejects.toThrow(
      "存在未应用的界面修改，请先在界面中点击【应用修改】后再保存",
    );
    expect(api.Save).not.toHaveBeenCalled();

    // 3. saveAs 必须拦截
    await expect(store.saveAs()).rejects.toThrow(
      "存在未应用的界面修改，请先在界面中点击【应用修改】后再保存",
    );
    expect(api.SaveAsDialog).not.toHaveBeenCalled();

    // 4. 用户应用或放弃 GUI 修改后（GUI 不再 dirty），普通的文本草稿保存才能顺利执行
    guiDirty.value = false;
    expect(store.hasGUIDirty(tab.index)).toBe(false);
    expect(store.isTextDirty(tab)).toBe(true);

    const saved = await store.saveActiveTab();
    expect(saved).toBe(true);
    expect(api.SetText).toHaveBeenCalledWith(10, "modified text in dsl");

    unreg();
  });
});
