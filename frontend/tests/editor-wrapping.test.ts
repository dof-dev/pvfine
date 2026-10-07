import { expect, test } from "vitest";
import { hasLongLine, longLineThreshold } from "../src/editorWrapping";

test("empty and ordinary documents keep wrapping enabled", () => {
  expect(hasLongLine("")).toBe(false);
  expect(hasLongLine("[section]\n1 2 3\n[/section]")).toBe(false);
});

test("only lines exceeding the threshold disable wrapping", () => {
  expect(hasLongLine("x".repeat(longLineThreshold))).toBe(false);
  expect(hasLongLine("x".repeat(longLineThreshold + 1))).toBe(true);
  expect(hasLongLine("short\n" + "x".repeat(longLineThreshold + 1))).toBe(true);
});

test("LF, CRLF and CR reset the line length", () => {
  for (const separator of ["\n", "\r\n", "\r"]) {
    expect(hasLongLine(["x".repeat(longLineThreshold), "y".repeat(longLineThreshold)].join(separator))).toBe(false);
  }
});

test("large total size does not disable wrapping for short lines", () => {
  expect(hasLongLine(("x".repeat(100) + "\n").repeat(30_000))).toBe(false);
});
