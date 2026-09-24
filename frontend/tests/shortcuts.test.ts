import { describe, expect, it, vi } from "vitest";
import {
  assignBinding,
  bindingFromEvent,
  commandForBinding,
  dispatchShortcut,
  effectiveBinding,
  formatBinding,
  isValidShortcutBinding,
  reservedBindingReason,
} from "../src/shortcuts";

function keyEvent(code: string, options: Partial<KeyboardEvent> = {}): KeyboardEvent {
  return {
    code,
    ctrlKey: false,
    metaKey: false,
    altKey: false,
    shiftKey: false,
    defaultPrevented: false,
    isComposing: false,
    repeat: false,
    preventDefault: vi.fn(),
    ...options,
  } as KeyboardEvent;
}

describe("应用快捷键", () => {
  it("将不同平台的主修饰键规范为 Mod 并使用物理键码", () => {
    expect(bindingFromEvent(keyEvent("KeyS", { metaKey: true, shiftKey: true }), true)).toBe("Mod+Shift+KeyS");
    expect(bindingFromEvent(keyEvent("KeyS", { ctrlKey: true, shiftKey: true }), false)).toBe("Mod+Shift+KeyS");
    expect(bindingFromEvent(keyEvent("Backslash", { ctrlKey: true }), false)).toBe("Mod+Backslash");
    expect(formatBinding("Mod+Backslash", true)).toMatch(/^⌘ /);
    expect(formatBinding("Mod+KeyO", false)).toBe("Ctrl+O");
  });

  it("提供新增应用命令的默认键位", () => {
    const defaults = {
      "search.advanced": "Mod+Shift+KeyF",
      "settings.open": "Mod+Comma",
      "editor.closeOthers": "Mod+Shift+KeyW",
      "editor.closeAll": "Mod+Alt+KeyW",
    } as const;
    for (const [command, binding] of Object.entries(defaults)) {
      expect(effectiveBinding(command as keyof typeof defaults, {})).toBe(binding);
      expect(commandForBinding(binding, {})).toBe(command);
    }
    expect(effectiveBinding("version.open", {})).toBe("");
    expect(bindingFromEvent(keyEvent("Comma", { metaKey: true }), true)).toBe("Mod+Comma");
  });

  it("拒绝裸字母、仅修饰键和系统或编辑器保留键", () => {
    expect(bindingFromEvent(keyEvent("KeyA"), false)).toBeNull();
    expect(bindingFromEvent(keyEvent("KeyA", { shiftKey: true }), false)).toBeNull();
    expect(bindingFromEvent(keyEvent("ControlLeft", { ctrlKey: true }), false)).toBeNull();
    expect(isValidShortcutBinding("Alt+Tab")).toBe(false);
    expect(isValidShortcutBinding("F5")).toBe(true);
    expect(isValidShortcutBinding("Shift+F5")).toBe(true);
    expect(reservedBindingReason("Mod+KeyQ")).toMatch(/系统/);
    expect(reservedBindingReason("Alt+ArrowLeft")).toMatch(/编辑器/);
    expect(reservedBindingReason("Mod+Shift+KeyG")).toMatch(/编辑器/);
  });

  it("确认替换时解绑旧命令，清除和恢复默认有确定语义", () => {
    const assigned = assignBinding("settings.open", "Mod+KeyO", {});
    expect(assigned.displaced).toBe("archive.open");
    expect(assigned.overrides["archive.open"]).toBe("");
    expect(commandForBinding("Mod+KeyO", assigned.overrides)).toBe("settings.open");
    const cleared = assignBinding("settings.open", "", assigned.overrides);
    expect(effectiveBinding("settings.open", cleared.overrides)).toBe("");
    const restored = assignBinding("archive.open", "Mod+KeyO", cleared.overrides);
    expect(restored.overrides["archive.open"]).toBeUndefined();
    expect(effectiveBinding("archive.open", restored.overrides)).toBe("Mod+KeyO");
  });

  it("弹窗、已处理的编辑器事件和不可用命令优先于应用分发", () => {
    const execute = vi.fn();
    const event = keyEvent("KeyO", { ctrlKey: true });
    expect(dispatchShortcut(event, {}, true, execute, undefined, undefined, false)).toBeNull();
    expect(dispatchShortcut(keyEvent("KeyO", { ctrlKey: true, defaultPrevented: true }), {}, false, execute, undefined, undefined, false)).toBeNull();
    expect(dispatchShortcut(event, {}, false, execute, undefined, () => false, false)).toBeNull();
    expect(execute).not.toHaveBeenCalled();
    expect(dispatchShortcut(event, {}, false, execute, undefined, () => true, false)).toBe("archive.open");
    expect(event.preventDefault).toHaveBeenCalledOnce();
    expect(execute).toHaveBeenCalledWith("archive.open");
  });

  it("关闭其他和关闭全部标签的组合键分发到不同命令", () => {
    const execute = vi.fn();
    expect(dispatchShortcut(keyEvent("KeyW", { ctrlKey: true, shiftKey: true }), {}, false, execute, undefined, undefined, false)).toBe("editor.closeOthers");
    expect(dispatchShortcut(keyEvent("KeyW", { ctrlKey: true, altKey: true }), {}, false, execute, undefined, undefined, false)).toBe("editor.closeAll");
    expect(execute.mock.calls.map(([command]) => command)).toEqual(["editor.closeOthers", "editor.closeAll"]);
  });

  it("workspace.save 快捷键异常拦截：saveActiveTab 抛出错误时捕获并路由至消息提示", async () => {
    const errorMessages: string[] = [];
    const mockMessageApi = {
      error: (msg: string) => errorMessages.push(msg),
    };

    // 模拟 saveActiveTab 抛出 GUI 未应用修改错误
    const saveActiveTab = vi.fn().mockRejectedValue(
      new Error("当前文件存在未应用的界面修改，请先在界面中点击【应用修改】后再保存"),
    );

    function executeSaveShortcut() {
      saveActiveTab().catch((err: any) => {
        mockMessageApi.error(err?.message ?? String(err));
      });
    }

    const event = keyEvent("KeyS", { ctrlKey: true });
    const dispatched = dispatchShortcut(
      event,
      {},
      false,
      (cmd) => {
        if (cmd === "workspace.save") executeSaveShortcut();
      },
      undefined,
      () => true,
      false,
    );

    expect(dispatched).toBe("workspace.save");
    expect(saveActiveTab).toHaveBeenCalledOnce();

    // 等待微任务异步捕获完成
    await Promise.resolve();

    expect(errorMessages).toEqual([
      "当前文件存在未应用的界面修改，请先在界面中点击【应用修改】后再保存",
    ]);
  });
});
