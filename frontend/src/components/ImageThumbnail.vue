<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Document24Regular } from "@vicons/fluent";
import { NIcon } from "naive-ui";
import type { ImageReference } from "../../bindings/pvfine/services/models";
import { useImageStore } from "../stores/images";

const props = withDefaults(
  defineProps<{
    reference?: ImageReference | null;
    size?: number;
    showFallback?: boolean;
  }>(),
  { reference: null, size: 16, showFallback: false }
);

const images = useImageStore();
const dataUrl = ref("");
let requestID = 0;

const style = computed(() => ({
  width: `${props.size}px`,
  height: `${props.size}px`,
}));

async function load(): Promise<void> {
  const request = ++requestID;
  dataUrl.value = "";
  const data = await images.loadImage(props.reference);
  if (request !== requestID) return;
  dataUrl.value = data?.dataUrl ?? "";
}

watch(
  () => [props.reference?.path, props.reference?.index, images.revision] as const,
  () => void load(),
  { immediate: true }
);
</script>

<template>
  <span class="image-thumbnail" :style="style" aria-hidden="true">
    <img v-if="dataUrl" :src="dataUrl" :width="size" :height="size" alt="" />
    <NIcon v-else-if="showFallback" :size="Math.max(12, size - 1)"><Document24Regular /></NIcon>
  </span>
</template>

<style scoped>
.image-thumbnail {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  vertical-align: middle;
}
.image-thumbnail img {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: contain;
}
</style>

