import { nextTick } from "vue";
import { setMCPActive } from "./mcpActivity";
import { Events } from "@wailsio/runtime";
import { MCPService } from "../bindings/pvfine/services";
import { useEditorStore } from "./stores/editor";
import { useArchiveStore } from "./stores/archive";
import { useScriptStore } from "./stores/script";
import { useImportStore } from "./stores/import";
import { useExportStore } from "./stores/export";
import { useBatchStore } from "./stores/batch";
import { useVersionStore } from "./stores/version";

/** Every window acknowledges a preparation barrier before an AI task starts. */
export function installMCPBridge(scriptWindow: boolean): void {
  const id = scriptWindow ? "script-workspace" : "main";
  const editor = scriptWindow ? null : useEditorStore();
  const archive = useArchiveStore();
  const script = useScriptStore();
  const imports = scriptWindow ? null : useImportStore();
  const exports = scriptWindow ? null : useExportStore();
  const batch = scriptWindow ? null : useBatchStore();
  const version = scriptWindow ? null : useVersionStore();
  const isBusy = () => !!(editor?.saving || editor?.guiApplying || editor?.opening || archive.loading || script.running || script.applying || imports?.running || exports?.running || exports?.preparing || batch?.loading || batch?.applying || version?.committing);
  let operation = "";
  let barrierGeneration = 0;
  let pendingSync = Promise.resolve();
  let syncFailed = false;
  const root = document.getElementById("app")!;
  const banner = document.createElement("div");
  banner.setAttribute("role", "status");
  banner.style.cssText = "position:fixed;bottom:18px;left:50%;transform:translateX(-50%);z-index:99999;padding:10px 18px;border-radius:8px;background:#25334a;color:white;font-size:13px;display:none;pointer-events:none";
  const label = document.createElement("span");
  label.textContent = "AI 正在操作当前 PVF… ";
  banner.appendChild(label);
  let noticeTimer: ReturnType<typeof setTimeout> | undefined;
  const cancel = document.createElement("button");
  cancel.textContent = "取消脚本";
  cancel.style.cssText = "margin-left:12px;cursor:pointer;pointer-events:auto";
  cancel.addEventListener("click", () => { void MCPService.CancelActiveScript(); });
  banner.appendChild(cancel);
  document.body.appendChild(banner);
  const release = () => {
    barrierGeneration++;
    operation = "";
    setMCPActive(false);
    root.inert = false;
    banner.style.display = "none";
  };
  // Block app-level shortcuts as well as native editing while the barrier holds.
  const blockInput = (event: Event) => {
    if (operation && !banner.contains(event.target as Node)) { event.preventDefault(); event.stopImmediatePropagation(); }
  };
  const inputEvents = ["keydown", "pointerdown", "click", "beforeinput", "drop"];
  for (const name of inputEvents) window.addEventListener(name, blockInput, true);
  const offPrepare = Events.On("mcp:prepare", async (event: any) => {
    const data = event?.data ?? event;
    if (!data?.operationId) return;
    clearTimeout(noticeTimer);
    label.textContent = "AI 正在操作当前 PVF… ";
    barrierGeneration++;
    operation = data.operationId;
    setMCPActive(true);
    cancel.style.display = data.kind === "script_run" ? "inline" : "none";
    root.inert = true;
    banner.style.display = "block";
    await nextTick();
    await pendingSync;
    let dirty = syncFailed;
    try {
      if (editor) dirty = dirty || editor.dirtyCount > 0 || editor.archiveDiffDrafts().length > 0;
    } catch { dirty = true; }
    const busy = isBusy();
    await MCPService.Acknowledge(id, operation, dirty, busy);
  });
  const offWorkspace = Events.On("mcp:workspace-changed", (event: any) => {
    const data = event?.data ?? event;
    if (!editor) return;
    pendingSync = pendingSync.then(async () => {
      if (data.kind === "archive_open") editor.closeAllTabs();
      else if (data.kind !== "archive_close") await editor.syncMCPWorkspace();
      syncFailed = false;
    }).catch(() => { syncFailed = true; });
  });
  const offRelease = Events.On("mcp:released", async (event: any) => {
    const data = event?.data ?? event;
    await pendingSync;
    if (operation === data?.operationId) release();
  });
  const offFinished = Events.On("mcp:task-finished", async (event: any) => {
    await pendingSync;
    const data = event?.data ?? event;
    const names: Record<string, string> = { script_run: "脚本运行", script_apply: "应用脚本", import_prepare: "导入预览", import_apply: "导入", export_prepare: "导出预览", export_execute: "导出", archive_open: "打开归档", archive_close: "关闭归档", archive_save: "保存", archive_save_as: "生成副本" };
    if (operation) return;
    label.textContent = data.status === "failed" ? `AI ${names[data.kind] ?? "任务"}失败：${data.error ?? "请查看任务结果"}` : `AI ${names[data.kind] ?? "任务"}完成${data.outputPath ? `：${data.outputPath}` : ""}`;
    cancel.style.display = "none";
    banner.style.display = "block";
    noticeTimer = setTimeout(() => { if (!operation) banner.style.display = "none"; }, 6000);
  });
  // Refreshing the window recovers retained AI script previews without touching source.
  const heartbeat = async () => {
    const generation = barrierGeneration;
    try {
      let dirty = false;
      try { dirty = !!editor && (editor.dirtyCount > 0 || editor.archiveDiffDrafts().length > 0); } catch { dirty = true; }
      await MCPService.ReportWindow(id, dirty, isBusy());
      const state = await MCPService.WindowState();
      if (generation !== barrierGeneration) return;
      if (state?.operationId) {
        operation = state.operationId;
        setMCPActive(true);
        root.inert = true;
        banner.style.display = "block";
        cancel.style.display = state.kind === "script_run" ? "inline" : "none";
      } else if (operation) release();
    } catch { /* A closing runtime will disconnect naturally. */ }
  };
  void heartbeat();
  const timer = window.setInterval(() => void heartbeat(), 3000);
  window.addEventListener("beforeunload", () => {
    window.clearInterval(timer);
    offPrepare(); offRelease(); offFinished(); offWorkspace(); clearTimeout(noticeTimer); release(); banner.remove();
    for (const name of inputEvents) window.removeEventListener(name, blockInput, true);
    void MCPService.UnregisterWindow(id);
  }, { once: true });
}
