import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { assertMCPIdle, setMCPActive } from "../src/mcpActivity";

const state = vi.hoisted(() => ({
  handlers: new Map<string, (event: any) => any>(),
  drafts: vi.fn(() => [] as unknown[]),
  sync: vi.fn().mockResolvedValue(undefined),
  acknowledge: vi.fn().mockResolvedValue(undefined),
  report: vi.fn().mockResolvedValue(undefined),
  snapshot: vi.fn().mockResolvedValue({ operationId: "" }),
  unregister: vi.fn(),
  script: { running: false, applying: false },
}));
vi.mock("@wailsio/runtime", () => ({ Events: { On: (name: string, handler: (event: any) => any) => { state.handlers.set(name, handler); return vi.fn(); } } }));
vi.mock("../bindings/pvfine/services", () => ({ MCPService: { Acknowledge: state.acknowledge, ReportWindow: state.report, WindowState: state.snapshot, UnregisterWindow: state.unregister, CancelActiveScript: vi.fn() } }));
vi.mock("../src/stores/editor", () => ({ useEditorStore: () => ({ archiveDiffDrafts: state.drafts, syncMCPWorkspace: state.sync, closeAllTabs: vi.fn() }) }));
vi.mock("../src/stores/archive", () => ({ useArchiveStore: () => ({ loading: false }) }));
vi.mock("../src/stores/script", () => ({ useScriptStore: () => state.script }));
vi.mock("../src/stores/import", () => ({ useImportStore: () => ({ running: false }) }));
vi.mock("../src/stores/export", () => ({ useExportStore: () => ({ preparing: false, running: false }) }));
vi.mock("../src/stores/batch", () => ({ useBatchStore: () => ({ loading: false, applying: false }) }));
vi.mock("../src/stores/version", () => ({ useVersionStore: () => ({ committing: false }) }));
import { installMCPBridge } from "../src/mcpBridge";

let root: { inert: boolean };
let unload: (() => void) | undefined;
beforeEach(() => {
  vi.clearAllMocks(); state.handlers.clear(); state.drafts.mockReturnValue([]); state.script.running = false;
  state.snapshot.mockResolvedValue({ operationId: "" });
  root = { inert: false };
  const node = () => ({ style: { cssText: "", display: "" }, setAttribute: vi.fn(), appendChild: vi.fn(), addEventListener: vi.fn(), contains: () => false, remove: vi.fn(), textContent: "" });
  vi.stubGlobal("document", { getElementById: () => root, createElement: node, body: { appendChild: vi.fn() } });
  vi.stubGlobal("window", { addEventListener: (name: string, fn: () => void) => { if (name === "beforeunload") unload = fn; }, removeEventListener: vi.fn(), setInterval: vi.fn(() => 1), clearInterval: vi.fn() });
});
afterEach(() => { unload?.(); unload = undefined; setMCPActive(false); vi.unstubAllGlobals(); });

test("AI 准备屏障冻结编辑，拒绝未提交草稿，结束后释放", async () => {
  installMCPBridge(false);
  await vi.waitFor(() => expect(state.report).toHaveBeenCalled());
  state.drafts.mockReturnValue([{}]);
  await state.handlers.get("mcp:prepare")!({ data: { operationId: "operation-a", kind: "archive_save" } });
  expect(root.inert).toBe(true);
  expect(() => assertMCPIdle()).toThrow("AI 正在操作");
  expect(state.acknowledge).toHaveBeenCalledWith("main", "operation-a", true, false);
  await state.handlers.get("mcp:released")!({ data: { operationId: "operation-a" } });
  expect(root.inert).toBe(false);
  expect(() => assertMCPIdle()).not.toThrow();
});
test("GUI 草稿检查报错也按未提交草稿处理", async () => {
  installMCPBridge(false);
  state.drafts.mockImplementation(() => { throw new Error("GUI draft"); });
  await state.handlers.get("mcp:prepare")!({ data: { operationId: "operation-b" } });
  expect(state.acknowledge).toHaveBeenCalledWith("main", "operation-b", true, false);
});
test("窗口重载后恢复运行中的屏障，独立脚本源码不会计作归档草稿", async () => {
  state.snapshot.mockResolvedValue({ operationId: "running-task", kind: "script_run" } as any);
  installMCPBridge(true);
  await vi.waitFor(() => expect(root.inert).toBe(true));
  expect(state.drafts).not.toHaveBeenCalled();
  expect(() => assertMCPIdle()).toThrow();
});

test("旧的心跳响应不能释放刚建立的准备屏障", async () => {
  let resolve!: (value: any) => void;
  state.snapshot.mockReturnValue(new Promise(done => { resolve = done; }));
  installMCPBridge(false);
  await vi.waitFor(() => expect(state.snapshot).toHaveBeenCalled());
  await state.handlers.get("mcp:prepare")!({ data: { operationId: "new-operation" } });
  resolve({ operationId: "" });
  await Promise.resolve(); await Promise.resolve();
  expect(root.inert).toBe(true);
  expect(() => assertMCPIdle()).toThrow();
});

test("应用后的标签同步完成前，不释放界面也不确认下一个操作", async () => {
  let resolve!: () => void;
  state.sync.mockReturnValue(new Promise<void>(done => { resolve = done; }));
  installMCPBridge(false);
  await state.handlers.get("mcp:prepare")!({ data: { operationId: "apply-task" } });
  state.handlers.get("mcp:workspace-changed")!({ data: { kind: "script_apply" } });
  const releasing = state.handlers.get("mcp:released")!({ data: { operationId: "apply-task" } });
  await Promise.resolve();
  expect(root.inert).toBe(true);
  resolve(); await releasing;
  expect(state.sync).toHaveBeenCalled();
  expect(root.inert).toBe(false);
});
