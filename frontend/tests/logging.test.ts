import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { createApp } from "vue";
import { createPinia, setActivePinia } from "pinia";

const runtime = vi.hoisted(() => ({
  handlers: new Map<string, (event: { data: unknown }) => void>(),
  emit: vi.fn(),
}));
vi.mock("@wailsio/runtime", () => ({
  Events: {
    On: (name: string, handler: (event: { data: unknown }) => void) => runtime.handlers.set(name, handler),
    Emit: runtime.emit,
  },
}));
import { installLogging } from "../src/logging";
import { useLogStore } from "../src/stores/logs";

const listeners = new Map<string, (event: any) => void>();

beforeEach(() => {
  setActivePinia(createPinia());
  runtime.handlers.clear();
  listeners.clear();
  runtime.emit.mockReset().mockResolvedValue(undefined);
  vi.stubGlobal("window", { addEventListener: (name: string, handler: (event: any) => void) => listeners.set(name, handler) });
  vi.spyOn(console, "error").mockImplementation(() => {});
  vi.spyOn(console, "warn").mockImplementation(() => {});
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

test("subscribes before requesting history and receives backend logs", () => {
  installLogging(createApp({}));
  expect(runtime.emit).toHaveBeenCalledWith("app:logs-request");
  runtime.handlers.get("app:logs")?.({ data: [{ id: 1, timestamp: "2026-10-07T12:00:00Z", level: "INFO", message: "startup" }] });
  runtime.handlers.get("app:log")?.({ data: { id: 2, timestamp: "2026-10-07T12:00:01Z", level: "ERROR", message: "failed" } });
  expect(useLogStore().entries.map((entry) => entry.message)).toEqual(["startup", "failed"]);
});

test("captures console, browser and Vue exceptions", () => {
  const app = createApp({});
  const previous = vi.fn();
  app.config.errorHandler = previous;
  installLogging(app);
  console.error("caught", new Error("console error"));
  console.warn("warning");
  listeners.get("error")?.({ error: new Error("browser error") });
  listeners.get("unhandledrejection")?.({ reason: new Error("promise error") });
  app.config.errorHandler?.(new Error("vue error"), null, "render");
  const logs = useLogStore();
  expect(logs.entries).toHaveLength(5);
  expect(logs.errorCount).toBe(4);
  expect(previous).toHaveBeenCalledOnce();
});
