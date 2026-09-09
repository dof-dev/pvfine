<script setup lang="ts">
import { onUnmounted, ref } from "vue";
import {
  useEditorStore,
  type EditorLayoutNode,
  type EditorLayoutSplit,
} from "../stores/editor";
import EditorPane from "./EditorPane.vue";
import type { ResolvedThemeId } from "../theme";

defineOptions({ name: "EditorLayout" });

const props = defineProps<{
  node: EditorLayoutNode;
  themeId: ResolvedThemeId;
}>();

const editor = useEditorStore();
const host = ref<HTMLDivElement | null>(null);
const resizing = ref(false);
let resizeMove: ((event: PointerEvent) => void) | null = null;
let resizeEnd: (() => void) | null = null;

function childStyle(node: EditorLayoutSplit, first: boolean): Record<string, string> {
  const ratio = first ? node.ratio : 1 - node.ratio;
  return { flex: `${ratio} 1 0` };
}

function onResizeStart(): void {
  if (props.node.kind !== "split" || !host.value || resizing.value) return;
  const splitNode = props.node;
  const orientation = splitNode.orientation;
  const splitId = splitNode.id;
  const rect = host.value.getBoundingClientRect();
  if (rect.width <= 0 || rect.height <= 0) return;

  resizing.value = true;
  document.body.style.cursor = orientation === "columns" ? "col-resize" : "row-resize";
  document.body.style.userSelect = "none";

  resizeMove = (event: PointerEvent) => {
    const value =
      orientation === "columns"
        ? (event.clientX - rect.left) / rect.width
        : (event.clientY - rect.top) / rect.height;
    editor.setSplitRatio(splitId, value);
  };
  resizeEnd = () => onResizeEnd();
  window.addEventListener("pointermove", resizeMove);
  window.addEventListener("pointerup", resizeEnd);
  window.addEventListener("pointercancel", resizeEnd);
}

function onResizeEnd(): void {
  if (!resizing.value) return;
  resizing.value = false;
  document.body.style.cursor = "";
  document.body.style.userSelect = "";
  if (resizeMove) window.removeEventListener("pointermove", resizeMove);
  if (resizeEnd) {
    window.removeEventListener("pointerup", resizeEnd);
    window.removeEventListener("pointercancel", resizeEnd);
  }
  resizeMove = null;
  resizeEnd = null;
}

onUnmounted(onResizeEnd);
</script>

<template>
  <div v-if="node.kind === 'pane'" class="editor-layout-node editor-layout-pane">
    <EditorPane :pane-id="node.paneId" :theme-id="props.themeId" />
  </div>

  <div
    v-else
    ref="host"
    class="editor-layout-node editor-layout-split"
    :class="`editor-layout-split--${node.orientation}`"
  >
    <div class="editor-layout-child" :style="childStyle(node, true)">
      <EditorLayout :node="node.first" :theme-id="props.themeId" />
    </div>

    <div
      class="editor-layout-resizer"
      role="separator"
      :aria-orientation="node.orientation === 'columns' ? 'vertical' : 'horizontal'"
      :aria-valuenow="Math.round(node.ratio * 100)"
      aria-valuemin="20"
      aria-valuemax="80"
      tabindex="0"
      @pointerdown.prevent="onResizeStart"
    />

    <div class="editor-layout-child" :style="childStyle(node, false)">
      <EditorLayout :node="node.second" :theme-id="props.themeId" />
    </div>
  </div>
</template>

<style scoped>
.editor-layout-node {
  width: 100%;
  height: 100%;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
}
.editor-layout-split {
  display: flex;
}
.editor-layout-split--columns {
  flex-direction: row;
}
.editor-layout-split--rows {
  flex-direction: column;
}
.editor-layout-child {
  min-width: 0;
  min-height: 0;
  overflow: hidden;
}
.editor-layout-split--columns > .editor-layout-child {
  min-width: 180px;
}
.editor-layout-split--rows > .editor-layout-child {
  min-height: 120px;
}
.editor-layout-resizer {
  flex: 0 0 4px;
  z-index: 10;
  background: var(--pvf-surface-inset);
}
.editor-layout-split--columns > .editor-layout-resizer {
  cursor: col-resize;
}
.editor-layout-split--rows > .editor-layout-resizer {
  cursor: row-resize;
}
.editor-layout-resizer:hover,
.editor-layout-resizer:focus-visible {
  background: var(--pvf-effect-split-hover);
  outline: none;
}
</style>
