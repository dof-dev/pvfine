import { beforeEach, describe, expect, it, vi } from "vitest";
import { createPinia, setActivePinia } from "pinia";
import { nextTick } from "vue";

const api = vi.hoisted(() => ({
  GetFileBasic: vi.fn(), GetFile: vi.fn(), SetText: vi.fn(), GetAnnotations: vi.fn(),
  Save: vi.fn(), ResolveFiles: vi.fn(), refreshInfo: vi.fn(), showArchiveEditor: vi.fn(),
  events: new Map<string, Array<() => void>>(),
}));
vi.mock("@wailsio/runtime", () => ({ Events: { On: (name: string, fn: () => void) => {
  api.events.set(name, [...(api.events.get(name) ?? []), fn]);
} } }));
vi.mock("../bindings/pvfine/services", () => ({ EditorService: api, ArchiveService: api, FileGUIService: {} }));
vi.mock("../src/stores/archive", () => ({ useArchiveStore: () => ({ refreshInfo: api.refreshInfo }) }));
vi.mock("../src/stores/script", () => ({ useScriptStore: () => api }));
vi.mock("../src/stores/explorer", () => ({ useExplorerStore: () => ({ getFilePath: (index: number) => `skill/test/${index}.skl`, refreshTreeTags: vi.fn() }) }));
import { useEditorStore } from "../src/stores/editor";

function meta(index: number, text = "original", path = `skill/test/${index}.skl`) {
  return { index, path, text, editable: true, dataType: 1, size: text.length, tags: [], annotations: [], modified: false };
}
beforeEach(() => {
  setActivePinia(createPinia());
  vi.resetAllMocks();
  api.events.clear();
  api.GetFileBasic.mockImplementation(async (index) => meta(index));
  api.GetFile.mockResolvedValue(null);
  api.GetAnnotations.mockResolvedValue([]);
  api.SetText.mockResolvedValue(undefined);
});

describe("通用技能 GUI 独立草稿", () => {
  it("浏览超过 20 个技能也不创建标签，切换和关闭 GUI 后仍保留草稿", async () => {
    const editor = useEditorStore();
    for (let index = 1; index <= 25; index++) await editor.openGUIFile(index);
    editor.updateContent(1, "modified");
    editor.closeAllTabs();
    expect((await editor.openGUIFile(1)).text).toBe("modified");
    expect(editor.tabs).toHaveLength(0);
    expect(editor.activeKey).toBeNull();
    expect(api.showArchiveEditor).not.toHaveBeenCalled();
    expect(editor.dirtyCount).toBe(1);
    expect(await editor.saveFileTab(1)).toBe(true);
    expect(api.SetText).toHaveBeenCalledExactlyOnceWith(1, "modified");
    expect(editor.tabs).toHaveLength(0);
    expect(editor.dirtyCount).toBe(0);
  });

  it("显式打开编辑器才创建标签，共用原草稿并避免重复保存", async () => {
    const editor = useEditorStore();
    const draft = await editor.openGUIFile(1);
    editor.updateContent(1, "GUI draft");
    await editor.openFile(1);
    expect(editor.tabs).toHaveLength(1);
    expect(editor.tabs[0]).toBe(draft);
    expect(editor.activeTab?.text).toBe("GUI draft");
    editor.updateContent(1, "text draft");
    expect(await editor.openGUIFile(1)).toBe(draft);
    expect(draft.text).toBe("text draft");
    expect(editor.dirtyCount).toBe(1);
    expect(await editor.saveAllDirty()).toBe(1);
    expect(api.SetText).toHaveBeenCalledTimes(1);
    expect(api.GetFileBasic).toHaveBeenCalledTimes(1);
  });

  it("主工具栏保存包含独立草稿，写入失败时保留草稿与脏状态", async () => {
    const editor = useEditorStore();
    await editor.openGUIFile(1);
    await editor.openGUIFile(2);
    editor.updateContent(1, "first");
    editor.updateContent(2, "second");
    api.SetText.mockRejectedValueOnce(new Error("write failed"));
    await expect(editor.saveFileTab(1)).rejects.toThrow("write failed");
    expect(editor.getFileDraft(1)?.original).toBe("original");
    expect(editor.dirtyCount).toBe(2);
    expect(editor.saving).toBe(false);
    api.Save.mockResolvedValue({ modifiedCount: 0 });
    await editor.save();
    expect(api.SetText).toHaveBeenCalledWith(1, "first");
    expect(api.SetText).toHaveBeenCalledWith(2, "second");
    expect(api.Save).toHaveBeenCalledOnce();
    expect(editor.dirtyCount).toBe(0);
    expect(editor.tabs).toHaveLength(0);
  });

  it("文件索引变化时按路径重绑定，保存使用新索引且保留草稿", async () => {
    const editor = useEditorStore();
    await editor.openGUIFile(1);
    editor.updateContent(1, "draft");
    api.ResolveFiles.mockResolvedValue([{ path: "skill/test/1.skl", fileIndex: 100, size: 8, dataType: 1, tags: [], isDir: false }]);
    await editor.refreshAfterArchiveChange();
    expect(editor.getFileDraft("skill/test/1.skl")?.index).toBe(100);
    expect(editor.getFileDraft(1)).toBeUndefined();
    expect(editor.tabs).toHaveLength(0);
    await editor.saveFileTab(100);
    expect(api.SetText).toHaveBeenCalledExactlyOnceWith(100, "draft");
  });

  it("关闭归档清空草稿，并拒绝跨归档的慢请求", async () => {
    let resolve!: (value: ReturnType<typeof meta>) => void;
    api.GetFileBasic.mockReturnValue(new Promise((done) => { resolve = done; }));
    const editor = useEditorStore();
    const pending = editor.openGUIFile(1);
    await nextTick();
    for (const fn of api.events.get("archive:closed") ?? []) fn();
    resolve(meta(1));
    await expect(pending).rejects.toThrow("归档已切换");
    expect(editor.getFileDraft(1)).toBeUndefined();
    expect(editor.dirtyCount).toBe(0);
    expect(editor.tabs).toHaveLength(0);
  });
});
