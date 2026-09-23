import { expect, test } from "vitest";
import { ChangeSet } from "@codemirror/state";
import { annotationAt, indexAnnotations, referenceAt } from "../src/editorAnnotations";
import type { EditorAnnotation } from "../bindings/pvfine/services/models";

function annotation(start: number, end: number, targetFileIndex = 7): EditorAnnotation {
  return { start, end, targetFileIndex, title: "地图", content: "标注", type: "reference", ruleIds: [], image: null, inlineImage: false, placeholder: null };
}

test("插入后链接位置跟随文本，标注对象不被复制", () => {
  const item = annotation(10, 20);
  const ranges = indexAnnotations([item], 100).map(ChangeSet.of({ from: 0, insert: "abc" }, 100));
  expect(referenceAt(ranges, 12)).toBeUndefined();
  expect(referenceAt(ranges, 13)).toBe(item);
  expect(referenceAt(ranges, 23)).toBeUndefined();
});

test("插入边界不扩张标注，删除目标后链接消失", () => {
  const item = annotation(10, 20);
  const ranges = indexAnnotations([item], 100);
  const inserted = ranges.map(ChangeSet.of([{ from: 10, insert: "x" }, { from: 20, insert: "y" }], 100));
  expect(referenceAt(inserted, 10)).toBeUndefined();
  expect(referenceAt(inserted, 11)).toBe(item);
  expect(referenceAt(inserted, 21)).toBeUndefined();
  const deleted = ranges.map(ChangeSet.of({ from: 10, to: 20 }, 100));
  expect(referenceAt(deleted, 10)).toBeUndefined();
});

test("非链接和越界范围不会误触发跳转", () => {
  const ranges = indexAnnotations([annotation(-20, 2, -1), annotation(200, 300)], 100);
  expect(referenceAt(ranges, 0)).toBeUndefined();
  expect(referenceAt(ranges, 100)).toBeUndefined();
});

test("隐藏标注时仍能按文本范围定位非链接标注", () => {
  const item = annotation(10, 20, -1);
  const ranges = indexAnnotations([item], 100).map(ChangeSet.of({ from: 0, insert: "abc" }, 100));
  expect(annotationAt(ranges, 12)).toBeUndefined();
  expect(annotationAt(ranges, 13)).toBe(item);
  expect(annotationAt(ranges, 22)).toBe(item);
  expect(annotationAt(ranges, 23)).toBeUndefined();
});

test("五万条标注编辑后仍正确定位远端链接并共享对象", () => {
  const items = Array.from({ length: 50000 }, (_, index) => annotation(index * 30, index * 30 + 20, index));
  const ranges = indexAnnotations(items, 1500000).map(ChangeSet.of({ from: 0, insert: "abc" }, 1500000));
  expect(referenceAt(ranges, 49999 * 30 + 3)).toBe(items[49999]);
  expect(ranges.size).toBe(50000);
});
