<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from "vue";
import { Events } from "@wailsio/runtime";
import {
  NAlert,
  NButton,
  NIcon,
  NInputNumber,
  NModal,
  NSpin,
  NTag,
  useDialog,
  useMessage,
} from "naive-ui";
import { ArrowSync24Regular, Save24Regular } from "@vicons/fluent";
import { useArchiveStore } from "../stores/archive";
import {
  DROP_RATE_LABELS,
  DROP_RATE_SECTION_META,
  groupTotal,
  useDropRateStore,
  type DropRateSectionForm,
} from "../stores/dropRate";

const archive = useArchiveStore();
const dropRate = useDropRateStore();
const dialog = useDialog();
const message = useMessage();

const activeTab = ref("hell");
const busy = computed(() => dropRate.loading || dropRate.applying);

watch(
  () => dropRate.sections,
  (sections) => {
    if (sections.length > 0 && !sections.some((s) => s.key === activeTab.value)) {
      activeTab.value = sections[0].key;
    }
  },
  { immediate: true },
);

const currentSection = computed(() => {
  return dropRate.sections.find((s) => s.key === activeTab.value) ?? dropRate.sections[0];
});

function sectionInvalid(section: DropRateSectionForm): boolean {
  return section.groups.some((group) => !groupTotalValid(group));
}

function groupTotalValid(group: { rates: Array<number | null> }): boolean {
  return groupTotal(group) === 100;
}

function groupDiff(group: { rates: Array<number | null> }): string {
  const total = groupTotal(group);
  if (total === null || total === 100) return "";
  const diff = Math.round((total - 100) * 100) / 100;
  return diff > 0 ? `+${diff.toFixed(2)}%` : `${diff.toFixed(2)}%`;
}

function sectionMeta(key: string) {
  return DROP_RATE_SECTION_META[key] ?? {
    title: key,
    groupLabels: [],
    path: "",
  };
}

function close(): void {
  if (!dropRate.dirty) {
    dropRate.close();
    return;
  }
  dialog.warning({
    title: "放弃修改？",
    content: "未应用的掉率修改将会丢失。",
    positiveText: "放弃",
    negativeText: "继续编辑",
    onPositiveClick: () => dropRate.close(),
  });
}

function reload(): void {
  if (!dropRate.dirty) {
    void reloadData();
    return;
  }
  dialog.warning({
    title: "重新加载？",
    content: "未应用的掉率修改将会丢失，是否重载？",
    positiveText: "重新加载",
    negativeText: "取消",
    onPositiveClick: () => reloadData(),
  });
}

async function reloadData(): Promise<void> {
  if (busy.value) return;
  const loaded = await dropRate.load();
  if (loaded) message.success("掉率已重新加载");
  else message.error(dropRate.error || "读取掉率失败");
}

async function apply(): Promise<void> {
  if (!dropRate.canApply) {
    message.warning(dropRate.validationError || "请先修正掉率输入");
    return;
  }
  const applied = await dropRate.apply();
  if (applied) {
    message.success("已应用到工作区，保存 PVF 生效");
  } else if (dropRate.error) {
    message.error(dropRate.error);
  }
}

function handleUpdateShow(show: boolean): void {
  if (show) return;
  close();
}

const archiveIdentity = computed(() => `${archive.info?.path ?? ""}|${archive.info?.format ?? ""}`);
const stopArchiveWatch = watch(archiveIdentity, (next, previous) => {
  if (!dropRate.visible || next === previous) return;
  dropRate.close();
  dropRate.reset();
});
const stopArchiveOpened = Events.On("archive:opened", () => {
  if (!dropRate.visible) return;
  dropRate.close();
  dropRate.reset();
});
const stopArchiveClosed = Events.On("archive:closed", () => {
  if (!dropRate.visible) return;
  dropRate.close();
  dropRate.reset();
});

onUnmounted(() => {
  stopArchiveWatch();
  stopArchiveOpened();
  stopArchiveClosed();
});
</script>

<template>
  <NModal
    :show="dropRate.visible"
    preset="card"
    style="width: min(840px, calc(100vw - 32px))"
    :mask-closable="!busy"
    @update:show="handleUpdateShow"
  >
    <template #header>
      <div class="drop-rate-header">
        <span class="drop-rate-header-title">基础掉率</span>
        <NTag v-if="dropRate.pvfVersion" size="small" :bordered="false" type="info" round>
          {{ dropRate.pvfVersion }}
        </NTag>
      </div>
    </template>

    <div class="drop-rate-modal">
      <NAlert v-if="dropRate.error" type="error" :show-icon="false">
        {{ dropRate.error }}
      </NAlert>

      <div v-if="dropRate.loading" class="drop-rate-loading">
        <NSpin size="small" />
        <span>加载中…</span>
      </div>

      <div v-else-if="currentSection" class="drop-rate-body">
        <!-- 顶部分段切换导航 -->
        <div class="drop-rate-nav">
          <div class="segmented-control" role="tablist">
            <button
              v-for="section in dropRate.sections"
              :key="section.key"
              type="button"
              role="tab"
              :aria-selected="activeTab === section.key"
              class="segment-item"
              :class="{ 'segment-item--active': activeTab === section.key }"
              @click="activeTab = section.key"
            >
              <span class="segment-label">{{ sectionMeta(section.key).title }}</span>
              <span v-if="sectionInvalid(section)" class="segment-dot segment-dot--invalid" title="存在未配平项" />
            </button>
          </div>
        </div>

        <!-- 针对当前分类的独立纯净数据表格 -->
        <div class="table-container">
          <table class="drop-rate-table">
            <thead>
              <tr>
                <th class="col-difficulty">难度</th>
                <th
                  v-for="(label, rateIndex) in DROP_RATE_LABELS"
                  :key="label"
                  class="col-rate"
                >
                  <div class="rate-header-cell">
                    <span class="rarity-dot" :class="`rarity-dot--${rateIndex}`" />
                    <span class="rarity-title" :class="`rarity-title--${rateIndex}`">{{ label }} (%)</span>
                  </div>
                </th>
                <th class="col-total">总和</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="(group, groupIndex) in currentSection.groups"
                :key="`${currentSection.key}-${groupIndex}`"
                class="data-row"
                :class="{ 'data-row--invalid': !groupTotalValid(group) }"
              >
                <td class="col-difficulty">
                  <span class="difficulty-tag">
                    {{ sectionMeta(currentSection.key).groupLabels[groupIndex] }}
                  </span>
                </td>
                <td
                  v-for="(label, rateIndex) in DROP_RATE_LABELS"
                  :key="label"
                  class="col-rate"
                >
                  <NInputNumber
                    v-model:value="group.rates[rateIndex]"
                    :min="0"
                    :max="100"
                    :step="0.01"
                    :precision="2"
                    :show-button="false"
                    size="small"
                    class="clean-input"
                    :aria-label="`${sectionMeta(currentSection.key).title} ${sectionMeta(currentSection.key).groupLabels[groupIndex]} ${label}`"
                  />
                </td>
                <td class="col-total">
                  <div
                    class="total-display"
                    :class="{
                      'total-display--valid': groupTotalValid(group),
                      'total-display--invalid': !groupTotalValid(group),
                    }"
                  >
                    <span class="total-value">
                      {{ groupTotal(group) === null ? "—" : `${groupTotal(group)?.toFixed(2)}%` }}
                    </span>
                    <span v-if="groupDiff(group)" class="total-diff-tag">
                      {{ groupDiff(group) }}
                    </span>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <!-- 当前文件路径的弱化技术参考 -->
        <div class="meta-path-hint">
          <span>文件：{{ sectionMeta(currentSection.key).path }}</span>
        </div>
      </div>
    </div>

    <template #footer>
      <div class="drop-rate-footer">
        <NButton size="small" quaternary :disabled="busy" @click="reload">
          <template #icon><NIcon><ArrowSync24Regular /></NIcon></template>
          重新加载
        </NButton>
        <div class="drop-rate-footer-status">
          <span v-if="!dropRate.loading && dropRate.validationError" class="footer-validation-text">
            {{ dropRate.validationError }}
          </span>
          <span v-else-if="dropRate.dirty && dropRate.canApply" class="footer-dirty-text">
            修改未保存
          </span>
        </div>
        <NButton size="small" :disabled="busy" @click="close">取消</NButton>
        <NButton
          size="small"
          type="primary"
          :loading="dropRate.applying"
          :disabled="!dropRate.canApply"
          @click="apply"
        >
          <template #icon><NIcon><Save24Regular /></NIcon></template>
          应用到工作区
        </NButton>
      </div>
    </template>
  </NModal>
</template>

<style scoped>
.drop-rate-header {
  display: flex;
  align-items: center;
  gap: 8px;
}
.drop-rate-header-title {
  font-size: 15px;
  font-weight: 600;
  color: var(--pvf-text-primary);
}
.drop-rate-modal {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.drop-rate-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  min-height: 200px;
  color: var(--pvf-text-muted);
}
.drop-rate-body {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.drop-rate-nav {
  display: flex;
  justify-content: center;
}
.segmented-control {
  display: inline-flex;
  padding: 3px;
  background: var(--pvf-surface-inset);
  border: 1px solid var(--pvf-border-subtle);
  border-radius: 8px;
  gap: 2px;
}
.segment-item {
  position: relative;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 5px 20px;
  background: transparent;
  border: none;
  border-radius: 6px;
  color: var(--pvf-text-secondary);
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.15s ease;
  user-select: none;
}
.segment-item:hover:not(.segment-item--active) {
  color: var(--pvf-text-primary);
  background: var(--pvf-surface-hover);
}
.segment-item--active {
  background: var(--pvf-surface-card);
  color: var(--pvf-text-primary);
  font-weight: 600;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.12);
}
.segment-dot--invalid {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--pvf-warning, #f0a020);
}
.table-container {
  border: 1px solid var(--pvf-border-subtle);
  border-radius: 8px;
  overflow: hidden;
  background: var(--pvf-surface-card);
}
.drop-rate-table {
  width: 100%;
  border-collapse: collapse;
  text-align: left;
  font-size: 12px;
}
.drop-rate-table thead {
  background: var(--pvf-surface-subtle);
  border-bottom: 1px solid var(--pvf-border-subtle);
}
.drop-rate-table th {
  padding: 9px 12px;
  font-weight: 600;
  color: var(--pvf-text-secondary);
  user-select: none;
}
.col-difficulty {
  width: 110px;
  padding-left: 18px !important;
}
.col-rate {
  width: 96px;
  text-align: center;
}
.col-total {
  width: 130px;
  text-align: right;
  padding-right: 18px !important;
}
.rate-header-cell {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 5px;
}
.rarity-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  flex-shrink: 0;
}
.rarity-dot--0 { background: #94a3b8; }
.rarity-dot--1 { background: #0ea5e9; }
.rarity-dot--2 { background: #a855f7; }
.rarity-dot--3 { background: #ec4899; }
.rarity-dot--4 { background: #f59e0b; }

.rarity-title--4 {
  color: #f59e0b;
  font-weight: 650;
}
.data-row {
  border-bottom: 1px solid var(--pvf-border-subtle);
  transition: background-color 0.12s ease;
}
.data-row:last-child {
  border-bottom: none;
}
.data-row:hover {
  background: var(--pvf-surface-hover);
}
.data-row--invalid {
  background: rgba(240, 160, 32, 0.05);
}
.data-row--invalid:hover {
  background: rgba(240, 160, 32, 0.09);
}
.data-row td {
  padding: 8px 12px;
  vertical-align: middle;
}
.difficulty-tag {
  font-weight: 600;
  color: var(--pvf-text-primary);
  font-size: 13px;
}
.clean-input {
  width: 82px;
  margin: 0 auto;
}
.clean-input :deep(.n-input) {
  background-color: var(--pvf-surface-inset);
  text-align: center;
}
.clean-input :deep(.n-input__input-el) {
  text-align: center;
  font-variant-numeric: tabular-nums;
  font-weight: 500;
}
.total-display {
  display: inline-flex;
  align-items: center;
  justify-content: flex-end;
  gap: 6px;
  font-variant-numeric: tabular-nums;
  font-size: 12px;
  font-weight: 600;
}
.total-display--valid {
  color: var(--pvf-success, #18a058);
}
.total-display--invalid {
  color: var(--pvf-warning, #f0a020);
}
.total-diff-tag {
  font-size: 11px;
  padding: 1px 5px;
  border-radius: 3px;
  background: rgba(240, 160, 32, 0.18);
  font-weight: 600;
}
.meta-path-hint {
  display: flex;
  justify-content: flex-end;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 11px;
  color: var(--pvf-text-muted);
  opacity: 0.6;
  padding: 0 4px;
}
.drop-rate-footer {
  display: flex;
  align-items: center;
  gap: 8px;
}
.drop-rate-footer-status {
  flex: 1;
  display: flex;
  align-items: center;
  overflow: hidden;
  font-size: 12px;
}
.footer-validation-text {
  color: var(--pvf-warning, #f0a020);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.footer-dirty-text {
  color: var(--pvf-text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
