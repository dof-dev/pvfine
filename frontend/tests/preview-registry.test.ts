import { expect, test } from "vitest";
import { getPreviewProvider } from "../src/previews/registry";
import type { PreviewFile } from "../src/previews/types";

const file: PreviewFile = { index: 1, path: "equipment/test.equ", text: "", editable: true };

test("只有包含 equipment type section 的 .equ 文件启用装备预览", () => {
  expect(getPreviewProvider(file)).toBeNull();
  expect(getPreviewProvider({ ...file, text: "[name]\n`测试`\n[equipment type]\n`[armor]`" })?.id).toBe("equ");
  expect(getPreviewProvider({ ...file, text: "[equipment type]\r\n`[weapon]`" })?.id).toBe("equ");
  expect(getPreviewProvider({ ...file, text: "# [equipment type]\n[name]\n`测试`" })).toBeNull();
  expect(getPreviewProvider({ ...file, text: "[name]\n`[equipment type]`" })).toBeNull();
  expect(getPreviewProvider({ ...file, path: "equipment/test.txt", text: "[equipment type]\n`[armor]`" })).toBeNull();
});
