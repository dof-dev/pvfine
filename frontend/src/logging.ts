import type { App } from "vue";
import { Events } from "@wailsio/runtime";
import { useLogStore } from "./stores/logs";

export function installLogging(app: App): void {
  const logs = useLogStore();
  Events.On("app:log", (event) => logs.ingest(event.data));
  Events.On("app:logs", (event) => logs.ingest(event.data));
  void Events.Emit("app:logs-request").catch((error) => logs.add("WARN", "日志", error));

  for (const [method, level] of [["error", "ERROR"], ["warn", "WARN"]] as const) {
    const original = console[method].bind(console);
    console[method] = (...args: unknown[]) => {
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
    logs.add("ERROR", "Vue", info, error);
    previous?.(error, instance, info);
  };
}
