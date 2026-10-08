import { StreamLanguage } from "@codemirror/language";
import { lua } from "@codemirror/legacy-modes/mode/lua";

export const luaLanguage = StreamLanguage.define(lua);

export function archiveEditorLanguage(path: string): "pvf" | "lua" {
  return /\.lua$/i.test(path) ? "lua" : "pvf";
}
