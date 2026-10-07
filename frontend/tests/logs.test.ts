import { beforeEach, expect, test } from "vitest";
import { createPinia, setActivePinia } from "pinia";
import { formatLogValue, logCapacity, useLogStore } from "../src/stores/logs";

beforeEach(() => setActivePinia(createPinia()));

function backend(id: number, level = "INFO") {
  return { id, timestamp: "2026-10-07T10:00:00Z", source: "Go", level, message: `entry ${id}` };
}

test("records while collapsed and filters exact selected levels", () => {
  const logs = useLogStore();
  logs.add("DEBUG", "test", "debug");
  logs.add("INFO", "test", "info");
  logs.add("WARN", "test", "warning");
  logs.add("ERROR", "test", new Error("failure"));
  expect(logs.expanded).toBe(false);
  expect(logs.entries).toHaveLength(4);
  expect(logs.filtered).toHaveLength(3);
  expect(logs.errorCount).toBe(1);
  logs.levels = ["ERROR", "DEBUG"];
  expect(logs.filtered.map((entry) => entry.level)).toEqual(["DEBUG", "ERROR"]);
  logs.levels = [];
  expect(logs.filtered).toEqual([]);
});

test("history and live events are deduplicated and backend order is restored", () => {
  const logs = useLogStore();
  logs.ingest(backend(3));
  logs.ingest([backend(1), backend(2), backend(3)]);
  expect(logs.entries.map((entry) => entry.backendID)).toEqual([1, 2, 3]);
  logs.ingest(backend(2));
  expect(logs.entries).toHaveLength(3);
});

test("retains only the latest entries", () => {
  const logs = useLogStore();
  logs.ingest(Array.from({ length: logCapacity + 50 }, (_, i) => backend(i + 1)));
  expect(logs.entries).toHaveLength(logCapacity);
  expect(logs.entries[0].backendID).toBe(51);
  logs.add("ERROR", "test", "latest");
  expect(logs.entries).toHaveLength(logCapacity);
  expect(logs.entries.at(-1)?.message).toBe("latest");
});

test("clear prevents delayed history replay from restoring cleared entries", () => {
  const logs = useLogStore();
  logs.ingest(backend(10));
  logs.clear();
  logs.ingest([backend(5), backend(10), backend(11)]);
  expect(logs.entries.map((entry) => entry.backendID)).toEqual([11]);
  logs.add("ERROR", "test", "new error");
  expect(logs.errorCount).toBe(1);
  logs.clear();
  expect(logs.errorCount).toBe(0);
});

test("handles ERR, unknown levels and malformed events", () => {
  const logs = useLogStore();
  logs.ingest([null, {}, { ...backend(0) }, backend(1, "ERR"), backend(2, "unknown")]);
  expect(logs.entries.map((entry) => entry.level)).toEqual(["ERROR", "INFO"]);
});

test("formats exceptions, objects and circular console arguments", () => {
  expect(formatLogValue(new Error("failure"))).toContain("failure");
  expect(formatLogValue({ state: "error" })).toBe('{"state":"error"}');
  const circular: { self?: unknown } = {};
  circular.self = circular;
  expect(formatLogValue(circular)).toBe("[object Object]");
});
