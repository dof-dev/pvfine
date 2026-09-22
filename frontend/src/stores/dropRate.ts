import { computed, ref } from "vue";
import { defineStore } from "pinia";
import { DropService } from "../../bindings/pvfine/services";
import type {
  DropRateApplyRequest,
  DropRateDocument,
  DropRateGroup,
  DropRateSection,
} from "../../bindings/pvfine/services/models";
import { useArchiveStore } from "./archive";

export const DROP_RATE_LABELS = ["白云", "蓝天", "稀有", "神器", "史诗"] as const;

export interface DropRateGroupForm {
  rates: Array<number | null>;
}

export interface DropRateSectionForm {
  key: string;
  groups: DropRateGroupForm[];
}

export const DROP_RATE_SECTION_META: Record<
  string,
  { title: string; groupLabels: string[]; path: string }
> = {
  hell: {
    title: "深渊",
    groupLabels: ["困难", "非常困难"],
    path: "etc/itemdropinfo_monster_hell.etc",
  },
  flip: {
    title: "翻牌",
    groupLabels: ["困难"],
    path: "etc/itemdropinfo_clearreward.etc",
  },
  monster: {
    title: "小怪",
    groupLabels: ["普通", "冒险", "王者", "地狱"],
    path: "etc/itemdropinfo_monseter.etc",
  },
  elite: {
    title: "精英",
    groupLabels: ["普通", "冒险", "王者", "地狱"],
    path: "etc/itemdropinfo_monseter_extra.etc",
  },
};

export function percentToBasisPoints(value: number | null): number | null {
  if (value === null || !Number.isFinite(value)) return null;
  if (value < 0 || value > 100) return null;
  const scaled = Math.round(value * 100);
  if (Math.abs(value * 100 - scaled) > 1e-7) return null;
  return scaled;
}

function ratesToBasisPoints(rates: Array<number | null>): number[] | null {
  const values = rates.map(percentToBasisPoints);
  if (values.some((value) => value === null)) return null;
  return values as number[];
}

export function groupTotal(group: DropRateGroupForm): number | null {
  const values = ratesToBasisPoints(group.rates);
  if (!values) return null;
  return values.reduce((sum, value) => sum + value, 0) / 100;
}

export function validateDropRateSections(sections: DropRateSectionForm[]): string {
  const seen = new Set<string>();
  for (const section of sections) {
    const meta = DROP_RATE_SECTION_META[section.key];
    if (!meta) return `未知掉率类型: ${section.key}`;
    if (seen.has(section.key)) return `掉率类型重复: ${section.key}`;
    seen.add(section.key);
    if (section.groups.length !== meta.groupLabels.length) {
      return `${meta.title}的分组数量不正确`;
    }
    for (let groupIndex = 0; groupIndex < section.groups.length; groupIndex++) {
      const group = section.groups[groupIndex];
      if (group.rates.length !== DROP_RATE_LABELS.length) {
        return `${meta.title} ${meta.groupLabels[groupIndex]}的概率数量不正确`;
      }
      const values = ratesToBasisPoints(group.rates);
      if (!values) {
        return `${meta.title} ${meta.groupLabels[groupIndex]}包含无效概率（最多两位小数）`;
      }
      const sum = values.reduce((total, value) => total + value, 0);
      if (sum !== 10000) {
        return `${meta.title} ${meta.groupLabels[groupIndex]}的总和必须为 100.00%`;
      }
    }
  }
  if (seen.size !== Object.keys(DROP_RATE_SECTION_META).length) {
    return "掉率数据不完整";
  }
  return "";
}

function formFromDocument(document: DropRateDocument): DropRateSectionForm[] {
  return (document.sections ?? [])
    .filter((section): section is DropRateSection => !!section)
    .map((section) => ({
      key: section.key,
      groups: (section.groups ?? [])
        .filter((group): group is DropRateGroup => !!group)
        .map((group) => ({
          rates: (group.rates ?? []).map((rate) => rate / 100),
        })),
    }));
}

function formFingerprint(sections: DropRateSectionForm[]): string {
  return JSON.stringify(
    sections.map((section) => ({
      key: section.key,
      groups: section.groups.map((group) => group.rates),
    })),
  );
}

function requestFromForm(revision: number, sections: DropRateSectionForm[]): DropRateApplyRequest {
  return {
    revision,
    sections: sections.map((section) => ({
      key: section.key,
      groups: section.groups.map((group) => ({
        rates: group.rates.map((rate) => percentToBasisPoints(rate) ?? 0),
      })),
    })),
  };
}

export const useDropRateStore = defineStore("dropRate", () => {
  const archive = useArchiveStore();
  const visible = ref(false);
  const loading = ref(false);
  const applying = ref(false);
  const error = ref("");
  const revision = ref(0);
  const pvfVersion = ref("");
  const sections = ref<DropRateSectionForm[]>([]);
  const baseline = ref("");

  const supported = computed(
    () =>
      archive.open &&
      (archive.info?.format === "standard" || archive.info?.format === "alternate"),
  );
  const dirty = computed(() => formFingerprint(sections.value) !== baseline.value);
  const validationError = computed(() => validateDropRateSections(sections.value));
  const canApply = computed(
    () =>
      visible.value &&
      !loading.value &&
      !applying.value &&
      sections.value.length > 0 &&
      !validationError.value,
  );

  function reset(): void {
    revision.value = 0;
    pvfVersion.value = "";
    sections.value = [];
    baseline.value = "";
    error.value = "";
  }

  async function load(): Promise<boolean> {
    if (!supported.value) {
      error.value = "基础掉率编辑器仅支持已打开的 90US/90CN PVF";
      return false;
    }
    loading.value = true;
    error.value = "";
    try {
      const document = await DropService.Read();
      if (!document) throw new Error("后端没有返回掉率数据");
      const next = formFromDocument(document);
      sections.value = next;
      revision.value = document.revision;
      pvfVersion.value = document.pvfVersion;
      baseline.value = formFingerprint(next);
      return true;
    } catch (value: any) {
      error.value = String(value?.message ?? value ?? "读取掉率失败");
      return false;
    } finally {
      loading.value = false;
    }
  }

  async function open(): Promise<boolean> {
    if (!supported.value || loading.value || applying.value) return false;
    visible.value = true;
    const loaded = await load();
    if (!loaded) visible.value = false;
    return loaded;
  }

  function close(): void {
    if (loading.value || applying.value) return;
    visible.value = false;
  }

  async function apply(): Promise<boolean> {
    if (!canApply.value) {
      error.value = validationError.value || "当前不能应用掉率修改";
      return false;
    }
    applying.value = true;
    error.value = "";
    try {
      const result = await DropService.Apply(requestFromForm(revision.value, sections.value));
      revision.value = result.revision;
      baseline.value = formFingerprint(sections.value);
      return true;
    } catch (value: any) {
      error.value = String(value?.message ?? value ?? "应用掉率失败");
      return false;
    } finally {
      applying.value = false;
    }
  }

  return {
    visible,
    loading,
    applying,
    error,
    revision,
    pvfVersion,
    sections,
    supported,
    dirty,
    validationError,
    canApply,
    load,
    open,
    close,
    apply,
    reset,
  };
});
