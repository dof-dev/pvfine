import { useArchiveStore } from "./stores/archive";
import { useEditorStore } from "./stores/editor";
import { useFileSetStore } from "./stores/fileSets";
import { useScriptStore } from "./stores/script";
import { useVersionStore } from "./stores/version";

/**
 * 工作区是否有未保存的修改。退出确认与定时缓存都依赖它，避免两处判定漂移。
 */
export function hasUnsavedWorkspaceChanges(): boolean {
  const archive = useArchiveStore();
  const editor = useEditorStore();
  const fileSets = useFileSetStore();
  const version = useVersionStore();
  const script = useScriptStore();
  return (
    archive.modifiedCount > 0 ||
    editor.dirtyCount > 0 ||
    fileSets.dirty ||
    // 工作区分离后脚本内容在独立窗口里：本窗口的 script.dirty 停留在分离那一刻
    // 的值（在那边保存也不会同步回来），必须改用独立窗口上报的 dirty，否则
    // 会在已保存的情况下误报有未保存修改。
    (script.workspaceDetached ? script.detachedDirty : script.dirty) ||
    version.status.changedFiles > 0 ||
    version.status.needsSave
  );
}

/**
 * 工作区是否有会写进 PVF 的改动。定时缓存只关心归档内容：文件集与独立脚本
 * 工作区各自保存到自己的磁盘文件，缓存它们没有意义。
 */
export function hasArchiveChanges(): boolean {
  const archive = useArchiveStore();
  const editor = useEditorStore();
  return archive.modifiedCount > 0 || editor.dirtyCount > 0;
}
