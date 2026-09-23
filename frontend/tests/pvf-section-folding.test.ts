import { expect, test } from "vitest";
import { EditorState } from "@codemirror/state";
import { foldable } from "@codemirror/language";
import { pvfSectionFolding } from "../src/pvfSectionFolding";

function foldedText(state: EditorState, lineNumber: number): string | null {
  const line = state.doc.line(lineNumber);
  const range = foldable(state, line.from, line.to);
  return range ? state.doc.sliceString(range.from, range.to) : null;
}

test("成对 section 包含嵌套内容与结束标签，无结束标签停在下一个同级 section", () => {
  const state = EditorState.create({
    doc: "[outer]\n[parent]\n1\n[child]\n2\n[/child]\n[second]\n3\n[/outer]\n[after]\n4",
    extensions: pvfSectionFolding,
  });

  expect(foldedText(state, 1)).toBe("\n[parent]\n1\n[child]\n2\n[/child]\n[second]\n3\n[/outer]");
  expect(foldedText(state, 2)).toBe("\n1");
  expect(foldedText(state, 4)).toBe("\n2\n[/child]");
  expect(foldedText(state, 7)).toBe("\n3");
  expect(foldedText(state, 10)).toBe("\n4");
});

test("忽略注释和字符串中的标签，文档编辑后更新折叠边界", () => {
  const state = EditorState.create({
    doc: "[first]\n`[fake]`\n# [comment]\n[second]\n2",
    extensions: pvfSectionFolding,
  });
  expect(foldedText(state, 1)).toBe("\n`[fake]`\n# [comment]");
  expect(foldedText(state, 2)).toBeNull();
  expect(foldedText(state, 3)).toBeNull();

  const next = state.update({ changes: { from: state.doc.length, insert: "\n[third]\n3" } }).state;
  expect(foldedText(next, 4)).toBe("\n2");
  expect(foldedText(next, 6)).toBe("\n3");
});
