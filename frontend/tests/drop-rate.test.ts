import { expect, test } from "vitest";
import {
  groupTotal,
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
