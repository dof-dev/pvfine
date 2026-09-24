<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import {
  NAlert,
  NButton,
  NIcon,
  NInput,
  NInputNumber,
  NModal,
  NSlider,
  NSpin,
  NTag,
  NTooltip,
  useDialog,
  useMessage,
} from "naive-ui";
import {
  Add24Regular,
  ArrowClockwise24Regular,
  ArrowDown16Regular,
  ArrowUp16Regular,
  Checkmark24Regular,
  Delete16Regular,
  Dismiss24Regular,
  Edit16Regular,
  Filter24Regular,
  Globe24Regular,
  Search24Regular,
} from "@vicons/fluent";
import { FileGUIService } from "../../../bindings/pvfine/services";
import type { GUIFile } from "../../gui/types";
import { useFileGUIStore } from "../../stores/fileGUI";
import { useEditorStore } from "../../stores/editor";
import { createWorldDropDraftState, createWorldDropSession } from "../../gui/state";
import {
  batchAddItems,
  batchDeleteItems,
  calcItemPercentage,
  calcLevelTotalWeight,
  countLevelMatches,
  countMatchingItems,
  filterItems,
  findFirstLevelWithMatch,
  getLevelsInInterval,
  isFilterCriteriaActive,
  isPositiveInt32,
  isStoredItemId,
  MIN_INT32,
  moveItem,
  nextFormKey,
  toWorldDropLevels,
  validateAllLevels,
  validateItem,
  validateLevelInterval,
  type EditableWorldDropItem,
  type EditableWorldDropLevel,
  type WorldDropFilterCriteria,
} from "../../gui/worldDropForm";
import ItemPicker from "../ItemPicker.vue";
import ImageThumbnail from "../ImageThumbnail.vue";
import type { SearchHit } from "../../../bindings/pvfine/services/models";

const props = defineProps<{ file: GUIFile; active: boolean; paneId?: string }>();
const emit = defineEmits<{ (e: "close"): void }>();

const gui = useFileGUIStore();
const editor = useEditorStore();
const message = useMessage();
const dialog = useDialog();

const containerRef = ref<HTMLDivElement | null>(null);
const isNarrow = ref(false);
let resizeObserver: ResizeObserver | null = null;

onMounted(() => {
  if (containerRef.value && typeof ResizeObserver !== "undefined") {
    resizeObserver = new ResizeObserver((entries) => {
      const width = entries[0]?.contentRect.width ?? 0;
      isNarrow.value = width > 0 && width < 640;
    });
    resizeObserver.observe(containerRef.value);
  }
});

const session = createWorldDropSession(FileGUIService.ReadWorldDrop);
const { document, loading, error, selectedLevel } = session;
const draft = createWorldDropDraftState();
const { editableLevels, isDirty } = draft;

const staleConflict = ref(false);
const applying = ref(false);
const loadedText = ref<string>("");
const loadedArchiveIdentity = ref<string>("");

// 精准选择的道具（来自“从物品库选取”，严格全等匹配 ID）
const selectedExactItem = ref<{
  id: number;
  name: string;
  icon?: any;
} | null>(null);

// 自由文本筛选（用户输入的 ID 或名称）
const filterQuery = ref<string>("");

// 统一筛选条件计算属性
const filterCriteria = computed<WorldDropFilterCriteria>(() => {
  if (selectedExactItem.value !== null) {
    return {
      exactItemId: selectedExactItem.value.id,
      query: "",
    };
  }
  return {
    exactItemId: null,
    query: filterQuery.value,
  };
});

const isFilterActive = computed(() => isFilterCriteriaActive(filterCriteria.value));

const existingLevelNumbers = computed(() => editableLevels.value.map((l) => l.level));
const sortedLevelNumbers = computed(() => [...existingLevelNumbers.value].sort((a, b) => a - b));

function intervalSliderValue(start: number | null, end: number | null): number[] {
  const levels = sortedLevelNumbers.value;
  return [Math.max(0, levels.indexOf(start ?? levels[0])), Math.max(0, levels.indexOf(end ?? levels[0]))];
}

function updateInterval(target: { startLevel: number | null; endLevel: number | null }, value: number[]): void {
  const levels = sortedLevelNumbers.value;
  const start = levels[Math.min(...value)];
  const end = levels[Math.max(...value)];
  if (start === undefined || end === undefined) return;
  target.startLevel = start;
  target.endLevel = end;
}

function levelSliderTooltip(index: number): string {
  return `Lv. ${sortedLevelNumbers.value[index] ?? ""}`;
}

const currentLevel = computed(() => {
  return editableLevels.value.find((lvl) => lvl.level === selectedLevel.value) ?? null;
});

const currentItems = computed(() => currentLevel.value?.items ?? []);
// 即使存在筛选，当前等级总权重与百分比也必须严格基于全部道具计算，保证真实准确
const currentTotalWeight = computed(() => calcLevelTotalWeight(currentItems.value));

const displayedItems = computed(() => {
  return filterItems(currentItems.value, filterCriteria.value);
});

// 添加 / 编辑道具对话框状态（支持等级区间）
const itemModal = ref<{
  show: boolean;
  mode: "add" | "edit";
  itemKey?: number;
  originalId?: number;
  id: string;
  name: string;
  weight: number | null;
  icon?: any;
  startLevel: number | null;
  endLevel: number | null;
  error: string;
}>({
  show: false,
  mode: "add",
  id: "",
  name: "",
  weight: 100,
  icon: null,
  startLevel: null,
  endLevel: null,
  error: "",
});

// 批量删除对话框状态
const batchDeleteModal = ref<{
  show: boolean;
  id: string;
  name: string;
  icon?: any;
  startLevel: number | null;
  endLevel: number | null;
  error: string;
}>({
  show: false,
  id: "",
  name: "",
  icon: null,
  startLevel: null,
  endLevel: null,
  error: "",
});

// 筛选选择器对话框状态
const filterPickerModal = ref<{
  show: boolean;
  id: string;
  name: string;
  icon?: any;
}>({
  show: false,
  id: "",
  name: "",
  icon: null,
});

function syncFromDocument(): void {
  draft.sync(document.value);
  const levels = editableLevels.value;
  if (selectedLevel.value === null || !levels.some((l) => l.level === selectedLevel.value)) {
    selectedLevel.value = levels[0]?.level ?? null;
  }
  staleConflict.value = false;
}

/**
 * 重新加载数据。
 * 当存在未保存修改且未传 force 时，必须弹出确认，避免用户编辑被意外丢弃。
 */
async function reload(force = false): Promise<void> {
  let userConfirmedDiscard = force;
  if (isDirty.value && !userConfirmedDiscard) {
    const confirmed = await new Promise<boolean>((resolve) => {
      dialog.warning({
        title: "确认重新加载",
        content: "当前界面存在未应用的修改，重新加载将丢失这些修改并拉取最新内容。确定重新加载吗？",
        positiveText: "放弃修改并重新加载",
        negativeText: "取消",
        onPositiveClick: () => resolve(true),
        onNegativeClick: () => resolve(false),
        onClose: () => resolve(false),
        onMaskClick: () => resolve(false),
        onEsc: () => resolve(false),
      });
    });
    if (!confirmed) return;
    userConfirmedDiscard = true;
  }

  // 捕获请求发起时的文件文本与归档标识
  const requestText = props.file.text;
  const requestArchiveIdentity = `${props.file.index}\u0000${props.file.path}\u0000${gui.epoch}`;

  const success = await session.load(props.file, String(gui.epoch));
  if (!success) {
    return;
  }

  // 异步返回后，检查文本或归档是否在请求过程中发生了变动
  const currentArchiveIdentity = `${props.file.index}\u0000${props.file.path}\u0000${gui.epoch}`;
  const textChangedDuringLoad = props.file.text !== requestText;
  const identityChangedDuringLoad = currentArchiveIdentity !== requestArchiveIdentity;

  if (textChangedDuringLoad || identityChangedDuringLoad) {
    staleConflict.value = true;
    return;
  }

  if (isDirty.value && !userConfirmedDiscard) {
    staleConflict.value = true;
    return;
  }

  loadedText.value = requestText;
  loadedArchiveIdentity.value = requestArchiveIdentity;
  syncFromDocument();
  staleConflict.value = false;
}

// 监听文件和归档变化：仅在真实文本变动或归档切换时判定冲突，标签切换与单纯 revision tick 绝不清除 dirty 状态
watch(
  () => [props.active, props.file.index, props.file.path, props.file.text, gui.epoch] as const,
  async ([active, index, path, text, epoch]) => {
    if (editor.guiApplying) return;
    if (!active) {
      return;
    }

    const archiveId = `${index}\u0000${path}\u0000${epoch}`;
    const identityChanged = loadedArchiveIdentity.value !== "" && loadedArchiveIdentity.value !== archiveId;
    const textChanged = loadedText.value !== "" && loadedText.value !== text;

    if (identityChanged) {
      if (isDirty.value) {
        staleConflict.value = true;
      } else {
        await reload(false);
      }
      return;
    }

    if (textChanged) {
      if (isDirty.value) {
        staleConflict.value = true;
      } else {
        await reload(false);
      }
      return;
    }

    // 初次载入
    if (!document.value && !loading.value) {
      await reload(false);
    }
  },
  { immediate: true, flush: "sync" },
);

// 注册当前文件在该视图的未保存修改检查器（支持多窗格各自分离注册）
let unregisterDirtyChecker: (() => void) | null = null;
watch(
  () => [props.file.index, isDirty.value] as const,
  ([index]) => {
    if (unregisterDirtyChecker) unregisterDirtyChecker();
    unregisterDirtyChecker = editor.registerGUIDirtyChecker(index, () => isDirty.value);
  },
  { immediate: true },
);

// 注册模式切换守卫（绑定到本组件所属的实际 paneId）
function getTargetPaneId(): string {
  return props.paneId || editor.activePaneId;
}

let unregisterGuard: (() => void) | null = null;
function setupGuard() {
  if (unregisterGuard) unregisterGuard();
  const modes = editor.getGUIModes(getTargetPaneId());
  unregisterGuard = modes.registerGuard(props.file.index, async () => {
    if (!isDirty.value) return true;
    return new Promise<boolean>((resolve) => {
      dialog.warning({
        title: "未应用的修改",
        content: "全局掉率界面有未保存的修改，切换到 DSL 模式将丢失这些修改。确定放弃吗？",
        positiveText: "放弃修改",
        negativeText: "留在界面",
        onPositiveClick: () => {
          syncFromDocument();
          resolve(true);
        },
        onNegativeClick: () => resolve(false),
        onClose: () => resolve(false),
        onMaskClick: () => resolve(false),
        onEsc: () => resolve(false),
      });
    });
  });
}
watch(() => [props.file.index, props.paneId, editor.activePaneId], setupGuard, { immediate: true });

onBeforeUnmount(() => {
  session.invalidate(true);
  resizeObserver?.disconnect();
  if (unregisterDirtyChecker) unregisterDirtyChecker();
  if (unregisterGuard) unregisterGuard();
});

function handleClose(): void {
  emit("close");
}

// 提交应用：提交全量 levels 快照（包含由于筛选而当前在视图中隐藏的全部行）
async function handleApply(): Promise<void> {
  if (!props.file.editable) {
    message.warning("只读文件无法应用修改");
    return;
  }
  if (staleConflict.value || (loadedText.value !== "" && props.file.text !== loadedText.value)) {
    message.error("检测到底层文件文本已在外部被修改，请先重新加载最新数据以避免覆盖冲突。");
    return;
  }
  const levels = toWorldDropLevels(editableLevels.value);
  const valErr = validateAllLevels(levels);
  if (valErr) {
    message.error(valErr);
    return;
  }
  if (!document.value) return;

  applying.value = true;
  try {
    await editor.applyWorldDropEdit({
      fileIndex: props.file.index,
      path: props.file.path,
      text: props.file.text,
      revision: document.value.revision,
      levels,
    });
    message.success("全局掉率修改已成功应用");
    staleConflict.value = false;
    loadedText.value = props.file.text;
    await reload(true);
  } catch (err: any) {
    message.error(`应用失败：${err?.message ?? err}`);
  } finally {
    applying.value = false;
  }
}

// 道具添加 / 编辑操作
function openAddItem(): void {
  if (editableLevels.value.length === 0) {
    message.warning("当前无可用等级配置，无法添加道具");
    return;
  }
  const initLevel = selectedLevel.value ?? editableLevels.value[0]?.level ?? null;
  itemModal.value = {
    show: true,
    mode: "add",
    id: "",
    name: "",
    weight: 100,
    icon: null,
    startLevel: initLevel,
    endLevel: initLevel,
    error: "",
  };
}

function openEditItem(item: EditableWorldDropItem): void {
  itemModal.value = {
    show: true,
    mode: "edit",
    itemKey: item.key,
    originalId: item.id,
    id: String(item.id),
    name: item.name,
    weight: item.weight,
    icon: null,
    startLevel: null,
    endLevel: null,
    error: "",
  };
}

function onPickerSelect(hit: SearchHit | null): void {
  if (hit) {
    itemModal.value.id = hit.id;
    itemModal.value.name = hit.name || `未知物品 #${hit.id}`;
    itemModal.value.icon = hit.icon ?? null;
  }
}

function onIdChange(val: number | null): void {
  const nextId = val !== null ? String(val) : "";
  if (nextId !== itemModal.value.id) {
    itemModal.value.id = nextId;
    itemModal.value.name = nextId ? `未知物品 #${nextId}` : "";
    itemModal.value.icon = null;
  }
}

const addItemIntervalPreview = computed(() => {
  if (itemModal.value.mode !== "add") return "";
  const s = itemModal.value.startLevel;
  const e = itemModal.value.endLevel;
  if (s === null || e === null) return "";
  const err = validateLevelInterval(s, e, existingLevelNumbers.value);
  if (err) return err;
  const targets = getLevelsInInterval(editableLevels.value, s, e);
  return `将向区间内已配置的 ${targets.length} 个等级分别添加该道具（若已存在同道具则直接追加新行）`;
});

function confirmItemModal(): void {
  const rawId = Number.parseInt(itemModal.value.id.trim(), 10);
  const rawWeight = itemModal.value.weight;
  if (!Number.isFinite(rawId)) {
    itemModal.value.error = "请输入有效的道具 ID";
    return;
  }
  if (rawWeight === null || !Number.isFinite(rawWeight)) {
    itemModal.value.error = "请输入有效的道具权重";
    return;
  }
  const keepingExistingId = itemModal.value.mode === "edit" && rawId === itemModal.value.originalId;
  const err = validateItem(rawId, rawWeight, keepingExistingId);
  if (err) {
    itemModal.value.error = err;
    return;
  }

  const resolvedName = itemModal.value.name.trim() || `未知物品 #${rawId}`;

  if (itemModal.value.mode === "add") {
    const s = itemModal.value.startLevel;
    const e = itemModal.value.endLevel;
    const intervalErr = validateLevelInterval(s, e, existingLevelNumbers.value);
    if (intervalErr) {
      itemModal.value.error = intervalErr;
      return;
    }
    const res = batchAddItems(editableLevels.value, s!, e!, {
      id: rawId,
      weight: rawWeight,
      name: resolvedName,
    });
    message.success(`已向 ${res.addedCount} 个等级添加道具`);
    itemModal.value.show = false;
  } else if (itemModal.value.mode === "edit" && itemModal.value.itemKey !== undefined) {
    const level = currentLevel.value;
    if (!level) return;
    const item = level.items.find((i) => i.key === itemModal.value.itemKey);
    if (item) {
      item.id = rawId;
      item.weight = rawWeight;
      item.name = resolvedName;
    }
    itemModal.value.show = false;
  }
}

function deleteItem(itemKey: number): void {
  const level = currentLevel.value;
  if (!level) return;
  const idx = level.items.findIndex((i) => i.key === itemKey);
  if (idx >= 0) {
    level.items.splice(idx, 1);
  }
}

function handleMoveItem(displayIndex: number, direction: "up" | "down"): void {
  // 筛选激活时严格禁用排序，避免打乱隐藏行的原始排列
  if (isFilterActive.value) return;
  const level = currentLevel.value;
  if (!level) return;
  const target = direction === "up" ? displayIndex - 1 : displayIndex + 1;
  moveItem(level.items, displayIndex, target);
}

// 批量删除操作
function openBatchDelete(): void {
  if (editableLevels.value.length === 0) {
    message.warning("当前无可用等级配置");
    return;
  }
  const initLevel = selectedLevel.value ?? editableLevels.value[0]?.level ?? null;
  batchDeleteModal.value = {
    show: true,
    id: "",
    name: "",
    icon: null,
    startLevel: initLevel,
    endLevel: initLevel,
    error: "",
  };
}

function onBatchDeletePickerSelect(hit: SearchHit | null): void {
  if (hit) {
    batchDeleteModal.value.id = hit.id;
    batchDeleteModal.value.name = hit.name || `未知物品 #${hit.id}`;
    batchDeleteModal.value.icon = hit.icon ?? null;
  }
}

function onBatchDeleteIdChange(val: number | null): void {
  const nextId = val !== null ? String(val) : "";
  if (nextId !== batchDeleteModal.value.id) {
    batchDeleteModal.value.id = nextId;
    batchDeleteModal.value.name = nextId ? `未知物品 #${nextId}` : "";
    batchDeleteModal.value.icon = null;
  }
}

const batchDeletePreview = computed(() => {
  const rawId = Number.parseInt(batchDeleteModal.value.id.trim(), 10);
  const s = batchDeleteModal.value.startLevel;
  const e = batchDeleteModal.value.endLevel;
  if (!isStoredItemId(rawId) || s === null || e === null) return null;
  const intervalErr = validateLevelInterval(s, e, existingLevelNumbers.value);
  if (intervalErr) {
    return { count: 0, message: intervalErr };
  }
  const match = countMatchingItems(editableLevels.value, s, e, rawId);
  if (match.matchingRows === 0) {
    return { count: 0, message: `选定区间 (Lv. ${s} ~ Lv. ${e}) 内未匹配到道具 #${rawId} 的掉落记录` };
  }
  return {
    count: match.matchingRows,
    message: `在选定区间内匹配到 ${match.matchingLevels.length} 个等级，共 ${match.matchingRows} 条掉落记录将完全删除（保留空等级）`,
  };
});

function confirmBatchDeleteModal(): void {
  const rawId = Number.parseInt(batchDeleteModal.value.id.trim(), 10);
  if (!isStoredItemId(rawId)) {
    batchDeleteModal.value.error = "请输入有效的道具 ID";
    return;
  }
  const s = batchDeleteModal.value.startLevel;
  const e = batchDeleteModal.value.endLevel;
  const intervalErr = validateLevelInterval(s, e, existingLevelNumbers.value);
  if (intervalErr) {
    batchDeleteModal.value.error = intervalErr;
    return;
  }
  const match = countMatchingItems(editableLevels.value, s, e, rawId);
  if (match.matchingRows === 0) {
    message.info(`选定等级区间 (Lv. ${s} ~ Lv. ${e}) 内未找到该道具记录，未做修改`);
    batchDeleteModal.value.show = false;
    return;
  }

  const itemName = batchDeleteModal.value.name.trim() || `未知物品 #${rawId}`;
  dialog.warning({
    title: "确认批量删除",
    content: `确定在 Lv. ${s} ~ Lv. ${e} 区间内删除道具「${itemName}」吗？将从 ${match.matchingLevels.length} 个等级中完全移除共 ${match.matchingRows} 条掉落记录。`,
    positiveText: "删除",
    negativeText: "取消",
    onPositiveClick: () => {
      const res = batchDeleteItems(editableLevels.value, s!, e!, rawId);
      message.success(`已在 ${res.affectedLevels.length} 个等级中成功删除了 ${res.removedCount} 条道具记录`);
      batchDeleteModal.value.show = false;
    },
  });
}

// 筛选操作
function openFilterPicker(): void {
  filterPickerModal.value = {
    show: true,
    id: selectedExactItem.value ? String(selectedExactItem.value.id) : "",
    name: selectedExactItem.value ? selectedExactItem.value.name : "",
    icon: selectedExactItem.value ? selectedExactItem.value.icon : null,
  };
}

function onFilterPickerSelect(hit: SearchHit | null): void {
  if (hit) {
    filterPickerModal.value.id = hit.id;
    filterPickerModal.value.name = hit.name || `未知物品 #${hit.id}`;
    filterPickerModal.value.icon = hit.icon ?? null;
  }
}

function confirmFilterPicker(): void {
  const rawId = Number.parseInt(filterPickerModal.value.id.trim(), 10);
  if (!isPositiveInt32(rawId)) return;

  selectedExactItem.value = {
    id: rawId,
    name: filterPickerModal.value.name || `物品 #${rawId}`,
    icon: filterPickerModal.value.icon ?? null,
  };
  filterQuery.value = "";
  filterPickerModal.value.show = false;

  // 导航偏好：若当前选中的等级没有该道具掉落，优先跳转到首个包含该道具掉落的等级
  const currentMatches = currentLevel.value ? countLevelMatches(currentLevel.value, filterCriteria.value) : 0;
  if (currentMatches === 0) {
    const firstMatchLevel = findFirstLevelWithMatch(editableLevels.value, filterCriteria.value);
    if (firstMatchLevel !== null) {
      selectedLevel.value = firstMatchLevel;
    }
  }
}

function onFilterQueryInput(val: string): void {
  filterQuery.value = val;
  // 用户主动输入文字检索时，清除精准物品筛选，切换为自由文本模糊搜索
  if (selectedExactItem.value !== null) {
    selectedExactItem.value = null;
  }
}

function clearFilter(): void {
  selectedExactItem.value = null;
  filterQuery.value = "";
}
</script>

<template>
  <div
    ref="containerRef"
    class="world-drop-view"
    :class="{ 'is-narrow': isNarrow }"
    :aria-busy="loading || applying"
    aria-label="全局掉率编辑器"
  >
    <!-- 头部工具栏 -->
    <header class="world-drop-header">
      <div class="header-left">
        <NIcon size="20" class="header-icon"><Globe24Regular /></NIcon>
        <h2 class="header-title">全局掉率</h2>
        <span class="file-path-badge">{{ file.path }}</span>
        <NTag v-if="isDirty" size="small" type="warning" round class="dirty-tag">未应用修改</NTag>
        <NTag v-if="!file.editable" size="small" type="default" round>只读</NTag>
      </div>

      <div class="header-actions">
        <NButton
          size="small"
          quaternary
          :loading="loading"
          :disabled="applying"
          title="重新从当前文本加载数据"
          @click="() => reload(false)"
        >
          <template #icon><NIcon><ArrowClockwise24Regular /></NIcon></template>
          重新加载
        </NButton>

        <NTooltip trigger="hover" :disabled="!staleConflict">
          <template #trigger>
            <span>
              <NButton
                size="small"
                type="primary"
                :loading="applying"
                :disabled="!isDirty || !file.editable || loading || staleConflict"
                title="将界面的掉率配置同步至归档 Overlay"
                @click="handleApply"
              >
                <template #icon><NIcon><Checkmark24Regular /></NIcon></template>
                应用修改
              </NButton>
            </span>
          </template>
          底层文本已被修改，请先重新加载最新数据以避免冲突
        </NTooltip>

        <button
          type="button"
          class="header-close-btn"
          title="关闭全局掉率界面"
          aria-label="关闭"
          @click="handleClose"
        >
          <NIcon size="14"><Dismiss24Regular /></NIcon>
        </button>
      </div>
    </header>

    <!-- 道具筛选工具条 -->
    <div class="filter-toolbar">
      <div class="filter-controls">
        <NInput
          :value="filterQuery"
          size="small"
          clearable
          :placeholder="
            selectedExactItem
              ? `已精准筛选道具 #${selectedExactItem.id}（输入文字将切换为自由模糊搜索）`
              : '按道具 ID 或名称筛选（展示各等级掉率）...'
          "
          class="filter-search-input"
          @update:value="onFilterQueryInput"
          @clear="clearFilter"
        >
          <template #prefix>
            <NIcon size="14"><Search24Regular /></NIcon>
          </template>
        </NInput>
        <NButton size="small" secondary @click="openFilterPicker">
          <template #icon><NIcon size="14"><Filter24Regular /></NIcon></template>
          从物品库选取
        </NButton>
        <NButton
          v-if="isFilterActive"
          size="small"
          quaternary
          @click="clearFilter"
        >
          清除筛选
        </NButton>
      </div>

      <div v-if="isFilterActive" class="filter-meta">
        <NTag
          v-if="selectedExactItem"
          size="small"
          type="primary"
          round
          closable
          @close="clearFilter"
        >
          精准筛选: {{ selectedExactItem.name }} (ID: {{ selectedExactItem.id }})
        </NTag>
        <NTag
          v-else
          size="small"
          type="info"
          round
          closable
          @close="clearFilter"
        >
          自由筛选: {{ filterQuery }}
        </NTag>
        <span class="filter-tip">已禁用行排序 · 保存时仍会完整保留所有隐藏道具</span>
      </div>
    </div>

    <!-- 冲突与错误提示 -->
    <div v-if="staleConflict" class="banner-box">
      <NAlert type="warning" title="检测到文本更新冲突" closable @close="staleConflict = false">
        <div class="alert-content">
          <span>检测到底层文件文本已被修改（例如在分屏中编辑了 DSL 或归档变动）。为防止覆盖冲突，请先点击【重新加载】获取最新内容后再进行编辑与应用。</span>
          <NButton size="tiny" type="warning" ghost @click="() => reload(false)">重新加载最新数据</NButton>
        </div>
      </NAlert>
    </div>

    <div v-if="error" class="banner-box">
      <NAlert type="error" title="读取全局掉率失败">
        <div class="alert-content">
          <span>{{ error }}</span>
          <NButton size="tiny" type="error" ghost @click="() => reload(true)">重试</NButton>
        </div>
      </NAlert>
    </div>

    <!-- 加载状态 -->
    <div v-if="loading && !document" class="loading-state" role="status" aria-live="polite">
      <NSpin size="large" />
      <span>正在读取全局掉率数据…</span>
    </div>

    <!-- 主体区域：左侧只读等级导航 + 右侧当前等级表格 -->
    <div v-else-if="document" class="world-drop-body">
      <!-- 左侧：等级导航列表（纯只读导航，无等级增删改） -->
      <aside class="level-sidebar" aria-label="掉落等级列表">
        <div class="sidebar-header">
          <span class="sidebar-title">等级列表 ({{ editableLevels.length }})</span>
        </div>

        <div class="level-list" role="listbox" aria-label="掉落等级">
          <div
            v-for="lvl in editableLevels"
            :key="lvl.key"
            role="option"
            :aria-selected="selectedLevel === lvl.level"
            tabindex="0"
            class="level-item"
            :class="{
              active: selectedLevel === lvl.level,
              'is-dimmed': isFilterActive && countLevelMatches(lvl, filterCriteria) === 0,
            }"
            @click="selectedLevel = lvl.level"
            @keydown.enter="selectedLevel = lvl.level"
            @keydown.space.prevent="selectedLevel = lvl.level"
          >
            <div class="level-info">
              <span class="level-label">Lv. {{ lvl.level }}</span>
              <span v-if="!isFilterActive" class="level-badge">{{ lvl.items.length }} 物品</span>
              <span
                v-else
                class="level-badge filter-match-badge"
                :class="{ 'zero-match': countLevelMatches(lvl, filterCriteria) === 0 }"
              >
                {{ countLevelMatches(lvl, filterCriteria) }} / {{ lvl.items.length }} 匹配
              </span>
            </div>
          </div>

          <div v-if="editableLevels.length === 0" class="sidebar-empty">
            <span>暂无等级配置</span>
          </div>
        </div>
      </aside>

      <!-- 右侧：当前选中等级的掉落物品表格 -->
      <section class="level-detail" aria-label="当前等级掉落物品">
        <div v-if="currentLevel" class="detail-container">
          <!-- 等级信息栏与数据格式展示 -->
          <div class="detail-header">
            <div class="detail-title-group">
              <h3 class="detail-title">Lv. {{ currentLevel.level }} 掉落列表</h3>
              <!-- 协议结构展示：等级 -> 0 -> [物品项] -> -1 -->
              <div class="protocol-bar" title="PVF 内部结构：等级声明后紧跟固定值 0，段落以 -1 结束。本界面已自动维护该结构。">
                <span class="proto-tag">格式结构</span>
                <span class="proto-token">Lv. {{ currentLevel.level }}</span>
                <span class="proto-sep">&rarr;</span>
                <span class="proto-token proto-fixed">0 (固定值)</span>
                <span class="proto-sep">&rarr;</span>
                <span class="proto-token">[{{ currentItems.length }} 个物品]</span>
                <span class="proto-sep">&rarr;</span>
                <span class="proto-token proto-term">-1 (结束符)</span>
              </div>
            </div>

            <div class="detail-summary">
              <div class="metric-item">
                <span class="metric-label">总权重:</span>
                <span class="metric-val">{{ currentTotalWeight }}</span>
              </div>
              <div class="metric-item">
                <span class="metric-label">道具数:</span>
                <span class="metric-val">
                  <template v-if="isFilterActive">
                    {{ displayedItems.length }} / {{ currentItems.length }}
                  </template>
                  <template v-else>
                    {{ currentItems.length }}
                  </template>
                </span>
              </div>
              <NButton
                size="small"
                type="primary"
                :disabled="!file.editable"
                title="添加掉落道具（支持指定等级区间）"
                @click="openAddItem"
              >
                <template #icon><NIcon><Add24Regular /></NIcon></template>
                添加道具
              </NButton>
              <NButton
                size="small"
                type="error"
                secondary
                :disabled="!file.editable || editableLevels.length === 0"
                title="在指定等级区间内批量删除指定道具"
                @click="openBatchDelete"
              >
                <template #icon><NIcon><Delete16Regular /></NIcon></template>
                批量删除
              </NButton>
            </div>
          </div>

          <!-- 物品数据表格 -->
          <div class="table-scroller">
            <table class="item-table" role="table" aria-label="掉落道具明细">
              <thead>
                <tr>
                  <th class="col-index">#</th>
                  <th class="col-item">掉落道具</th>
                  <th class="col-id">道具 ID</th>
                  <th class="col-weight">原始权重</th>
                  <th class="col-ratio">概率参考</th>
                  <th v-if="file.editable" class="col-actions">操作</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="(item, idx) in displayedItems" :key="item.key" class="table-row">
                  <td class="col-index">{{ idx + 1 }}</td>
                  <td class="col-item">
                    <div class="item-meta-cell">
                      <span class="item-name" :title="item.name">{{ item.name }}</span>
                    </div>
                  </td>
                  <td class="col-id">
                    <span class="id-tag">{{ item.id }}</span>
                  </td>
                  <td class="col-weight">
                    <span class="weight-cell">{{ item.weight }}</span>
                  </td>
                  <td class="col-ratio">
                    <NTooltip trigger="hover">
                      <template #trigger>
                        <span
                          class="ratio-cell"
                          :class="{ 'ratio-zero': currentTotalWeight <= 0 }"
                        >
                          {{ calcItemPercentage(item.weight, currentTotalWeight) }}
                        </span>
                      </template>
                      {{ currentTotalWeight <= 0 ? "当前等级总权重为 0，展示中立参考百分比" : `权重 ${item.weight} / 等级总权重 ${currentTotalWeight}` }}
                    </NTooltip>
                  </td>
                  <td v-if="file.editable" class="col-actions">
                    <div class="row-actions">
                      <button
                        type="button"
                        class="action-btn"
                        :disabled="isFilterActive || idx === 0"
                        :title="isFilterActive ? '筛选状态下禁用顺序调整' : '上移'"
                        :aria-label="isFilterActive ? '筛选状态下禁用上移' : '上移'"
                        @click="handleMoveItem(idx, 'up')"
                      >
                        <NIcon size="12"><ArrowUp16Regular /></NIcon>
                      </button>
                      <button
                        type="button"
                        class="action-btn"
                        :disabled="isFilterActive || idx === displayedItems.length - 1"
                        :title="isFilterActive ? '筛选状态下禁用顺序调整' : '下移'"
                        :aria-label="isFilterActive ? '筛选状态下禁用下移' : '下移'"
                        @click="handleMoveItem(idx, 'down')"
                      >
                        <NIcon size="12"><ArrowDown16Regular /></NIcon>
                      </button>
                      <button
                        type="button"
                        class="action-btn"
                        title="编辑道具 ID 与权重"
                        aria-label="编辑道具"
                        @click="openEditItem(item)"
                      >
                        <NIcon size="12"><Edit16Regular /></NIcon>
                      </button>
                      <button
                        type="button"
                        class="action-btn action-btn-danger"
                        title="删除该道具"
                        aria-label="删除道具"
                        @click="deleteItem(item.key)"
                      >
                        <NIcon size="12"><Delete16Regular /></NIcon>
                      </button>
                    </div>
                  </td>
                </tr>
              </tbody>
            </table>

            <div v-if="displayedItems.length === 0" class="empty-level-box">
              <p class="empty-text">
                {{
                  isFilterActive
                    ? (selectedExactItem
                        ? `当前等级无道具「${selectedExactItem.name}」(ID: ${selectedExactItem.id}) 的掉落记录`
                        : `当前等级无匹配「${filterQuery}」的掉落道具`)
                    : '当前等级暂无配置掉落道具（允许配置空等级）'
                }}
              </p>
              <NButton v-if="isFilterActive" size="small" secondary @click="clearFilter">
                清除筛选
              </NButton>
              <NButton v-else-if="file.editable" size="small" secondary @click="openAddItem">
                添加第一个道具
              </NButton>
            </div>
          </div>
        </div>

        <div v-else class="detail-empty">
          <p>请在左侧选择一个掉落等级</p>
        </div>
      </section>
    </div>

    <!-- 添加 / 编辑道具对话框 -->
    <NModal
      v-model:show="itemModal.show"
      preset="card"
      :title="itemModal.mode === 'add' ? '添加掉落道具' : '编辑掉落道具'"
      style="width: 440px;"
      :mask-closable="false"
    >
      <div class="form-body">
        <div v-if="itemModal.mode === 'add'" class="form-item">
          <label class="form-label">添加至等级区间 (闭区间 [起始, 结束]):</label>
          <div class="level-interval-row">
            <span>Lv. {{ itemModal.startLevel }}</span>
            <span class="interval-sep">至</span>
            <span>Lv. {{ itemModal.endLevel }}</span>
          </div>
          <div class="level-range-slider">
            <NSlider
              range
              :value="intervalSliderValue(itemModal.startLevel, itemModal.endLevel)"
              :min="0"
              :max="Math.max(1, sortedLevelNumbers.length - 1)"
              :step="1"
              :disabled="sortedLevelNumbers.length <= 1"
              :format-tooltip="levelSliderTooltip"
              aria-label="添加道具的等级区间"
              @update:value="(value: number[]) => updateInterval(itemModal, value)"
            />
          </div>
          <div v-if="addItemIntervalPreview" class="match-preview-box has-matches">
            <span class="preview-icon">ℹ</span>
            <span class="preview-message">{{ addItemIntervalPreview }}</span>
          </div>
        </div>

        <div class="form-item">
          <label class="form-label">道具检索 / 关联:</label>
          <ItemPicker
            v-model="itemModal.id"
            :label="itemModal.name"
            @select="onPickerSelect"
          />
        </div>

        <div class="form-item">
          <label class="form-label">道具 ID（新增需为正整数）:</label>
          <NInputNumber
            :value="itemModal.id.trim() ? Number(itemModal.id) : null"
            :min="itemModal.mode === 'edit' && (itemModal.originalId ?? 1) <= 0 ? MIN_INT32 : 1"
            :max="2147483647"
            :precision="0"
            placeholder="道具 ID"
            style="width: 100%;"
            @update:value="onIdChange"
          />
        </div>

        <div class="form-item">
          <label class="form-label">道具名称 (由系统自动关联):</label>
          <div class="item-name-preview" :title="itemModal.name">
            <ImageThumbnail v-if="itemModal.icon" :reference="itemModal.icon" :size="20" show-fallback />
            <span class="preview-text">{{ itemModal.name || (itemModal.id ? `未知物品 #${itemModal.id}` : '未选择道具') }}</span>
          </div>
        </div>

        <div class="form-item">
          <label class="form-label">原始权重 (非负整数):</label>
          <NInputNumber
            v-model:value="itemModal.weight"
            :min="0"
            :max="2147483647"
            :precision="0"
            placeholder="掉落权重"
            style="width: 100%;"
            @keydown.enter.prevent="confirmItemModal"
          />
        </div>

        <div v-if="itemModal.error" class="modal-error">{{ itemModal.error }}</div>
      </div>
      <template #footer>
        <div class="modal-footer">
          <NButton size="small" @click="itemModal.show = false">取消</NButton>
          <NButton size="small" type="primary" @click="confirmItemModal">确定</NButton>
        </div>
      </template>
    </NModal>

    <!-- 批量删除道具对话框 -->
    <NModal
      v-model:show="batchDeleteModal.show"
      preset="card"
      title="批量删除掉落道具"
      style="width: 440px;"
      :mask-closable="false"
    >
      <div class="form-body">
        <div class="form-item">
          <label class="form-label">目标道具检索 / 关联:</label>
          <ItemPicker
            v-model="batchDeleteModal.id"
            :label="batchDeleteModal.name"
            @select="onBatchDeletePickerSelect"
          />
        </div>

        <div class="form-item">
          <label class="form-label">已有道具 ID:</label>
          <NInputNumber
            :value="batchDeleteModal.id.trim() ? Number(batchDeleteModal.id) : null"
            :min="MIN_INT32"
            :max="2147483647"
            :precision="0"
            placeholder="请输入待删除道具 ID"
            style="width: 100%;"
            @update:value="onBatchDeleteIdChange"
          />
        </div>

        <div class="form-item">
          <label class="form-label">道具名称预览:</label>
          <div class="item-name-preview" :title="batchDeleteModal.name">
            <ImageThumbnail v-if="batchDeleteModal.icon" :reference="batchDeleteModal.icon" :size="20" show-fallback />
            <span class="preview-text">{{ batchDeleteModal.name || (batchDeleteModal.id ? `未知物品 #${batchDeleteModal.id}` : '未选择道具') }}</span>
          </div>
        </div>

        <div class="form-item">
          <label class="form-label">删除等级区间 (闭区间 [起始, 结束]):</label>
          <div class="level-interval-row">
            <span>Lv. {{ batchDeleteModal.startLevel }}</span>
            <span class="interval-sep">至</span>
            <span>Lv. {{ batchDeleteModal.endLevel }}</span>
          </div>
          <div class="level-range-slider">
            <NSlider
              range
              :value="intervalSliderValue(batchDeleteModal.startLevel, batchDeleteModal.endLevel)"
              :min="0"
              :max="Math.max(1, sortedLevelNumbers.length - 1)"
              :step="1"
              :disabled="sortedLevelNumbers.length <= 1"
              :format-tooltip="levelSliderTooltip"
              aria-label="批量删除的等级区间"
              @update:value="(value: number[]) => updateInterval(batchDeleteModal, value)"
            />
          </div>
        </div>

        <!-- 匹配统计预览 -->
        <div v-if="batchDeletePreview" class="match-preview-box" :class="{ 'has-matches': batchDeletePreview.count > 0 }">
          <span class="preview-icon">ℹ</span>
          <span class="preview-message">{{ batchDeletePreview.message }}</span>
        </div>

        <div v-if="batchDeleteModal.error" class="modal-error">{{ batchDeleteModal.error }}</div>
      </div>
      <template #footer>
        <div class="modal-footer">
          <NButton size="small" @click="batchDeleteModal.show = false">取消</NButton>
          <NButton
            size="small"
            type="error"
            :disabled="!batchDeleteModal.id || batchDeleteModal.startLevel === null || batchDeleteModal.endLevel === null"
            @click="confirmBatchDeleteModal"
          >
            确定删除
          </NButton>
        </div>
      </template>
    </NModal>

    <!-- 筛选选择道具对话框 -->
    <NModal
      v-model:show="filterPickerModal.show"
      preset="card"
      title="从物品库选择筛选目标"
      style="width: 440px;"
      :mask-closable="true"
    >
      <div class="form-body">
        <div class="form-item">
          <label class="form-label">检索物品并设为精准筛选目标（按 ID 唯一过滤）：</label>
          <ItemPicker
            v-model="filterPickerModal.id"
            :label="filterPickerModal.name"
            @select="onFilterPickerSelect"
          />
        </div>
      </div>
      <template #footer>
        <div class="modal-footer">
          <NButton size="small" @click="filterPickerModal.show = false">取消</NButton>
          <NButton size="small" type="primary" :disabled="!filterPickerModal.id" @click="confirmFilterPicker">
            确定筛选
          </NButton>
        </div>
      </template>
    </NModal>
  </div>
</template>

<style scoped>
.world-drop-view {
  width: 100%;
  height: 100%;
  min-height: 0;
  display: flex;
  flex-direction: column;
  background: var(--pvf-window-background-solid);
  color: var(--pvf-text-primary);
  font-size: 13px;
  overflow: hidden;
  box-sizing: border-box;
  user-select: none;
  container-type: inline-size;
  container-name: world-drop;
}

/* 顶部工具栏 */
.world-drop-header {
  flex: 0 0 40px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 14px;
  background: var(--pvf-surface-panel);
  border-bottom: 1px solid var(--pvf-border-subtle);
  box-sizing: border-box;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.header-icon {
  color: var(--pvf-primary);
  flex-shrink: 0;
}

.header-title {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
  color: var(--pvf-text-primary);
  white-space: nowrap;
}

.file-path-badge {
  font-size: 11px;
  color: var(--pvf-text-muted);
  font-family: monospace;
  background: var(--pvf-surface-subtle);
  padding: 2px 6px;
  border-radius: 3px;
  border: 1px solid var(--pvf-border-subtle);
}

.dirty-tag {
  font-size: 11px;
}

.header-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.header-close-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  border-radius: 4px;
  border: none;
  background: transparent;
  color: var(--pvf-text-muted);
  cursor: pointer;
  transition: all 0.15s ease;
}

.header-close-btn:hover {
  background: var(--pvf-surface-hover);
  color: var(--pvf-text-primary);
}

.header-close-btn:focus-visible {
  outline: 2px solid var(--pvf-primary);
  outline-offset: 1px;
}

/* 筛选工具条 */
.filter-toolbar {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 6px 14px;
  background: var(--pvf-surface-subtle);
  border-bottom: 1px solid var(--pvf-border-subtle);
  gap: 12px;
  flex-wrap: wrap;
}

.filter-controls {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 1;
  max-width: 480px;
}

.filter-search-input {
  flex: 1;
}

.filter-meta {
  display: flex;
  align-items: center;
  gap: 8px;
}

.filter-tip {
  font-size: 11px;
  color: var(--pvf-text-muted);
}

/* 提示条容器 */
.banner-box {
  padding: 8px 12px 0;
}

.alert-content {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}

.loading-state {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 14px;
  color: var(--pvf-text-muted);
}

/* 主体区域 */
.world-drop-body {
  flex: 1;
  min-height: 0;
  display: flex;
  overflow: hidden;
}

/* 左侧等级导航 */
.level-sidebar {
  width: 220px;
  flex-shrink: 0;
  border-right: 1px solid var(--pvf-border-subtle);
  display: flex;
  flex-direction: column;
  background: var(--pvf-surface-subtle);
  overflow: hidden;
}

.sidebar-header {
  flex: 0 0 36px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 12px;
  border-bottom: 1px solid var(--pvf-border-subtle);
}

.sidebar-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--pvf-text-secondary);
}

.level-list {
  flex: 1;
  overflow-y: auto;
  padding: 6px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.level-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 7px 10px;
  border-radius: 4px;
  background: transparent;
  border: 1px solid transparent;
  cursor: pointer;
  transition: all 0.15s ease;
}

.level-item:hover {
  background: var(--pvf-surface-hover);
}

.level-item.active {
  background: var(--pvf-surface-selected);
  border-color: var(--pvf-primary-soft);
}

.level-item.is-dimmed {
  opacity: 0.38;
}

.level-item:focus-visible {
  outline: 2px solid var(--pvf-primary);
  outline-offset: -1px;
}

.level-info {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  gap: 8px;
  min-width: 0;
}

.level-label {
  font-weight: 600;
  font-size: 13px;
  color: var(--pvf-text-primary);
}

.level-badge {
  font-size: 11px;
  color: var(--pvf-text-muted);
}

.filter-match-badge {
  color: var(--pvf-primary);
  font-weight: 600;
}

.filter-match-badge.zero-match {
  color: var(--pvf-text-muted);
  font-weight: normal;
}

.sidebar-empty {
  padding: 30px 10px;
  text-align: center;
  color: var(--pvf-text-muted);
}

/* 右侧等级详情 */
.level-detail {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: var(--pvf-window-background-solid);
}

.detail-container {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.detail-header {
  flex: 0 0 auto;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 10px 14px;
  border-bottom: 1px solid var(--pvf-border-subtle);
  background: var(--pvf-surface-subtle);
}

.detail-title-group {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.detail-title {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
  color: var(--pvf-text-primary);
}

/* 协议格式结构条 */
.protocol-bar {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  background: var(--pvf-surface-panel);
  border: 1px solid var(--pvf-border-subtle);
  padding: 2px 8px;
  border-radius: 4px;
  font-size: 11px;
  font-family: monospace;
}

.proto-tag {
  color: var(--pvf-text-muted);
  font-size: 10px;
  font-weight: 600;
}

.proto-token {
  color: var(--pvf-text-secondary);
}

.proto-fixed {
  color: var(--pvf-info);
  font-weight: bold;
}

.proto-term {
  color: var(--pvf-warning);
  font-weight: bold;
}

.proto-sep {
  color: var(--pvf-text-muted);
}

.detail-summary {
  display: flex;
  align-items: center;
  gap: 12px;
}

.metric-item {
  display: flex;
  align-items: center;
  gap: 5px;
  font-size: 12px;
}

.metric-label {
  color: var(--pvf-text-muted);
}

.metric-val {
  font-weight: bold;
  color: var(--pvf-text-primary);
}

/* 表格容器 */
.table-scroller {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  overflow-x: auto;
}

.item-table {
  width: 100%;
  border-collapse: collapse;
  text-align: left;
  font-size: 12px;
}

.item-table th {
  position: sticky;
  top: 0;
  z-index: 1;
  background: var(--pvf-surface-card);
  color: var(--pvf-text-secondary);
  font-weight: 600;
  padding: 8px 12px;
  border-bottom: 1px solid var(--pvf-border-subtle);
  white-space: nowrap;
}

.item-table td {
  padding: 8px 12px;
  border-bottom: 1px solid var(--pvf-border-subtle);
  vertical-align: middle;
}

.table-row:hover {
  background: var(--pvf-surface-hover);
}

.col-index {
  width: 44px;
  text-align: center;
  color: var(--pvf-text-muted);
}

.col-item {
  min-width: 180px;
}

.item-meta-cell {
  display: flex;
  align-items: center;
  gap: 8px;
}

.item-name {
  color: var(--pvf-text-primary);
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 260px;
}

.col-id {
  width: 90px;
}

.id-tag {
  font-family: monospace;
  font-size: 11px;
  background: var(--pvf-surface-subtle);
  padding: 2px 6px;
  border-radius: 3px;
  border: 1px solid var(--pvf-border-subtle);
  color: var(--pvf-text-secondary);
}

.col-weight {
  width: 100px;
  font-variant-numeric: tabular-nums;
  font-family: monospace;
}

.weight-cell {
  font-weight: 600;
  color: var(--pvf-text-primary);
}

.col-ratio {
  width: 100px;
  font-variant-numeric: tabular-nums;
  font-family: monospace;
}

.ratio-cell {
  color: var(--pvf-primary);
  font-weight: 600;
}

.ratio-zero {
  color: var(--pvf-text-muted);
  font-weight: normal;
}

.col-actions {
  width: 120px;
  text-align: right;
}

.row-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 4px;
}

.action-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  padding: 0;
  border-radius: 3px;
  border: 1px solid var(--pvf-border-subtle);
  background: var(--pvf-surface-card);
  color: var(--pvf-text-secondary);
  cursor: pointer;
  transition: all 0.15s ease;
}

.action-btn:hover:not(:disabled) {
  background: var(--pvf-surface-hover);
  color: var(--pvf-primary);
  border-color: var(--pvf-primary);
}

.action-btn-danger:hover:not(:disabled) {
  background: var(--pvf-surface-error);
  color: var(--pvf-error);
  border-color: var(--pvf-error);
}

.action-btn:disabled {
  opacity: 0.35;
  cursor: not-allowed;
}

.action-btn:focus-visible {
  outline: 2px solid var(--pvf-primary);
  outline-offset: 1px;
}

.empty-level-box {
  padding: 50px 20px;
  text-align: center;
  color: var(--pvf-text-muted);
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
}

.empty-text {
  margin: 0;
  font-size: 13px;
}

.detail-empty {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--pvf-text-muted);
}

/* 模态框通用表单样式 */
.form-body {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.form-item {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.form-label {
  font-size: 12px;
  color: var(--pvf-text-secondary);
  font-weight: 500;
}

.level-interval-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  font-variant-numeric: tabular-nums;
}

.level-range-slider {
  padding: 2px 8px 0;
}

.interval-sep {
  color: var(--pvf-text-muted);
  font-size: 12px;
  flex-shrink: 0;
}

.item-name-preview {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 10px;
  background: var(--pvf-surface-subtle);
  border: 1px solid var(--pvf-border-subtle);
  border-radius: 3px;
  min-height: 32px;
  box-sizing: border-box;
}

.preview-text {
  font-size: 12px;
  color: var(--pvf-text-primary);
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.match-preview-box {
  display: flex;
  align-items: flex-start;
  gap: 6px;
  padding: 6px 10px;
  border-radius: 4px;
  background: var(--pvf-surface-subtle);
  border: 1px solid var(--pvf-border-subtle);
  font-size: 12px;
  color: var(--pvf-text-secondary);
}

.match-preview-box.has-matches {
  background: rgba(32, 128, 240, 0.08);
  border-color: rgba(32, 128, 240, 0.3);
  color: var(--pvf-text-primary);
}

.preview-icon {
  font-weight: bold;
  color: var(--pvf-primary);
  flex-shrink: 0;
}

.preview-message {
  line-height: 1.4;
}

.modal-error {
  color: var(--pvf-error);
  font-size: 12px;
}

.modal-footer {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
}

/* 窄分屏响应式处理：支持原生 CSS Container Queries 与 .is-narrow 类名降级 */
@container world-drop (max-width: 640px) {
  .world-drop-body {
    flex-direction: column;
  }
  .level-sidebar {
    width: 100%;
    max-height: 180px;
    border-right: none;
    border-bottom: 1px solid var(--pvf-border-subtle);
  }
  .protocol-bar {
    display: none;
  }
  .detail-header {
    flex-direction: column;
    align-items: stretch;
  }
  .detail-summary {
    justify-content: space-between;
  }
  .filter-toolbar {
    flex-direction: column;
    align-items: stretch;
  }
  .filter-controls {
    max-width: 100%;
  }
}

.world-drop-view.is-narrow .world-drop-body {
  flex-direction: column;
}
.world-drop-view.is-narrow .level-sidebar {
  width: 100%;
  max-height: 180px;
  border-right: none;
  border-bottom: 1px solid var(--pvf-border-subtle);
}
.world-drop-view.is-narrow .protocol-bar {
  display: none;
}
.world-drop-view.is-narrow .detail-header {
  flex-direction: column;
  align-items: stretch;
}
.world-drop-view.is-narrow .detail-summary {
  justify-content: space-between;
}
.world-drop-view.is-narrow .filter-toolbar {
  flex-direction: column;
  align-items: stretch;
}
.world-drop-view.is-narrow .filter-controls {
  max-width: 100%;
}
</style>
