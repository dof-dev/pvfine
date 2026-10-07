import { computed, ref } from "vue";
import { defineStore } from "pinia";

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
  const entries = ref<LogEntry[]>([]);
  const expanded = ref(false);
  const levels = ref<LogLevel[]>(["INFO", "WARN", "ERROR"]);
  const follow = ref(true);
  let sequence = 0;
  let clearedBackendID = 0;
  let latestBackendID = 0;
  const seenBackendIDs = new Set<number>();
  const filtered = computed(() => entries.value.filter((entry) => levels.value.includes(entry.level)));
  const errorCount = computed(() => entries.value.filter((entry) => entry.level === "ERROR").length);

  function retain() {
    if (entries.value.length > logCapacity) entries.value.splice(0, entries.value.length - logCapacity);
    seenBackendIDs.clear();
    for (const entry of entries.value) {
      if (entry.backendID !== undefined) seenBackendIDs.add(entry.backendID);
    }
  }

  function add(level: LogLevel, source: string, ...values: unknown[]) {
    entries.value.push({
      id: `frontend-${++sequence}`, timestamp: new Date().toISOString(), level, source,
      message: values.map(formatLogValue).join(" "),
    });
    retain();
  }

  function ingest(data: unknown) {
    const batch = Array.isArray(data) ? data : [data];
    for (const value of batch) {
      if (!value || typeof value !== "object") continue;
      const item = value as Record<string, unknown>;
      const id = Number(item.id);
      if (!Number.isSafeInteger(id) || id <= clearedBackendID || seenBackendIDs.has(id) ||
        typeof item.message !== "string" || typeof item.timestamp !== "string") continue;
      const rawLevel = String(item.level).toUpperCase();
      const level: LogLevel = rawLevel === "ERR" ? "ERROR" :
        logLevels.includes(rawLevel as LogLevel) ? rawLevel as LogLevel : "INFO";
      seenBackendIDs.add(id);
      latestBackendID = Math.max(latestBackendID, id);
      entries.value.push({
        id: `backend-${id}`, backendID: id, timestamp: item.timestamp,
        level, source: String(item.source ?? "Go"), message: item.message,
      });
    }
    entries.value.sort((a, b) => {
      if (a.backendID !== undefined && b.backendID !== undefined) return a.backendID - b.backendID;
      return Date.parse(a.timestamp) - Date.parse(b.timestamp);
    });
    retain();
  }

  function clear() {
    clearedBackendID = latestBackendID;
    entries.value = [];
    seenBackendIDs.clear();
  }

  return { entries, expanded, levels, follow, filtered, errorCount, add, ingest, clear };
});
