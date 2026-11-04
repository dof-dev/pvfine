import { beforeEach, expect, test } from "vitest";
import { createPinia, setActivePinia } from "pinia";
import type { ImportPreview } from "../bindings/pvfine/services/models";
import { useImportStore } from "../src/stores/import";

beforeEach(() => setActivePinia(createPinia()));

function preview(): ImportPreview {
  return {
    targetDir: "", mode: "text", totalFiles: 3, importedCount: 2,
    overwrittenCount: 1, convertedCount: 1, entriesTruncated: false,
    entries: [
      { sourcePath: "/source/a.str", targetPath: "dir/a.str", overwrite: false, sourceEncoding: "GBK", dataType: 3, size: 4 },
      { sourcePath: "/source/b.str", targetPath: "dir/b.str", overwrite: true, sourceEncoding: "UTF-8", dataType: 3, size: 4 },
      { sourcePath: "/source/c.str", targetPath: "other/c.str", overwrite: false, sourceEncoding: "UTF-8", dataType: 3, size: 4 },
    ],
  };
}

test("预览默认全选，取消部分选择同步统计且只保留勾选文件", () => {
  const store = useImportStore();
  store.preview = preview();
  expect(store.allChecked).toBe(true);
  expect(store.selectedEntries).toHaveLength(3);
  store.checkedKeys = ["dir/b.str", "unknown", "dir"];
  expect(store.selectedEntries.map(entry => entry.targetPath)).toEqual(["dir/b.str"]);
  expect(store.partiallyChecked).toBe(true);
  expect(store.selectedAddedCount).toBe(0);
  expect(store.selectedOverwriteCount).toBe(1);
  expect(store.selectedConvertedCount).toBe(0);
  store.selectAll(false);
  expect(store.selectedEntries).toEqual([]);
  expect(store.allChecked).toBe(false);
  expect(store.partiallyChecked).toBe(false);
  store.selectAll(true);
  expect(store.selectedAddedCount).toBe(2);
  expect(store.selectedConvertedCount).toBe(1);
});

test("重新预览恢复默认全选，关闭时清除选择", () => {
  const store = useImportStore();
  store.preview = preview();
  store.selectAll(false);
  store.preview = preview();
  expect(store.allChecked).toBe(true);
  store.close();
  expect(store.checkedKeys).toEqual([]);
  expect(store.selectedEntries).toEqual([]);
});
