import { computed, ref } from "vue";
import { defineStore } from "pinia";
import { Events } from "@wailsio/runtime";
import { ArchiveService } from "../../bindings/pvfine/services";
import type {
  AdvancedSearchDetail,
  AdvancedSearchHit,
  AdvancedSearchIndexStatus,
} from "../../bindings/pvfine/services/models";
import { useArchiveStore } from "./archive";

export type AdvancedSearchMode = "binary" | "string";
export type AdvancedStringMatch = "text" | "regex";

export interface AdvancedSearchItem {
  key: string;
  name: string;
  path: string;
  size: number;
  dataType: number;
  fileIndex: number;
  details: AdvancedSearchDetail[];
}

/** 高级搜索状态:弹窗隐藏时仍保留当前归档的查询与结果。 */
export const useAdvancedSearchStore = defineStore("advancedSearch", () => {
  const archive = useArchiveStore();
  const visible = ref(false);
  const mode = ref<AdvancedSearchMode>("string");
  const stringMatch = ref<AdvancedStringMatch>("text");
  const query = ref("");
  const scopePath = ref("");
  const hits = ref<AdvancedSearchItem[]>([]);
  const nextCursor = ref(-1);
  const searching = ref(false);
  const stale = ref(false);
  const error = ref("");
  const indexStatus = ref<AdvancedSearchIndexStatus>({
    state: "idle",
    stage: "",
    done: 0,
    total: 0,
    error: "",
  });

  let requestId = 0;
  let retryAfterIndex = false;

  const regexEnabled = computed(() => mode.value === "string" && stringMatch.value === "regex");
  const hasResults = computed(() => hits.value.length > 0);

  function eventData(event: any): any {
    return event?.data ?? event;
  }

  function readIndexStatus(data: any): AdvancedSearchIndexStatus {
    return {
      state: String(data?.state ?? "idle"),
      stage: String(data?.stage ?? ""),
      done: Number(data?.done ?? 0),
      total: Number(data?.total ?? 0),
      error: String(data?.error ?? ""),
    };
  }

  function errorMessage(value: any): string {
    return String(value?.message ?? value ?? "高级搜索失败");
  }

  function toItem(hit: AdvancedSearchHit): AdvancedSearchItem {
    return {
      key: `${hit.fileIndex}:${hit.path}`,
      name: hit.name ?? "",
      path: hit.path,
      size: hit.size,
      dataType: hit.dataType,
      fileIndex: hit.fileIndex,
      details: (hit.details ?? []).filter(
        (detail): detail is AdvancedSearchDetail => !!detail
      ),
    };
  }

  function open() {
    visible.value = true;
  }

  function close() {
    visible.value = false;
  }

  function clear() {
    requestId++;
    query.value = "";
    hits.value = [];
    nextCursor.value = -1;
    searching.value = false;
    stale.value = false;
    error.value = "";
    retryAfterIndex = false;
  }

  function resetForArchive() {
    requestId++;
    visible.value = false;
    mode.value = "string";
    stringMatch.value = "text";
    scopePath.value = "";
    clear();
    indexStatus.value = readIndexStatus(null);
  }

  async function search() {
    const request = ++requestId;
    error.value = "";
    stale.value = false;
    hits.value = [];
    nextCursor.value = -1;
    retryAfterIndex = false;
    if (!query.value.trim()) {
      searching.value = false;
      return;
    }
    if (!archive.open) {
      error.value = "请先打开归档文件";
      return;
    }
    if (regexEnabled.value) {
      try {
        // 这是前端即时反馈；最终语法仍由 Go RE2 后端校验。
        new RegExp(query.value);
      } catch (value: any) {
        error.value = `正则表达式无效: ${value?.message ?? value}`;
        return;
      }
    }

    searching.value = true;
    try {
      const result = await ArchiveService.AdvancedSearch(
        mode.value,
        query.value,
        scopePath.value,
        regexEnabled.value,
        0,
        200
      );
      if (request !== requestId) return;
      const page = (result?.hits ?? [])
        .filter((hit): hit is AdvancedSearchHit => !!hit)
        .map(toItem);
      hits.value = page;
      nextCursor.value = result?.nextCursor ?? -1;
    } catch (value) {
      if (request !== requestId) return;
      const message = errorMessage(value);
      error.value = message;
      retryAfterIndex = message.includes("索引正在构建");
    } finally {
      if (request === requestId) searching.value = false;
    }
  }

  async function loadMore() {
    if (searching.value || nextCursor.value < 0 || !query.value.trim()) return;
    const request = requestId;
    searching.value = true;
    error.value = "";
    try {
      const result = await ArchiveService.AdvancedSearch(
        mode.value,
        query.value,
        scopePath.value,
        regexEnabled.value,
        nextCursor.value,
        200
      );
      if (request !== requestId) return;
      const page = (result?.hits ?? [])
        .filter((hit): hit is AdvancedSearchHit => !!hit)
        .map(toItem);
      hits.value.push(...page);
      nextCursor.value = result?.nextCursor ?? -1;
    } catch (value) {
      if (request === requestId) error.value = errorMessage(value);
    } finally {
      if (request === requestId) searching.value = false;
    }
  }

  function markStale() {
    indexStatus.value = readIndexStatus(null);
    if (query.value.trim() || hits.value.length > 0) stale.value = true;
  }

  Events.On("archive:opened", () => resetForArchive());
  Events.On("archive:closed", () => resetForArchive());
  Events.On("archive:advanced-search-stale", () => markStale());
  Events.On("archive:advanced-index-progress", (event: any) => {
    indexStatus.value = readIndexStatus(eventData(event));
  });
  Events.On("archive:advanced-index-ready", (event: any) => {
    indexStatus.value = readIndexStatus(eventData(event));
    if (retryAfterIndex && visible.value) {
      retryAfterIndex = false;
      void search();
    }
  });
  Events.On("archive:advanced-index-error", (event: any) => {
    indexStatus.value = readIndexStatus(eventData(event));
  });

  return {
    visible,
    mode,
    stringMatch,
    query,
    scopePath,
    hits,
    nextCursor,
    searching,
    stale,
    error,
    indexStatus,
    regexEnabled,
    hasResults,
    open,
    close,
    clear,
    search,
    loadMore,
  };
});
