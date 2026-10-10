import type { App } from "vue";
import { Events } from "@wailsio/runtime";
import { useLogStore } from "./stores/logs";
import { developmentLoggingEnabled } from "./loggingEnvironment";

export function installLogging(app: App): void {
  const logs = useLogStore();
  const development = developmentLoggingEnabled();
  Events.On("app:log", (event) => logs.enqueueBackendLogs(event.data));
  Events.On("app:logs", (event) => logs.enqueueBackendLogs(event.data));
  void Events.Emit("app:logs-request").catch((error) => logs.add("WARN", "日志", error));

  for (const [method, level] of [["error", "ERROR"], ["warn", "WARN"]] as const) {
    const original = console[method].bind(console);
    console[method] = (...args: unknown[]) => {
      if (!development && level === "WARN" && typeof args[0] === "string" && args[0].startsWith("[Vue warn]")) return;
      original(...args);
      logs.add(level, "前端", ...args);
    };
  }
  window.addEventListener("error", (event) => {
    logs.add("ERROR", "前端异常", event.error ?? event.message);
  });
  window.addEventListener("unhandledrejection", (event) => {
    logs.add("ERROR", "未处理异常", event.reason);
  });
  const previous = app.config.errorHandler;
  app.config.errorHandler = (error, instance, info) => {
    if (development) logs.add("ERROR", "Vue", info, error);
    else logs.add("ERROR", "Vue", error);
    previous?.(error, instance, info);
  };
}
