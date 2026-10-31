import { expect, test, vi } from "vitest";
import { createPinia, setActivePinia } from "pinia";
import { reactive } from "vue";
const api = vi.hoisted(() => ({ archive: null as any, Read: vi.fn() }));
vi.mock("../src/stores/archive", () => ({ useArchiveStore: () => api.archive }));
vi.mock("../bindings/pvfine/services", () => ({ DropService: api }));
import {
  groupTotal,
  useDropRateStore,
  percentToBasisPoints,
  validateDropRateSections,
  type DropRateSectionForm,
} from "../src/stores/dropRate";

function section(key: string, groupCount: number): DropRateSectionForm {
  return {
    key,
    groups: Array.from({ length: groupCount }, () => ({
      rates: [60, 24.49, 10.51, 5, 0],
    })),
  };
}

test("百分比转换只接受两位小数并使用百分之一单位", () => {
  expect(percentToBasisPoints(12.34)).toBe(1234);
  expect(percentToBasisPoints(0)).toBe(0);
  expect(percentToBasisPoints(12.345)).toBeNull();
  expect(percentToBasisPoints(100.01)).toBeNull();
});

test("每组总和以百分比显示并保持精度", () => {
  expect(groupTotal({ rates: [60, 24.49, 10.51, 5, 0] })).toBe(100);
  expect(groupTotal({ rates: [60, null, 10, 5, 0] })).toBeNull();
});

test("四类掉率的表单校验要求每组总和为 100.00%", () => {
  const sections = [
    section("hell", 2),
    section("flip", 1),
    section("monster", 4),
    section("elite", 4),
  ];
  expect(validateDropRateSections(sections)).toBe("");

  sections[2].groups[0].rates[0] = 60.01;
  expect(validateDropRateSections(sections)).toContain("小怪 普通");
});


test("已验证的六个版本可以打开基础掉率，其余格式仍禁用", async () => {
 setActivePinia(createPinia());
 api.archive = reactive({ open: true, info: { format: "tw" } });
 const store = useDropRateStore();
 const sections = [section("hell",2), section("flip",1), section("monster",4), section("elite",4)];
 api.Read.mockResolvedValue({ pvfVersion: "TW", revision: 1, sections: sections.map(s => ({ key:s.key,groups:s.groups.map(g => ({ rates:g.rates.map(v => Math.round(v! * 100)) })) })) });
 expect(store.supported).toBe(true);
 expect(await store.load()).toBe(true);
 expect(store.pvfVersion).toBe("TW");
 expect(store.validationError).toBe("");
 for (const format of ["standard","alternate","paged110","paged100cn","paged115cn"]) { api.archive.info.format=format; expect(store.supported).toBe(true); }
 for (const format of ["paged115","paged","recovered"]) { api.archive.info.format=format; expect(store.supported).toBe(false); }
 api.archive.info.format="tw"; api.archive.open=false; expect(store.supported).toBe(false);
});
