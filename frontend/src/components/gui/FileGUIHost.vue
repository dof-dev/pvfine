<script setup lang="ts">
import { computed } from "vue";
import { getGUIProvider } from "../../gui/registry";
import type { GUIFile } from "../../gui/types";

const props = defineProps<{ file: GUIFile; active: boolean }>();
const provider = computed(() => getGUIProvider(props.file));
</script>

<template>
  <div class="file-gui-host" :aria-label="`${provider?.label ?? ''} GUI`">
    <Suspense>
      <component v-if="provider" :is="provider.component" :file="file" :active="active" />
      <template #fallback><div class="gui-opening" role="status">正在加载商店…</div></template>
    </Suspense>
  </div>
</template>

<style scoped>
.file-gui-host { flex: 1; min-width: 0; min-height: 0; overflow: auto; background: #211e19; }
.gui-opening { padding: 24px; color: #decfa9; }
</style>
