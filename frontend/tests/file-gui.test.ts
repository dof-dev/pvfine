import { expect, test } from "vitest";
import { createGUIModes, createShopSession } from "../src/gui/state";
import { getGUIProvider } from "../src/gui/registry";
import type { ShopDocument } from "../bindings/pvfine/services/models";

const file = { index: 1, path: "itemshop/test.shp", text: "unsaved draft", editable: true };
function document(name = "商店"): ShopDocument {
  return {
    name, categoryType: "basic job", categories: [{ id: "0", name: "鬼剑士" }, { id: "1", name: "格斗家" }],
    tabs: [0, 1].map((sourceStart) => ({ name: `分页${sourceStart}`, sourceStart, groups: [] })), issues: [],
  };
}
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

test("仅支持的文件有 GUI，默认文本，各窗格独立并可清理", () => {
  expect(getGUIProvider(file)?.readOnly).toBe(true);
  expect(getGUIProvider({ ...file, path: "itemshop/SHOP.SHP" })?.id).toBe("shop");
  expect(getGUIProvider({ ...file, path: "a.equ" })).toBeNull();
  const left = createGUIModes(), right = createGUIModes();
  expect(left.entries.get(1)?.mode ?? "text").toBe("text");
  left.set(1, "gui");
  expect(right.entries.has(1)).toBe(false);
  left.set(1, "text");
  expect(left.entries.get(1)?.opened).toBe(true);
  left.prune([]);
  expect(left.entries.size).toBe(0);
  right.set(1, "gui"); right.reset();
  expect(right.entries.size).toBe(0);
});

test("慢加载立即显示状态，读取草稿，默认第一分类；刷新保留选择", async () => {
  const pending = deferred<ShopDocument>();
  const calls: unknown[] = [];
  const session = createShopSession(async (index, text) => { calls.push([index, text]); return pending.promise; });
  const loading = session.load(file, "archive1");
  expect(session.loading.value).toBe(true);
  expect(session.document.value).toBeNull();
  expect(calls).toEqual([[1, "unsaved draft"]]);
  pending.resolve(document()); await loading;
  expect(session.categoryID.value).toBe("0");
  session.tabIndex.value = 1; session.categoryID.value = "1";
  await session.load({ ...file, text: "new draft" }, "archive1");
  expect(session.tabIndex.value).toBe(1);
  expect(session.categoryID.value).toBe("1");
  expect(file.text).toBe("unsaved draft");
});

test("失败停止加载，保留上次内容，重试恢复", async () => {
  let fail = false;
  const session = createShopSession(async () => { if (fail) throw new Error("读取失败"); return document(); });
  await session.load(file, "archive1"); fail = true;
  await session.load(file, "archive1");
  expect(session.loading.value).toBe(false);
  expect(session.document.value?.name).toBe("商店");
  expect(session.error.value).toBe("读取失败");
  fail = false; await session.load(file, "archive1");
  expect(session.error.value).toBe("");
});

test("切换文件或同路径重新打开归档时清空旧结果，过期请求不能覆盖", async () => {
  const a = deferred<ShopDocument>(), b = deferred<ShopDocument>();
  let count = 0;
  const session = createShopSession(() => (++count === 1 ? a : b).promise);
  const first = session.load(file, "epoch1");
  const second = session.load(file, "epoch2");
  expect(session.document.value).toBeNull();
  b.resolve(document("新归档")); await second;
  a.resolve(document("旧归档")); await first;
  expect(session.document.value?.name).toBe("新归档");
  const third = session.load({ ...file, index: 2 }, "epoch2");
  expect(session.document.value).toBeNull();
  await third;
});

test("卸载或退出 GUI 后忽略请求；空响应明确失败", async () => {
  const pending = deferred<ShopDocument>();
  const session = createShopSession(() => pending.promise);
  const loading = session.load(file, "epoch1");
  session.invalidate(true);
  pending.resolve(document()); await loading;
  expect(session.document.value).toBeNull();
  expect(session.loading.value).toBe(false);
  const empty = createShopSession(async () => null);
  await empty.load(file, "epoch1");
  expect(empty.error.value).toBe("未收到商店数据");
  expect(empty.loading.value).toBe(false);
});
