<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from "vue";
import {
  EditorView,
  keymap,
  lineNumbers,
  highlightActiveLine,
  highlightActiveLineGutter,
  drawSelection,
  highlightWhitespace,
  rectangularSelection,
  crosshairCursor,
} from "@codemirror/view";
import { EditorState, Compartment } from "@codemirror/state";
import {
  defaultKeymap,
  history,
  historyKeymap,
  indentWithTab,
} from "@codemirror/commands";
import { searchKeymap, highlightSelectionMatches } from "@codemirror/search";
import { pvfHighlighting, pvfLanguage } from "../pvfLanguage";

const props = defineProps<{
  doc: string;
  readOnly?: boolean;
}>();

const emit = defineEmits<{
  (e: "change", text: string): void;
}>();

const host = ref<HTMLDivElement | null>(null);
let view: EditorView | null = null;
const readOnlyComp = new Compartment();

// 与暗色界面匹配的极简主题
const darkTheme = EditorView.theme(
  {
    "&": { height: "100%", fontSize: "13px", backgroundColor: "transparent" },
    ".cm-scroller": {
      fontFamily: "'SF Mono', Menlo, Consolas, 'Courier New', monospace",
      lineHeight: "1.55",
      userSelect: "text",
    },
    ".cm-gutters": {
      backgroundColor: "transparent",
      border: "none",
      color: "rgba(128,128,128,0.5)",
    },
    ".cm-content": { padding: "8px 0" },
    ".cm-activeLine": { backgroundColor: "rgba(128,128,128,0.08)" },
    ".cm-activeLineGutter": { backgroundColor: "rgba(128,128,128,0.08)" },
    ".cm-selectionMatch": { backgroundColor: "rgba(80,140,255,0.25)" },
  },
  { dark: true }
);

function makeExtensions() {
  return [
    lineNumbers(),
    highlightActiveLineGutter(),
    highlightActiveLine(),
    history(),
    drawSelection(),
    highlightWhitespace(),
    rectangularSelection(),
    crosshairCursor(),
    highlightSelectionMatches(),
    keymap.of([...defaultKeymap, ...historyKeymap, ...searchKeymap, indentWithTab]),
    readOnlyComp.of(EditorState.readOnly.of(!!props.readOnly)),
    pvfLanguage.extension,
    pvfHighlighting,
    darkTheme,
    EditorView.lineWrapping,
    EditorView.updateListener.of((u) => {
      if (u.docChanged) emit("change", u.state.doc.toString());
    }),
  ];
}

onMounted(() => {
  view = new EditorView({
    state: EditorState.create({ doc: props.doc, extensions: makeExtensions() }),
    parent: host.value!,
  });
});

onBeforeUnmount(() => {
  view?.destroy();
  view = null;
});

// 外部文档切换(标签切换):内容不同才整体替换
watch(
  () => props.doc,
  (doc) => {
    if (!view) return;
    const current = view.state.doc.toString();
    if (doc !== current) {
      view.dispatch({ changes: { from: 0, to: current.length, insert: doc } });
    }
  }
);

watch(
  () => props.readOnly,
  (ro) => {
    view?.dispatch({
      effects: readOnlyComp.reconfigure(EditorState.readOnly.of(!!ro)),
    });
  }
);
</script>

<template>
  <div ref="host" class="code-editor" />
</template>

<style scoped>
.code-editor {
  flex: 1;
  min-width: 0;
  min-height: 0;
  height: auto;
  overflow: hidden;
}
.code-editor :deep(.cm-editor),
.code-editor :deep(.cm-scroller) {
  min-height: 0;
  height: 100%;
}
.code-editor :deep(.cm-scroller) {
  flex: 1 1 auto;
  max-height: 100%;
  overflow: auto;
}
</style>
