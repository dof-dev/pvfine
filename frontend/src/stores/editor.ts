import { defineStore } from "pinia";
import { ref, computed } from "vue";
import { EditorService } from "../../bindings/pvfine/services";
import type { EditorAnnotation, FileMeta } from "../../bindings/pvfine/services/models";
import { useArchiveStore } from "./archive";

export interface EditorTab {
  index: number;
  path: string;
  title: string;
  dataType: number;
  size: number;
  editable: boolean;
  original: string; // 打开时的文本(脏判定基准)
  text: string; // 当前编辑器内容
  modified: boolean; // 后端 overlay 状态
  annotations: EditorAnnotation[];
}

/** 编辑器状态:多标签 + 防抖内存同步 */
export const useEditorStore = defineStore("editor", () => {
  const tabs = ref<EditorTab[]>([]);
  const activeKey = ref<number | null>(null);
  const opening = ref(false);
  const saving = ref(false);

  const activeTab = computed(() => tabs.value.find((t) => t.index === activeKey.value) ?? null);
  const dirtyCount = computed(() => tabs.value.filter((t) => t.text !== t.original).length);

  let syncTimer: number | undefined;
  const pendingSync = new Set<number>();

  /** 打开文件(已打开则激活) */
  async function openFile(index: number) {
    const existing = tabs.value.find((t) => t.index === index);
    if (existing) {
      activeKey.value = index;
      return;
    }
    opening.value = true;
    try {
      const meta: FileMeta | null = await EditorService.GetFile(index);
      if (!meta) return;
      // 标签上限,防误开大量文件
      if (tabs.value.length >= 20) {
        throw new Error("打开的标签过多,请先关闭一些(上限 20)");
      }
      tabs.value.push({
        index,
        path: meta.path,
        title: meta.path.split("/").pop() ?? meta.path,
        dataType: meta.dataType,
        size: meta.size,
        editable: meta.editable,
        original: meta.text,
        text: meta.text,
        modified: meta.modified,
        annotations: (meta.annotations ?? []).filter(
          (annotation): annotation is EditorAnnotation => !!annotation
        ),
      });
      activeKey.value = index;
    } finally {
      opening.value = false;
    }
  }

  function closeTab(index: number) {
    const idx = tabs.value.findIndex((t) => t.index === index);
    if (idx < 0) return;
    tabs.value.splice(idx, 1);
    pendingSync.delete(index);
    if (activeKey.value === index) {
      const next = tabs.value[Math.min(idx, tabs.value.length - 1)];
      activeKey.value = next ? next.index : null;
    }
  }

  /** 编辑器内容变化:立即更新本地脏状态,防抖同步到后端 overlay */
  function updateContent(index: number, text: string) {
    const tab = tabs.value.find((t) => t.index === index);
    if (!tab || !tab.editable) return;
    tab.text = text;
    pendingSync.add(index);
    window.clearTimeout(syncTimer);
    syncTimer = window.setTimeout(async () => {
      const batch = [...pendingSync];
      pendingSync.clear();
      for (const i of batch) {
        const t = tabs.value.find((x) => x.index === i);
        if (!t) continue;
        try {
          await syncTab(t);
        } catch (e) {
          console.error("sync failed", e);
        }
      }
      await useArchiveStore().refreshInfo();
    }, 400);
  }

  /** 保存到源文件 */
  async function save() {
    const archive = useArchiveStore();
    saving.value = true;
    try {
      await flushPending();
      const info = await EditorService.Save();
      archive.info = info;
      // 保存成功后,文本与基准重置(overlay 已清空)
      for (const t of tabs.value) {
        t.original = t.text;
        t.modified = false;
      }
      return info;
    } finally {
      saving.value = false;
    }
  }

  /** 另存为新 PVF,返回保存路径(取消返回 null) */
  async function saveAs() {
    const archive = useArchiveStore();
    saving.value = true;
    try {
      await flushPending();
      const path = await EditorService.SaveAsDialog();
      if (path) {
        for (const t of tabs.value) {
          t.original = t.text;
          t.modified = false;
        }
        await archive.refreshInfo();
      }
      return path || null;
    } finally {
      saving.value = false;
    }
  }

  async function flushPending() {
    window.clearTimeout(syncTimer);
    const batch = [...pendingSync];
    pendingSync.clear();
    for (const i of batch) {
      const t = tabs.value.find((x) => x.index === i);
      if (t) await syncTab(t);
    }
  }

  async function syncTab(tab: EditorTab) {
    const text = tab.text;
    await EditorService.SetText(tab.index, text);
    const annotations = (await EditorService.GetAnnotations(tab.index)) ?? [];
    const current = tabs.value.find((item) => item.index === tab.index);
    if (!current || current.text !== text) return;
    current.annotations = annotations.filter(
      (annotation): annotation is EditorAnnotation => !!annotation
    );
  }

  async function refreshAnnotations() {
    await flushPending();
    await Promise.all(
      tabs.value.map(async (tab) => {
        const text = tab.text;
        const annotations = (await EditorService.GetAnnotations(tab.index)) ?? [];
        const current = tabs.value.find((item) => item.index === tab.index);
        if (!current || current.text !== text) return;
        current.annotations = annotations.filter(
          (annotation): annotation is EditorAnnotation => !!annotation
        );
      })
    );
  }

  return {
    tabs,
    activeKey,
    activeTab,
    dirtyCount,
    opening,
    saving,
    openFile,
    closeTab,
    updateContent,
    save,
    saveAs,
    flushPending,
    refreshAnnotations,
  };
});
