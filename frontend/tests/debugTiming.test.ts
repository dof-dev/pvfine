import { beforeEach, describe, expect, it, vi } from "vitest";
import { createPinia, setActivePinia } from "pinia";
import { startDebugTiming } from "../src/stores/debugTiming";
import { useLogStore } from "../src/stores/logs";

describe("debug timing", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.restoreAllMocks();
  });

  it("records request start and elapsed time at DEBUG level", () => {
    vi.spyOn(performance, "now").mockReturnValueOnce(100).mockReturnValueOnce(123.5);
    const finish = startDebugTiming("editor.metadata-request", "file=42");
    finish("failed");
    const logs = useLogStore();
    expect(logs.entries.map((entry) => entry.level)).toEqual(["DEBUG", "DEBUG"]);
    expect(logs.entries[0].message).toBe("editor.metadata-request started file=42");
    expect(logs.entries[1].message).toBe("editor.metadata-request failed file=42 elapsed=23.50ms");
    expect(logs.filtered).toHaveLength(0);
    logs.levels.push("DEBUG");
    expect(logs.filtered).toHaveLength(2);
  });
});
