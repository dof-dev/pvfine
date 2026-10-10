import { computed, ref, shallowRef } from "vue";
import { defineStore } from "pinia";
import { developmentLoggingEnabled } from "../loggingEnvironment";

export const logLevels = ["DEBUG", "INFO", "WARN", "ERROR"] as const;
export type LogLevel = typeof logLevels[number];
export interface LogEntry {
  id: string;
  timestamp: string;
  level: LogLevel;
  source: string;
  message: string;
  backendID?: number;
}
export const logCapacity = 2000;

export function formatLogValue(value: unknown): string {
  if (value instanceof Error) return value.stack || value.message;
  if (typeof value === "string") return value;
  try {
    return JSON.stringify(value) ?? String(value);
  } catch {
    return String(value);
  }
}

export const useLogStore = defineStore("logs", () => {
  // 日志条目只追加、不编辑，以快照发布，避免为历史记录创建深层响应式代理。
  const entries = shallowRef<LogEntry[]>([]);
  const expanded = ref(false);
  const levels = ref<LogLevel[]>(["INFO", "WARN", "ERROR"]);
  const follow = ref(true);
  let sequence = 0;
  let clearedBackendID = 0;
  let latestBackendID = 0;
  let pendingBackendLogs: unknown[] = [];
  let backendLogTimer: ReturnType<typeof setTimeout> | undefined;
  const seenBackendIDs = new Set<number>();
  const filtered = computed(() => entries.value.filter((entry) => levels.value.includes(entry.level)));
  const errorCount = computed(() => entries.value.filter((entry) => entry.level === "ERROR").length);

  function publish(next: LogEntry[]) {
    const excess = Math.max(0, next.length - logCapacity);
    for (let index = 0; index < excess; index++) {
      const id = next[index].backendID;
      if (id !== undefined) seenBackendIDs.delete(id);
    }
    entries.value = excess ? next.slice(excess) : next;
  }

  function add(level: LogLevel, source: string, ...values: unknown[]) {
    if (level === "DEBUG" && !developmentLoggingEnabled()) return;
    publish([...entries.value, {
      id: `frontend-${++sequence}`, timestamp: new Date().toISOString(), level, source,
      message: values.map(formatLogValue).join(" "),
    }]);
  }

  function ingest(data: unknown) {
    const batch = Array.isArray(data) ? data : [data];
    const development = developmentLoggingEnabled();
    const next = entries.value.slice();
    for (const value of batch) {
      if (!value || typeof value !== "object") continue;
      const item = value as Record<string, unknown>;
      const id = Number(item.id);
      if (!Number.isSafeInteger(id) || id <= clearedBackendID || seenBackendIDs.has(id) ||
        typeof item.message !== "string" || typeof item.timestamp !== "string") continue;
      const rawLevel = String(item.level).toUpperCase();
      const level: LogLevel = rawLevel === "ERR" ? "ERROR" :
        logLevels.includes(rawLevel as LogLevel) ? rawLevel as LogLevel : "INFO";
      const source = String(item.source ?? "Go");
      if (!development && (level === "DEBUG" || source === "Go" && level !== "WARN" && level !== "ERROR")) continue;
      seenBackendIDs.add(id);
      latestBackendID = Math.max(latestBackendID, id);
      next.push({
        id: `backend-${id}`, backendID: id, timestamp: item.timestamp,
        level, source, message: item.message,
      });
    }
    if (next.length === entries.value.length) return;
    next.sort((a, b) => {
      if (a.backendID !== undefined && b.backendID !== undefined) return a.backendID - b.backendID;
      return Date.parse(a.timestamp) - Date.parse(b.timestamp);
    });
    publish(next);
  }

  function flushBackendLogs() {
    if (backendLogTimer !== undefined) clearTimeout(backendLogTimer);
    backendLogTimer = undefined;
    const batch = pendingBackendLogs;
    pendingBackendLogs = [];
    if (batch.length) ingest(batch);
  }

  /** 同一批 Wails 事件只排序、裁剪和通知界面一次，避免日志阻塞文件响应。 */
  function enqueueBackendLogs(data: unknown) {
    if (Array.isArray(data)) pendingBackendLogs.push(...data);
    else pendingBackendLogs.push(data);
    if (backendLogTimer === undefined) backendLogTimer = setTimeout(flushBackendLogs, 16);
  }

  function clear() {
    // 先更新已接收日志的 ID 水位，延迟到达的历史不能恢复清空前的记录。
    flushBackendLogs();
    clearedBackendID = latestBackendID;
    entries.value = [];
    seenBackendIDs.clear();
  }

  return { entries, expanded, levels, follow, filtered, errorCount, add, ingest, enqueueBackendLogs, clear };
});
