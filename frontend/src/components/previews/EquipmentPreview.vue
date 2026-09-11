<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { NButton, NIcon, NSpin } from "naive-ui";
import { Money24Filled } from "@vicons/fluent";
import { PreviewService } from "../../../bindings/pvfine/services";
import type {
  EquipmentPreviewAttribute,
  EquipmentPreviewDocument,
  EquipmentSkillLevelup,
  ImageData,
  PreviewIssue,
} from "../../../bindings/pvfine/services/models";
import { useImageStore } from "../../stores/images";
import type { PreviewFile } from "../../previews/types";

const props = defineProps<{
  file: PreviewFile;
  active: boolean;
}>();

const images = useImageStore();
const document = ref<EquipmentPreviewDocument | null>(null);
const parserIssues = ref<PreviewIssue[]>([]);
const parserLoading = ref(false);
const iconData = ref<ImageData | null>(null);
const detailMode = ref(false);

let parseTimer: number | undefined;
let parseRequest = 0;

const rarityClass = computed(() => {
  const rarity = document.value?.rarity ?? 0;
  return ["normal", "magic", "rare", "artifact", "epic", "brave", "legendary"][rarity] ?? "unknown";
});
const explanation = computed(() => {
  if (detailMode.value && document.value?.detailExplain) return document.value.detailExplain;
  return document.value?.baseExplain ?? "";
});
const usableJobs = computed(() => document.value?.usableJobs ?? []);
const baseAttributes = computed(() => document.value?.baseAttributes ?? []);
const fourDimensions = computed(() => document.value?.fourDimensions ?? []);
const otherAttributes = computed(() => document.value?.otherAttributes ?? []);
const skillLevelups = computed(() => document.value?.skillLevelups ?? []);
const footerNeedsSeparator = computed(() => {
  const current = document.value;
  if (!current) return false;
  const hasOtherEffect = !!current.baseExplain || !!current.detailExplain;
  const hasOtherAttribute = otherAttributes.value.length > 0;
  return (hasOtherEffect || hasOtherAttribute) &&
    skillLevelups.value.length === 0 &&
    !current.flavorText &&
    !current.durabilityText;
});
const qualityParts = computed(() => {
  const value = document.value?.qualityText ?? "";
  const match = value.match(/^(.*?)(\([^)]*\))$/);
  return {
    label: match?.[1] ?? value,
    detail: match?.[2] ?? "",
  };
});
const allIssues = computed(() => {
  const issues = [...parserIssues.value];
  if (document.value?.icon && !iconData.value) {
    issues.push({
      severity: "warning",
      line: 0,
      section: "icon",
      message: `图片加载失败：${document.value.icon.path}[${document.value.icon.index}]`,
    });
  }
  return issues;
});
const visibleIssues = computed(() => allIssues.value.slice(0, 5));
const omittedIssueCount = computed(() => Math.max(0, allIssues.value.length - visibleIssues.value.length));

function clearParseTimer(): void {
  if (parseTimer !== undefined) {
    window.clearTimeout(parseTimer);
    parseTimer = undefined;
  }
}

function clearForPathChange(): void {
  document.value = null;
  parserIssues.value = [];
  iconData.value = null;
  detailMode.value = false;
}

async function loadIcon(next: EquipmentPreviewDocument, request: number): Promise<void> {
  iconData.value = null;
  if (!next.icon || next.icon.path.trim() === "" || next.icon.index < 0) return;
  const data = await images.loadImage(next.icon);
  if (request === parseRequest) iconData.value = data;
}

async function parseText(text: string, request: number): Promise<void> {
  parserLoading.value = true;
  try {
    const result = await PreviewService.ParseEQU(props.file.index, text);
    if (request !== parseRequest || !result) return;
    parserIssues.value = result.issues ?? [];
    const hasErrors = parserIssues.value.some((issue) => issue.severity === "error");
    if (!hasErrors || !document.value) {
      document.value = result;
      await loadIcon(result, request);
    }
  } catch (error: any) {
    if (request !== parseRequest) return;
    parserIssues.value = [{
      severity: "error",
      line: 1,
      message: `EQU 解析失败：${error?.message ?? error}`,
    }];
    // 保留 document.value 和上一次成功的图标，避免编辑中的短暂错误清空画面。
  } finally {
    if (request === parseRequest) parserLoading.value = false;
  }
}

function scheduleParse(): void {
  clearParseTimer();
  const request = ++parseRequest;
  parserLoading.value = true;
  parseTimer = window.setTimeout(() => {
    parseTimer = undefined;
    void parseText(props.file.text, request);
  }, 150);
}

function onWindowKeydown(event: KeyboardEvent): void {
  if (!props.active || event.key !== "F4" || !document.value?.detailExplain) return;
  event.preventDefault();
  detailMode.value = !detailMode.value;
}

function attributeClass(attribute: EquipmentPreviewAttribute): string {
  return attribute.negative ? "equ-attribute equ-attribute--negative" : "equ-attribute";
}

function skillText(skill: EquipmentSkillLevelup): string {
  return `[${skill.skill}] 技能 Lv +${skill.level}`;
}

function otherAttributeClass(attribute: EquipmentPreviewAttribute): string {
  return attribute.negative
    ? "equ-attribute equ-attribute--other equ-attribute--negative"
    : "equ-attribute equ-attribute--other";
}

function formatIssue(issue: PreviewIssue): string {
  return `${issue.line > 0 ? `第 ${issue.line} 行：` : ""}${issue.message}`;
}

watch(
  () => [props.file.path, props.file.text] as const,
  ([path], previous) => {
    if (previous && previous[0] !== path) clearForPathChange();
    scheduleParse();
  },
  { immediate: true },
);

watch(
  () => images.revision,
  () => {
    const current = document.value;
    if (current && parseRequest > 0) void loadIcon(current, parseRequest);
  },
);

onMounted(() => window.addEventListener("keydown", onWindowKeydown));
onBeforeUnmount(() => {
  parseRequest++;
  clearParseTimer();
  window.removeEventListener("keydown", onWindowKeydown);
});
</script>

<template>
  <div class="equ-preview">
    <div v-if="document" class="equ-tooltip">
      <div class="equ-header">
        <div v-if="iconData?.dataUrl" class="equ-icon-wrap">
          <img class="equ-icon" :src="iconData.dataUrl" alt="装备图标" />
        </div>
        <div class="equ-title-block">
          <div class="equ-name" :class="`equ-name--${rarityClass}`">
            {{ document.name || "未命名装备" }}
          </div>
          <div v-if="document.name2" class="equ-name2">{{ document.name2 }}</div>
        </div>
      </div>

      <div class="equ-summary">
        <div v-if="document.qualityText || document.rarityLabel" class="equ-summary-top">
          <span v-if="document.qualityText" class="equ-quality">
            <span class="equ-quality-label">{{ qualityParts.label }}</span><span v-if="qualityParts.detail" class="equ-quality-detail">{{ qualityParts.detail }}</span>
          </span>
          <span v-if="document.rarityLabel" class="equ-rarity" :class="`equ-rarity--${rarityClass}`">{{ document.rarityLabel }}</span>
        </div>
        <div
          v-if="baseAttributes.length || document.equipmentType || document.itemGroupName || document.attachType"
          class="equ-summary-columns"
        >
          <div v-if="baseAttributes.length" class="equ-summary-column equ-summary-column--base">
            <div v-for="attribute in baseAttributes" :key="`base:${attribute.label}`" :class="attributeClass(attribute)">
              {{ attribute.label }}{{ attribute.value }}
            </div>
          </div>
          <div class="equ-summary-column equ-summary-column--category">
            <div v-if="document.equipmentType" class="equ-attribute">{{ document.equipmentType }}</div>
            <div v-if="document.itemGroupName" class="equ-attribute">{{ document.itemGroupName }}</div>
            <div v-if="document.attachType" class="equ-attribute">{{ document.attachType }}</div>
          </div>
        </div>
        <div v-if="document.minimumLevelText" class="equ-summary-line equ-summary-line--right">{{ document.minimumLevelText }}</div>
        <div v-if="usableJobs.length" class="equ-summary-line equ-summary-line--right">{{ usableJobs.join("、") }}可以使用</div>
      </div>

      <div v-if="fourDimensions.length" class="equ-section">
        <div v-for="attribute in fourDimensions" :key="`four:${attribute.label}`" :class="attributeClass(attribute)">
          {{ attribute.label }}{{ attribute.value }}
        </div>
      </div>

      <div v-if="otherAttributes.length" class="equ-section">
        <div v-for="(attribute, index) in otherAttributes" :key="`other:${attribute.label}:${index}`" :class="otherAttributeClass(attribute)">
          {{ attribute.label }}{{ attribute.value }}
        </div>
      </div>

      <div v-if="skillLevelups.length" class="equ-section equ-skills">
        <div v-for="(skill, index) in skillLevelups" :key="`skill:${skill.job}:${skill.skill}:${index}`" class="equ-attribute">
          <div class="equ-skill-job">{{ skill.job }}</div>
          <div class="equ-skill-name">{{ skillText(skill) }}</div>
        </div>
      </div>

      <div v-if="explanation" class="equ-section equ-explain" :class="{ 'equ-explain--detail': detailMode }">
        {{ explanation }}
      </div>

      <div v-if="document.flavorText" class="equ-section equ-flavor">{{ document.flavorText }}</div>
      <div v-if="document.detailExplain" class="equ-detail-toggle">
        <NButton quaternary size="tiny" class="equ-f4-button" @click="detailMode = !detailMode">
          {{ detailMode ? "返回基础说明(F4)" : "查看详细说明(F4)" }}
        </NButton>
      </div>

      <div v-if="document.durabilityText" class="equ-durability">耐久度 {{ document.durabilityText }}</div>
      <div v-if="document.weightText || document.priceText" class="equ-footer" :class="{ 'equ-footer--separated': footerNeedsSeparator }">
        <span v-if="document.weightText">{{ document.weightText }}</span>
        <span v-if="document.priceText" class="equ-price" title="售价">
          <NIcon :size="14"><Money24Filled /></NIcon>
          <span>{{ document.priceText }}</span>
        </span>
      </div>
    </div>

    <div v-else class="equ-empty">等待有效的装备数据…</div>
    <NSpin v-if="parserLoading" class="equ-loading" :size="14" />

    <div v-if="visibleIssues.length" class="equ-issues">
      <div v-for="(issue, index) in visibleIssues" :key="`${issue.line}:${issue.message}:${index}`" :class="`equ-issue equ-issue--${issue.severity}`">
        {{ formatIssue(issue) }}
      </div>
      <div v-if="omittedIssueCount">还有 {{ omittedIssueCount }} 条问题未显示</div>
    </div>
  </div>
</template>

<style scoped>
.equ-preview {
  position: relative;
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  overflow: auto;
  color: #fff;
  font-family: "Microsoft YaHei", "Noto Sans CJK SC", sans-serif;
  font-size: 12px;
  line-height: 1.38;
}
.equ-tooltip {
  min-width: 0;
  padding: 10px 12px 8px;
}
.equ-header {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 9px;
  min-height: 50px;
}
.equ-icon-wrap {
  display: flex;
  width: 50px;
  height: 50px;
  flex: 0 0 50px;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  background: rgba(255, 255, 255, 0.08);
}
.equ-icon {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: contain;
  image-rendering: auto;
}
.equ-title-block {
  min-width: 0;
}
.equ-name {
  overflow: hidden;
  font-size: 15px;
  font-weight: 700;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.equ-name--normal,
.equ-rarity--normal { color: #fff; }
.equ-name--magic,
.equ-rarity--magic { color: #5e9dff; }
.equ-name--rare,
.equ-rarity--rare { color: #b46cff; }
.equ-name--artifact,
.equ-rarity--artifact { color: #dc57b7; }
.equ-name--epic,
.equ-rarity--epic { color: #ffd438; }
.equ-name--brave,
.equ-rarity--brave { color: #ff4a4a; }
.equ-name--legendary,
.equ-rarity--legendary { color: #ff7903; }
.equ-name--unknown,
.equ-rarity--unknown { color: #fff; }
.equ-name2 {
  margin-top: 1px;
  color: #aaa;
  font-size: 11px;
}
.equ-summary,
.equ-section {
  margin-top: 7px;
  padding-top: 6px;
  border-top: 1px solid rgba(255, 255, 255, 0.12);
}
.equ-summary {
  color: #eee;
}
.equ-summary-top {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  min-height: 18px;
}
.equ-quality-label {
  color: #ffd12a;
}
.equ-quality-detail {
  margin-left: 2px;
  color: #999;
}
.equ-summary-columns {
  display: grid;
  grid-template-columns: minmax(0, 1.45fr) minmax(76px, 0.75fr);
  gap: 10px;
  margin-top: 2px;
}
.equ-summary-column {
  min-width: 0;
}
.equ-summary-column--category {
  text-align: right;
}
.equ-summary-line {
  min-height: 17px;
  color: #eee;
}
.equ-summary-line--right {
  text-align: right;
}
.equ-rarity {
  font-weight: 700;
}
.equ-attribute {
  min-height: 17px;
  color: #fff;
  white-space: pre-wrap;
  word-break: break-word;
}
.equ-attribute--negative {
  color: #ff7070;
}
.equ-attribute--other {
  color: #5fdcff;
}
.equ-attribute--other.equ-attribute--negative {
  color: #d0b5d9;
}
.equ-skills {
  color: #eee;
}
.equ-skill-job {
  color: #ffc32b;
}
.equ-skill-name {
  padding-left: 12px;
  color: #eee;
}
.equ-explain {
  color: #5fdcff;
  white-space: pre-wrap;
  word-break: break-word;
}
.equ-explain--detail {
  color: #5fdcff;
}
.equ-detail-toggle {
  display: flex;
  justify-content: center;
  margin-top: 3px;
}
.equ-f4-button {
  color: #caff4a;
  font-size: 11px;
}
.equ-flavor {
  color: #bdbdbd;
  white-space: pre-wrap;
  word-break: break-word;
}
.equ-durability {
  margin-top: 8px;
  padding-top: 6px;
  color: #aaa;
  border-top: 1px solid rgba(255, 255, 255, 0.12);
  font-size: 11px;
  text-align: right;
}
.equ-footer {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  gap: 4px 10px;
  margin-top: 2px;
  padding-top: 0;
  color: #aaa;
  font-size: 11px;
}
.equ-footer--separated {
  margin-top: 7px;
  padding-top: 6px;
  border-top: 1px solid rgba(255, 255, 255, 0.12);
}
.equ-price {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  color: #d6d6d6;
}
.equ-price :deep(.n-icon) {
  color: #f3c45f;
}
.equ-empty {
  display: flex;
  min-height: 120px;
  align-items: center;
  justify-content: center;
  padding: 18px;
  color: rgba(255, 255, 255, 0.6);
}
.equ-loading {
  position: absolute;
  top: 8px;
  right: 8px;
}
.equ-issues {
  flex: 0 0 auto;
  margin: 0 10px 8px;
  color: #ffd47a;
  font-size: 10px;
  line-height: 1.45;
}
.equ-issue--error {
  color: #ff8e8e;
}
</style>
