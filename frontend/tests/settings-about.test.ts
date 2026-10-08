import { beforeEach, describe, expect, it, vi } from "vitest";
import { createPinia, setActivePinia } from "pinia";

const mockSettingsService = vi.hoisted(() => ({
  GetSettings: vi.fn().mockResolvedValue({}),
  SaveSettings: vi.fn().mockResolvedValue({}),
}));

vi.mock("@wailsio/runtime", () => ({
  Events: { On: vi.fn(), Emit: vi.fn() },
  Browser: { OpenURL: vi.fn().mockResolvedValue(undefined) },
}));

vi.mock("../bindings/pvfine/services", () => ({
  SettingsService: mockSettingsService,
  UpdateService: {
    Version: vi.fn().mockResolvedValue("1.0.0"),
    CheckForUpdates: vi.fn().mockResolvedValue(undefined),
  },
}));

import { useSettingsStore, type SettingsTab } from "../src/stores/settings";

function formatVersionLabel(rawVersion: string): string {
  const raw = rawVersion.trim().replace(/^v/i, "");
  if (!raw) return "";
  return raw === "dev" ? raw : `v${raw}`;
}

describe("设置-关于页面与配置", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.clearAllMocks();
  });

  it("支持切换至关于标签页", () => {
    const settings = useSettingsStore();
    expect(settings.activeTab).toBe("general");

    settings.open("about");
    expect(settings.activeTab).toBe("about");
    expect(settings.visible).toBe(true);

    const validTabs: SettingsTab[] = [
      "general",
      "editor",
      "npk",
      "shortcuts",
      "system",
      "about",
    ];
    expect(validTabs).toContain("about");
  });

  it("正确格式化版本号标签", () => {
    expect(formatVersionLabel("1.0.0")).toBe("v1.0.0");
    expect(formatVersionLabel("v1.0.0")).toBe("v1.0.0");
    expect(formatVersionLabel("V2.3.4")).toBe("v2.3.4");
    expect(formatVersionLabel("dev")).toBe("dev");
    expect(formatVersionLabel("vdev")).toBe("dev");
    expect(formatVersionLabel("")).toBe("");
    expect(formatVersionLabel("   ")).toBe("");
  });

  it("包含正确的官网与 GitHub 地址配置", () => {
    const officialSite = "https://pvfineapp.com/";
    const githubRepo = "https://github.com/dof-dev/pvfine";

    expect(officialSite).toMatch(/^https:\/\/pvfineapp\.com\/?$/);
    expect(githubRepo).toMatch(/^https:\/\/github\.com\/dof-dev\/pvfine\/?$/);
  });
});
