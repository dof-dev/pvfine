import { expect, test } from "vitest";
import { EditorState } from "@codemirror/state";
import { ensureSyntaxTree } from "@codemirror/language";
import { highlightTree, tags, tagHighlighter } from "@lezer/highlight";
import { archiveEditorLanguage, luaLanguage } from "../src/luaLanguage";

test("归档中的 Lua 后缀识别兼容大小写，其余文件保持 PVF 语法", () => {
  expect(archiveEditorLanguage("scripts/example.lua")).toBe("lua");
  expect(archiveEditorLanguage("scripts/example.LUA")).toBe("lua");
  expect(archiveEditorLanguage("scripts/example.lua.bak")).toBe("pvf");
  expect(archiveEditorLanguage("equipment/example.equ")).toBe("pvf");
});

test("Lua 高亮识别关键字、数字、转义字符串以及跨行长字符串和注释", () => {
  const doc = [
    "local count = 42",
    'local text = "hello\\\"world"',
    "-- single line comment",
    "local long = [=[first",
    "second]=]",
    "--[==[block",
    "comment]==]",
    "return count",
  ].join("\n");
  const state = EditorState.create({ doc, extensions: luaLanguage.extension });
  const tree = ensureSyntaxTree(state, doc.length, 1000);
  expect(tree).not.toBeNull();
  const highlighted: Record<string, string[]> = {};
  highlightTree(tree!, tagHighlighter([
    { tag: tags.keyword, class: "keyword" },
    { tag: tags.number, class: "number" },
    { tag: tags.string, class: "string" },
    { tag: tags.comment, class: "comment" },
  ]), (from, to, style) => {
    (highlighted[style] ??= []).push(doc.slice(from, to));
  });
  expect(highlighted.keyword).toContain("local");
  expect(highlighted.keyword).toContain("return");
  expect(highlighted.number).toEqual(["42"]);
  expect(highlighted.string?.join("\n")).toContain('[=[first\nsecond]=]');
  expect(highlighted.string).toContain('"hello\\\"world"');
  expect(highlighted.comment).toContain("-- single line comment");
  expect(highlighted.comment?.join("\n")).toContain("--[==[block\ncomment]==]");
});
