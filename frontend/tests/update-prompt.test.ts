import { beforeEach, expect, test, vi } from "vitest";
import { createSSRApp } from "vue";
import { renderToString } from "@vue/server-renderer";
import UpdatePrompt from "../src/components/UpdatePrompt.vue";

const state = vi.hoisted(() => ({
  listeners: new Map<string, (event: any) => void>(),
  info: vi.fn(), emit: vi.fn().mockResolvedValue(undefined), error: vi.fn(),
}));
vi.mock("@wailsio/runtime", () => ({ Events: {
  On: (name: string, callback: (event: any) => void) => {
    state.listeners.set(name, callback);
    return () => state.listeners.delete(name);
  },
  Emit: state.emit,
} }));
vi.mock("naive-ui", () => ({
  useDialog: () => ({ info: state.info }),
  useMessage: () => ({ info: vi.fn(), error: state.error }),
}));
beforeEach(async () => {
  vi.clearAllMocks();
  state.listeners.clear();
  state.info.mockReturnValue({ destroy: vi.fn() });
  await renderToString(createSSRApp(UpdatePrompt));
});
function requestUpdate() {
  state.listeners.get("app:update-available")!({ data: {
    currentVersion: "1.2.3", version: "1.2.4", responseEvent: "app:update-response:1",
  } });
  return state.info.mock.calls.at(-1)![0];
}

test("显示版本且等待明确点击下载，重复点击只提交一次", async () => {
  const prompt = requestUpdate();
  expect(prompt.content).toContain("1.2.3");
  expect(prompt.content).toContain("1.2.4");
  expect(prompt.positiveText).toBe("下载更新");
  expect(prompt.autoFocus).toBe(false);
  expect(state.emit).not.toHaveBeenCalled();
  requestUpdate();
  expect(state.info).toHaveBeenCalledTimes(1);
  await prompt.onPositiveClick();
  await prompt.onPositiveClick();
  expect(state.emit).toHaveBeenCalledExactlyOnceWith("app:update-response:1", { accepted: true });
});
for (const action of ["onNegativeClick", "onClose", "onEsc"]) {
  test(`${action} 取消下载并允许再次提示`, async () => {
    const prompt = requestUpdate();
    await prompt[action]();
    expect(state.emit).toHaveBeenCalledExactlyOnceWith("app:update-response:1", { accepted: false });
    requestUpdate();
    expect(state.info).toHaveBeenCalledTimes(2);
  });
}
test("确认发送失败时提示错误，允许重试", async () => {
  const prompt = requestUpdate();
  state.emit.mockRejectedValueOnce(new Error("断开连接"));
  await expect(prompt.onPositiveClick()).resolves.toBe(false);
  expect(state.error).toHaveBeenCalled();
  await prompt.onNegativeClick();
  expect(state.emit).toHaveBeenLastCalledWith("app:update-response:1", { accepted: false });
});
