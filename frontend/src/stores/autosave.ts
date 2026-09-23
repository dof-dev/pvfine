import { defineStore } from "pinia";
import { ref, watch } from "vue";
import { AutosaveService } from "../../bindings/pvfine/services";
import type { AutosaveStatus, RecoveryInfo } from "../../bindings/pvfine/services/models";
import { hasArchiveChanges } from "../unsaved";
import { useArchiveStore } from "./archive";
import { useEditorStore } from "./editor";
import { useSettingsStore } from "./settings";
import { useVersionStore } from "./version";

/** 定时缓存服务最小时钟间隔(秒),与后端设置校验保持一致。 */
const minTimerSeconds = 30;

/** 定时把未保存的工作区另存到备份缓存,并负责启动时的恢复提示。 */
export const useAutosaveStore = defineStore("autosave", () => {
  const archive = useArchiveStore();
  const editor = useEditorStore();
  const settings = useSettingsStore();
  const version = useVersionStore();

  const status = ref<AutosaveStatus | null>(null);
  const running = ref(false);
  const restoring = ref(false);
  const lastSnapshotAt = ref(0);
  const lastError = ref("");
  const pendingRecovery = ref<RecoveryInfo | null>(null);
  const checked = ref(false);
  let timer: number | undefined;

  function stopTimer(): void {
    if (timer !== undefined) window.clearInterval(timer);
    timer = undefined;
  }

  function restartTimer(): void {
    stopTimer();
    if (!settings.autosaveEnabled) return;
    const seconds = Math.max(minTimerSeconds, settings.autosaveIntervalSeconds);
    timer = window.setInterval(() => void tick(), seconds * 1000);
  }

  /** 一次定时快照:先把编辑器草稿写进 overlay,再让后端另存缓存。 */
  async function tick(): Promise<void> {
    if (running.value || !settings.autosaveEnabled || !archive.open) return;
    if (editor.saving || archive.loading || version.status.loading) return;
    if (!hasArchiveChanges()) return;
    running.value = true;
    try {
      if (editor.dirtyCount > 0) await editor.saveAllDirty();
      if (!hasArchiveChanges()) return;
      await AutosaveService.CreateSnapshot();
      lastSnapshotAt.value = Date.now();
      lastError.value = "";
      void refreshStatus();
    } catch (error: any) {
      // 缓存失败不能打断编辑:记录后在设置页展示。
      lastError.value = String(error?.message ?? error);
    } finally {
      running.value = false;
    }
  }

  async function refreshStatus(): Promise<AutosaveStatus | null> {
    try {
      status.value = await AutosaveService.Status();
      // 后端记录的错误是权威值:这里同步覆盖,避免旧的失败信息一直挂在设置页。
      lastError.value = status.value?.lastError ?? "";
    } catch (error) {
      console.error("load autosave status failed", error);
    }
    return status.value;
  }

  /** 查询待恢复备份。启动阶段只查询一次,重复调用直接复用结果。 */
  async function checkRecovery(): Promise<RecoveryInfo | null> {
    if (checked.value) return pendingRecovery.value;
    checked.value = true;
    try {
      pendingRecovery.value = await AutosaveService.PendingRecovery();
    } catch (error) {
      console.error("check autosave recovery failed", error);
      pendingRecovery.value = null;
    }
    return pendingRecovery.value;
  }

  /**
   * 恢复备份:成功后工作区仍是“未保存”状态,保存才会写回原文件。
   * 打开原文件与重放内容都发生在后端,大归档耗时较长,调用方应展示加载态。
   */
  async function restore(): Promise<boolean> {
    if (restoring.value) return false;
    restoring.value = true;
    try {
      const info = await AutosaveService.Restore();
      pendingRecovery.value = null;
      // 后端广播了 archive:opened,但事件到达顺序不确定:先落一次本地状态,
      // 让工具栏与状态栏立刻反映新的工作区。
      if (info) archive.info = info;
      void refreshStatus();
      return true;
    } catch (error: any) {
      lastError.value = String(error?.message ?? error);
      throw error;
    } finally {
      restoring.value = false;
    }
  }

  /** 选择缓存文件位置;返回空字符串表示用户取消。 */
  async function chooseCachePath(): Promise<string> {
    const path = await AutosaveService.ChooseCachePath();
    return typeof path === "string" ? path : "";
  }

  /** 丢弃备份缓存(恢复提示里的“丢弃备份”、设置页的“删除备份”)。 */
  async function discard(): Promise<boolean> {
    try {
      const removed = await AutosaveService.Discard();
      pendingRecovery.value = null;
      void refreshStatus();
      return removed !== false;
    } catch (error: any) {
      lastError.value = String(error?.message ?? error);
      throw error;
    }
  }

  /** 退出时确认放弃未保存修改:与其配套的缓存也没有保护对象了。 */
  async function discardForWorkspace(): Promise<void> {
    try {
      await AutosaveService.DropForWorkspace();
      pendingRecovery.value = null;
      void refreshStatus();
    } catch (error) {
      // 退出流程不能被缓存清理阻塞。
      console.error("drop autosave backup failed", error);
    }
  }

  /** 启动时调用:刷新缓存状态、启动定时器、检查是否有待恢复的备份。 */
  async function initialize(): Promise<void> {
    await refreshStatus();
    restartTimer();
    await checkRecovery();
  }

  watch(
    () => settings.autosaveEnabled,
    (enabled, previous) => {
      restartTimer();
      // 刚打开开关时不必等一个完整周期。
      if (enabled && previous === false) void tick();
    }
  );
  watch(
    () => settings.autosaveIntervalSeconds,
    () => restartTimer()
  );

  return {
    status,
    running,
    restoring,
    lastSnapshotAt,
    lastError,
    pendingRecovery,
    checked,
    initialize,
    tick,
    refreshStatus,
    checkRecovery,
    restore,
    chooseCachePath,
    discard,
    discardForWorkspace,
    restartTimer,
    stopTimer,
  };
});
