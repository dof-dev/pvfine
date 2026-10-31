import { describe, expect, it, vi } from "vitest";
import { guiProviders, getGUIProvider } from "../src/gui/registry";
import { guiPathMatches, newGUIFilePath, searchGUIFiles } from "../src/gui/launcher";
import type { SearchHit, SearchResult } from "../bindings/pvfine/services/models";

const provider = (id: string) => guiProviders.find(item => item.id === id)!;
const hit = (path: string, fileIndex = 1) => ({ path, fileIndex, name: "测试" }) as SearchHit;
const result = (hits: SearchHit[], nextCursor = -1) => ({ hits, nextCursor }) as SearchResult;

describe("文件 GUI 工具入口", () => {
  it("每个 GUI 都提供可识别的新建路径和类型搜索入口", () => {
    expect(guiProviders.map(item => item.id)).toEqual(["shop", "world-drop", "skill", "quest", "upgrade", "forge", "item", "independent-drop"]);
    for (const item of guiProviders) {
      expect(item.launcher.queries.length).toBeGreaterThan(0);
      for (const path of item.launcher.paths) {
        expect(newGUIFilePath(item, path)).toBe(path);
        expect(getGUIProvider({ index: 0, path, text: "", editable: true })?.id).toBe(item.id);
      }
    }
  });

  it("校验完整路径和准确文件类型，支持大小写及 Windows 分隔符", () => {
    expect(newGUIFilePath(provider("shop"), " /NPC\\Shop\\NEW.SHP/ ")).toBe("NPC/Shop/NEW.SHP");
    for (const path of ["", "npc//a.shp", "npc/../a.shp", "./a.shp", "npc/a.shp.txt", "npc/a.skl"]) {
      expect(() => newGUIFilePath(provider("shop"), path)).toThrow();
    }
    expect(() => newGUIFilePath(provider("forge"), "etc/new.etc")).toThrow("支持的文件类型或路径");
    expect(guiPathMatches(provider("world-drop"), "etc/otherworlddrop.etc")).toBe(false);
  });

  it("关键词结果跨过无关分页，并去重、保留后续游标", async () => {
    const search = vi.fn()
      .mockResolvedValueOnce(result([hit("npc/a.npc")], 100))
      .mockResolvedValueOnce(result([hit("npc/a.shp"), hit("npc/a.shp")], 200));
    const page = await searchGUIFiles(provider("shop"), "测试", { queryIndex: 0, offset: 0 }, search, () => true);
    expect(page.hits.map(item => item.path)).toEqual(["npc/a.shp"]);
    expect(page.next).toEqual({ queryIndex: 0, offset: 200 });
    expect(search).toHaveBeenNthCalledWith(2, "测试", 100, 100);
  });

  it("空关键词依次搜索装备和消耗品，未注册文件也可按扩展名打开", async () => {
    const search = vi.fn().mockResolvedValueOnce(result([hit("equipment/a.equ")])).mockResolvedValueOnce(result([hit("stackable/a.stk", 2)]));
    const first = await searchGUIFiles(provider("item"), "", { queryIndex: 0, offset: 0 }, search, () => true);
    expect(first.next).toEqual({ queryIndex: 1, offset: 0 });
    const second = await searchGUIFiles(provider("item"), "", first.next!, search, () => true);
    expect(second.hits[0]?.path).toBe("stackable/a.stk");
    expect(second.next).toBeNull();
    expect(search.mock.calls.map(call => call[0])).toEqual(["*.equ", "*.stk"]);
  });

  it("限定一次扫描量，保留继续搜索入口", async () => {
    const search = vi.fn().mockImplementation(async (_query, cursor) => result([hit("other.txt")], cursor + 100));
    const page = await searchGUIFiles(provider("quest"), "name", { queryIndex: 0, offset: 0 }, search, () => true);
    expect(search).toHaveBeenCalledTimes(10);
    expect(page.hits).toEqual([]);
    expect(page.next).toEqual({ queryIndex: 0, offset: 1000 });
  });

  it("归档或查询变化时丢弃旧搜索结果并停止翻页", async () => {
    const search = vi.fn().mockResolvedValue(result([hit("npc/a.shp")], 100));
    expect(await searchGUIFiles(provider("shop"), "", { queryIndex: 0, offset: 0 }, search, () => false)).toEqual({ hits: [], next: null });
    expect(search).toHaveBeenCalledTimes(1);
  });
});
