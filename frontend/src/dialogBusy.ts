import type { DialogReactive } from "naive-ui";

/**
 * 把确认弹窗切换到“进行中”状态：主按钮进入 loading 并禁用次按钮，同时屏蔽
 * 关闭按钮、ESC 与遮罩点击，避免长耗时操作（如恢复缓存）期间被误关闭。
 * 结束后传入原始按钮文案即可恢复交互。
 */
export function setDialogBusy(
  instance: DialogReactive,
  busy: boolean,
  positiveText: string,
  busyText = "处理中…"
): void {
  instance.loading = busy;
  instance.positiveText = busy ? busyText : positiveText;
  instance.negativeButtonProps = { disabled: busy };
  instance.closable = !busy;
  instance.maskClosable = !busy;
  instance.closeOnEsc = !busy;
}
