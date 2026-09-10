import { computed, ref, watch } from "vue";
import { defineStore } from "pinia";
import { Events } from "@wailsio/runtime";
import { BatchService } from "../../bindings/pvfine/services";
import type {
  BatchApplyResult,
  BatchFilePreview,
  BatchPreviewPage,
  StructuredOperation,
} from "../../bindings/pvfine/services/models";

export type BatchMode = "text" | "structured";

export interface BatchTextForm {
  find: string;
  replacement: string;
  regex: boolean;
}

export interface BatchOperationForm extends StructuredOperation {
  id: number;
}

/** 批处理弹窗状态与当前会话内的预览计划。 */
export const useBatchStore = defineStore("batch", () => {
  const visible = ref(false);
  const mode = ref<BatchMode>("text");
  const scopeLabel = ref("");
  const scopePaths = ref<string[]>([]);
  const text = ref<BatchTextForm>({ find: "", replacement: "", regex: false });
  let operationID = 1;
  let requestID = 0;
  let planFingerprint = "";
  let selectionMode: "all" | "none" | "some" = "all";
  let excludedIndexes = new Set<number>();
  const operations = ref<BatchOperationForm[]>([newOperation()]);
  const rows = ref<BatchFilePreview[]>([]);
  const planId = ref("");
  const nextCursor = ref(-1);
  const requestedFiles = ref(0);
  const matchedFiles = ref(0);
  const matchedOccurrences = ref(0);
  const changedFiles = ref(0);
  const selectedIndexes = ref<Set<number>>(new Set());
  const loading = ref(false);
  const applying = ref(false);
  const error = ref("");
  const stale = ref(false);
  const hasPreview = computed(() => !!planId.value && !stale.value);
  const selectedCount = computed(() => selectedIndexes.value.size);
  const canApply = computed(
    () =>
      hasPreview.value &&
      !loading.value &&
      !applying.value &&
      (selectedCount.value > 0 || (selectionMode === "all" && changedFiles.value > 0))
  );
  const requestFingerprint = computed(() =>
    JSON.stringify({
      mode: mode.value,
      text: text.value,
      operations: operations.value,
      paths: scopePaths.value,
    })
  );

  watch(requestFingerprint, (fingerprint) => {
    if (planId.value && planFingerprint && fingerprint !== planFingerprint) stale.value = true;
  });

  function newOperation(): BatchOperationForm {
    return {
      id: operationID++,
      kind: "set",
      section: "",
      tokenIndex: -1,
      value: "",
      createIfMissing: false,
      operator: "+",
      operand: "",
      operandEnd: "",
      anchorSection: "",
      hasEndTag: false,
    };
  }

  function normalizePaths(paths: string[]): string[] {
    const seen = new Set<string>();
    const result: string[] = [];
    for (const rawPath of paths) {
      const path = rawPath.replaceAll("\\", "/").replace(/^\/+|\/+$/g, "").trim();
      if (!path || seen.has(path)) continue;
      seen.add(path);
      result.push(path);
    }
    return result;
  }

  function resetPreview(): void {
    requestID++;
    rows.value = [];
    planId.value = "";
    nextCursor.value = -1;
    requestedFiles.value = 0;
    matchedFiles.value = 0;
    matchedOccurrences.value = 0;
    changedFiles.value = 0;
    selectedIndexes.value = new Set();
    selectionMode = "all";
    excludedIndexes = new Set();
    error.value = "";
    stale.value = false;
    planFingerprint = "";
  }

  function open(paths: string[], label: string): void {
    scopePaths.value = normalizePaths(paths);
    scopeLabel.value = label;
    mode.value = "text";
    text.value = { find: "", replacement: "", regex: false };
    operations.value = [newOperation()];
    resetPreview();
    visible.value = scopePaths.value.length > 0;
    if (scopePaths.value.length === 0) error.value = "没有可处理的文件";
  }

  function close(): void {
    if (loading.value || applying.value) return;
    visible.value = false;
  }

  function addOperation(initial?: Partial<StructuredOperation>): BatchOperationForm {
    const base = newOperation();
    const op: BatchOperationForm = {
      ...base,
      ...initial,
      id: base.id,
    };
    operations.value.push(op);
    return op;
  }

  function removeOperation(id: number): void {
    if (operations.value.length <= 1) return;
    const index = operations.value.findIndex((operation) => operation.id === id);
    if (index >= 0) operations.value.splice(index, 1);
  }

  function moveOperation(id: number, direction: -1 | 1): void {
    const index = operations.value.findIndex((operation) => operation.id === id);
    const next = index + direction;
    if (index < 0 || next < 0 || next >= operations.value.length) return;
    const [operation] = operations.value.splice(index, 1);
    operations.value.splice(next, 0, operation);
  }

  function applyPage(page: BatchPreviewPage, append: boolean): void {
    const pageRows = (page.rows ?? []).filter(
      (row): row is BatchFilePreview => !!row
    );
    if (append) rows.value.push(...pageRows);
    else rows.value = pageRows;
    for (const row of pageRows) {
      if (
        row.status === "changed" &&
        row.fileIndex >= 0 &&
        selectionMode === "all" &&
        !excludedIndexes.has(row.fileIndex)
      ) {
        const next = new Set(selectedIndexes.value);
        next.add(row.fileIndex);
        selectedIndexes.value = next;
      }
    }
    planId.value = page.planId;
    nextCursor.value = page.nextCursor;
    requestedFiles.value = page.requestedFiles;
    matchedFiles.value = page.matchedFiles;
    matchedOccurrences.value = page.matchedOccurrences;
    changedFiles.value = page.changedFiles;
  }

  function requestPayload() {
    return {
      mode: mode.value,
      paths: [...scopePaths.value],
      text:
        mode.value === "text"
          ? {
              find: text.value.find,
              replacement: text.value.replacement,
              regex: text.value.regex,
            }
          : null,
      operations:
        mode.value === "structured"
          ? operations.value.map(({ id: _id, ...operation }) => operation)
          : [],
    };
  }

  async function preview(): Promise<void> {
    if (loading.value || applying.value) return;
    const request = ++requestID;
    loading.value = true;
    error.value = "";
    stale.value = false;
    rows.value = [];
    planId.value = "";
    nextCursor.value = -1;
    planFingerprint = "";
    selectedIndexes.value = new Set();
    selectionMode = "all";
    excludedIndexes = new Set();
    try {
      const page = await BatchService.Preview(requestPayload());
      if (request !== requestID || !page) return;
      applyPage(page, false);
      planFingerprint = requestFingerprint.value;
    } catch (value: any) {
      if (request === requestID) error.value = errorMessage(value);
    } finally {
      if (request === requestID) loading.value = false;
    }
  }

  async function loadMore(): Promise<boolean> {
    if (loading.value || !planId.value || nextCursor.value < 0 || stale.value) return false;
    const request = requestID;
    const cursor = nextCursor.value;
    loading.value = true;
    try {
      const page = await BatchService.PreviewPage(planId.value, cursor, 100);
      if (request !== requestID || !page) return false;
      applyPage(page, true);
      return nextCursor.value !== cursor;
    } catch (value: any) {
      if (request === requestID) error.value = errorMessage(value);
      return false;
    } finally {
      if (request === requestID) loading.value = false;
    }
  }

  async function loadAll(): Promise<void> {
    while (nextCursor.value >= 0 && !stale.value) {
      const progressed = await loadMore();
      if (!progressed) {
        if (error.value) throw new Error(error.value);
        break;
      }
    }
  }

  function toggleSelected(fileIndex: number): void {
    const next = new Set(selectedIndexes.value);
    if (selectionMode === "all") {
      if (next.has(fileIndex)) {
        next.delete(fileIndex);
        excludedIndexes.add(fileIndex);
      } else {
        next.add(fileIndex);
        excludedIndexes.delete(fileIndex);
      }
    } else if (next.has(fileIndex)) {
      next.delete(fileIndex);
    } else {
      next.add(fileIndex);
    }
    selectedIndexes.value = next;
    if (selectionMode === "none") selectionMode = "some";
  }

  function selectAll(): void {
    selectionMode = "all";
    excludedIndexes = new Set();
    const next = new Set<number>();
    for (const row of rows.value) {
      if (row.status === "changed" && row.fileIndex >= 0) next.add(row.fileIndex);
    }
    selectedIndexes.value = next;
  }

  function selectNone(): void {
    selectionMode = "none";
    excludedIndexes = new Set();
    selectedIndexes.value = new Set();
  }

  async function apply(): Promise<BatchApplyResult> {
    if (!canApply.value) throw new Error("没有选中的变更文件");
    applying.value = true;
    error.value = "";
    try {
      await loadAll();
      if (stale.value || !planId.value) throw new Error("批处理预览已过期,请重新预览");
      return await BatchService.Apply(planId.value, [...selectedIndexes.value]);
    } catch (value: any) {
      const message = errorMessage(value);
      if (message.includes("预览已过期")) stale.value = true;
      error.value = message;
      throw value;
    } finally {
      applying.value = false;
    }
  }

  function errorMessage(value: any): string {
    return String(value?.message ?? value ?? "批处理失败");
  }

  function markStale(): void {
    if (planId.value) stale.value = true;
  }

  Events.On("archive:advanced-search-stale", markStale);
  Events.On("archive:batch-applied", markStale);
  Events.On("archive:opened", () => {
    visible.value = false;
    resetPreview();
  });
  Events.On("archive:closed", () => {
    visible.value = false;
    resetPreview();
  });

  return {
    visible,
    mode,
    scopeLabel,
    scopePaths,
    text,
    operations,
    rows,
    planId,
    nextCursor,
    requestedFiles,
    matchedFiles,
    matchedOccurrences,
    changedFiles,
    selectedIndexes,
    loading,
    applying,
    error,
    stale,
    hasPreview,
    selectedCount,
    canApply,
    open,
    close,
    addOperation,
    removeOperation,
    moveOperation,
    preview,
    loadMore,
    loadAll,
    toggleSelected,
    selectAll,
    selectNone,
    apply,
    resetPreview,
  };
});
