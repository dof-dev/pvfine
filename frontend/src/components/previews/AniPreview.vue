<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import {
  ArrowClockwise24Regular,
  Next24Regular,
  Pause24Regular,
  Play24Regular,
  Previous24Regular,
} from "@vicons/fluent";
import {
  NAlert,
  NButton,
  NIcon,
  NSelect,
  NSlider,
  NSpin,
  NTag,
} from "naive-ui";
import { PreviewService } from "../../../bindings/pvfine/services";
import type {
  AniPreviewDocument,
  AniPreviewFrame,
  AniPreviewLayer,
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
const lastValidDocument = ref<AniPreviewDocument | null>(null);
const parserIssues = ref<PreviewIssue[]>([]);
const parserLoading = ref(false);
const frameIndex = ref(0);
const renderedFrameIndex = ref(0);
const playing = ref(false);
const speed = ref(1);
const imageError = ref("");
const imageCache = ref(new Map<string, ImageData | null>());
const pendingImages = new Map<string, Promise<ImageData | null>>();

let parseTimer: number | undefined;
let playTimer: number | undefined;
let parseRequest = 0;
let frameRequest = 0;

const speedOptions = [0.25, 0.5, 1, 2, 4].map((value) => ({
  label: `${value}x`,
  value,
}));

const document = computed(() => lastValidDocument.value);
const frames = computed(() => document.value?.frames ?? []);
const renderedFrame = computed<AniPreviewFrame | null>(
  () => frames.value[renderedFrameIndex.value] ?? null,
);
const currentFrame = computed<AniPreviewFrame | null>(
  () => frames.value[frameIndex.value] ?? null,
);
const currentDelay = computed(() => currentFrame.value?.delayMs ?? 0);
const hasParserErrors = computed(() =>
  parserIssues.value.some((issue) => issue.severity === "error"),
);
const displayIssues = computed(() => {
  const issues = [...parserIssues.value];
  if (imageError.value) {
    issues.push({
      severity: "error",
      line: 0,
      message: imageError.value,
    });
  }
  return issues;
});
const issueType = computed(() =>
  hasParserErrors.value || !!imageError.value ? "error" : "warning",
);
const visibleIssues = computed(() => displayIssues.value.slice(0, 6));
const omittedIssueCount = computed(() =>
  Math.max(0, displayIssues.value.length - visibleIssues.value.length),
);

const stageBounds = computed(() => {
  const layers = (renderedFrame.value?.layers ?? []).filter((layer) => !isEmptyImageLayer(layer));
  const padding = 10;
  let minX = 0;
  let minY = 0;
  let maxX = 120;
  let maxY = 80;
  for (const layer of layers) {
    const data = imageCache.value.get(imageKey(layer.image));
    const width = data?.width ?? 24;
    const height = data?.height ?? 24;
    minX = Math.min(minX, layer.x);
    minY = Math.min(minY, layer.y);
    maxX = Math.max(maxX, layer.x + width);
    maxY = Math.max(maxY, layer.y + height);
  }
  return {
    width: Math.max(160, maxX - minX + padding * 2),
    height: Math.max(100, maxY - minY + padding * 2),
    offsetX: padding - minX,
    offsetY: padding - minY,
  };
});

const renderedLayers = computed(() => {
  const layers = (renderedFrame.value?.layers ?? []).filter((layer) => !isEmptyImageLayer(layer));
  return layers.map((layer, index) => {
    const data = imageCache.value.get(imageKey(layer.image));
    return {
      key: `${imageKey(layer.image)}:${index}`,
      layer,
      data,
      left: layer.x + stageBounds.value.offsetX,
      top: layer.y + stageBounds.value.offsetY,
    };
  });
});
const visibleLayerCount = computed(() => renderedLayers.value.length);

function imageKey(reference: { path: string; index: number }): string {
  return `${reference.path.replaceAll("\\", "/").toLowerCase()}\u0000${reference.index}`;
}

function isEmptyImageLayer(layer: AniPreviewLayer): boolean {
  return layer.image.path.trim() === "" || layer.image.index < 0;
}

function clearPlayTimer(): void {
  if (playTimer !== undefined) {
    window.clearTimeout(playTimer);
    playTimer = undefined;
  }
}

function clearParseTimer(): void {
  if (parseTimer !== undefined) {
    window.clearTimeout(parseTimer);
    parseTimer = undefined;
  }
}

function setImageCache(key: string, data: ImageData | null): void {
  const next = new Map(imageCache.value);
  next.set(key, data);
  imageCache.value = next;
}

function loadLayer(layer: AniPreviewLayer): Promise<ImageData | null> {
  const key = imageKey(layer.image);
  if (imageCache.value.has(key)) return Promise.resolve(imageCache.value.get(key) ?? null);
  const pending = pendingImages.get(key);
  if (pending) return pending;

  const request = images.loadImage(layer.image)
    .then((data) => {
      setImageCache(key, data);
      return data;
    })
    .catch(() => {
      setImageCache(key, null);
      return null;
    })
    .finally(() => {
      if (pendingImages.get(key) === request) pendingImages.delete(key);
    });
  pendingImages.set(key, request);
  return request;
}

async function loadFrame(frame: AniPreviewFrame): Promise<string[]> {
  const loaded = await Promise.all(
    (frame.layers ?? []).filter((layer) => !isEmptyImageLayer(layer)).map(async (layer) => ({
      layer,
      data: await loadLayer(layer),
    })),
  );
  return loaded
    .filter((item) => !item.data)
    .map((item) => `${item.layer.image.path}[${item.layer.image.index}]`);
}

async function showFrame(index: number): Promise<boolean> {
  const currentDocument = document.value;
  if (!currentDocument || frames.value.length === 0) return false;
  const bounded = Math.max(0, Math.min(frames.value.length - 1, Math.trunc(index)));
  frameIndex.value = bounded;
  const request = ++frameRequest;
  const frame = frames.value[bounded];
  const missing = await loadFrame(frame);
  if (request !== frameRequest) return false;
  if (missing.length > 0) {
    imageError.value = `图片加载失败：${missing.join("、")}`;
    playing.value = false;
    clearPlayTimer();
    frameIndex.value = renderedFrameIndex.value;
    return false;
  }

  imageError.value = "";
  renderedFrameIndex.value = bounded;
  frameIndex.value = bounded;
  preloadNextFrame();
  if (playing.value) schedulePlayback();
  return true;
}

function preloadNextFrame(): void {
  const currentDocument = document.value;
  if (!currentDocument || frames.value.length < 2) return;
  let next = renderedFrameIndex.value + 1;
  if (next >= frames.value.length) {
    if (!currentDocument.loop) return;
    next = 0;
  }
  void loadFrame(frames.value[next]);
}

function schedulePlayback(): void {
  clearPlayTimer();
  if (!props.active || !playing.value || !document.value || !renderedFrame.value) return;
  const delay = Math.max(10, Math.round(currentDelay.value / speed.value));
  playTimer = window.setTimeout(() => {
    playTimer = undefined;
    const currentDocument = document.value;
    if (!currentDocument || !playing.value) return;
    const next = renderedFrameIndex.value + 1;
    if (next >= frames.value.length) {
      if (!currentDocument.loop) {
        playing.value = false;
        return;
      }
      void showFrame(0);
      return;
    }
    void showFrame(next);
  }, delay);
}

function play(): void {
  if (!document.value || frames.value.length === 0) return;
  if (!document.value.loop && renderedFrameIndex.value >= frames.value.length - 1) {
    void showFrame(0).then(() => {
      playing.value = true;
      schedulePlayback();
    });
    return;
  }
  playing.value = true;
  schedulePlayback();
}

function pause(): void {
  playing.value = false;
  clearPlayTimer();
}

function stepFrame(delta: number): void {
  pause();
  const next = Math.max(0, Math.min(frames.value.length - 1, renderedFrameIndex.value + delta));
  void showFrame(next);
}

function onFrameSlider(value: number | [number, number]): void {
  const next = typeof value === "number" ? value : value[0];
  pause();
  void showFrame(next);
}

function onSpeedChange(value: string | number | null): void {
  if (typeof value !== "number") return;
  speed.value = value;
  if (playing.value) schedulePlayback();
}

async function parseText(text: string, request: number): Promise<void> {
  parserLoading.value = true;
  try {
    const result = await PreviewService.ParseANI(text);
    if (request !== parseRequest || !result) return;
    parserIssues.value = result.issues ?? [];
    if (!result.valid || !result.frames?.length) {
      playing.value = false;
      clearPlayTimer();
      return;
    }

    const previousDocument = lastValidDocument.value;
    lastValidDocument.value = result;
    imageError.value = "";
    const nextIndex = Math.min(
      previousDocument ? renderedFrameIndex.value : 0,
      result.frames.length - 1,
    );
    frameIndex.value = nextIndex;
    renderedFrameIndex.value = nextIndex;
    if (!previousDocument && props.active) playing.value = true;
    void showFrame(nextIndex);
  } catch (error: any) {
    if (request !== parseRequest) return;
    parserIssues.value = [{
      severity: "error",
      line: 1,
      message: `ANI 解析失败：${error?.message ?? error}`,
    }];
    playing.value = false;
    clearPlayTimer();
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

watch(
  () => [props.file.path, props.file.text] as const,
  ([path], oldValue) => {
    if (oldValue && oldValue[0] !== path) {
      lastValidDocument.value = null;
      parserIssues.value = [];
      imageError.value = "";
      frameIndex.value = 0;
      renderedFrameIndex.value = 0;
      playing.value = false;
      clearPlayTimer();
    }
    scheduleParse();
  },
  { immediate: true },
);

watch(
  () => props.active,
  (active) => {
    if (!active) {
      clearPlayTimer();
      return;
    }
    if (playing.value) schedulePlayback();
  },
  { immediate: true },
);

watch(
  () => images.revision,
  () => {
    imageCache.value = new Map();
    pendingImages.clear();
    if (document.value && renderedFrame.value) void showFrame(renderedFrameIndex.value);
  },
);

onBeforeUnmount(() => {
  parseRequest++;
  frameRequest++;
  clearParseTimer();
  clearPlayTimer();
  pendingImages.clear();
});
</script>

<template>
  <div class="ani-preview">
    <div class="ani-preview-summary">
      <span v-if="document && frames.length">帧 {{ renderedFrameIndex + 1 }}/{{ frames.length }}</span>
      <span v-else>ANI 预览</span>
      <NTag size="tiny" :bordered="false" :type="document?.loop ? 'success' : 'default'">
        {{ document?.loop ? "循环" : "单次" }}
      </NTag>
      <NTag v-if="document?.shadow" size="tiny" :bordered="false" type="warning">
        阴影未模拟
      </NTag>
      <NSpin v-if="parserLoading" :size="14" />
    </div>

    <NAlert
      v-if="visibleIssues.length"
      :type="issueType"
      :show-icon="false"
      class="ani-preview-alert"
    >
      <div v-for="(issue, index) in visibleIssues" :key="`${issue.line}:${issue.message}:${index}`">
        <span v-if="issue.line > 0">第 {{ issue.line }} 行： </span>{{ issue.message }}
      </div>
      <div v-if="omittedIssueCount > 0">还有 {{ omittedIssueCount }} 条问题未显示</div>
    </NAlert>

    <div v-if="!document || !frames.length" class="ani-preview-empty">
      <span>等待有效的 ANI 结构…</span>
    </div>

    <div v-else class="ani-stage-viewport">
      <div
        class="ani-stage"
        :style="{ width: `${stageBounds.width}px`, height: `${stageBounds.height}px` }"
      >
        <div
          class="ani-origin-marker"
          :style="{ left: `${stageBounds.offsetX}px`, top: `${stageBounds.offsetY}px` }"
          aria-hidden="true"
        >
          <span>0,0</span>
        </div>
        <div v-if="visibleLayerCount === 0" class="ani-empty-frame-label">空图片帧</div>
        <template v-for="item in renderedLayers" :key="item.key">
          <img
            v-if="item.data?.dataUrl"
            class="ani-stage-layer"
            :src="item.data.dataUrl"
            :alt="`${item.layer.image.path}[${item.layer.image.index}]`"
            :style="{ left: `${item.left}px`, top: `${item.top}px`, width: `${item.data.width}px`, height: `${item.data.height}px` }"
          />
          <div
            v-else
            class="ani-stage-layer-placeholder"
            :style="{ left: `${item.left}px`, top: `${item.top}px` }"
          >
            图片
          </div>
        </template>
      </div>
    </div>

    <div v-if="document && frames.length" class="ani-preview-controls">
      <NButton quaternary size="tiny" :disabled="!frames.length" aria-label="上一帧" @click="stepFrame(-1)">
        <template #icon><NIcon :size="15"><Previous24Regular /></NIcon></template>
      </NButton>
      <NButton quaternary size="tiny" aria-label="播放/暂停" @click="playing ? pause() : play()">
        <template #icon>
          <NIcon :size="15"><Pause24Regular v-if="playing" /><Play24Regular v-else /></NIcon>
        </template>
      </NButton>
      <NButton quaternary size="tiny" :disabled="!frames.length" aria-label="下一帧" @click="stepFrame(1)">
        <template #icon><NIcon :size="15"><Next24Regular /></NIcon></template>
      </NButton>
      <NSelect
        size="tiny"
        class="ani-speed-select"
        :value="speed"
        :options="speedOptions"
        @update:value="onSpeedChange"
      />
    </div>

    <div v-if="document && frames.length > 1" class="ani-frame-slider">
      <NSlider
        :value="frameIndex"
        :min="0"
        :max="frames.length - 1"
        :step="1"
        :tooltip="false"
        @update:value="onFrameSlider"
      />
    </div>
    <div v-if="document && frames.length" class="ani-preview-footer">
      <span>延迟 {{ currentDelay }}ms</span>
      <span>{{ visibleLayerCount }} 个图层</span>
      <span v-if="document.frameMax !== frames.length">声明 {{ document.frameMax }} 帧</span>
      <NButton quaternary size="tiny" aria-label="重新播放" @click="play">
        <template #icon><NIcon :size="13"><ArrowClockwise24Regular /></NIcon></template>
      </NButton>
    </div>
  </div>
</template>

<style scoped>
.ani-preview {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  padding: 7px 8px 6px;
  gap: 6px;
  font-size: 11px;
}
.ani-preview-summary,
.ani-preview-controls,
.ani-preview-footer {
  display: flex;
  align-items: center;
  min-width: 0;
  gap: 5px;
}
.ani-preview-summary {
  min-height: 20px;
  color: var(--pvf-text-secondary);
}
.ani-preview-summary > span:first-child {
  margin-right: auto;
}
.ani-preview-alert {
  flex: 0 0 auto;
  max-height: 68px;
  overflow: auto;
  padding: 5px 7px;
  font-size: 10px;
  line-height: 1.45;
}
.ani-preview-empty {
  display: flex;
  flex: 1 1 auto;
  min-height: 100px;
  align-items: center;
  justify-content: center;
  color: var(--pvf-text-muted);
  background: var(--pvf-surface-inset);
  border: 1px dashed var(--pvf-border-subtle);
  border-radius: 5px;
}
.ani-stage-viewport {
  display: flex;
  flex: 1 1 auto;
  min-height: 112px;
  overflow: auto;
  background: var(--pvf-surface-inset);
  border: 1px solid var(--pvf-border-subtle);
  border-radius: 5px;
}
.ani-stage {
  position: relative;
  flex: 0 0 auto;
  overflow: hidden;
  background-color: rgba(128, 128, 128, 0.08);
  background-image: linear-gradient(45deg, rgba(128, 128, 128, 0.14) 25%, transparent 25%),
    linear-gradient(-45deg, rgba(128, 128, 128, 0.14) 25%, transparent 25%),
    linear-gradient(45deg, transparent 75%, rgba(128, 128, 128, 0.14) 75%),
    linear-gradient(-45deg, transparent 75%, rgba(128, 128, 128, 0.14) 75%);
  background-position: 0 0, 0 8px, 8px -8px, -8px 0;
  background-size: 16px 16px;
}
.ani-origin-marker {
  position: absolute;
  z-index: 1;
  width: 1px;
  height: 1px;
  pointer-events: none;
  background: var(--pvf-error);
  box-shadow: 0 0 0 3px var(--pvf-error-surface);
}
.ani-origin-marker::before,
.ani-origin-marker::after {
  position: absolute;
  display: block;
  content: "";
  background: var(--pvf-error);
  opacity: 0.55;
}
.ani-origin-marker::before {
  top: 0;
  left: -5px;
  width: 11px;
  height: 1px;
}
.ani-origin-marker::after {
  top: -5px;
  left: 0;
  width: 1px;
  height: 11px;
}
.ani-origin-marker span {
  position: absolute;
  top: 4px;
  left: 4px;
  color: var(--pvf-error);
  font-size: 9px;
  white-space: nowrap;
}
.ani-empty-frame-label {
  position: absolute;
  top: 50%;
  left: 50%;
  z-index: 2;
  color: var(--pvf-text-muted);
  font-size: 10px;
  transform: translate(-50%, -50%);
  white-space: nowrap;
}
.ani-stage-layer,
.ani-stage-layer-placeholder {
  position: absolute;
  z-index: 2;
  display: block;
  object-fit: contain;
}
.ani-stage-layer-placeholder {
  width: 24px;
  height: 24px;
  color: var(--pvf-warning);
  font-size: 9px;
  line-height: 24px;
  text-align: center;
  background: var(--pvf-surface-warning);
  border: 1px solid var(--pvf-warning);
  border-radius: 3px;
}
.ani-preview-controls {
  flex: 0 0 auto;
  justify-content: center;
}
.ani-speed-select {
  width: 64px;
  margin-left: 4px;
}
.ani-frame-slider {
  flex: 0 0 auto;
  padding: 0 3px;
}
.ani-preview-footer {
  flex: 0 0 auto;
  color: var(--pvf-text-muted);
}
.ani-preview-footer span:nth-child(2) {
  margin-left: auto;
}
.ani-preview-footer span:nth-child(3) {
  color: var(--pvf-warning);
}
</style>
