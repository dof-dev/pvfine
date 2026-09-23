<script setup lang="ts">
import { computed } from "vue";
import { getGUIProvider } from "../../gui/registry";
import type { GUIFile } from "../../gui/types";

const props = defineProps<{ file: GUIFile; active: boolean }>();
const emit = defineEmits<{ (e: "close"): void }>();
const provider = computed(() => getGUIProvider(props.file));
</script>

<template>
  <div class="file-gui-host" :aria-label="`${provider?.label ?? ''} GUI`">
    <Suspense>
      <component
        v-if="provider"
        :is="provider.component"
        :file="file"
        :active="active"
        @close="emit('close')"
      />
      <template #fallback><div class="gui-opening" role="status">正在加载商店…</div></template>
    </Suspense>
  </div>
</template>

<style scoped>
.file-gui-host {
  flex: 1;
  min-width: 0;
  min-height: 0;
  height: 100%;
  overflow-y: auto;
  overflow-x: hidden;
  background: transparent;
  padding: 8px 12px;
  box-sizing: border-box;
  display: flex;
  justify-content: center;
  align-items: stretch;
}
.gui-opening {
  padding: 24px;
  color: #eed28b;
  font-size: 13px;
}
</style>
